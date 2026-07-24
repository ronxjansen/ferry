package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// HookType represents lifecycle hook types
type HookType string

const (
	HookPreBuild    HookType = "pre_build"     // Local, before building
	HookPreDeploy   HookType = "pre_deploy"    // Remote, after image exists on host, before cutover
	HookPostDeploy  HookType = "post_deploy"   // Remote, after cutover
	HookPostAppBoot HookType = "post_app_boot" // Remote, after the proxy reports the target live
)

// HookTypes lists all lifecycle hooks in execution order.
var HookTypes = []HookType{HookPreBuild, HookPreDeploy, HookPostDeploy, HookPostAppBoot}

// Hook is a list of entries; each entry is either a path to a script file or
// an inline shell command.
type Hook []string

// Server is one target machine. Each server becomes a Docker context
// named ferry-<name>. SSH auth comes from the system SSH config.
type Server struct {
	Name string `yaml:"-"`
	Host string `yaml:"host"`
	User string `yaml:"user"`
	Port int    `yaml:"port"`
}

// UnmarshalYAML accepts both the short form (`vps-1: 1.2.3.4`) and the long
// form (host/user/port keys).
func (s *Server) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		s.Host = node.Value
		return nil
	}
	type plain Server
	return node.Decode((*plain)(s))
}

// Address returns user@host for SSH.
func (s *Server) Address() string {
	return fmt.Sprintf("%s@%s", s.User, s.Host)
}

// Proxy configures kamal-proxy (one container per proxied server).
type Proxy struct {
	Image     string `yaml:"image"`
	Network   string `yaml:"network"`
	HTTPPort  int    `yaml:"http_port"`
	HTTPSPort int    `yaml:"https_port"`
}

// Build configures how images get onto servers.
type Build struct {
	Method string `yaml:"method"` // "remote" | "pull" | "" (auto per service)
	Retain int    `yaml:"retain"` // stopped old-version containers kept per service
}

// Registry configures a registry for build method "pull" with private images.
type Registry struct {
	Server   string `yaml:"server"`
	Username string `yaml:"username"`
	Password string `yaml:"password"` // name of a variable in the env file, never a literal
}

// Env configures environment delivery.
type Env struct {
	File       string `yaml:"file"`
	Encryption string `yaml:"encryption"` // "none" | "sops-age"
}

// Health maps to kamal-proxy --health-check-* flags.
type Health struct {
	Path     string `yaml:"path"`
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
}

// Service is the ferry.yaml overlay on a compose service. The key must exist
// in the compose project.
type Service struct {
	Name    string            `yaml:"-"`
	Servers []string          `yaml:"servers"`
	Domain  string            `yaml:"domain"`
	Domains []string          `yaml:"domains"`
	Port    int               `yaml:"port"`
	Health  Health            `yaml:"health"`
	Proxy   map[string]string `yaml:"proxy"` // passthrough kamal-proxy deploy flags
	EnvFile string            `yaml:"env_file"`
	Build   string            `yaml:"build"` // per-service override of build.method
	Job     bool              `yaml:"job"`
}

// AllDomains returns domain + domains merged.
func (s *Service) AllDomains() []string {
	var out []string
	if s.Domain != "" {
		out = append(out, s.Domain)
	}
	out = append(out, s.Domains...)
	return out
}

// Preview configures preview deployments.
type Preview struct {
	Domain   string   `yaml:"domain"`
	Server   string   `yaml:"server"`
	Services []string `yaml:"services"`
	Isolate  []string `yaml:"isolate"`
	EnvFile  string   `yaml:"env_file"`
	TTL      string   `yaml:"ttl"`
}

// TTLDuration parses the TTL; zero means "never".
func (p *Preview) TTLDuration() (time.Duration, error) {
	if p.TTL == "" {
		return 0, nil
	}
	return time.ParseDuration(p.TTL)
}

// Command is a user-defined command for `ferry run`.
type Command struct {
	Name        string `yaml:"-"`
	Description string `yaml:"description"`
	Service     string `yaml:"service"` // runs inside this service's container...
	Server      string `yaml:"server"`  // ...or on a host
	Command     string `yaml:"command"`
	Interactive bool   `yaml:"interactive"`
}

// Config is the parsed ferry.yaml.
type Config struct {
	Name     string              `yaml:"name"`
	Compose  []string            `yaml:"compose"`
	Servers  map[string]*Server  `yaml:"servers"`
	Proxy    Proxy               `yaml:"proxy"`
	Build    Build               `yaml:"build"`
	Registry *Registry           `yaml:"registry"`
	Env      Env                 `yaml:"env"`
	Services map[string]*Service `yaml:"services"`
	Preview  Preview             `yaml:"preview"`
	Hooks    map[HookType]Hook   `yaml:"hooks"`
	Commands map[string]*Command `yaml:"commands"`

	// Dir is the directory containing ferry.yaml; relative paths resolve
	// against it.
	Dir string `yaml:"-"`
}

var nameRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// Load reads and parses a ferry.yaml config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{Dir: filepath.Dir(abs)}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) setDefaults() {
	if c.Name == "" {
		c.Name = nameRe.ReplaceAllString(strings.ToLower(filepath.Base(c.Dir)), "-")
		c.Name = strings.Trim(c.Name, "-")
	}
	if len(c.Compose) == 0 {
		for _, candidate := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"} {
			if _, err := os.Stat(filepath.Join(c.Dir, candidate)); err == nil {
				c.Compose = []string{candidate}
				break
			}
		}
	}
	for name, s := range c.Servers {
		s.Name = name
		if s.User == "" {
			s.User = "root"
		}
		if s.Port == 0 {
			s.Port = 22
		}
	}
	if c.Proxy.Image == "" {
		c.Proxy.Image = "basecamp/kamal-proxy:v0.9.0"
	}
	if c.Proxy.Network == "" {
		c.Proxy.Network = "ferry"
	}
	if c.Proxy.HTTPPort == 0 {
		c.Proxy.HTTPPort = 80
	}
	if c.Proxy.HTTPSPort == 0 {
		c.Proxy.HTTPSPort = 443
	}
	if c.Build.Retain == 0 {
		c.Build.Retain = 5
	}
	if c.Env.Encryption == "" {
		c.Env.Encryption = "none"
	}
	for name, s := range c.Services {
		s.Name = name
	}
	for name, cmd := range c.Commands {
		cmd.Name = name
	}
}

func (c *Config) validate() error {
	if len(c.Servers) == 0 {
		return fmt.Errorf("no servers defined")
	}
	if len(c.Services) == 0 {
		return fmt.Errorf("no services defined")
	}
	for name, s := range c.Servers {
		if s.Host == "" {
			return fmt.Errorf("server %q has no host", name)
		}
	}
	for name, svc := range c.Services {
		if len(svc.Servers) == 0 {
			return fmt.Errorf("service %q: servers is required", name)
		}
		for _, srv := range svc.Servers {
			if _, ok := c.Servers[srv]; !ok {
				return fmt.Errorf("service %q references unknown server %q", name, srv)
			}
		}
		if svc.Build != "" && svc.Build != "remote" && svc.Build != "pull" {
			return fmt.Errorf("service %q: build must be \"remote\" or \"pull\"", name)
		}
	}
	if m := c.Build.Method; m != "" && m != "remote" && m != "pull" {
		return fmt.Errorf("build.method must be \"remote\" or \"pull\"")
	}
	if e := c.Env.Encryption; e != "none" && e != "sops-age" {
		return fmt.Errorf("env.encryption must be \"none\" or \"sops-age\"")
	}
	if c.Preview.Server != "" {
		if _, ok := c.Servers[c.Preview.Server]; !ok {
			return fmt.Errorf("preview.server references unknown server %q", c.Preview.Server)
		}
	}
	for _, s := range c.Preview.Services {
		if _, ok := c.Services[s]; !ok {
			return fmt.Errorf("preview.services references unknown service %q", s)
		}
	}
	for _, s := range c.Preview.Isolate {
		if _, ok := c.Services[s]; !ok {
			return fmt.Errorf("preview.isolate references unknown service %q", s)
		}
	}
	if _, err := c.Preview.TTLDuration(); err != nil {
		return fmt.Errorf("preview.ttl: %w", err)
	}
	for name, cmd := range c.Commands {
		if cmd.Command == "" {
			return fmt.Errorf("command %q has no command", name)
		}
		if cmd.Service == "" && cmd.Server == "" {
			return fmt.Errorf("command %q needs either service or server", name)
		}
		if cmd.Service != "" {
			if _, ok := c.Services[cmd.Service]; !ok {
				return fmt.Errorf("command %q references unknown service %q", name, cmd.Service)
			}
		}
		if cmd.Server != "" {
			if _, ok := c.Servers[cmd.Server]; !ok {
				return fmt.Errorf("command %q references unknown server %q", name, cmd.Server)
			}
		}
	}
	for ht := range c.Hooks {
		valid := false
		for _, known := range HookTypes {
			if ht == known {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("unknown hook %q", ht)
		}
	}
	return nil
}

// GetServer returns a server by name.
func (c *Config) GetServer(name string) (*Server, error) {
	if s, ok := c.Servers[name]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("server %q not found (available: %s)", name, strings.Join(c.ServerNames(), ", "))
}

// GetService returns a service overlay by name.
func (c *Config) GetService(name string) (*Service, error) {
	if s, ok := c.Services[name]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("service %q not found (available: %s)", name, strings.Join(c.ServiceNames(), ", "))
}

// GetCommand returns a custom command by name.
func (c *Config) GetCommand(name string) (*Command, error) {
	if cmd, ok := c.Commands[name]; ok {
		return cmd, nil
	}
	return nil, fmt.Errorf("command %q not found (available: %s)", name, strings.Join(c.CommandNames(), ", "))
}

// ServerNames returns sorted server names.
func (c *Config) ServerNames() []string {
	return sortedKeys(c.Servers)
}

// ServiceNames returns sorted service names.
func (c *Config) ServiceNames() []string {
	return sortedKeys(c.Services)
}

// CommandNames returns sorted command names.
func (c *Config) CommandNames() []string {
	return sortedKeys(c.Commands)
}

// ContextName returns the Docker context name for a server.
func (c *Config) ContextName(server *Server) string {
	return fmt.Sprintf("ferry-%s-%s", c.Name, server.Name)
}

// EnvFilesFor returns the env files (global first, then service overlay) for a
// service, resolved against the config dir. Missing global file is only an
// error when explicitly configured.
func (c *Config) EnvFilesFor(svc *Service) []string {
	var files []string
	if c.Env.File != "" {
		files = append(files, filepath.Join(c.Dir, c.Env.File))
	}
	if svc != nil && svc.EnvFile != "" {
		files = append(files, filepath.Join(c.Dir, svc.EnvFile))
	}
	return files
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
