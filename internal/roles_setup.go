package ferry

import (
	"context"
	"fmt"
)

var Setup = []Role{
	&InstallDockerRole{},
	&BootstrapFerryRole{},
	&InitTraefikServiceRole{},
}

type InstallDockerRole struct{}

func (s *InstallDockerRole) Description() string {
	return "Install Docker"
}

func (s *InstallDockerRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask("docker --version").SkipByOnOutputMatch(7, "Docker version"),
		NewTask("sudo apt-get update"),
		NewTask("sudo apt-get install -y apt-transport-https ca-certificates curl software-properties-common"),
		NewTask("curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -"),
		NewTask("sudo add-apt-repository \"deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable\""),
		NewTask("sudo apt-get update"),
		NewTask("sudo apt-get install -y docker-ce docker-ce-cli containerd.io"),
		NewTask("sudo usermod -aG docker $USER"),
	}
}

type InstallSopsRole struct{}

func (s *InstallSopsRole) Description() string {
	return "Install SOPS"
}

func (s *InstallSopsRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask("sudo apt-get update"),
		NewTask("curl -LO https://github.com/getsops/sops/releases/download/v3.9.0/sops-v3.9.0.linux.amd64"),
		NewTask("sudo mv sops-v3.9.0.linux.amd64 /usr/local/bin/sops"),
		NewTask("sudo chmod +x /usr/local/bin/sops"),
		NewTask("sops --version"),
	}
}

type BootstrapFerryRole struct{}

func (s *BootstrapFerryRole) Description() string {
	return "Bootstrap the ferry CLI"
}

func (s *BootstrapFerryRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask("mkdir -p $HOME/ferry"),
		NewTask("mkdir -p $HOME/ferry/letsencrypt"),
		NewTask("touch $HOME/ferry/letsencrypt/acme.json"),
		NewTask("chmod 0600 $HOME/ferry/letsencrypt/acme.json"),
	}
}

type InitTraefikServiceRole struct{}

func (s *InitTraefikServiceRole) Description() string {
	return "Initialize the traefik service"
}

func (s *InitTraefikServiceRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask("docker network create --attachable traefik-network").IgnoreError(),
		NewTask(`if [ -n "$(docker ps --filter name=traefik --format '{{.Names}}')" ]; then
			echo "Error: Traefik container is already running"
			exit 1
		fi`).SkipByOnOutputMatch(1, "Error: Traefik container is already running"),
		NewTask(fmt.Sprintf(`docker run -d \
		--name traefik \
		--network traefik-network \
		-p 80:80 \
		-p 443:443 \
		-p 8080:8080 \
		-v /var/run/docker.sock:/var/run/docker.sock:ro \
		-v $HOME/ferry/letsencrypt:/letsencrypt \
		-e TZ=UTC \
		traefik:v3.1.4 \
		--api.insecure=true \
		--providers.docker=true \
		--providers.docker.exposedbydefault=true \
		--entrypoints.web.address=:80 \
		--entrypoints.websecure.address=:443 \
		--certificatesresolvers.myresolver.acme.httpchallenge=true \
		--certificatesresolvers.myresolver.acme.httpchallenge.entrypoint=web \
		--certificatesresolvers.myresolver.acme.email=%s \
		--certificatesresolvers.myresolver.acme.storage=/letsencrypt/acme.json
		`, cfg.CertResolver),
		),
	}
}
