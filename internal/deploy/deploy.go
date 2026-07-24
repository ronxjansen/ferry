// Package deploy implements the deploy, rollback and preview flows: context
// builds or registry pulls, git-SHA versioning, health-gated cutover via
// kamal-proxy, retention, lock and audit.
package deploy

import (
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/audit"
	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/ronxjansen/ferry/internal/envfile"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/hooks"
	"github.com/ronxjansen/ferry/internal/plan"
)

// Deployer carries one invocation's context: version, performer, flags.
type Deployer struct {
	Plan      *plan.Plan
	Version   string
	Performer string
	Timeout   time.Duration
	SkipBuild bool

	Log func(format string, args ...any)

	contexts map[string]*exec.Docker
}

func (d *Deployer) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(format, args...)
	}
}

func (d *Deployer) cfg() *config.Config { return d.Plan.Config }

// run executes a local command; a var so tests can intercept the argv.
var run = exec.Run

// Docker returns (creating if needed) the Docker context handle for a server.
func (d *Deployer) Docker(s *config.Server) (*exec.Docker, error) {
	if d.contexts == nil {
		d.contexts = map[string]*exec.Docker{}
	}
	name := d.cfg().ContextName(s)
	if dk, ok := d.contexts[name]; ok {
		return dk, nil
	}
	if _, err := run("docker", "context", "inspect", name); err != nil {
		if _, err := run(append([]string{"docker"}, dockercmd.ContextCreate(name, s)...)...); err != nil {
			return nil, fmt.Errorf("failed to create docker context %s: %w", name, err)
		}
		d.logf("Created docker context %s (ssh://%s)", name, s.Address())
	}
	dk := &exec.Docker{Context: name, Server: s}
	d.contexts[name] = dk
	return dk, nil
}

// Host returns the plain-SSH handle for a server.
func (d *Deployer) Host(s *config.Server) *exec.Host {
	return &exec.Host{Server: s}
}

// PrimaryServer is where the deploy lock and audit log live: the first server
// of the first target.
func (d *Deployer) PrimaryServer() *config.Server {
	return d.Plan.Targets[0].Servers[0]
}

func (d *Deployer) hookRunner() *hooks.Runner {
	return &hooks.Runner{Hooks: d.cfg().Hooks, Dir: d.cfg().Dir}
}

func (d *Deployer) hookEnv(t *plan.Target, s *config.Server, started time.Time) hooks.Env {
	return hooks.Env{
		Version:   d.Version,
		Service:   t.Name,
		Server:    s.Name,
		Image:     t.ImageRef(d.Version),
		Performer: d.Performer,
		Runtime:   time.Since(started),
	}
}

// Deploy runs the full flow for the given targets (dependency order, rolling
// across each target's servers).
func (d *Deployer) Deploy(targets []*plan.Target) error {
	started := time.Now()
	for _, t := range targets {
		if err := d.hookRunner().RunLocal(config.HookPreBuild, d.hookEnv(t, t.Servers[0], started)); err != nil {
			return err
		}
		for _, s := range t.Servers {
			if err := d.deployOne(t, s, started); err != nil {
				return fmt.Errorf("deploy %s on %s: %w", t.Name, s.Name, err)
			}
		}
		d.audit(fmt.Sprintf("deploy %s", t.Name))
	}
	return nil
}

func (d *Deployer) deployOne(t *plan.Target, s *config.Server, started time.Time) error {
	dk, err := d.Docker(s)
	if err != nil {
		return err
	}
	host := d.Host(s)

	if err := d.ensureNetwork(dk); err != nil {
		return err
	}
	image, err := d.EnsureImage(t, s)
	if err != nil {
		return err
	}

	envPath, envChanged, cleanup, err := d.prepareEnv(t, s, "")
	if err != nil {
		return err
	}
	defer cleanup()
	// Reusing an existing same-version container is only safe when nothing it
	// was created with has changed; env changes and dirty-tree deploys force a
	// fresh container.
	recreate := envChanged || strings.HasSuffix(d.Version, "-dirty")

	if err := d.hookRunner().RunRemote(host, config.HookPreDeploy, d.hookEnv(t, s, started)); err != nil {
		return err
	}

	opts := dockercmd.RunOpts{
		Name:         t.ContainerName(d.Version),
		Project:      d.cfg().Name,
		Service:      t.Name,
		Version:      d.Version,
		Network:      d.cfg().Proxy.Network,
		EnvFile:      envPath,
		Detach:       true,
		PublishPorts: !t.Proxied(),
	}

	if t.Overlay.Job {
		if err := d.runJob(dk, t, image, opts); err != nil {
			return err
		}
		return d.hookRunner().RunRemote(host, config.HookPostDeploy, d.hookEnv(t, s, started))
	}

	// Everything running that is not the target version gets stopped after
	// (or, unproxied, before) the new version is up — including stale
	// versions from crashed half-deploys.
	old := d.runningContainers(dk, t.Name, "")
	delete(old, opts.Name)

	if t.Proxied() {
		if err := d.cutover(dk, t, s, image, opts, old, recreate); err != nil {
			return err
		}
	} else {
		if err := d.restartInPlace(dk, t, image, opts, old); err != nil {
			return err
		}
	}

	if err := d.hookRunner().RunRemote(host, config.HookPostDeploy, d.hookEnv(t, s, started)); err != nil {
		return err
	}
	if err := d.hookRunner().RunRemote(host, config.HookPostAppBoot, d.hookEnv(t, s, started)); err != nil {
		return err
	}

	return d.Prune(dk, t)
}

