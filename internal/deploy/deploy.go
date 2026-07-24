// Package deploy implements the deploy, rollback and preview flows: context
// builds or registry pulls, git-SHA versioning, health-gated cutover via
// kamal-proxy, retention, lock and audit.
package deploy

import (
	"fmt"
	"io"
	"maps"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/audit"
	"github.com/ronxjansen/ferry/internal/buildctx"
	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/ronxjansen/ferry/internal/envfile"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/hooks"
	"github.com/ronxjansen/ferry/internal/plan"
	"github.com/ronxjansen/ferry/internal/remotejob"
)

// Deployer carries one invocation's context: version, performer, flags.
type Deployer struct {
	Plan      *plan.Plan
	Version   string
	Performer string
	Timeout   time.Duration
	SkipBuild bool
	Recreate  bool // force fresh containers even when nothing changed

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
	recreate := envChanged || strings.HasSuffix(d.Version, "-dirty") || d.Recreate

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

	switch {
	case t.Stateful():
		if err := d.converge(dk, t, s, image, opts, old); err != nil {
			return err
		}
	case t.Proxied():
		if err := d.cutover(dk, t, s, image, opts, old, recreate); err != nil {
			return err
		}
	default:
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
		if err := d.startContainer(dk, t, image, opts); err != nil {
			return err
		}
		created = true
	}

	proxyArgs := dockercmd.ProxyDeploy(dockercmd.ProxyDeployOpts{
		Service:       t.ProxyService(),
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

// converge deploys a stateful service (db, redis, ...): the container has a
// stable name, and deploys leave it running untouched unless its creation
// config — image, merged env, ports, volumes, command — actually changed, as
// recorded in the ferry.config-hash label. Only a real change (or --recreate)
// bounces it; the app version moving does not.
func (d *Deployer) converge(dk *exec.Docker, t *plan.Target, s *config.Server, image string, opts dockercmd.RunOpts, old map[string]string) error {
	var env []byte
	if opts.EnvFile != "" {
		var err error
		if env, err = os.ReadFile(opts.EnvFile); err != nil {
			return err
		}
	}
	opts.ConfigHash = dockercmd.ConfigHash(t.Compose, image, opts, env)

	state := d.containerState(dk, opts.Name)
	unchanged := !d.Recreate && d.containerLabel(dk, opts.Name, dockercmd.LabelConfigHash) == opts.ConfigHash
	if state == "running" && unchanged {
		d.logf("%s@%s: unchanged, leaving %s running", t.Name, s.Name, opts.Name)
		return nil
	}

	// Anything else running under this service label (pre-stateful versioned
	// names, crashed strays) stops before the new container starts: a
	// single-writer store must never run twice. No rollback value either —
	// state lives in volumes — so remove them outright.
	for id := range old {
		d.logf("%s@%s: stopping %s", t.Name, s.Name, id)
		if _, err := dk.Run("stop", id); err != nil {
			return err
		}
		dk.Run("rm", id)
	}

	if state != "" && unchanged {
		d.logf("%s@%s: starting stopped %s", t.Name, s.Name, opts.Name)
		if _, err := dk.Run("start", opts.Name); err != nil {
			return err
		}
	} else {
		if state != "" {
			d.logf("%s@%s: config changed, recreating %s", t.Name, s.Name, opts.Name)
			dk.Run("stop", opts.Name)
			dk.Run("rm", "-f", opts.Name)
		} else {
			d.logf("%s@%s: starting %s", t.Name, s.Name, opts.Name)
		}
		if err := d.startContainer(dk, t, image, opts); err != nil {
			return err
		}
	}
	if err := d.waitHealthy(dk, opts.Name); err != nil {
		logs, _ := dk.Run("logs", "--tail", "50", opts.Name)
		return fmt.Errorf("%w\ncontainer logs:\n%s", err, logs)
	}
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
	if err := d.startContainer(dk, t, image, opts); err != nil {
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
			return "", fmt.Errorf("image %s not on %s and building is disabled (--skip-build=false builds it from the working tree)", image, s.Name)
		}
		if err := d.remoteBuild(t, s, image); err != nil {
			return "", err
		}
	default:
		if err := d.registryLogin(dk); err != nil {
			return "", err
		}
		d.logf("%s@%s: pulling %s", t.Name, s.Name, image)
		// A transient drop mid-pull retries cheaply: completed layers stay
		// in the daemon's cache.
		if _, err := exec.RunRetry(func() (string, error) {
			return "", dk.Stream("pull", image)
		}); err != nil {
			return "", err
		}
	}
	return image, nil
}

// remoteBuild builds the image on the host without depending on an unbroken
// connection: the context ships as one tarball, the build itself runs as a
// detached job on the server, and ferry follows its log over a reconnecting
// stream. A dropped link (or a killed ferry) leaves the build running; the
// follow loop — or a rerun — re-attaches instead of starting over.
func (d *Deployer) remoteBuild(t *plan.Target, s *config.Server, image string) error {
	host := d.Host(s)
	job := remotejob.New(host, fmt.Sprintf("build-%s-%s", t.Name, d.Version))

	if job.Running() {
		d.logf("%s@%s: build of %s already in flight, re-attaching", t.Name, s.Name, image)
	} else {
		if err := d.registryLoginHost(host); err != nil {
			return err
		}
		ctxDir := t.BuildContextDir()
		remoteCtx := job.Dir() + "/ctx"
		d.logf("%s@%s: shipping build context from %s", t.Name, s.Name, ctxDir)
		ship := func() (string, error) {
			pr, pw := io.Pipe()
			go func() {
				pw.CloseWithError(buildctx.WriteTar(pw, ctxDir, t.Compose.Build.Dockerfile))
			}()
			defer pr.Close()
			return host.RunReader(pr, fmt.Sprintf(`rm -rf %[1]q && mkdir -p %[1]q && tar -xzf - -C %[1]q`, remoteCtx))
		}
		if _, err := exec.RunRetry(ship); err != nil {
			return fmt.Errorf("failed to ship build context: %w", err)
		}
		d.logf("%s@%s: building %s on the host (detached, survives disconnects)", t.Name, s.Name, image)
		if err := job.Start(dockercmd.BuildScript(t.Compose.Build, image, remoteCtx)); err != nil {
			return err
		}
	}

	code, err := job.Follow(os.Stdout)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("build of %s failed with exit code %d (full log: %s on %s)", image, code, job.LogPath(), s.Name)
	}
	job.Clean()
	return nil
}

func (d *Deployer) registryPassword() (string, error) {
	reg := d.cfg().Registry
	password := os.Getenv(reg.Password)
	if password == "" {
		env, err := envfile.Load(d.cfg().EnvFilesFor(nil), d.cfg().Env.Encryption == "sops-age")
		if err == nil {
			password = env[reg.Password]
		}
	}
	if password == "" {
		return "", fmt.Errorf("registry password variable %s not set (env or %s)", reg.Password, d.cfg().Env.File)
	}
	return password, nil
}

func (d *Deployer) registryLogin(dk *exec.Docker) error {
	reg := d.cfg().Registry
	if reg == nil {
		return nil
	}
	password, err := d.registryPassword()
	if err != nil {
		return err
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

// registryLoginHost logs the host's own docker into the registry: a detached
// build runs server-side, so private base images can't ride on the local
// client's credentials the way tunneled builds did.
func (d *Deployer) registryLoginHost(h *exec.Host) error {
	reg := d.cfg().Registry
	if reg == nil {
		return nil
	}
	password, err := d.registryPassword()
	if err != nil {
		return err
	}
	command := fmt.Sprintf("docker login --username %s --password-stdin", dockercmd.ShellQuote(reg.Username))
	if reg.Server != "" {
		command += " " + dockercmd.ShellQuote(reg.Server)
	}
	if _, err := h.RunInput([]byte(password), command); err != nil {
		return fmt.Errorf("registry login on %s failed: %w", h.Server.Name, err)
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
// The same recheck covers a retried create colliding with its own first
// attempt whose reply was lost to the link.
func (d *Deployer) ensureNetwork(dk *exec.Docker) error {
	return ensureNetworkNamed(dk, d.cfg().Proxy.Network)
}

func ensureNetworkNamed(dk *exec.Docker, name string) error {
	if _, err := dk.Run("network", "inspect", name); err == nil {
		return nil
	}
	if _, err := dk.Run("network", "create", "--attachable", name); err != nil {
		if _, e := dk.Run("network", "inspect", name); e != nil {
			return err
		}
	}
	return nil
}

// startContainer runs a new container, tolerating the retried-run edge: if a
// transient failure lost the reply to a `docker run` that actually created
// the container, the retry's name conflict resolves by starting what exists.
func (d *Deployer) startContainer(dk *exec.Docker, t *plan.Target, image string, opts dockercmd.RunOpts) error {
	_, err := dk.Run(dockercmd.Run(t.Compose, image, opts)...)
	if err == nil {
		return nil
	}
	switch d.containerState(dk, opts.Name) {
	case "running":
		return nil
	case "created", "exited":
		_, startErr := dk.Run("start", opts.Name)
		return startErr
	}
	return err
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

// containerLabel returns one label of a container, "" when absent.
func (d *Deployer) containerLabel(dk *exec.Docker, name, label string) string {
	out, err := dk.Run("inspect", "--format", fmt.Sprintf("{{index .Config.Labels %q}}", label), name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
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
// rollback depth) and removes older ones with their images. Stateful services
// have nothing retained: one stable container, no version history.
func (d *Deployer) Prune(dk *exec.Docker, t *plan.Target) error {
	if t.Stateful() {
		return nil
	}
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
