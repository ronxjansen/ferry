package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// HookType represents lifecycle hook types
type HookType string

const (
	HookPreBuild    HookType = "pre_build"     // Before image build/pull (local)
	HookPreDeploy   HookType = "pre_deploy"    // Before container start (remote)
	HookPostDeploy  HookType = "post_deploy"   // After deploy complete (remote)
	HookPostAppBoot HookType = "post_app_boot" // After health check passes (remote)
)

// Hook defines commands to run at lifecycle points
type Hook struct {
	Commands []string `yaml:"commands,flow"` // Inline commands
	Script   string   `yaml:"script"`        // Path to script file
}

// DependsOnCondition defines dependency conditions (Docker Compose style)
type DependsOnCondition string

const (
	ConditionStarted   DependsOnCondition = "service_started"
	ConditionHealthy   DependsOnCondition = "service_healthy"
	ConditionCompleted DependsOnCondition = "service_completed_successfully"
)

// Dependency defines a service dependency
type Dependency struct {
	Service   string             `yaml:"service"`
	Condition DependsOnCondition `yaml:"condition"`
}

// AppType defines the type of application
type AppType string

const (
	AppTypeApp     AppType = "app"     // Default: proxy-enabled
	AppTypeService AppType = "service" // No proxy (databases, etc)
	AppTypeJob     AppType = "job"     // One-shot (migrations)
)

// Command defines a custom CLI command
type Command struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Command     string `yaml:"command"`
}

type Config struct {
	Servers  []Server            `yaml:"servers"`
	Proxy    Proxy               `yaml:"proxy"`
	Apps     []App               `yaml:"apps"`
	Hooks    map[HookType]Hook   `yaml:"hooks"`    // Global hooks
	Commands []Command           `yaml:"commands"` // Custom CLI commands
}

type Server struct {
	Name    string `yaml:"name"`
	Host    string `yaml:"host"`
	User    string `yaml:"user"`
	Port    int    `yaml:"port"`
	KeyFile string `yaml:"key_file"`
}

type Proxy struct {
	Type      string `yaml:"type"`
	Network   string `yaml:"network"`
	AcmeEmail string `yaml:"acme_email"`
}

type App struct {
	Name          string              `yaml:"name"`
	Server        string              `yaml:"server"`
	Image         string              `yaml:"image"`
	Port          int                 `yaml:"port"`
	Domain        string              `yaml:"domain"`
	DeployMethod  string              `yaml:"deploy_method"`
	EnvFile       string              `yaml:"env_file"`
	DockerFile    string              `yaml:"docker_file"`
	DockerContext string              `yaml:"docker_context"`
	Networks      []string            `yaml:"networks"`
	Volumes       []string            `yaml:"volumes"`
	Health        HealthCheck         `yaml:"health"`
	Type          AppType             `yaml:"type"`       // app, service, or job
	Command       []string            `yaml:"command"`    // Container command override
	DependsOn     []Dependency        `yaml:"depends_on"` // Service dependencies
	Hooks         map[HookType]Hook   `yaml:"hooks"`      // App-specific hooks
	Env           map[string]string   `yaml:"env"`        // Inline environment variables
}

type HealthCheck struct {
	Path        string   `yaml:"path"`
	Interval    string   `yaml:"interval"`
	Timeout     string   `yaml:"timeout"`
	Test        []string `yaml:"test"`         // Health check command (e.g., ["CMD-SHELL", "pg_isready"])
	Retries     int      `yaml:"retries"`      // Number of retries before unhealthy
	StartPeriod string   `yaml:"start_period"` // Time to wait before starting checks
}

// Load reads and parses a ferry.yaml config file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	cfg.setDefaults()
	return &cfg, nil
}

func (c *Config) setDefaults() {
	// Server defaults
	for i := range c.Servers {
		if c.Servers[i].Port == 0 {
			c.Servers[i].Port = 22
		}
		if c.Servers[i].User == "" {
			c.Servers[i].User = "root"
		}
	}

	// Proxy defaults
	if c.Proxy.Type == "" {
		c.Proxy.Type = "traefik"
	}
	if c.Proxy.Network == "" {
		c.Proxy.Network = "traefik-network"
	}

	// App defaults
	for i := range c.Apps {
		if c.Apps[i].Port == 0 {
			c.Apps[i].Port = 8080
		}
		if c.Apps[i].DeployMethod == "" {
			c.Apps[i].DeployMethod = "pull"
		}
		if c.Apps[i].Type == "" {
			c.Apps[i].Type = AppTypeApp
		}
		if c.Apps[i].Health.Path == "" {
			c.Apps[i].Health.Path = "/health"
		}
		if c.Apps[i].Health.Interval == "" {
			c.Apps[i].Health.Interval = "30s"
		}
		if c.Apps[i].Health.Timeout == "" {
			c.Apps[i].Health.Timeout = "5s"
		}
		if c.Apps[i].Health.Retries == 0 {
			c.Apps[i].Health.Retries = 3
		}
		// Set default dependency condition
		for j := range c.Apps[i].DependsOn {
			if c.Apps[i].DependsOn[j].Condition == "" {
				c.Apps[i].DependsOn[j].Condition = ConditionStarted
			}
		}
		// Ensure app is connected to proxy network (only for app type)
		if len(c.Apps[i].Networks) == 0 && c.Apps[i].Type == AppTypeApp {
			c.Apps[i].Networks = []string{c.Proxy.Network}
		}
	}
}

// GetServer returns a server by name
func (c *Config) GetServer(name string) (*Server, error) {
	for i := range c.Servers {
		if c.Servers[i].Name == name {
			return &c.Servers[i], nil
		}
	}
	return nil, fmt.Errorf("server not found: %s", name)
}

// GetApp returns an app by name
func (c *Config) GetApp(name string) (*App, error) {
	for i := range c.Apps {
		if c.Apps[i].Name == name {
			return &c.Apps[i], nil
		}
	}
	return nil, fmt.Errorf("app not found: %s", name)
}

// GetServer returns the server configured for this app
func (a *App) GetServer(cfg *Config) (*Server, error) {
	return cfg.GetServer(a.Server)
}

// ServerNames returns a list of all server names
func (c *Config) ServerNames() []string {
	names := make([]string, len(c.Servers))
	for i, s := range c.Servers {
		names[i] = s.Name
	}
	return names
}

// AppNames returns a list of all app names
func (c *Config) AppNames() []string {
	names := make([]string, len(c.Apps))
	for i, a := range c.Apps {
		names[i] = a.Name
	}
	return names
}

// GetCommand returns a command by name
func (c *Config) GetCommand(name string) (*Command, error) {
	for i := range c.Commands {
		if c.Commands[i].Name == name {
			return &c.Commands[i], nil
		}
	}
	return nil, fmt.Errorf("command not found: %s", name)
}

// CommandNames returns a list of all command names
func (c *Config) CommandNames() []string {
	names := make([]string, len(c.Commands))
	for i, cmd := range c.Commands {
		names[i] = cmd.Name
	}
	return names
}