// cutover makes the target version's container run and delegates the swap to
// kamal-proxy, which blocks until the target is healthy, swaps traffic
// atomically and drains the old one. Failure leaves the running version
// untouched. Idempotent: an already-running same-version container is
// re-registered without a restart; a retained stopped one is started
// (rollback-forward) instead of rebuilt.
func (d *Deployer) cutover(dk *exec.Docker, t *plan.Target, s *config.Server, image string, opts dockercmd.RunOpts, old map[string]string, recreate bool) error {
	created := false
	switch state := d.containerState(dk, opts.Name); {
	case state == "running" && !recreate:
		d.logf("%s@%s: %s already running, re-registering", t.Name, s.Name, opts.Name)
	case state == "exited" && !recreate:
		d.logf("%s@%s: starting retained %s", t.Name, s.Name, opts.Name)
		if _, err := dk.Run("start", opts.Name); err != nil {
			return err
		}
	default:
		if state != "" {
			dk.Run("rm", "-f", opts.Name)
		}
		d.logf("%s@%s: starting %s", t.Name, s.Name, opts.Name)
		if _, err := dk.Run(dockercmd.Run(t.Compose, image, opts)...); err != nil {
			return err
		}
		created = true
	}

	proxyArgs := dockercmd.ProxyDeploy(dockercmd.ProxyDeployOpts{
		Service:       t.Name,
		Target:        fmt.Sprintf("%s:%d", opts.Name, t.Port()),
		Hosts:         t.DomainsOn(s),
		Health:        t.Overlay.Health,
		DeployTimeout: d.Timeout.String(),
		DrainTimeout:  "30s",
		Passthrough:   t.Overlay.Proxy,
	})
	d.logf("%s@%s: registering with kamal-proxy (%s)", t.Name, s.Name, strings.Join(t.DomainsOn(s), ", "))
	if _, err := dk.Run(proxyArgs...); err != nil {
		logs, _ := dk.Run("logs", "--tail", "50", opts.Name)
		if created {
			dk.Run("rm", "-f", opts.Name)
		}
		return fmt.Errorf("kamal-proxy refused to cut over (old version keeps serving): %w\ncontainer logs:\n%s", err, logs)
	}

	// The proxy already drained the old target; keep it stopped for rollback.
	for id := range old {
		dk.Run("stop", id)
	}
	d.logf("%s@%s: live at %s", t.Name, s.Name, strings.Join(t.DomainsOn(s), ", "))
	return nil
}

// restartInPlace deploys an unproxied service: stop-then-start (single-writer
// stores must not run twice); the brief blip is accepted and documented.
func (d *Deployer) restartInPlace(dk *exec.Docker, t *plan.Target, image string, opts dockercmd.RunOpts, old map[string]string) error {
	for id := range old {
		d.logf("%s: stopping %s", t.Name, id)
		if _, err := dk.Run("stop", id); err != nil {
			return err
		}
	}
	// Always recreate: a fresh container picks up env and compose changes.
	if d.containerState(dk, opts.Name) != "" {
		dk.Run("rm", "-f", opts.Name)
	}
	d.logf("%s: starting %s", t.Name, opts.Name)
	if _, err := dk.Run(dockercmd.Run(t.Compose, image, opts)...); err != nil {
		return err
	}
	if err := d.waitHealthy(dk, opts.Name); err != nil {
		logs, _ := dk.Run("logs", "--tail", "50", opts.Name)
		return fmt.Errorf("%w\ncontainer logs:\n%s", err, logs)
	}
	return nil
}

