package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/ronxjansen/ferry/internal/envfile"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configJSON bool

// resolvedService is the merged view of one service (compose + overlay).
type resolvedService struct {
	Servers     []string `json:"servers" yaml:"servers"`
	Image       string   `json:"image" yaml:"image"`
	Proxied     bool     `json:"proxied" yaml:"proxied"`
	Domains     []string `json:"domains,omitempty" yaml:"domains,omitempty"`
	Port        int      `json:"port,omitempty" yaml:"port,omitempty"`
	Job         bool     `json:"job,omitempty" yaml:"job,omitempty"`
	BuildMethod string   `json:"build_method" yaml:"build_method"`
	EnvKeys     []string `json:"env_keys,omitempty" yaml:"env_keys,omitempty"`
}

type resolvedConfig struct {
	Name     string                     `json:"name" yaml:"name"`
	Compose  []string                   `json:"compose" yaml:"compose"`
	Servers  map[string]string          `json:"servers" yaml:"servers"`
	Proxy    map[string]any             `json:"proxy" yaml:"proxy"`
	Build    map[string]any             `json:"build" yaml:"build"`
	Services map[string]resolvedService `json:"services" yaml:"services"`
	Order    []string                   `json:"deploy_order" yaml:"deploy_order"`
	Warnings []string                   `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Print the fully resolved config (compose + overlay merged, env values redacted)",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := loadPlan()
		if err != nil {
			return err
		}
		cfg := p.Config

		out := resolvedConfig{
			Name:    cfg.Name,
			Compose: cfg.Compose,
			Servers: map[string]string{},
			Proxy: map[string]any{
				"image": cfg.Proxy.Image, "network": cfg.Proxy.Network,
				"http_port": cfg.Proxy.HTTPPort, "https_port": cfg.Proxy.HTTPSPort,
			},
			Build:    map[string]any{"retain": cfg.Build.Retain},
			Services: map[string]resolvedService{},
			Warnings: p.Warnings(),
		}
		for name, s := range cfg.Servers {
			out.Servers[name] = fmt.Sprintf("ssh://%s:%d", s.Address(), s.Port)
		}
		for _, t := range p.Targets {
			out.Order = append(out.Order, t.Name)
			rs := resolvedService{
				Servers:     t.Overlay.Servers,
				Image:       t.ImageRef("<git-sha>"),
				Proxied:     t.Proxied(),
				Job:         t.Overlay.Job,
				BuildMethod: t.BuildMethod(),
			}
			if t.Proxied() {
				rs.Domains = t.DomainsOn(t.Servers[0])
				rs.Port = t.Port()
			}
			// Env values are secrets: show only the keys.
			env, err := envfile.Load(cfg.EnvFilesFor(t.Overlay), cfg.Env.Encryption == "sops-age")
			if err == nil {
				for k := range t.Compose.Environment {
					env[k] = ""
				}
				for k := range env {
					rs.EnvKeys = append(rs.EnvKeys, k)
				}
				sort.Strings(rs.EnvKeys)
			}
			out.Services[t.Name] = rs
		}

		if configJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}
		return yaml.NewEncoder(os.Stdout).Encode(out)
	},
}

func init() {
	configCmd.Flags().BoolVar(&configJSON, "json", false, "Output JSON for tooling")
	rootCmd.AddCommand(configCmd)
}
