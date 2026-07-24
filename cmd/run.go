package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <command>",
	Short: "Run a user-defined command from ferry.yaml (in a container or on a host)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		command, err := p.Config.GetCommand(args[0])
		if err != nil {
			return err
		}

		if command.Server != "" {
			s, err := p.Config.GetServer(command.Server)
			if err != nil {
				return err
			}
			host := hostFor(s)
			if command.Interactive {
				return host.Interactive(command.Command)
			}
			return host.Stream(command.Command)
		}

		t, err := p.Target(command.Service)
		if err != nil {
			return err
		}
		d := newDeployer(p, "", time.Minute)
		dk, container, err := findContainer(d, t.Name, t.Servers[0])
		if err != nil {
			return err
		}
		if command.Interactive {
			return dk.Interactive("exec", "-it", container, "sh", "-c", command.Command)
		}
		return dk.Stream("exec", container, "sh", "-c", command.Command)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
