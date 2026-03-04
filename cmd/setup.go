package cmd

import (
	"fmt"
	"strings"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/proxy"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var setupCmd = &cobra.Command{
	Use:   "setup [server]",
	Short: "Setup a server with Docker and Traefik",
	Long:  `Install Docker and initialize Traefik on a server`,
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

		p, err := proxy.New(cfg.Proxy)
		if err != nil {
			return err
		}

		for _, server := range servers {
			logger.Info("Setting up server", zap.String("server", server.Name))

			executor, err := exec.NewSSHExecutor(server)
			if err != nil {
				return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
			}

			// Check if Docker is installed
			if err := installDockerIfNeeded(executor); err != nil {
				executor.Close()
				return fmt.Errorf("failed to install Docker on %s: %w", server.Name, err)
			}

			// Initialize proxy
			if err := p.Init(executor); err != nil {
				executor.Close()
				return fmt.Errorf("failed to initialize proxy on %s: %w", server.Name, err)
			}

			executor.Close()
			logger.Info("Server setup complete", zap.String("server", server.Name))
		}

		return nil
	},
}

func installDockerIfNeeded(executor exec.Executor) error {
	// Check if Docker is installed
	output, err := executor.Run("docker --version")
	if err == nil && strings.Contains(output, "Docker version") {
		logger.Info("Docker already installed")
		return nil
	}

	logger.Info("Installing Docker")

	commands := []string{
		"sudo apt-get update",
		"sudo apt-get install -y apt-transport-https ca-certificates curl software-properties-common",
		"curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -",
		`sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"`,
		"sudo apt-get update",
		"sudo apt-get install -y docker-ce docker-ce-cli containerd.io",
		"sudo usermod -aG docker $USER",
	}

	for _, cmd := range commands {
		if _, err := executor.Run(cmd); err != nil {
			return fmt.Errorf("command failed: %s: %w", cmd, err)
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
