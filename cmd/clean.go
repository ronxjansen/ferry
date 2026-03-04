package cmd

import (
	"fmt"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var cleanCmd = &cobra.Command{
	Use:   "clean [server]",
	Short: "Remove all Ferry resources from a server",
	Long:  `Stop Traefik, remove all containers, networks, and the ferry directory`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
			logger.Info("Cleaning server", zap.String("server", server.Name))

			executor, err := exec.NewSSHExecutor(server)
			if err != nil {
				return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
			}

			// Stop and remove traefik
			executor.Run("docker stop traefik")
			executor.Run("docker rm traefik")

			// Remove traefik network
			executor.Run(fmt.Sprintf("docker network rm %s", cfg.Proxy.Network))

			// Full cleanup
			executor.Run("docker system prune -a -f")
			executor.Run("docker volume prune -f")

			// Remove ferry directory
			executor.Run("rm -rf $HOME/ferry")

			executor.Close()
			logger.Info("Server cleaned", zap.String("server", server.Name))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
}
