package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/envfile"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage environment files on the servers",
}

var envPushCmd = &cobra.Command{
	Use:   "push [service...]",
	Short: "Ship merged env files to the servers (0600, only when changed)",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		targets, err := p.Select(args)
		if err != nil {
			return err
		}
		d := newDeployer(p, "", time.Minute)
		for _, t := range targets {
			for _, s := range t.Servers {
				env, err := d.MergedEnv(t, s)
				if err != nil {
					return err
				}
				if len(env) == 0 {
					continue
				}
				shipped, err := envfile.Ship(hostFor(s), t.Name, envfile.Render(env))
				if err != nil {
					return err
				}
				if shipped {
					fmt.Printf("%s@%s: updated\n", t.Name, s.Name)
				} else {
					fmt.Printf("%s@%s: unchanged\n", t.Name, s.Name)
				}
			}
		}
		return nil
	},
}

var envDiffCmd = &cobra.Command{
	Use:   "diff [service...]",
	Short: "Compare local env against what's on the servers (keys only, no values)",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		targets, err := p.Select(args)
		if err != nil {
			return err
		}
		d := newDeployer(p, "", time.Minute)
		for _, t := range targets {
			for _, s := range t.Servers {
				local, err := d.MergedEnv(t, s)
				if err != nil {
					return err
				}
				remoteRaw, _ := hostFor(s).Run(fmt.Sprintf("cat %q 2>/dev/null", envfile.RemotePath(t.Name)))
				remote := parseEnvContent(remoteRaw)

				var added, removed, changed []string
				for k, v := range local {
					rv, ok := remote[k]
					switch {
					case !ok:
						added = append(added, k)
					case rv != v:
						changed = append(changed, k)
					}
				}
				for k := range remote {
					if _, ok := local[k]; !ok {
						removed = append(removed, k)
					}
				}
				sort.Strings(added)
				sort.Strings(removed)
				sort.Strings(changed)

				if len(added)+len(removed)+len(changed) == 0 {
					fmt.Printf("%s@%s: in sync\n", t.Name, s.Name)
					continue
				}
				fmt.Printf("%s@%s:\n", t.Name, s.Name)
				for _, k := range added {
					fmt.Printf("  + %s\n", k)
				}
				for _, k := range changed {
					fmt.Printf("  ~ %s\n", k)
				}
				for _, k := range removed {
					fmt.Printf("  - %s\n", k)
				}
			}
		}
		return nil
	},
}

func parseEnvContent(content string) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			env[strings.TrimSpace(k)] = v
		}
	}
	return env
}

func init() {
	envCmd.AddCommand(envPushCmd)
	envCmd.AddCommand(envDiffCmd)
	rootCmd.AddCommand(envCmd)
}
