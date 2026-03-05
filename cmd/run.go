package cmd

import (
	"fmt"
	"strings"

	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	runServer string
)

var runCmd = &cobra.Command{
	Use:   "run <command-name>",
	Short: "Run a custom command defined in ferry.yaml",
	Long: `Run a custom command defined in the commands section of ferry.yaml.

Commands can be interactive (like console) or non-interactive (like migrate).
Use --server to specify which server to run the command on.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		commandName := args[0]

		command, err := cfg.GetCommand(commandName)
		if err != nil {
			return fmt.Errorf("command '%s' not found. Available commands: %v", commandName, cfg.CommandNames())
		}

		// Determine server
		var serverName string
		if runServer != "" {
			serverName = runServer
		} else if len(cfg.Servers) == 1 {
			serverName = cfg.Servers[0].Name
		} else {
			return fmt.Errorf("multiple servers configured, please specify --server")
		}

		server, err := cfg.GetServer(serverName)
		if err != nil {
			return fmt.Errorf("server '%s' not found. Available servers: %v", serverName, cfg.ServerNames())
		}

		logger.Info("Running command",
			zap.String("command", commandName),
			zap.String("server", server.Name))

		executor, err := exec.NewSSHExecutor(*server)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
		}
		defer executor.Close()

		// Check if command is interactive (contains -it or -i)
		isInteractive := strings.Contains(command.Command, " -it ") ||
			strings.Contains(command.Command, " -i ") ||
			strings.HasSuffix(command.Command, " -it") ||
			strings.HasSuffix(command.Command, " -i")

		if isInteractive {
			return executor.RunInteractive(command.Command)
		}

		output, err := executor.Run(command.Command)
		if err != nil {
			return err
		}

		if output != "" {
			fmt.Println(output)
		}

		return nil
	},
}

func init() {
	runCmd.Flags().StringVarP(&runServer, "server", "s", "", "Server to run command on")
	rootCmd.AddCommand(runCmd)
}
