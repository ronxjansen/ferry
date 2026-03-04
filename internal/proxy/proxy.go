package proxy

import (
	"fmt"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
)

// Proxy interface for reverse proxy management
type Proxy interface {
	// Init initializes the proxy on the server
	Init(executor exec.Executor) error
	// Status returns the proxy status
	Status(executor exec.Executor) (string, error)
	// AddApp configures the proxy for an app
	AddApp(executor exec.Executor, app config.App) error
	// RemoveApp removes proxy configuration for an app
	RemoveApp(executor exec.Executor, appName string) error
	// DockerLabels returns the labels needed for a container
	DockerLabels(app config.App) []string
	// Network returns the proxy network name
	Network() string
}

// New creates a new Proxy based on the configuration
func New(cfg config.Proxy) (Proxy, error) {
	switch cfg.Type {
	case "traefik", "":
		return NewTraefik(cfg), nil
	default:
		return nil, fmt.Errorf("unknown proxy type: %s", cfg.Type)
	}
}
