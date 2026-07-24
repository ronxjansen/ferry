// Package plan merges the compose project with the ferry.yaml overlay into
// resolved deploy targets. Compose is the source of truth for what a service
// is; ferry.yaml adds where it runs and how it is exposed.
package plan

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/ronxjansen/ferry/internal/config"
)

// Target is one deployable service: a compose service plus its overlay,
// resolved against concrete servers.
type Target struct {
	Name    string
	Overlay *config.Service
	Compose types.ServiceConfig
	Servers []*config.Server

	cfg *config.Config
}

// Plan is the fully resolved deployment model, targets in dependency order.
type Plan struct {
	Config  *config.Config
	Project *types.Project
	Targets []*Target
}

// Load reads the compose project and resolves it against cfg.
func Load(cfg *config.Config) (*Plan, error) {
	if len(cfg.Compose) == 0 {
		return nil, fmt.Errorf("no compose file found (looked for compose.yaml / docker-compose.yml in %s)", cfg.Dir)
	}
	files := make([]string, len(cfg.Compose))
	for i, f := range cfg.Compose {
		if filepath.IsAbs(f) {
			files[i] = f
		} else {
			files[i] = filepath.Join(cfg.Dir, f)
		}
	}

	opts, err := cli.NewProjectOptions(files,
		cli.WithWorkingDirectory(cfg.Dir),
		cli.WithName(cfg.Name),
		cli.WithOsEnv,
		cli.WithDotEnv,
	)
	if err != nil {
		return nil, err
	}
	project, err := opts.LoadProject(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load compose project: %w", err)
	}

	var targets []*Target
	for name, overlay := range cfg.Services {
		svc, ok := project.Services[name]
		if !ok {
			return nil, fmt.Errorf("service %q in ferry.yaml does not exist in the compose project (compose services: %s)",
				name, strings.Join(composeServiceNames(project), ", "))
		}
		t := &Target{Name: name, Overlay: overlay, Compose: svc, cfg: cfg}
		for _, srv := range overlay.Servers {
			server, err := cfg.GetServer(srv)
			if err != nil {
				return nil, err
			}
			t.Servers = append(t.Servers, server)
		}
		if svc.Build == nil && svc.Image == "" {
			return nil, fmt.Errorf("service %q has neither build nor image in compose", name)
		}
		if t.Proxied() && t.Port() == 0 {
			return nil, fmt.Errorf("service %q is proxied but has no port (set services.%s.port in ferry.yaml, or expose/ports in compose)", name, name)
		}
		targets = append(targets, t)
	}

	ordered, err := sortByDependencies(targets)
	if err != nil {
		return nil, err
	}

	return &Plan{Config: cfg, Project: project, Targets: ordered}, nil
}

// Target returns a deploy target by service name.
func (p *Plan) Target(name string) (*Target, error) {
	for _, t := range p.Targets {
		if t.Name == name {
			return t, nil
		}
	}
	return nil, fmt.Errorf("service %q not found (available: %s)", name, strings.Join(p.Config.ServiceNames(), ", "))
}

// Select returns the named targets in dependency order; empty names = all.
func (p *Plan) Select(names []string) ([]*Target, error) {
	if len(names) == 0 {
		return p.Targets, nil
	}
	want := map[string]bool{}
	for _, n := range names {
		if _, err := p.Target(n); err != nil {
			return nil, err
		}
		want[n] = true
	}
	var out []*Target
	for _, t := range p.Targets {
		if want[t.Name] {
			out = append(out, t)
		}
	}
	return out, nil
}

// Proxied reports whether the service is registered with kamal-proxy: it has
// a domain, or an explicit port (which gets an sslip.io fallback domain).
func (t *Target) Proxied() bool {
	return len(t.Overlay.AllDomains()) > 0 || t.Overlay.Port != 0
}

// Port returns the container port the proxy targets: the overlay port, else
// the first compose expose entry, else the first compose ports target.
func (t *Target) Port() int {
	if t.Overlay.Port != 0 {
		return t.Overlay.Port
	}
	for _, e := range t.Compose.Expose {
		if p, err := strconv.Atoi(e); err == nil {
			return p
		}
	}
	if len(t.Compose.Ports) > 0 {
		return int(t.Compose.Ports[0].Target)
	}
	return 0
}

// ProxyService returns the kamal-proxy service name, prefixed with the
// project name: kamal-proxy keys registrations by service name globally per
// server, so two projects sharing a server with a same-named service would
// otherwise clobber each other's routing and TLS host allowlist.
func (t *Target) ProxyService() string {
	return fmt.Sprintf("%s-%s", t.cfg.Name, t.Name)
}

// DomainsOn returns the domains for this service on a given server; without
// configured domains it falls back to <service>.<ip-with-dashes>.sslip.io so
// first contact needs zero DNS setup.
func (t *Target) DomainsOn(server *config.Server) []string {
	if d := t.Overlay.AllDomains(); len(d) > 0 {
		return d
	}
	return []string{fmt.Sprintf("%s.%s.sslip.io", t.Name, strings.ReplaceAll(server.Host, ".", "-"))}
}

// BuildMethod resolves to "remote" or "pull": per-service override, global
// build.method, else remote when compose has build:. Image-only services
// (postgres:16, ...) are always pull — there is nothing to build.
func (t *Target) BuildMethod() string {
	if !t.Versioned() {
		return "pull"
	}
	if t.Overlay.Build != "" {
		return t.Overlay.Build
	}
	if t.Config().Build.Method != "" {
		return t.Config().Build.Method
	}
	return "remote"
}

