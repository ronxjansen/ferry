package cmd

import (
	"fmt"
	"os"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/plan"
	"github.com/spf13/cobra"
)

// Version is the ferry CLI version.
const Version = "0.2.0"

var configFilePath string

var rootCmd = &cobra.Command{
	Use:           "ferry",
	Short:         "Deploy compose projects to your own servers",
	Long:          `Ferry is a tiny layer on top of Docker, Docker contexts and SSH. Your compose file stays the source of truth; ferry.yaml maps services to servers and adds zero-downtime deploys via kamal-proxy, preview environments, and environment variables.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() {
	rootCmd.PersistentFlags().StringVarP(&configFilePath, "config", "c", "./ferry.yaml", "Path to ferry.yaml")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// loadConfig loads ferry.yaml.
func loadConfig() (*config.Config, error) {
	return config.Load(configFilePath)
}

// loadPlan loads ferry.yaml and resolves it against the compose project.
func loadPlan() (*plan.Plan, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return plan.Load(cfg)
}

// infof prints a progress line to stderr, keeping stdout stable for scripting.
func infof(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
