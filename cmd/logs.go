package cmd

import (
	"fmt"
	"strings"

	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	logsFollow bool
	logsTail   int
)

var logsCmd = &cobra.Command{
	Use:   "logs <app>",
	Short: "View application logs",
	Long:  `View logs from a deployed application`,
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

		logger.Info("Fetching logs", zap.String("container", containerName))

		// Build logs command
		logsCommand := fmt.Sprintf("docker logs --tail=%d", logsTail)
		if logsFollow {
			logsCommand += " -f"
		}
		logsCommand += " " + containerName

		if logsFollow {
			return executor.RunInteractive(logsCommand)
		}

		output, err := executor.Run(logsCommand)
		if err != nil {
			return err
		}

		fmt.Print(output)
		return nil
	},
}

func findRunningContainer(executor *exec.SSHExecutor, appName string) (string, error) {
	// Try exact name
	output, err := executor.Run(fmt.Sprintf("docker ps --format '{{.Names}}' | grep -x '%s'", appName))
	containerName := strings.TrimSpace(output)
	if err == nil && containerName == appName {
		return containerName, nil
	}

	// Check staging container (deployment in progress)
	output, _ = executor.Run(fmt.Sprintf("docker ps --format '{{.Names}}' | grep -x '%s-new'", appName))
	containerName = strings.TrimSpace(output)
	if containerName == appName+"-new" {
		return containerName, nil
	}

	return "", fmt.Errorf("no running container found for %s", appName)
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVarP(&logsTail, "tail", "t", 100, "Number of lines to show from the end")
	rootCmd.AddCommand(logsCmd)
}