// Versioned reports whether this service's image is tagged with the git SHA.
// Image-only services (postgres:16, redis, ...) deploy the compose image
// verbatim.
func (t *Target) Versioned() bool {
	return t.Compose.Build != nil
}

// BuildContextDir returns the absolute build context directory for a
// versioned service (shipping it to a build host needs the real path).
func (t *Target) BuildContextDir() string {
	ctx := t.Compose.Build.Context
	if ctx == "" {
		ctx = "."
	}
	if filepath.IsAbs(ctx) {
		return ctx
	}
	return filepath.Join(t.cfg.Dir, ctx)
}

// ImageRef returns the image reference to build/pull/run for a version.
func (t *Target) ImageRef(version string) string {
	if !t.Versioned() {
		return t.Compose.Image
	}
	base := t.Compose.Image
	if base == "" {
		base = fmt.Sprintf("%s/%s", t.projectName(), t.Name)
	} else if i := strings.LastIndex(base, ":"); i > strings.LastIndex(base, "/") {
		base = base[:i]
	}
	return fmt.Sprintf("%s:%s", base, version)
}

// ContainerName returns the versioned container name (Kamal-style: no
// renames, no color juggling).
func (t *Target) ContainerName(version string) string {
	return fmt.Sprintf("%s-%s", t.Name, version)
}

// RunsOn reports whether the target is mapped to the given server.
func (t *Target) RunsOn(server *config.Server) bool {
	for _, s := range t.Servers {
		if s.Name == server.Name {
			return true
		}
	}
	return false
}

// Config returns the owning config.
func (t *Target) Config() *config.Config { return t.cfg }

// CrossServerEnv returns FERRY_SERVICE_<NAME>_HOST entries for every other
// deployed service that does not run on the given server — the 12-factor
// escape hatch since Docker networks don't span hosts.
func (p *Plan) CrossServerEnv(target *Target, server *config.Server) map[string]string {
	env := map[string]string{}
	for _, other := range p.Targets {
		if other.Name == target.Name {
			continue
		}
		if other.RunsOn(server) {
			continue // same host: compose service-name DNS works
		}
		key := "FERRY_SERVICE_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(other.Name)) + "_HOST"
		env[key] = other.Servers[0].Host
	}
	return env
}

// Warnings returns non-fatal config issues: depends_on edges that cross
// servers, where startup ordering is only best-effort.
func (p *Plan) Warnings() []string {
	var out []string
	for _, t := range p.Targets {
		for depName := range t.Compose.DependsOn {
			dep, err := p.Target(depName)
			if err != nil {
				continue // dependency is not deployed by ferry
			}
			for _, s := range t.Servers {
				if !dep.RunsOn(s) {
					out = append(out, fmt.Sprintf(
						"service %q depends on %q which does not run on server %q: startup ordering is best-effort (deploy order only)",
						t.Name, depName, s.Name))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// PreviewServices returns the targets deployed by `ferry preview`
// (preview.services, default all), in dependency order.
func (p *Plan) PreviewServices() ([]*Target, error) {
	return p.Select(p.Config.Preview.Services)
}

// PreviewServer resolves the preview server: preview.server, else the first
// server of the first proxied service.
func (p *Plan) PreviewServer() (*config.Server, error) {
	if p.Config.Preview.Server != "" {
		return p.Config.GetServer(p.Config.Preview.Server)
	}
	for _, t := range p.Targets {
		if t.Proxied() {
			return t.Servers[0], nil
		}
	}
	return nil, fmt.Errorf("no preview server: set preview.server or give a service a domain")
}

// PreviewDomain returns the base domain previews hang off: preview.domain,
// else the primary service domain, else an sslip.io fallback.
func (p *Plan) PreviewDomain() (string, error) {
	if p.Config.Preview.Domain != "" {
		return p.Config.Preview.Domain, nil
	}
	for _, t := range p.Targets {
		if d := t.Overlay.AllDomains(); len(d) > 0 {
			return d[0], nil
		}
	}
	server, err := p.PreviewServer()
	if err != nil {
		return "", fmt.Errorf("no preview domain: set preview.domain or give a service a domain")
	}
	return fmt.Sprintf("%s.sslip.io", strings.ReplaceAll(server.Host, ".", "-")), nil
}

func composeServiceNames(p *types.Project) []string {
	names := make([]string, 0, len(p.Services))
	for n := range p.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (t *Target) projectName() string {
	return t.Config().Name
}

// sortByDependencies orders targets topologically by compose depends_on edges
// restricted to deployed services (Kahn's algorithm, name-sorted for
// determinism).
func sortByDependencies(targets []*Target) ([]*Target, error) {
	byName := map[string]*Target{}
	for _, t := range targets {
		byName[t.Name] = t
	}
	inDegree := map[string]int{}
	dependents := map[string][]string{}
	for _, t := range targets {
		inDegree[t.Name] += 0
		for dep := range t.Compose.DependsOn {
			if _, ok := byName[dep]; !ok {
				continue
			}
			dependents[dep] = append(dependents[dep], t.Name)
			inDegree[t.Name]++
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	var ordered []*Target
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		ordered = append(ordered, byName[name])
		next := dependents[name]
		sort.Strings(next)
		for _, dep := range next {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
		sort.Strings(queue)
	}

	if len(ordered) != len(targets) {
		return nil, fmt.Errorf("circular depends_on between deployed services")
	}
	return ordered, nil
}
