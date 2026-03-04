package cmd

import (
	"fmt"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/proxy"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Manage the reverse proxy",
	Long:  `Initialize and manage the Traefik reverse proxy on your servers`,
}

var proxyInitCmd = &cobra.Command{
	Use:   "init [server]",
	Short: "Initialize the reverse proxy",
	Long:  `Initialize Traefik on a server or all servers if none specified`,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := proxy.New(cfg.Proxy)
		if err != nil {
			return err
		}

		var servers []config.Server
		if len(args) > 0 {
			server, err := cfg.GetServer(args[0])
			if err != nil {
				return fmt.Errorf("server '%s' not found. Available servers: %v", args[0], cfg.ServerNames())
			}
			servers = []config.Server{*server}
		} else {
			servers = cfg.Servers
		}

		for _, server := range servers {
			logger.Info("Initializing proxy", zap.String("server", server.Name))

			executor, err := exec.NewSSHExecutor(server)
			if err != nil {
				return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
			}

			if err := p.Init(executor); err != nil {
				executor.Close()
				return fmt.Errorf("failed to initialize proxy on %s: %w", server.Name, err)
			}

			executor.Close()
			logger.Info("Proxy initialized", zap.String("server", server.Name))
		}

		return nil
	},
}

var proxyStatusCmd = &cobra.Command{
	Use:   "status [server]",
	Short: "Show proxy status",
	Long:  `Show the status of Traefik on a server or all servers`,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := proxy.New(cfg.Proxy)
		if err != nil {
			return err
		}

		var servers []config.Server
		if len(args) > 0 {
			server, err := cfg.GetServer(args[0])
			if err != nil {
				return fmt.Errorf("server '%s' not found. Available servers: %v", args[0], cfg.ServerNames())
			}
			servers = []config.Server{*server}
		} else {
			servers = cfg.Servers
		}

		for _, server := range servers {
			executor, err := exec.NewSSHExecutor(server)
			if err != nil {
				return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
			}

			status, err := p.Status(executor)
			executor.Close()
			if err != nil {
				return fmt.Errorf("failed to get status from %s: %w", server.Name, err)
			}

			fmt.Printf("=== %s ===\n%s\n", server.Name, status)
		}

		return nil
	},
}

func init() {
	proxyCmd.AddCommand(proxyInitCmd)
	proxyCmd.AddCommand(proxyStatusCmd)
	rootCmd.AddCommand(proxyCmd)
}
