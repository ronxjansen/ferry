package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/spf13/cobra"
)

var cleanYes bool

var cleanCmd = &cobra.Command{
	Use:   "clean [server]",
	Short: "Full teardown: proxy, containers, network, contexts, ~/.ferry, prune",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		cfg := p.Config

		var servers []*config.Server
		if len(args) > 0 {
			s, err := cfg.GetServer(args[0])
			if err != nil {
				return err
			}
			servers = []*config.Server{s}
		} else {
			for _, name := range cfg.ServerNames() {
				servers = append(servers, cfg.Servers[name])
			}
		}

		if !cleanYes {
			names := make([]string, len(servers))
			for i, s := range servers {
				names[i] = s.Name
			}
			fmt.Fprintf(os.Stderr, "This removes ALL ferry state on %s: kamal-proxy, containers, volumes, images, ~/.ferry.\nType yes to continue: ", strings.Join(names, ", "))
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimSpace(answer) != "yes" {
				return fmt.Errorf("aborted")
			}
		}

		d := newDeployer(p, "", time.Minute)
		for _, s := range servers {
			infof("Cleaning %s", s.Name)
			dk, err := d.Docker(s)
			if err != nil {
				return err
			}

			out, _ := dk.Run(dockercmd.PsFilter(cfg.Name, "", true, "{{.Names}}")...)
			for _, name := range strings.Fields(out) {
				dk.Run("rm", "-f", name)
			}

			dk.Run("rm", "-f", dockercmd.ProxyContainerName)
			dk.Run("volume", "rm", "ferry-kamal-proxy")
			dk.Run("network", "rm", cfg.Proxy.Network)
			dk.Run("system", "prune", "--all", "--force")
			dk.Run("volume", "prune", "--force")

			hostFor(s).Run("rm -rf $HOME/.ferry")

			// Drop the local Docker context last — nothing left to manage.
			ctx := cfg.ContextName(s)
			if _, err := exec.Run("docker", "context", "rm", "--force", ctx); err != nil {
				infof("Warning: failed to remove docker context %s: %v", ctx, err)
			}

			infof("%s cleaned", s.Name)
		}
		return nil
	},
}

func init() {
	cleanCmd.Flags().BoolVarP(&cleanYes, "yes", "y", false, "Skip the confirmation prompt")
	rootCmd.AddCommand(cleanCmd)
}
