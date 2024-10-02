package cmd

import (
	ferry "github.com/ronxjansen/ferry/internal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove your application from your VPS",
	Long:  `Remove your application from your VPS`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := buildConfig()
		manager := ferry.NewCommandManager(cfg)
		err := manager.Run(ferry.RemoveCmnds)
		if err != nil {
			logger.Fatal("Failed to remove", zap.Error(err))
		}
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
