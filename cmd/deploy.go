package cmd

import (
	ferry "github.com/ronxjansen/ferry/internal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy your application to your VPS",
	Long:  `Deploy your application to your VPS`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Run: func(cmd *cobra.Command, args []string) {
		// Load config from file
		cfg := buildConfig()
		manager := ferry.NewCommandManager(cfg)
		err := manager.Run(ferry.Deploy)
		if err != nil {
			logger.Fatal("Failed to deploy", zap.Error(err))
		}
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
