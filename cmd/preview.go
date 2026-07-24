package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ronxjansen/ferry/internal/git"
	"github.com/ronxjansen/ferry/internal/lock"
	"github.com/spf13/cobra"
)

var (
	previewTimeout   time.Duration
	previewRemoveAll bool
)

var previewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Deploy a preview environment for the current git commit",
	Long: `Deploy a preview environment for HEAD: SHA-tagged build,
<sha>.<preview.domain> routing with automatic TLS via kamal-proxy, and
isolated clones of preview.isolate services with fresh volumes. Requires a
clean git tree; re-running on the same commit replaces the preview.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		sha, dirty, err := git.Version(p.Config.Dir)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("git tree is dirty; previews are keyed by commit — commit or stash first")
		}

		d := newDeployer(p, sha, previewTimeout)
		return lock.With(d.Host(d.PrimaryServer()), performer(), sha, "preview", func() error {
			return d.DeployPreview()
		})
	},
}

var previewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List running previews",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		d := newDeployer(p, "", previewTimeout)
		if err := d.ReapExpired(); err != nil {
			infof("Warning: preview reap failed: %v", err)
		}
		previews, err := d.ListPreviews()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 8, 2, ' ', 0)
		fmt.Fprintln(w, "SHA\tURL\tCREATED\tSERVER\tSERVICES")
		for _, pr := range previews {
			created := "-"
			if !pr.Created.IsZero() {
				created = pr.Created.Local().Format("2006-01-02 15:04")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", pr.SHA, pr.URL, created, pr.Server, strings.Join(pr.Services, ","))
		}
		return w.Flush()
	},
}

var previewRemoveCmd = &cobra.Command{
	Use:   "remove <sha...>",
	Short: "Tear down preview environments",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && !previewRemoveAll {
			return fmt.Errorf("give one or more preview SHAs, or --all")
		}
		p, err := loadPlan()
		if err != nil {
			return err
		}
		d := newDeployer(p, "", previewTimeout)
		shas := args
		if previewRemoveAll {
			previews, err := d.ListPreviews()
			if err != nil {
				return err
			}
			shas = nil
			for _, pr := range previews {
				shas = append(shas, pr.SHA)
			}
		}
		for _, sha := range shas {
			if err := d.RemovePreview(sha); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	previewCmd.Flags().DurationVar(&previewTimeout, "timeout", 5*time.Minute, "Health-gate and job timeout")
	previewRemoveCmd.Flags().BoolVar(&previewRemoveAll, "all", false, "Remove all previews")
	previewCmd.AddCommand(previewListCmd)
	previewCmd.AddCommand(previewRemoveCmd)
	rootCmd.AddCommand(previewCmd)
}