// runJob runs a one-shot service to completion; a non-zero exit fails the
// deploy before any cutover happens.
func (d *Deployer) runJob(dk *exec.Docker, t *plan.Target, image string, opts dockercmd.RunOpts) error {
	dk.Run("rm", "-f", opts.Name)
	opts.Detach = false
	opts.Rm = true
	d.logf("%s: running job", t.Name)
	if err := dk.Stream(dockercmd.Run(t.Compose, image, opts)...); err != nil {
		return fmt.Errorf("job %s failed, aborting deploy: %w", t.Name, err)
	}
	return nil
}

// EnsureImage makes the versioned image available on the server: skipped when
// it already exists (idempotent redeploys, rollback-forward), otherwise a
// context build on the host or a registry pull.
func (d *Deployer) EnsureImage(t *plan.Target, s *config.Server) (string, error) {
	dk, err := d.Docker(s)
	if err != nil {
		return "", err
	}
	image := t.ImageRef(d.Version)
	if _, err := dk.Run("image", "inspect", image); err == nil {
		return image, nil
	}

	switch {
	case t.BuildMethod() == "remote":
		if d.SkipBuild {
			return "", fmt.Errorf("image %s not on %s and --skip-build given", image, s.Name)
		}
		d.logf("%s@%s: building %s on the host", t.Name, s.Name, image)
		if err := dk.Stream(dockercmd.Build(t.Compose.Build, image)...); err != nil {
			return "", err
		}
	default:
		if err := d.registryLogin(dk); err != nil {
			return "", err
		}
		d.logf("%s@%s: pulling %s", t.Name, s.Name, image)
		if err := dk.Stream("pull", image); err != nil {
			return "", err
		}
	}
	return image, nil
}

func (d *Deployer) registryLogin(dk *exec.Docker) error {
	reg := d.cfg().Registry
	if reg == nil {
		return nil
	}
	password := os.Getenv(reg.Password)
	if password == "" {
		env, err := envfile.Load(d.cfg().EnvFilesFor(nil), d.cfg().Env.Encryption == "sops-age")
		if err == nil {
			password = env[reg.Password]
		}
	}
	if password == "" {
		return fmt.Errorf("registry password variable %s not set (env or %s)", reg.Password, d.cfg().Env.File)
	}
	args := dk.Args("login", "--username", reg.Username, "--password-stdin")
	if reg.Server != "" {
		args = append(args, reg.Server)
	}
	if _, err := exec.RunInput([]byte(password), args...); err != nil {
		return fmt.Errorf("registry login failed: %w", err)
	}
	return nil
}

// mergeEnv merges compose environment, the global and per-service env files,
// an optional extra overlay file, and cross-server service hosts.
func (d *Deployer) mergeEnv(t *plan.Target, s *config.Server, extraFile string, crossServer bool) (map[string]string, error) {
	env := map[string]string{}
	for k, v := range t.Compose.Environment {
		if v != nil {
			env[k] = *v
		}
	}
	files := d.cfg().EnvFilesFor(t.Overlay)
	if extraFile != "" {
		files = append(files, extraFile)
	}
	fromFiles, err := envfile.Load(files, d.cfg().Env.Encryption == "sops-age")
	if err != nil {
		return nil, err
	}
	maps.Copy(env, fromFiles)
	if crossServer {
		maps.Copy(env, d.Plan.CrossServerEnv(t, s))
	}
	return env, nil
}

// MergedEnv is the fully merged environment for a target on a server, as a
// deploy would deliver it (used by `ferry env push|diff`).
func (d *Deployer) MergedEnv(t *plan.Target, s *config.Server) (map[string]string, error) {
	return d.mergeEnv(t, s, "", true)
}

// mergeEnvToTemp writes the merged env to a local 0600 temp file for
// --env-file delivery (values go through the Docker API, never argv).
func (d *Deployer) mergeEnvToTemp(t *plan.Target, s *config.Server, extraFile string, crossServer bool) (string, func(), error) {
	env, err := d.mergeEnv(t, s, extraFile, crossServer)
	if err != nil {
		return "", nil, err
	}
	if len(env) == 0 {
		return "", func() {}, nil
	}
	tmp, err := envfile.WriteTemp(envfile.Render(env))
	if err != nil {
		return "", nil, err
	}
	return tmp, func() { os.Remove(tmp) }, nil
}

