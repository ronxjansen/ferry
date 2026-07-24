// Package dockercmd builds docker CLI argv slices. Builders are pure — no
// execution, no I/O — so the full command surface is testable without a
// server. cmd/* decides which context or host runs them.
package dockercmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/ronxjansen/ferry/internal/config"
)

// Ferry container labels; these drive ps, pruning, rollback and preview
// discovery — state lives on the server, not in local files.
const (
	LabelProject    = "ferry.project"
	LabelService    = "ferry.service"
	LabelVersion    = "ferry.version"
	LabelPreview    = "ferry.preview"
	LabelConfigHash = "ferry.config-hash"
)

// ProxyContainerName is the kamal-proxy container name on each server.
const ProxyContainerName = "kamal-proxy"

// ContextCreate builds argv to create a Docker context for a server.
func ContextCreate(name string, s *config.Server) []string {
	return []string{"context", "create", name, "--docker",
		fmt.Sprintf("host=ssh://%s@%s:%d", s.User, s.Host, s.Port)}
}

// RunOpts parameterizes a container start beyond what compose expresses.
type RunOpts struct {
	Name          string // container name (<service>-<version>)
	Project       string // ferry project name (label namespace)
	VolumeProject string // named-volume namespace; defaults to Project. Preview
	//                      isolate services get <project>-<sha> = fresh volumes.
	Service      string // ferry service name
	Version      string // git SHA
	Preview      string // preview SHA, empty for regular deploys
	Network      string // network to start on
	EnvFile      string // local merged env file (delivered via API, not argv)
	Detach       bool
	Rm           bool   // one-shot jobs
	PublishPorts bool   // publish compose ports: entries (unproxied services)
	ConfigHash   string // stateful services: digest of the creation config
}

// Run builds `docker run` argv for a compose service. Compose provides
// command, entrypoint, volumes, healthcheck, labels, restart; ferry injects
// name, network, labels, env-file and restart default unless-stopped.
func Run(svc types.ServiceConfig, image string, o RunOpts) []string {
	args := []string{"run"}
	if o.Detach {
		args = append(args, "--detach")
	}
	if o.Rm {
		args = append(args, "--rm")
	}
	args = append(args, "--name", o.Name)
	if o.Network != "" {
		args = append(args, "--network", o.Network, "--network-alias", o.Service)
	}

	if !o.Rm {
		restart := svc.Restart
		if restart == "" || restart == types.RestartPolicyAlways {
			restart = types.RestartPolicyUnlessStopped
		}
		if restart != types.RestartPolicyNo {
			args = append(args, "--restart", restart)
		}
	}

	if o.EnvFile != "" {
		args = append(args, "--env-file", o.EnvFile)
	}

	for _, l := range Labels(svc, o) {
		args = append(args, "--label", l)
	}

	volumeProject := o.VolumeProject
	if volumeProject == "" {
		volumeProject = o.Project
	}
	for _, v := range svc.Volumes {
		args = append(args, "--volume", volumeSpec(v, volumeProject))
	}

	if o.PublishPorts {
		for _, p := range svc.Ports {
			args = append(args, "--publish", portSpec(p))
		}
	}

	args = append(args, healthArgs(svc.HealthCheck)...)

	command := append([]string{}, svc.Command...)
	if len(svc.Entrypoint) > 0 {
		// docker run --entrypoint only takes the binary; remaining entrypoint
		// elements shift into the command.
		args = append(args, "--entrypoint", svc.Entrypoint[0])
		command = append(append([]string{}, svc.Entrypoint[1:]...), command...)
	}

	args = append(args, image)
	args = append(args, command...)
	return args
}

// Labels returns the ferry labels plus compose labels, sorted.
func Labels(svc types.ServiceConfig, o RunOpts) []string {
	labels := []string{
		fmt.Sprintf("%s=%s", LabelProject, o.Project),
		fmt.Sprintf("%s=%s", LabelService, o.Service),
		fmt.Sprintf("%s=%s", LabelVersion, o.Version),
	}
	if o.Preview != "" {
		labels = append(labels, fmt.Sprintf("%s=%s", LabelPreview, o.Preview))
	}
	if o.ConfigHash != "" {
		labels = append(labels, fmt.Sprintf("%s=%s", LabelConfigHash, o.ConfigHash))
	}
	for k, v := range svc.Labels {
		labels = append(labels, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(labels)
	return labels
}

// ConfigHash digests everything a container is created from — the full
// `docker run` argv (image, command, entrypoint, ports, volumes, healthcheck,
// labels, network) plus the merged env content — so a stateful service is
// recreated exactly when one of those inputs changes. The deploy version and
// the env-file temp path are excluded: they vary per deploy without changing
// the container; env changes register through the env content instead.
func ConfigHash(svc types.ServiceConfig, image string, o RunOpts, env []byte) string {
	o.Version = ""
	o.EnvFile = ""
	o.ConfigHash = ""
	h := sha256.New()
	for _, arg := range Run(svc, image, o) {
		h.Write([]byte(arg))
		h.Write([]byte{0})
	}
	h.Write(env)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func volumeSpec(v types.ServiceVolumeConfig, project string) string {
	source := v.Source
	if v.Type == "volume" && source != "" {
		// Namespace named volumes under the project, compose-style; preview
		// projects get fresh volumes for free.
		source = fmt.Sprintf("%s_%s", project, source)
	}
	spec := fmt.Sprintf("%s:%s", source, v.Target)
	if v.ReadOnly {
		spec += ":ro"
	}
	return spec
}

func portSpec(p types.ServicePortConfig) string {
	spec := fmt.Sprintf("%d", p.Target)
	if p.Published != "" {
		spec = fmt.Sprintf("%s:%s", p.Published, spec)
	}
	if p.HostIP != "" {
		spec = fmt.Sprintf("%s:%s", p.HostIP, spec)
	}
	if p.Protocol != "" && p.Protocol != "tcp" {
		spec += "/" + p.Protocol
	}
	return spec
}

func healthArgs(h *types.HealthCheckConfig) []string {
	if h == nil || len(h.Test) == 0 || h.Test[0] == "NONE" {
		return nil
	}
	var args []string
	switch h.Test[0] {
	case "CMD-SHELL":
		args = append(args, "--health-cmd", strings.Join(h.Test[1:], " "))
	case "CMD":
		args = append(args, "--health-cmd", strings.Join(h.Test[1:], " "))
	}
	if h.Interval != nil {
		args = append(args, "--health-interval", h.Interval.String())
	}
	if h.Timeout != nil {
		args = append(args, "--health-timeout", h.Timeout.String())
	}
	if h.Retries != nil {
		args = append(args, "--health-retries", fmt.Sprint(*h.Retries))
	}
	if h.StartPeriod != nil {
		args = append(args, "--health-start-period", h.StartPeriod.String())
	}
	return args
}

// Build builds `docker build` argv from the compose build section.
func Build(build *types.BuildConfig, image string) []string {
	args := []string{"build", "--tag", image}
	if build.Dockerfile != "" {
		args = append(args, "--file", build.Dockerfile)
	}
	if build.Target != "" {
		args = append(args, "--target", build.Target)
	}
	keys := make([]string, 0, len(build.Args))
	for k := range build.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := build.Args[k]; v != nil {
			args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, *v))
		}
	}
	ctx := build.Context
	if ctx == "" {
		ctx = "."
	}
	return append(args, ctx)
}

