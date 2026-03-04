package cmd

import (
	"fmt"

	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var attachCmd = &cobra.Command{
	Use:   "attach <app>",
	Short: "Attach to a running container",
	Long:  `Open an interactive shell in a running container`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]

		app, err := cfg.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app '%s' not found. Available apps: %v", appName, cfg.AppNames())
		}

		server, err := app.GetServer(cfg)
		if err != nil {
			return err
		}

		executor, err := exec.NewSSHExecutor(*server)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
		}
		defer executor.Close()

		// Find the running container
		containerName, err := findRunningContainer(executor, app.Name)
		if err != nil {
			return err
		}

		logger.Info("Attaching to container", zap.String("container", containerName))

		return executor.RunInteractive(fmt.Sprintf("docker exec -it %s /bin/sh", containerName))
	},
}

func init() {
	rootCmd.AddCommand(attachCmd)
}
