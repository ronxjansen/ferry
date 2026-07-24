package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/dockercmd"
	"github.com/ronxjansen/ferry/internal/envfile"
	"github.com/spf13/cobra"
)

var removeImages bool

var removeCmd = &cobra.Command{
	Use:   "remove <service>",
	Short: "Deregister a service from the proxy and remove its containers",
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

		for _, s := range t.Servers {
			dk, err := d.Docker(s)
			if err != nil {
				return err
			}
			if t.Proxied() {
				dk.Run(dockercmd.ProxyRemove(t.ProxyService())...)
			}
			out, _ := dk.Run("ps", "--all",
				"--filter", "label="+dockercmd.LabelProject+"="+p.Config.Name,
				"--filter", "label="+dockercmd.LabelService+"="+t.Name,
				"--format", "{{.Names}}\t{{.Image}}")
			for _, line := range strings.Split(out, "\n") {
				parts := strings.Split(line, "\t")
				if len(parts) < 2 || parts[0] == "" {
					continue
				}
				infof("%s@%s: removing %s", t.Name, s.Name, parts[0])
				dk.Run("rm", "-f", parts[0])
				if removeImages {
					dk.Run("rmi", parts[1])
				}
			}
			hostFor(s).Run(fmt.Sprintf("rm -rf %q", ".ferry/apps/"+t.Name))
		}
		infof("%s removed (env: %s deleted on hosts)", t.Name, envfile.RemotePath(t.Name))
		return nil
	},
}

func init() {
	removeCmd.Flags().BoolVar(&removeImages, "images", false, "Also remove the service's images")
	rootCmd.AddCommand(removeCmd)
}
