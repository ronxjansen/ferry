package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "Show what's running where: service, server, version, state, url",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		d := newDeployer(p, "", time.Minute)

		w := tabwriter.NewWriter(os.Stdout, 2, 8, 2, ' ', 0)
		fmt.Fprintln(w, "SERVICE\tSERVER\tVERSION\tSTATE\tURL")
		for _, t := range p.Targets {
			for _, s := range t.Servers {
				dk, err := d.Docker(s)
				if err != nil {
					return err
				}
				out, err := dk.Run("ps", "--all",
					"--filter", "label=ferry.project="+p.Config.Name,
					"--filter", "label=ferry.service="+t.Name,
					"--format", "{{.Label \"ferry.version\"}}\t{{.Status}}\t{{.Label \"ferry.preview\"}}")
				if err != nil {
					return fmt.Errorf("cannot reach %s: %w", s.Name, err)
				}
				var running, stopped [][]string
				for _, line := range strings.Split(out, "\n") {
					parts := strings.Split(line, "\t")
					if len(parts) < 3 || parts[0] == "" || parts[2] != "" {
						continue // skip malformed lines and previews
					}
					if strings.HasPrefix(parts[1], "Up") {
						running = append(running, parts)
					} else {
						stopped = append(stopped, parts)
					}
				}
				switch {
				case len(running) > 0:
					for _, parts := range running {
						url := "-"
						if t.Proxied() {
							url = "https://" + t.DomainsOn(s)[0]
						}
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.Name, s.Name, parts[0], parts[1], url)
					}
				case len(stopped) > 0:
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t-\n", t.Name, s.Name, stopped[0][0], stopped[0][1])
				default:
					fmt.Fprintf(w, "%s\t%s\t-\tnot deployed\t-\n", t.Name, s.Name)
				}
			}
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}
