package main

import (
	"fmt"
)

type Role interface {
	BuildTasks(cfg Config, server Server) []Task
}

type InstallDockerRole struct{}

func (s *InstallDockerRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask("docker --version", WithHandler(IgnoreError())),
		NewTask("sudo apt-get update"),
		NewTask("sudo apt-get install -y apt-transport-https ca-certificates curl software-properties-common"),
		NewTask("curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -"),
		NewTask("sudo add-apt-repository \"deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable\""),
		NewTask("sudo apt-get update"),
		NewTask("sudo apt-get install -y docker-ce docker-ce-cli containerd.io"),
		NewTask("sudo usermod -aG docker $USER"),
	}
}

type DeployTraefikServiceRole struct{}

func (s *DeployTraefikServiceRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(fmt.Sprintf(`docker run -d \
			--name %s \
			--network traefik-network \
			--env-file %s \
			--label "traefik.enable=true" \
			--label "traefik.http.routers.%s.rule=Host('%s')" \
			--label "traefik.http.services.%s.loadbalancer.server.port=%d"`,
			cfg.ContainerName, cfg.ContainerName, cfg.Domain, cfg.ContainerName, cfg.Port) +
			fmt.Sprintf(
				` \
			--label "traefik.http.routers.%s.service=%s" \
			--label "traefik.http.services.%s.loadbalancer.server.port=%d"`,
				cfg.ContainerName, cfg.ContainerName, cfg.ContainerName, cfg.Port) +
			fmt.Sprintf(
				` \
			--label "traefik.http.services.%s.loadbalancer.healthcheck.path=%s" \
			--label "traefik.http.services.%s.loadbalancer.healthcheck.interval=%s" \
			--label "traefik.http.services.%s.loadbalancer.healthcheck.timeout=%s" \
			%s`,
				cfg.ContainerName, cfg.HealthCheck.Path, cfg.ContainerName, cfg.HealthCheck.Interval, cfg.ContainerName, cfg.HealthCheck.Timeout, cfg.Image)),
	}
}

type UpdateEnvVarsRole struct{}

func (s *UpdateEnvVarsRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(fmt.Sprintf("scp %s %s@%s:%d", server.User, server.Host, server.Port, cfg.EnvFile)),
		NewTask(fmt.Sprintf("sops --encrypt --in-place --age1 --age-recipient %s %s", cfg.CertResolver, cfg.EnvFile)),
		NewTask(fmt.Sprintf("scp %s %s", cfg.EnvFile, server.User, server.Host, server.Port)),
	}
}

type StopTraefikServiceRole struct{}

func (s *StopTraefikServiceRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(fmt.Sprintf("docker stop %s", cfg.ContainerName)),
		NewTask(fmt.Sprintf("docker rm %s", cfg.ContainerName)),
	}
}

type PullDockerImageRole struct{}

func (s *PullDockerImageRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(fmt.Sprintf("docker pull %s", cfg.Image)),
		NewTask(fmt.Sprintf("docker tag %s %s", cfg.Image, cfg.ContainerName)),
	}
}

type InitTraefikServiceRole struct{}

func (s *InitTraefikServiceRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
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

type InstallSopsRole struct{}

func (s *InstallSopsRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask("sudo apt-get update"),
		NewTask("sudo apt-get install -y sops"),
		NewTask("sops --version"),
	}
}

type BootstrapAppDirRole struct{}

func (s *BootstrapAppDirRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(fmt.Sprintf("mkdir -p %s", server.AppDir)),
		NewTask(fmt.Sprintf("mkdir -p %s/letsencrypt", server.AppDir)),
	}
}

// TODO shouldnt we do this when we deploy?
type SyncEnvVarsRole struct{}

func (s *SyncEnvVarsRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(fmt.Sprintf("scp %s %s@%s:%d/%s", cfg.EnvFile, server.User, server.Host, server.Port, server.AppDir), WithRemote(false)),
		NewTask("sops --encrypt --in-place --age1 --age-recipient %s %s"),
	}
}
