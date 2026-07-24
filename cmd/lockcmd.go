package cmd

import (
	"fmt"
	"strings"

	"github.com/ronxjansen/ferry/internal/audit"
	"github.com/ronxjansen/ferry/internal/lock"
	"github.com/spf13/cobra"
)

var lockMessage string

var lockCmd = &cobra.Command{
	Use:       "lock [status|acquire|release]",
	Short:     "Manage the deploy lock (deploy, rollback and preview take it automatically)",
	Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"status", "acquire", "release"},
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		host := hostFor(p.Targets[0].Servers[0])

		action := "status"
		if len(args) > 0 {
			action = args[0]
		}
		switch action {
		case "acquire":
			return lock.Acquire(host, p.Config.Name, performer(), "-", lockMessage)
		case "release":
			return lock.Release(host, p.Config.Name)
		default:
			details, err := lock.Status(host, p.Config.Name)
			if err != nil {
				fmt.Println("Unlocked")
				return nil
			}
			fmt.Println(details)
			return nil
		}
	},
}

var auditLines int

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Tail the server-side audit log of mutating commands",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		out, err := audit.Tail(hostFor(p.Targets[0].Servers[0]), auditLines)
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) == "" {
			infof("Audit log is empty")
			return nil
		}
		fmt.Println(out)
		return nil
	},
}

func init() {
	lockCmd.Flags().StringVarP(&lockMessage, "message", "m", "", "Why the lock is held")
	auditCmd.Flags().IntVarP(&auditLines, "lines", "n", 50, "Number of lines to show")
	rootCmd.AddCommand(lockCmd)
	rootCmd.AddCommand(auditCmd)
}
