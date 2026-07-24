package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ferry.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimal = `
servers:
  vps-1: 165.232.100.10
  vps-2:
    host: 165.232.100.11
    user: deploy
    port: 2222

services:
  web:
    servers: [vps-1]
    domain: example.com
  worker:
    servers: [vps-2]
`

func TestLoadMinimal(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimal))
	if err != nil {
		t.Fatal(err)
	}

	short := cfg.Servers["vps-1"]
	if short.Host != "165.232.100.10" || short.User != "root" || short.Port != 22 {
		t.Errorf("short-form server defaults wrong: %+v", short)
	}
	long := cfg.Servers["vps-2"]
	if long.Host != "165.232.100.11" || long.User != "deploy" || long.Port != 2222 {
		t.Errorf("long-form server wrong: %+v", long)
	}
	if long.Address() != "deploy@165.232.100.11" {
		t.Errorf("Address() = %q", long.Address())
	}

	if cfg.Proxy.Network != "ferry" || cfg.Proxy.HTTPPort != 80 || cfg.Proxy.HTTPSPort != 443 {
		t.Errorf("proxy defaults wrong: %+v", cfg.Proxy)
	}
	if !strings.HasPrefix(cfg.Proxy.Image, "basecamp/kamal-proxy:") {
		t.Errorf("proxy image not pinned: %q", cfg.Proxy.Image)
	}
	if cfg.Build.Retain != 5 {
		t.Errorf("retain default = %d, want 5", cfg.Build.Retain)
	}
	if cfg.Services["web"].AllDomains()[0] != "example.com" {
		t.Errorf("web domain missing")
	}
	if got := cfg.ContextName(short); got != "ferry-"+cfg.Name+"-vps-1" {
		t.Errorf("context name = %q", got)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"unknown server ref", `
servers: {vps-1: 1.2.3.4}
services:
  web: {servers: [vps-9]}
`, "unknown server"},
		{"missing servers key", `
servers: {vps-1: 1.2.3.4}
services:
  web: {domain: example.com}
`, "servers is required"},
		{"bad build method", `
servers: {vps-1: 1.2.3.4}
build: {method: streaming}
services:
  web: {servers: [vps-1]}
`, "build.method"},
		{"bad ttl", `
servers: {vps-1: 1.2.3.4}
services:
  web: {servers: [vps-1]}
preview: {ttl: fortnight}
`, "preview.ttl"},
		{"command without target", `
servers: {vps-1: 1.2.3.4}
services:
  web: {servers: [vps-1]}
commands:
  broken: {command: echo hi}
`, "needs either service or server"},
		{"unknown top-level key (traefik is gone)", `
servers: {vps-1: 1.2.3.4}
proxy: {type: traefik}
services:
  web: {servers: [vps-1]}
`, "field type not found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestCustomCommandsAndHooks(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
servers: {vps-1: 1.2.3.4}
services:
  web: {servers: [vps-1], domain: example.com}
  db: {servers: [vps-1]}
hooks:
  pre_build: [./scripts/check.sh, echo hi]
commands:
  psql:
    service: db
    command: psql -U postgres
    interactive: true
  disk:
    server: vps-1
    command: df -h
`))
	if err != nil {
		t.Fatal(err)
	}
	psql, err := cfg.GetCommand("psql")
	if err != nil || !psql.Interactive || psql.Service != "db" {
		t.Errorf("psql command wrong: %+v err=%v", psql, err)
	}
	if len(cfg.Hooks[HookPreBuild]) != 2 {
		t.Errorf("hooks not parsed: %+v", cfg.Hooks)
	}
}
