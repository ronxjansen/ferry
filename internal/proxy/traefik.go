package proxy

import (
	"fmt"
	"strings"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
)

// Traefik implements the Proxy interface for Traefik
type Traefik struct {
	network   string
	acmeEmail string
}

// NewTraefik creates a new Traefik proxy
func NewTraefik(cfg config.Proxy) *Traefik {
	network := cfg.Network
	if network == "" {
		network = "traefik-network"
	}
	return &Traefik{
		network:   network,
		acmeEmail: cfg.AcmeEmail,
	}
}

// Network returns the proxy network name
func (t *Traefik) Network() string {
	return t.network
}

// Init initializes Traefik on the server
func (t *Traefik) Init(executor exec.Executor) error {
	// Create ferry directories
	commands := []string{
		"mkdir -p $HOME/ferry",
		"mkdir -p $HOME/ferry/letsencrypt",
		"touch $HOME/ferry/letsencrypt/acme.json",
		"chmod 0600 $HOME/ferry/letsencrypt/acme.json",
	}
	for _, cmd := range commands {
		if _, err := executor.Run(cmd); err != nil {
			return fmt.Errorf("failed to create ferry directories: %w", err)
		}
	}

	// Create network (ignore error if exists)
	executor.Run(fmt.Sprintf("docker network create --attachable %s", t.network))

	// Check if traefik is already running
	output, _ := executor.Run("docker ps --filter name=traefik --format '{{.Names}}'")
	if strings.Contains(output, "traefik") {
		return nil // Already running
	}

	// Run traefik container
	traefikCmd := t.buildTraefikRunCommand()
	if _, err := executor.Run(traefikCmd); err != nil {
		return fmt.Errorf("failed to start traefik: %w", err)
	}

	return nil
}

func (t *Traefik) buildTraefikRunCommand() string {
	cmd := `docker run -d \
		--name traefik \
		--network %s \
		--restart unless-stopped \
		-p 80:80 \
		-p 443:443 \
		-v /var/run/docker.sock:/var/run/docker.sock:ro \
		-v $HOME/ferry/letsencrypt:/letsencrypt \
		-e TZ=UTC \
		traefik:latest \
		--providers.docker=true \
		--providers.docker.exposedbydefault=false \
		--entrypoints.web.address=:80 \
		--entrypoints.websecure.address=:443 \
		--certificatesresolvers.myresolver.acme.httpchallenge=true \
		--certificatesresolvers.myresolver.acme.httpchallenge.entrypoint=web \
		--certificatesresolvers.myresolver.acme.email=%s \
		--certificatesresolvers.myresolver.acme.storage=/letsencrypt/acme.json`

	return fmt.Sprintf(cmd, t.network, t.acmeEmail)
}

// Status returns the Traefik status
func (t *Traefik) Status(executor exec.Executor) (string, error) {
	output, err := executor.Run("docker ps --filter name=traefik --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'")
	if err != nil {
		return "", fmt.Errorf("failed to get traefik status: %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return "Traefik is not running", nil
	}
	return output, nil
}

// AddApp is a no-op for Traefik since apps are configured via Docker labels
func (t *Traefik) AddApp(executor exec.Executor, app config.App) error {
	// Traefik uses Docker labels, so no explicit configuration needed
	// Ensure the network exists and traefik is connected
	executor.Run(fmt.Sprintf("docker network create --attachable %s", t.network))
	executor.Run(fmt.Sprintf("docker network connect %s traefik", t.network))
	return nil
}

// RemoveApp is a no-op for Traefik since configuration is removed with the container
func (t *Traefik) RemoveApp(executor exec.Executor, appName string) error {
	// Nothing to do - config is in container labels
	return nil
}

// DockerLabels returns the Traefik labels for a container
func (t *Traefik) DockerLabels(app config.App) []string {
	labels := []string{
		"traefik.enable=true",
		fmt.Sprintf("traefik.docker.network=%s", t.network),
		fmt.Sprintf("traefik.http.routers.%s.rule=Host(`%s`)", app.Name, app.Domain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints=websecure", app.Name),
		fmt.Sprintf("traefik.http.routers.%s.tls.certresolver=myresolver", app.Name),
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port=%d", app.Name, app.Port),
	}

	// Add HTTP to HTTPS redirect
	labels = append(labels,
		fmt.Sprintf("traefik.http.routers.%s-http.rule=Host(`%s`)", app.Name, app.Domain),
		fmt.Sprintf("traefik.http.routers.%s-http.entrypoints=web", app.Name),
		fmt.Sprintf("traefik.http.routers.%s-http.middlewares=%s-https-redirect", app.Name, app.Name),
		fmt.Sprintf("traefik.http.middlewares.%s-https-redirect.redirectscheme.scheme=https", app.Name),
	)

	return labels
}