// BuildScript renders the shell command for a host-side build: cd into the
// shipped context dir and build with "." as context, so compose-relative
// dockerfile paths resolve the same way they would locally.
func BuildScript(build *types.BuildConfig, image, ctxDir string) string {
	args := Build(build, image)
	args[len(args)-1] = "."
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = ShellQuote(a)
	}
	return fmt.Sprintf("cd %s && exec docker %s", ShellQuote(ctxDir), strings.Join(quoted, " "))
}

// ShellQuote makes a string safe for POSIX shell embedding (build-arg values
// can contain anything).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ProxyRun builds argv to boot the kamal-proxy container on a server.
func ProxyRun(p config.Proxy) []string {
	return []string{
		"run", "--detach",
		"--name", ProxyContainerName,
		"--network", p.Network,
		"--restart", "unless-stopped",
		"--publish", fmt.Sprintf("%d:80", p.HTTPPort),
		"--publish", fmt.Sprintf("%d:443", p.HTTPSPort),
		"--volume", "ferry-kamal-proxy:/home/kamal-proxy/.config/kamal-proxy",
		p.Image,
	}
}

// ProxyDeployOpts parameterizes a kamal-proxy target registration.
type ProxyDeployOpts struct {
	Service       string // proxy service name (web, or web-<sha> for previews)
	Target        string // <container>:<port>
	Hosts         []string
	Health        config.Health
	DeployTimeout string
	DrainTimeout  string
	Passthrough   map[string]string // advanced kamal-proxy deploy flags
}

// ProxyDeploy builds the `kamal-proxy deploy` argv (to run via docker exec in
// the proxy container). It blocks until the new target passes health checks,
// then atomically swaps traffic and drains the old one — that is the whole
// zero-downtime story.
func ProxyDeploy(o ProxyDeployOpts) []string {
	args := []string{"exec", ProxyContainerName, "kamal-proxy", "deploy", o.Service, "--target", o.Target, "--tls"}
	for _, h := range o.Hosts {
		args = append(args, "--host", h)
	}
	if o.Health.Path != "" {
		args = append(args, "--health-check-path", o.Health.Path)
	}
	if o.Health.Interval != "" {
		args = append(args, "--health-check-interval", o.Health.Interval)
	}
	if o.Health.Timeout != "" {
		args = append(args, "--health-check-timeout", o.Health.Timeout)
	}
	if o.DeployTimeout != "" {
		args = append(args, "--deploy-timeout", o.DeployTimeout)
	}
	if o.DrainTimeout != "" {
		args = append(args, "--drain-timeout", o.DrainTimeout)
	}
	keys := make([]string, 0, len(o.Passthrough))
	for k := range o.Passthrough {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		flag := "--" + strings.ReplaceAll(k, "_", "-")
		if v := o.Passthrough[k]; v == "true" {
			args = append(args, flag)
		} else {
			args = append(args, flag, v)
		}
	}
	return args
}

// ProxyRemove builds `kamal-proxy remove <service>` argv.
func ProxyRemove(service string) []string {
	return []string{"exec", ProxyContainerName, "kamal-proxy", "remove", service}
}

// ProxyStop builds `kamal-proxy stop` argv (maintenance mode).
func ProxyStop(service, message string) []string {
	args := []string{"exec", ProxyContainerName, "kamal-proxy", "stop", service}
	if message != "" {
		args = append(args, "--message", message)
	}
	return args
}

// ProxyResume builds `kamal-proxy resume <service>` argv.
func ProxyResume(service string) []string {
	return []string{"exec", ProxyContainerName, "kamal-proxy", "resume", service}
}

// PsFilter builds `docker ps` argv filtered on ferry labels. Empty service
// or preview matches any; preview "-" filters to non-preview containers.
func PsFilter(project, service string, all bool, format string) []string {
	args := []string{"ps"}
	if all {
		args = append(args, "--all")
	}
	args = append(args, "--filter", fmt.Sprintf("label=%s=%s", LabelProject, project))
	if service != "" {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", LabelService, service))
	}
	if format != "" {
		args = append(args, "--format", format)
	}
	return args
}
