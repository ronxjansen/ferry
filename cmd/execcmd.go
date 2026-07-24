package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var (
	execInteractive bool
	execServer      string
)

var execCmd = &cobra.Command{
	Use:   "exec <service> [-- cmd...]",
	Short: "Run a command in a service's container",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		command := args[1:]
		if len(command) == 0 {
			return fmt.Errorf("no command given (use `ferry shell %s` for an interactive shell)", args[0])
		}

		p, err := loadPlan()
		if err != nil {
			return err
		}
		t, err := p.Target(args[0])
		if err != nil {
			return err
		}
		server := t.Servers[0]
		if execServer != "" {
			if server, err = p.Config.GetServer(execServer); err != nil {
				return err
			}
		}

		d := newDeployer(p, "", time.Minute)
		dk, container, err := findContainer(d, t.Name, server)
		if err != nil {
			return err
		}

		dockerArgs := []string{"exec"}
		if execInteractive {
			dockerArgs = append(dockerArgs, "-it")
		}
		dockerArgs = append(dockerArgs, container)
		dockerArgs = append(dockerArgs, command...)
		if execInteractive {
			return dk.Interactive(dockerArgs...)
		}
		return dk.Stream(dockerArgs...)
	},
}

var shellCmd = &cobra.Command{
	Use:   "shell <service>",
	Short: "Open an interactive shell in a service's container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		t, err := p.Target(args[0])
		if err != nil {
			return err
		}
		d := newDeployer(p, "", time.Minute)
		dk, container, err := findContainer(d, t.Name, t.Servers[0])
		if err != nil {
			return err
		}
		return dk.Interactive("exec", "-it", container, "/bin/sh")
	},
}

var sshCmd = &cobra.Command{
	Use:   "ssh <server> [-- cmd...]",
	Short: "Interactive shell on a host, or a one-off host command",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		s, err := cfg.GetServer(args[0])
		if err != nil {
			return err
		}
		host := hostFor(s)
		command := shellJoin(args[1:])
		return host.Interactive(command)
	},
}

func init() {
	execCmd.Flags().BoolVarP(&execInteractive, "interactive", "i", false, "Interactive (allocate a TTY)")
	execCmd.Flags().StringVar(&execServer, "server", "", "Server to run on (default: the service's first server)")
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(sshCmd)
}
