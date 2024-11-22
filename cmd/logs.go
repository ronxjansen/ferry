package cmd

import (
	ferry "github.com/ronxjansen/ferry/internal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View logs from your apps",
	Long:  `View logs from your apps running on your VPS`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := buildConfig()
		manager := ferry.NewCommandManager(cfg)
		err := manager.Run(ferry.Logs)
		if err != nil {
			logger.Fatal("Failed to get logs", zap.Error(err))
		}
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
}
