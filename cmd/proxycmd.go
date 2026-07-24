package cmd

import (
	"fmt"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/plan"
	"github.com/ronxjansen/ferry/internal/proxy"
	"github.com/spf13/cobra"
)

var proxyLogsFollow bool

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Manage kamal-proxy on the servers",
}

// proxyServers resolves the servers to act on: the args, or every server
// hosting a proxied service.
func proxyServers(p *plan.Plan, args []string) ([]*config.Server, error) {
	if len(args) > 0 {
		var servers []*config.Server
		for _, name := range args {
			s, err := p.Config.GetServer(name)
			if err != nil {
				return nil, err
			}
			servers = append(servers, s)
		}
		return servers, nil
	}
	seen := map[string]bool{}
	var servers []*config.Server
	for _, t := range p.Targets {
		if !t.Proxied() {
			continue
		}
		for _, s := range t.Servers {
			if !seen[s.Name] {
				seen[s.Name] = true
				servers = append(servers, s)
			}
		}
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("no proxied services configured (give a service a domain or port)")
	}
	return servers, nil
}

func forEachProxyServer(args []string, fn func(s *config.Server, dk *exec.Docker) error) error {
	p, err := loadPlan()
	if err != nil {
		return err
	}
	servers, err := proxyServers(p, args)
	if err != nil {
		return err
	}
	d := newDeployer(p, "", time.Minute)
	for _, s := range servers {
		dk, err := d.Docker(s)
		if err != nil {
			return err
		}
		if err := fn(s, dk); err != nil {
			return err
		}
	}
	return nil
}

var proxyInitCmd = &cobra.Command{
	Use:   "init [server...]",
	Short: "Boot (or upgrade) the kamal-proxy container",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		return forEachProxyServer(args, func(s *config.Server, dk *exec.Docker) error {
			infof("%s: booting kamal-proxy", s.Name)
			return proxy.Boot(dk, p.Config.Proxy)
		})
	},
}

var proxyStatusCmd = &cobra.Command{
	Use:   "status [server...]",
	Short: "Show kamal-proxy status per server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return forEachProxyServer(args, func(s *config.Server, dk *exec.Docker) error {
			status, err := proxy.Status(dk)
			if err != nil {
				return err
			}
			fmt.Printf("%s\t%s\n", s.Name, status)
			return nil
		})
	},
}

var proxyLogsCmd = &cobra.Command{
	Use:   "logs [server...]",
	Short: "Show kamal-proxy logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return forEachProxyServer(args, func(s *config.Server, dk *exec.Docker) error {
			logsArgs := []string{"logs", "--tail", "100"}
			if proxyLogsFollow {
				logsArgs = append(logsArgs, "--follow")
			}
			return dk.Interactive(append(logsArgs, dockercmd.ProxyContainerName)...)
		})
	},
}

var proxyRestartCmd = &cobra.Command{
	Use:   "restart [server...]",
	Short: "Restart kamal-proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		return forEachProxyServer(args, func(s *config.Server, dk *exec.Docker) error {
			if _, err := dk.Run("restart", dockercmd.ProxyContainerName); err != nil {
				return err
			}
			infof("%s: kamal-proxy restarted", s.Name)
			return nil
		})
	},
}

func init() {
	proxyLogsCmd.Flags().BoolVarP(&proxyLogsFollow, "follow", "f", false, "Follow log output")
	proxyCmd.AddCommand(proxyInitCmd)
	proxyCmd.AddCommand(proxyStatusCmd)
	proxyCmd.AddCommand(proxyLogsCmd)
	proxyCmd.AddCommand(proxyRestartCmd)
	rootCmd.AddCommand(proxyCmd)
}
