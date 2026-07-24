package deploy

import (
	"fmt"
	"strings"

	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/plan"
)

// Rollback restarts a retained container and re-registers it with
// kamal-proxy (health-gated like any deploy). No rebuild, no transfer.
// version "" means the most recently retained one.
func (d *Deployer) Rollback(targets []*plan.Target, version string) error {
	type step struct {
		t         *plan.Target
		dk        *exec.Docker
		container string
		version   string
	}

	// Verify the retained container exists on every server before touching
	// anything — fail safe, not fail forward.
	var steps []step
	for _, t := range targets {
		// Jobs have nothing to restart; stateful services have no version
		// history to roll between — their data lives in volumes.
		if t.Overlay.Job || t.Stateful() {
			continue
		}
		for _, s := range t.Servers {
			dk, err := d.Docker(s)
			if err != nil {
				return err
			}
			container, ver, err := d.retainedContainer(dk, t, version)
			if err != nil {
				return fmt.Errorf("rollback %s on %s: %w", t.Name, s.Name, err)
			}
			steps = append(steps, step{t, dk, container, ver})
		}
	}

	for _, st := range steps {
		current := d.runningContainers(st.dk, st.t.Name, "")
		d.logf("%s: rolling back to %s (%s)", st.t.Name, st.version, st.container)
		if st.t.Proxied() {
			if _, err := st.dk.Run("start", st.container); err != nil {
				return err
			}
			args := dockercmd.ProxyDeploy(dockercmd.ProxyDeployOpts{
				Service:       st.t.ProxyService(),
				Target:        fmt.Sprintf("%s:%d", st.container, st.t.Port()),
				Hosts:         st.t.DomainsOn(st.dk.Server),
				Health:        st.t.Overlay.Health,
				DeployTimeout: d.Timeout.String(),
				DrainTimeout:  "30s",
				Passthrough:   st.t.Overlay.Proxy,
			})
			if _, err := st.dk.Run(args...); err != nil {
				st.dk.Run("stop", st.container)
				return fmt.Errorf("kamal-proxy refused the rollback target (current version keeps serving): %w", err)
			}
			for name := range current {
				st.dk.Run("stop", name)
			}
		} else {
			for name := range current {
				if _, err := st.dk.Run("stop", name); err != nil {
					return err
				}
			}
			if _, err := st.dk.Run("start", st.container); err != nil {
				return err
			}
		}
		d.audit(fmt.Sprintf("rollback %s to %s", st.t.Name, st.version))
	}
	return nil
}

// retainedContainer finds the stopped container to roll back to.
func (d *Deployer) retainedContainer(dk *exec.Docker, t *plan.Target, version string) (name, ver string, err error) {
	if version != "" {
		container := t.ContainerName(version)
		state, err := dk.Run("inspect", "--format", "{{.State.Status}}", container)
		if err != nil {
			return "", "", fmt.Errorf("no retained container for version %s (is it within build.retain?)", version)
		}
		if strings.TrimSpace(state) == "running" {
			return "", "", fmt.Errorf("version %s is already running", version)
		}
		return container, version, nil
	}

	out, _ := dk.Run("ps", "--all", "--filter", "status=exited", "--latest",
		"--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelProject, d.cfg().Name),
		"--filter", fmt.Sprintf("label=%s=%s", dockercmd.LabelService, t.Name),
		"--format", "{{.Names}}\t{{.Label \"ferry.version\"}}\t{{.Label \"ferry.preview\"}}")
	parts := strings.Split(strings.TrimSpace(out), "\t")
	if len(parts) < 2 || parts[0] == "" || (len(parts) > 2 && parts[2] != "") {
		return "", "", fmt.Errorf("no retained container found (nothing to roll back to)")
	}
	return parts[0], parts[1], nil
}
