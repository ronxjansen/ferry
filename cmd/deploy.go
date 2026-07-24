package cmd

import (
	"time"

	"github.com/ronxjansen/ferry/internal/lock"
	"github.com/spf13/cobra"
)

var (
	deployVersion   string
	deploySkipBuild bool
	deployTimeout   time.Duration
)

var deployCmd = &cobra.Command{
	Use:   "deploy [service...]",
	Short: "Deploy services (all, in dependency order, when none given)",
	Long: `Deploy services to their configured servers: build on the host through its
Docker context (or pull from a registry), start the git-SHA-versioned
container, and hand proxied services to kamal-proxy, which health-checks the
new version, swaps traffic atomically and drains the old one. Any failure
leaves the running version untouched.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		printWarnings(p)

		version, dirty, err := resolveVersion(p, deployVersion)
		if err != nil {
			return err
		}
		if dirty {
			infof("Warning: git tree is dirty, deploying as %s", version)
		}

		targets, err := p.Select(args)
		if err != nil {
			return err
		}

		d := newDeployer(p, version, deployTimeout)
		// A build context is always the working tree, never the named version's
		// tree — so building under --version would tag dirty files with someone
		// else's SHA. --version therefore only re-deploys an image that already
		// exists, unless the user explicitly asks to build (--skip-build=false).
		d.SkipBuild = deploySkipBuild
		if deployVersion != "" && !cmd.Flags().Changed("skip-build") {
			d.SkipBuild = true
		}

		started := time.Now()
		err = lock.With(d.Host(d.PrimaryServer()), p.Config.Name, performer(), version, "deploy", func() error {
			return d.Deploy(targets)
		})
		if err != nil {
			return err
		}
		infof("Deployed %s in %s", version, fmtDuration(time.Since(started)))
		return nil
	},
}

func init() {
	deployCmd.Flags().StringVar(&deployVersion, "version", "", "Redeploy a specific version (git SHA) already on the host; implies --skip-build")
	deployCmd.Flags().BoolVar(&deploySkipBuild, "skip-build", false, "Fail instead of building when the image is missing")
	deployCmd.Flags().DurationVar(&deployTimeout, "timeout", 5*time.Minute, "Health-gate and job timeout")
	rootCmd.AddCommand(deployCmd)
}
