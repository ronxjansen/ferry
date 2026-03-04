package cmd

import (
	"fmt"
	"strings"

	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var execCmd = &cobra.Command{
	Use:   "exec <server> <command>",
	Short: "Execute a command on a server",
	Long:  `Execute a command on a remote server via SSH`,
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		command := strings.Join(args[1:], " ")

		server, err := cfg.GetServer(serverName)
		if err != nil {
			return fmt.Errorf("server '%s' not found in config. Available servers: %v", serverName, cfg.ServerNames())
		}

		executor, err := exec.NewSSHExecutor(*server)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %w", serverName, err)
		}
		defer executor.Close()

		logger.Info("Executing command", zap.String("server", serverName), zap.String("command", command))

		output, err := executor.Run(command)
		if err != nil {
			return err
		}

		if output != "" {
			fmt.Print(output)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
}
