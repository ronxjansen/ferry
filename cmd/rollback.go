package cmd

import (
	"time"

	"github.com/ronxjansen/ferry/internal/lock"
	"github.com/spf13/cobra"
)

var (
	rollbackVersion string
	rollbackTimeout time.Duration
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback [service]",
	Short: "Restart the previous retained container and re-register it with kamal-proxy",
	Long: `Roll back to a retained stopped container: docker start, health-gated
re-registration with kamal-proxy, stop the current version. No rebuild, no
transfer. Without --version the most recently retained container is used.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		targets, err := p.Select(args)
		if err != nil {
			return err
		}

		d := newDeployer(p, rollbackVersion, rollbackTimeout)
		return lock.With(d.Host(d.PrimaryServer()), p.Config.Name, performer(), rollbackVersion, "rollback", func() error {
			return d.Rollback(targets, rollbackVersion)
		})
	},
}

func init() {
	rollbackCmd.Flags().StringVar(&rollbackVersion, "version", "", "Roll back to a specific retained version (git SHA)")
	rollbackCmd.Flags().DurationVar(&rollbackTimeout, "timeout", 5*time.Minute, "Health-gate timeout")
	rootCmd.AddCommand(rollbackCmd)
}
