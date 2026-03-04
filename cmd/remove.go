package cmd

import (
	"fmt"

	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var removeCmd = &cobra.Command{
	Use:   "remove <app>",
	Short: "Remove an application",
	Long:  `Stop and remove an application from its server`,
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

		logger.Info("Removing app", zap.String("app", appName), zap.String("server", server.Name))

		executor, err := exec.NewSSHExecutor(*server)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
		}
		defer executor.Close()

		// Stop and remove containers
		executor.Run(fmt.Sprintf("docker stop %s 2>/dev/null", app.Name))
		executor.Run(fmt.Sprintf("docker rm %s 2>/dev/null", app.Name))
		executor.Run(fmt.Sprintf("docker stop %s-new 2>/dev/null", app.Name))
		executor.Run(fmt.Sprintf("docker rm %s-new 2>/dev/null", app.Name))

		// Remove app directory
		appDir := fmt.Sprintf("$HOME/%s", app.Name)
		executor.Run(fmt.Sprintf("rm -rf %s", appDir))

		// Cleanup
		executor.Run("docker container prune -f")
		executor.Run("docker image prune -f")

		logger.Info("App removed", zap.String("app", appName))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
