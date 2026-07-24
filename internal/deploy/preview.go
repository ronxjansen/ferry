package deploy

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/ronxjansen/ferry/internal/hooks"
	"github.com/ronxjansen/ferry/internal/plan"
)

// previewProject is the compose-style project scope for one preview.
func (d *Deployer) previewProject(sha string) string {
	return fmt.Sprintf("%s-%s", d.cfg().Name, sha)
}

func previewContainerName(service, sha string) string {
	return fmt.Sprintf("%s-%s-preview", service, sha)
}

// previewProxyService is the kamal-proxy service name for a preview target,
// project-prefixed for the same reason as Target.ProxyService.
func (d *Deployer) previewProxyService(service, sha string) string {
	return fmt.Sprintf("%s-%s-%s", d.cfg().Name, service, sha)
}

// previewHost returns the routing host: <sha>.<base> for the primary proxied
// service, <service>-<sha>.<base> for further ones (both match a wildcard).
func previewHost(service, sha, base string, primary bool) string {
	if primary {
		return fmt.Sprintf("%s.%s", sha, base)
	}
	return fmt.Sprintf("%s-%s.%s", service, sha, base)
}

// DeployPreview deploys a preview environment for d.Version on the preview
// server: SHA-tagged build, isolated clones with fresh volumes for isolate
// services, per-preview TLS via kamal-proxy. Re-running on the same SHA
// replaces it.
func (d *Deployer) DeployPreview() error {
	sha := d.Version
	targets, err := d.Plan.PreviewServices()
	if err != nil {
		return err
	}
	server, err := d.Plan.PreviewServer()
	if err != nil {
		return err
	}
	base, err := d.Plan.PreviewDomain()
	if err != nil {
		return err
	}
	dk, err := d.Docker(server)
	if err != nil {
		return err
	}
	host := d.Host(server)
	started := time.Now()

	if err := d.ReapExpired(); err != nil {
		d.logf("warning: preview reap failed: %v", err)
	}

	if err := d.ensureNetwork(dk); err != nil {
		return err
	}
	network := d.previewProject(sha)
	if err := ensureNetworkNamed(dk, network); err != nil {
		return err
	}

	isolate := map[string]bool{}
	for _, s := range d.cfg().Preview.Isolate {
		isolate[s] = true
	}

	primaryAssigned := false
	for _, t := range targets {
		hookEnv := hooks.Env{
			Version: sha, Service: t.Name, Server: server.Name,
			Image: t.ImageRef(sha), Performer: d.Performer, Preview: sha,
			Runtime: time.Since(started),
		}
		if err := d.hookRunner().RunLocal(config.HookPreBuild, hookEnv); err != nil {
			return err
		}
		image, err := d.EnsureImage(t, server)
		if err != nil {
			return err
		}
		envPath, cleanup, err := d.preparePreviewEnv(t, server)
		if err != nil {
			return err
		}
		defer cleanup()

		if err := d.hookRunner().RunRemote(host, config.HookPreDeploy, hookEnv); err != nil {
			return err
		}

		volumeProject := d.cfg().Name
		if isolate[t.Name] {
			volumeProject = d.previewProject(sha)
		}
		opts := dockercmd.RunOpts{
			Name:          previewContainerName(t.Name, sha),
			Project:       d.cfg().Name,
			VolumeProject: volumeProject,
			Service:       t.Name,
			Version:       sha,
			Preview:       sha,
			Network:       network,
			EnvFile:       envPath,
			Detach:        true,
		}

		// Previews deploy fresh — no cutover dance.
		dk.Run("rm", "-f", opts.Name)

		if t.Overlay.Job {
			opts.Detach = false
			opts.Rm = true
			d.logf("preview %s: running job %s", sha, t.Name)
			if err := dk.Stream(dockercmd.Run(t.Compose, image, opts)...); err != nil {
				return fmt.Errorf("preview job %s failed: %w", t.Name, err)
			}
			continue
		}

		d.logf("preview %s: starting %s", sha, opts.Name)
		if err := d.startContainer(dk, t, image, opts); err != nil {
			return err
		}

		if t.Proxied() {
			if _, err := dk.Run("network", "connect", d.cfg().Proxy.Network, opts.Name); err != nil {
				return err
			}
			previewURL := previewHost(t.Name, sha, base, !primaryAssigned)
			primaryAssigned = true
			args := dockercmd.ProxyDeploy(dockercmd.ProxyDeployOpts{
				Service:       d.previewProxyService(t.Name, sha),
				Target:        fmt.Sprintf("%s:%d", opts.Name, t.Port()),
				Hosts:         []string{previewURL},
				Health:        t.Overlay.Health,
				DeployTimeout: d.Timeout.String(),
				DrainTimeout:  "10s",
				Passthrough:   t.Overlay.Proxy,
			})
			if _, err := dk.Run(args...); err != nil {
				logs, _ := dk.Run("logs", "--tail", "50", opts.Name)
				dk.Run("rm", "-f", opts.Name)
				return fmt.Errorf("preview %s failed health checks: %w\ncontainer logs:\n%s", t.Name, err, logs)
			}
			d.logf("preview %s: %s live at https://%s", sha, t.Name, previewURL)
		}

		if err := d.hookRunner().RunRemote(host, config.HookPostDeploy, hookEnv); err != nil {
			return err
		}
	}

	d.audit(fmt.Sprintf("preview deploy %s", sha))
	return nil
}

