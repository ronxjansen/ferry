package cmd

import (
	ferry "github.com/ronxjansen/ferry/internal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install dependencies on your VPS",
	Long:  `Install Traefik, Docker, SOPS and other dependencies on your VPS`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := buildConfig()
		manager := ferry.NewCommandManager(cfg)
		err := manager.Run(ferry.Setup)
		if err != nil {
			logger.Fatal("Failed to setup", zap.Error(err))
		}
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
