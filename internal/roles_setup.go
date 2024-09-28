package ferry

import (
	"context"
	"fmt"
)

var Setup = []Role{
	// &InstallDockerRole{},
	// &InstallSopsRole{},
	&InitTraefikServiceRole{},
}

type InstallDockerRole struct{}

func (s *InstallDockerRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask("docker --version").SkipByOnError(7),
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

func (s *InstallSopsRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask("sudo apt-get update"),
		NewTask("curl -LO https://github.com/getsops/sops/releases/download/v3.9.0/sops-v3.9.0.linux.amd64"),
		NewTask("sudo mv sops-v3.9.0.linux.amd64 /usr/local/bin/sops"),
		NewTask("sudo chmod +x /usr/local/bin/sops"),
		NewTask("sops --version"),
	}
}

type InitTraefikServiceRole struct{}

func (s *InitTraefikServiceRole) Description() string {
	return "Initialize the traefik service on the server"
}

func (s *InitTraefikServiceRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask("docker network create traefik-network").IgnoreError(),
		NewTask(`if docker ps --filter name="traefik" --format '{{.Names}}'; then
			echo "Error: Traefik container is already running"
			exit 0
		fi`).SkipByOnOutputMatch(1, "Error: Traefik container is already running"),
		NewTask(fmt.Sprintf(`docker run -d \
		--name traefik \
		--network traefik-network \
		-p 80:80 \
		-p 443:443 \
		-v /var/run/docker.sock:/var/run/docker.sock:ro \
		-v $PWD/letsencrypt:/letsencrypt \
		-e TZ=UTC \
		traefik:v3.1.4 \
		--api.insecure=true \
		--providers.docker=true \
		--providers.docker.exposedbydefault=false \
		--entrypoints.web.address=:80 \
		--entrypoints.websecure.address=:443 \
		--certificatesresolvers.myresolver.acme.email=%s \
		`, cfg.CertResolver)),
	}
}

type InitFerryConfigRole struct{}

func (s *InitFerryConfigRole) Description() string {
	return "Initialize the ferry config on the server"
}

func (s *InitFerryConfigRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{}
}