// prepareEnv additionally ships the merged env to the host (hash-checked,
// 0600) — server-side state for `ferry env` and hooks. The changed result
// reports whether the host copy was out of date.
func (d *Deployer) prepareEnv(t *plan.Target, s *config.Server, extraFile string) (path string, changed bool, cleanup func(), err error) {
	env, err := d.mergeEnv(t, s, extraFile, true)
	if err != nil {
		return "", false, nil, err
	}
	if len(env) == 0 {
		return "", false, func() {}, nil
	}
	content := envfile.Render(env)
	shipped, err := envfile.Ship(d.Host(s), t.Name, content)
	if err != nil {
		return "", false, nil, fmt.Errorf("failed to ship env file: %w", err)
	}
	if shipped {
		d.logf("%s@%s: env updated (%s)", t.Name, s.Name, envfile.RemotePath(t.Name))
	}
	tmp, err := envfile.WriteTemp(content)
	if err != nil {
		return "", false, nil, err
	}
	return tmp, shipped, func() { os.Remove(tmp) }, nil
}

// ensureNetwork creates the proxy network if it is missing. The deploy lock is
// per-project, so apps sharing a server can reach here at the same time: a
// create that loses the race is not an error as long as the network now exists.
func (d *Deployer) ensureNetwork(dk *exec.Docker) error {
	if _, err := dk.Run("network", "inspect", d.cfg().Proxy.Network); err == nil {
		return nil
	}
	if _, err := dk.Run("network", "create", "--attachable", d.cfg().Proxy.Network); err != nil {
		if _, e := dk.Run("network", "inspect", d.cfg().Proxy.Network); e != nil {
			return err
		}
	}
	return nil
}

// runningContainers returns name→version of running containers for a service
// (preview filters to that preview's containers; "" filters previews out).
func (d *Deployer) runningContainers(dk *exec.Docker, service, preview string) map[string]string {
	out, _ := dk.Run("ps", "--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelProject, d.cfg().Name),
		"--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelService, service),
		"--format", "{{.Names}}\t{{.Label \"ferry.version\"}}\t{{.Label \"ferry.preview\"}}")
	containers := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		if parts[2] != preview {
			continue
		}
		containers[parts[0]] = parts[1]
	}
	return containers
}

// containerState returns .State.Status for a container, "" when absent.
func (d *Deployer) containerState(dk *exec.Docker, name string) string {
	out, err := dk.Run("inspect", "--format", "{{.State.Status}}", name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (d *Deployer) waitHealthy(dk *exec.Docker, name string) error {
	deadline := time.Now().Add(d.Timeout)
	for time.Now().Before(deadline) {
		status, err := dk.Run("inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", name)
		if err != nil {
			return fmt.Errorf("container %s disappeared: %w", name, err)
		}
		switch strings.TrimSpace(status) {
		case "none", "healthy":
			return nil
		case "unhealthy":
			return fmt.Errorf("container %s is unhealthy", name)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %s to become healthy", name)
}

// Prune keeps the last build.retain stopped containers per service (the
// rollback depth) and removes older ones with their images.
func (d *Deployer) Prune(dk *exec.Docker, t *plan.Target) error {
	out, _ := dk.Run("ps", "--all", "--filter", "status=exited",
		"--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelProject, d.cfg().Name),
		"--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelService, t.Name),
		"--format", "{{.CreatedAt}}\t{{.Names}}\t{{.Image}}\t{{.Label \"ferry.preview\"}}")
	type retained struct{ created, name, image string }
	var stopped []retained
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 4 || parts[1] == "" || parts[3] != "" {
			continue
		}
		stopped = append(stopped, retained{parts[0], parts[1], parts[2]})
	}
	sort.Slice(stopped, func(i, j int) bool { return stopped[i].created > stopped[j].created })
	for i, c := range stopped {
		if i < d.cfg().Build.Retain {
			continue
		}
		d.logf("%s: pruning retained container %s", t.Name, c.name)
		dk.Run("rm", c.name)
		dk.Run("rmi", c.image) // no-op while other containers still use it
	}
	return nil
}

func (d *Deployer) audit(action string) {
	if err := audit.Record(d.Host(d.PrimaryServer()), d.Performer, d.Version, action); err != nil {
		d.logf("warning: failed to write audit log: %v", err)
	}
}
