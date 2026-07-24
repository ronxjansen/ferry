package cmd

import (
	"fmt"
	"time"

	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/spf13/cobra"
)

var maintenanceMessage string

var maintenanceCmd = &cobra.Command{
	Use:   "maintenance <service>",
	Short: "Put a proxied service into maintenance mode (503 + optional message)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return proxyToggle(args[0], func(service string) []string {
			return dockercmd.ProxyStop(service, maintenanceMessage)
		}, "maintenance")
	},
}

var liveCmd = &cobra.Command{
	Use:   "live <service>",
	Short: "Take a proxied service out of maintenance mode",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return proxyToggle(args[0], dockercmd.ProxyResume, "live")
	},
}

func proxyToggle(service string, build func(string) []string, action string) error {
	p, err := loadPlan()
	if err != nil {
		return err
	}
	t, err := p.Target(service)
	if err != nil {
		return err
	}
	if !t.Proxied() {
		return fmt.Errorf("service %s is not proxied", t.Name)
	}
	d := newDeployer(p, "", time.Minute)
	for _, s := range t.Servers {
		dk, err := d.Docker(s)
		if err != nil {
			return err
		}
		if _, err := dk.Run(build(t.Name)...); err != nil {
			return err
		}
		infof("%s@%s: %s", t.Name, s.Name, action)
	}
	return nil
}

func init() {
	maintenanceCmd.Flags().StringVar(&maintenanceMessage, "message", "", "Message shown on the maintenance page")
	rootCmd.AddCommand(maintenanceCmd)
	rootCmd.AddCommand(liveCmd)
}
