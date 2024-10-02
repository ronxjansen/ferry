package cmd

import (
	ferry "github.com/ronxjansen/ferry/internal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all Ferry related resources",
	Long:  `Remove all Ferry related resources`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := buildConfig()
		manager := ferry.NewCommandManager(cfg)
		err := manager.Run(ferry.CleanCmnds)
		if err != nil {
			logger.Fatal("Failed to clean", zap.Error(err))
		}
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
}
