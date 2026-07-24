// Package proxy manages the kamal-proxy container on each server. Ferry
// delegates health-gating, cutover, drain and TLS to it; this package only
// boots and babysits the container.
package proxy

import (
	"fmt"
	"strings"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/ronxjansen/ferry/internal/exec"
)

// EnsureNetwork creates the attachable proxy network if missing.
func EnsureNetwork(d *exec.Docker, network string) error {
	if _, err := d.Run("network", "inspect", network); err == nil {
		return nil
	}
	if _, err := d.Run("network", "create", "--attachable", network); err != nil {
		return fmt.Errorf("failed to create network %s: %w", network, err)
	}
	return nil
}

// Boot ensures kamal-proxy is running with the pinned image: starts it if
// stopped, recreates it on image change, boots it if absent.
func Boot(d *exec.Docker, p config.Proxy) error {
	if err := EnsureNetwork(d, p.Network); err != nil {
		return err
	}

	image, err := d.Run("inspect", "--format", "{{.Config.Image}}", dockercmd.ProxyContainerName)
	if err == nil {
		if strings.TrimSpace(image) != p.Image {
			// Version change: kamal-proxy state persists in its volume, so
			// recreate is safe. Brief proxy blip, existing targets restored
			// from persisted state.
			if _, err := d.Run("rm", "-f", dockercmd.ProxyContainerName); err != nil {
				return err
			}
			_, err := d.Run(dockercmd.ProxyRun(p)...)
			return err
		}
		running, _ := d.Run("inspect", "--format", "{{.State.Running}}", dockercmd.ProxyContainerName)
		if strings.TrimSpace(running) != "true" {
			_, err := d.Run("start", dockercmd.ProxyContainerName)
			return err
		}
		return nil
	}

	if _, err := d.Run(dockercmd.ProxyRun(p)...); err != nil {
		return fmt.Errorf("failed to boot kamal-proxy: %w", err)
	}
	return nil
}

// Status returns a one-line status of the proxy container.
func Status(d *exec.Docker) (string, error) {
	out, err := d.Run("ps", "--all", "--filter", "name="+dockercmd.ProxyContainerName,
		"--format", "{{.Names}}\t{{.Image}}\t{{.Status}}")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "kamal-proxy is not installed (run `ferry proxy init`)", nil
	}
	return out, nil
}
