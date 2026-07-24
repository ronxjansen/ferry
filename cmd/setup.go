package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/proxy"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup [server...]",
	Short: "Provision servers: install Docker, create contexts, boot kamal-proxy",
	Long: `Provision servers from scratch: install Docker via get.docker.com
(distro-agnostic), create the local Docker context per server, and boot
kamal-proxy on servers that host proxied services. Idempotent — safe to
re-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		cfg := p.Config

		servers := make([]*config.Server, 0, len(cfg.Servers))
		if len(args) > 0 {
			for _, name := range args {
				s, err := cfg.GetServer(name)
				if err != nil {
					return err
				}
				servers = append(servers, s)
			}
		} else {
			for _, name := range cfg.ServerNames() {
				servers = append(servers, cfg.Servers[name])
			}
		}

		proxied := map[string]bool{}
		for _, t := range p.Targets {
			if t.Proxied() {
				for _, s := range t.Servers {
					proxied[s.Name] = true
				}
			}
		}

		d := newDeployer(p, "", time.Minute)
		for _, s := range servers {
			infof("Setting up %s (%s)", s.Name, s.Host)
			host := &exec.Host{Server: s}

			if out, err := host.Run("docker --version"); err != nil || !strings.Contains(out, "Docker version") {
				infof("%s: installing Docker via get.docker.com", s.Name)
				if err := host.Stream("curl -fsSL https://get.docker.com | sh"); err != nil {
					return fmt.Errorf("failed to install Docker on %s: %w", s.Name, err)
				}
			} else {
				infof("%s: Docker already installed", s.Name)
			}

			if _, err := host.Run("mkdir -p .ferry/apps"); err != nil {
				return err
			}

			dk, err := d.Docker(s)
			if err != nil {
				return err
			}

			if proxied[s.Name] {
				infof("%s: booting kamal-proxy", s.Name)
				if err := proxy.Boot(dk, cfg.Proxy); err != nil {
					return err
				}
			} else if err := proxy.EnsureNetwork(dk, cfg.Proxy.Network); err != nil {
				return err
			}

			infof("%s: ready", s.Name)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