// preparePreviewEnv merges env with the preview overlay so previews never see
// prod credentials. Nothing is shipped to the host — previews are ephemeral.
func (d *Deployer) preparePreviewEnv(t *plan.Target, s *config.Server) (string, func(), error) {
	previewEnv := ""
	if d.cfg().Preview.EnvFile != "" {
		previewEnv = filepath.Join(d.cfg().Dir, d.cfg().Preview.EnvFile)
	}
	return d.mergeEnvToTemp(t, s, previewEnv, false)
}

// Preview describes one running preview environment.
type Preview struct {
	SHA      string
	URL      string
	Created  time.Time
	Server   string
	Services []string
}

// ListPreviews discovers previews from container labels on the preview
// server — works from any checkout, no local state.
func (d *Deployer) ListPreviews() ([]Preview, error) {
	server, err := d.Plan.PreviewServer()
	if err != nil {
		return nil, err
	}
	dk, err := d.Docker(server)
	if err != nil {
		return nil, err
	}
	base, err := d.Plan.PreviewDomain()
	if err != nil {
		return nil, err
	}

	out, err := dk.Run("ps", "--all",
		"--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelProject, d.cfg().Name),
		"--filter", "label="+dockercmd.LabelPreview,
		"--format", "{{.Label \"ferry.preview\"}}\t{{.Label \"ferry.service\"}}\t{{.CreatedAt}}")
	if err != nil {
		return nil, err
	}

	bySha := map[string]*Preview{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		sha := parts[0]
		p, ok := bySha[sha]
		if !ok {
			p = &Preview{SHA: sha, URL: "https://" + previewHost("", sha, base, true), Server: server.Name}
			bySha[sha] = p
		}
		p.Services = append(p.Services, parts[1])
		if created, err := time.Parse("2006-01-02 15:04:05 -0700 MST", parts[2]); err == nil {
			if p.Created.IsZero() || created.Before(p.Created) {
				p.Created = created
			}
		}
	}

	previews := make([]Preview, 0, len(bySha))
	for _, p := range bySha {
		sort.Strings(p.Services)
		previews = append(previews, *p)
	}
	sort.Slice(previews, func(i, j int) bool { return previews[i].Created.After(previews[j].Created) })
	return previews, nil
}

// RemovePreview tears down one preview: proxy targets, containers, volumes,
// network, images.
func (d *Deployer) RemovePreview(sha string) error {
	server, err := d.Plan.PreviewServer()
	if err != nil {
		return err
	}
	dk, err := d.Docker(server)
	if err != nil {
		return err
	}

	out, _ := dk.Run("ps", "--all",
		"--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelProject, d.cfg().Name),
		"--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelPreview, sha),
		"--format", "{{.Names}}\t{{.Label \"ferry.service\"}}\t{{.Image}}")
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("no preview found for %s", sha)
	}

	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		name, service, image := parts[0], parts[1], parts[2]
		dk.Run(dockercmd.ProxyRemove(d.previewProxyService(service, sha))...)
		d.logf("preview %s: removing %s", sha, name)
		dk.Run("rm", "-f", name)
		dk.Run("rmi", image) // no-op while prod still uses the same version
	}

	project := d.previewProject(sha)
	if volumes, _ := dk.Run("volume", "ls", "--quiet", "--filter", "name="+project+"_"); strings.TrimSpace(volumes) != "" {
		for _, v := range strings.Fields(volumes) {
			dk.Run("volume", "rm", v)
		}
	}
	dk.Run("network", "rm", project)

	d.audit(fmt.Sprintf("preview remove %s", sha))
	return nil
}

// ReapExpired removes previews older than preview.ttl (opportunistic, called
// on preview invocations).
func (d *Deployer) ReapExpired() error {
	ttl, err := d.cfg().Preview.TTLDuration()
	if err != nil || ttl == 0 {
		return err
	}
	previews, err := d.ListPreviews()
	if err != nil {
		return err
	}
	for _, p := range previews {
		if p.Created.IsZero() || time.Since(p.Created) < ttl {
			continue
		}
		d.logf("preview %s expired (older than %s), removing", p.SHA, d.cfg().Preview.TTL)
		if err := d.RemovePreview(p.SHA); err != nil {
			return err
		}
	}
	return nil
}
