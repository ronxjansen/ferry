package cmd

import (
	"github.com/spf13/cobra"

	ferry "github.com/ronxjansen/ferry/internal"
)

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach to a running container",
	Long:  `Attach to a running container in interactive shell mode`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildConfig()
		manager := ferry.NewCommandManager(cfg)
		return manager.Run(ferry.Attach)
	},
}

func init() {
	rootCmd.AddCommand(attachCmd)
}
