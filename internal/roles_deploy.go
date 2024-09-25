package ferry

import (
	"fmt"
)

type Role interface {
	BuildTasks(cfg Config, server Server) []Task
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
			cfg.ContainerName, cfg.EnvFile, cfg.ContainerName, cfg.Domain, cfg.ContainerName, cfg.Port) +
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
		NewTask(fmt.Sprintf("scp %s %s@%s:%d", cfg.EnvFile, server.User, server.Host, server.Port)),
		NewTask(fmt.Sprintf("sops --encrypt --in-place --age1 --age-recipient %s %s", cfg.CertResolver, cfg.EnvFile)),
		NewTask(fmt.Sprintf("scp %s %s@%s:%d/%s", cfg.EnvFile, server.User, server.Host, server.Port, server.AppDir)),
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

type BootstrapAppDirRole struct{}

func (s *BootstrapAppDirRole) Description() string {
	return "Bootstrap the application directory on the server if it doesn't exist"
}

func (s *BootstrapAppDirRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(fmt.Sprintf("mkdir -p %s", server.AppDir)),
		NewTask(fmt.Sprintf("mkdir -p %s/letsencrypt", server.AppDir)),
	}
}

// TODO shouldnt we do this when we deploy?
// type SyncEnvVarsRole struct{}

// func (s *SyncEnvVarsRole) Description() string {
// 	return "Sync environment variables to the server"
// }

// func (s *SyncEnvVarsRole) BuildTasks(cfg Config, server Server) []Task {
// 	return []Task{
// 		NewTask(fmt.Sprintf("scp %s %s@%s:%d/%s", cfg.EnvFile, server.User, server.Host, server.Port, server.AppDir), WithRemote(false)),
// 		NewTask("sops --encrypt --in-place --age1 --age-recipient %s %s"),
// 	}
// }

var Deploy = []Role{
	&BootstrapAppDirRole{},
	// &SyncEnvVarsRole{},
	&PullDockerImageRole{},
	// &StopTraefikServiceRole{},
	&DeployTraefikServiceRole{},
}
