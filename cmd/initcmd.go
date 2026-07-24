package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold ferry.yaml from an existing compose file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := os.Stat(configFilePath); err == nil {
			return fmt.Errorf("%s already exists", configFilePath)
		}

		dir, err := os.Getwd()
		if err != nil {
			return err
		}

		composeFile := ""
		for _, candidate := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"} {
			if _, err := os.Stat(filepath.Join(dir, candidate)); err == nil {
				composeFile = candidate
				break
			}
		}

		var services []string
		webish := ""
		if composeFile != "" {
			opts, err := cli.NewProjectOptions([]string{filepath.Join(dir, composeFile)},
				cli.WithWorkingDirectory(dir), cli.WithName("ferry-init"), cli.WithOsEnv, cli.WithDotEnv)
			if err == nil {
				if project, err := opts.LoadProject(context.Background()); err == nil {
					for name, svc := range project.Services {
						services = append(services, name)
						// The web-facing service gets the domain suggestion.
						if webish == "" && (len(svc.Expose) > 0 || len(svc.Ports) > 0 || svc.Build != nil) {
							webish = name
						}
					}
					sort.Strings(services)
				}
			}
			if len(services) == 0 {
				infof("Warning: could not parse %s; writing a generic skeleton", composeFile)
			}
		} else {
			infof("Warning: no compose file found; ferry needs one (compose is the source of truth)")
		}
		if webish == "" && len(services) > 0 {
			webish = services[0]
		}

		var b strings.Builder
		b.WriteString("# ferry.yaml — everything else comes from " + orDefault(composeFile, "your compose file") + "\n")
		b.WriteString("# Docs: https://github.com/ronxjansen/ferry\n\nservers:\n  vps-1: 203.0.113.10 # name: ip — SSH auth comes from your system SSH config\n\nservices:\n")
		if len(services) == 0 {
			b.WriteString("  web:\n    servers: [vps-1]\n    domain: example.com # remove for an sslip.io fallback domain, set port instead\n    port: 3000\n")
		}
		for _, svc := range services {
			fmt.Fprintf(&b, "  %s:\n    servers: [vps-1]\n", svc)
			if svc == webish {
				b.WriteString("    domain: example.com # only proxied services need a domain (or port for sslip.io)\n")
			}
		}
		b.WriteString("\n# env:\n#   file: .env.production\n\n# preview:\n#   domain: preview.example.com\n#   isolate: [db]\n")

		if err := os.WriteFile(configFilePath, []byte(b.String()), 0o644); err != nil {
			return err
		}
		fmt.Println("Wrote", configFilePath)

		hooksDir := filepath.Join(dir, ".ferry", "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			return err
		}
		sample := "#!/bin/sh\n# Rename without .sample and chmod +x to activate.\n# Env: FERRY_VERSION FERRY_SERVICE FERRY_SERVER FERRY_IMAGE FERRY_PERFORMER FERRY_RUNTIME FERRY_PREVIEW\nexit 0\n"
		for _, hook := range []string{"pre_build", "pre_deploy", "post_deploy", "post_app_boot"} {
			path := filepath.Join(hooksDir, hook+".sample")
			if _, err := os.Stat(path); os.IsNotExist(err) {
				if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
					return err
				}
			}
		}
		fmt.Println("Wrote .ferry/hooks/ samples")

		gitignore := filepath.Join(dir, ".gitignore")
		existing, _ := os.ReadFile(gitignore)
		if !strings.Contains(string(existing), ".env") {
			f, err := os.OpenFile(gitignore, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := f.WriteString("\n# ferry: env files hold secrets\n.env\n.env.*\n"); err != nil {
				return err
			}
			fmt.Println("Added .env entries to .gitignore")
		}
		return nil
	},
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func init() {
	rootCmd.AddCommand(initCmd)
}
