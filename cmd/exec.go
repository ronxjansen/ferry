package cmd

import (
	ferry "github.com/ronxjansen/ferry/internal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "Execute a command in a docker container on your VPS",
	Long:  `Execute a command in a docker container on your VPS`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := buildConfig()
		manager := ferry.NewCommandManager(cfg)
		err := manager.Run(ferry.NewRunDockerCommandRole(args))
		if err != nil {
			logger.Fatal("Failed to remove", zap.Error(err))
		}
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
}
