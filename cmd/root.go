package cmd

import (
	"os"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/spf13/cobra"
	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
)

var (
	configFilePath string
	cfg            *config.Config
	logger         = prettyconsole.NewLogger(zap.DebugLevel)
)

var rootCmd = &cobra.Command{
	Use:   "ferry",
	Short: "Deploy apps to your VPS without vendor lock-in",
	Long:  `Ferry deploys containerized applications to VPS servers with automatic TLS via Traefik.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for commands that don't need it
		if cmd.Name() == "help" || cmd.Name() == "version" {
			return nil
		}

		var err error
		cfg, err = config.Load(configFilePath)
		if err != nil {
			return err
		}
		return nil
	},
}

func Execute() {
	rootCmd.PersistentFlags().StringVarP(&configFilePath, "config", "c", "./ferry.yaml", "Path to ferry.yaml config file")
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
