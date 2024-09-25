package ferry

import (
	"fmt"
)

type Role interface {
	BuildTasks(cfg Config, server Server) []Task
}

func cmdf(cmd string, args ...any) string {
	return fmt.Sprintf(cmd, args...)
}

type DeployTraefikServiceRole struct{}

func (s *DeployTraefikServiceRole) BuildTasks(cfg Config, server Server) []Task {
	cmd := cmdf(`docker run -d --name %s`, cfg.ContainerName)
	cmd += cmdf(`--network traefik-network`)
	cmd += cmdf(`--env-file %s`, cfg.EnvFile)
	cmd += cmdf(`--label "traefik.enable=true"`)
	cmd += cmdf(`--label "traefik.http.routers.%s.rule=Host('%s')"`, cfg.ContainerName, cfg.Domain)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.server.port=%d"`, cfg.ContainerName, cfg.Port)
	cmd += cmdf(`--label "traefik.http.routers.%s.service=%s"`, cfg.ContainerName, cfg.ContainerName)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.server.port=%d"`, cfg.ContainerName, cfg.Port)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.path=%s"`, cfg.ContainerName, cfg.HealthCheck.Path)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.interval=%s"`, cfg.ContainerName, cfg.HealthCheck.Interval)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.timeout=%s"`, cfg.ContainerName, cfg.HealthCheck.Timeout)
	cmd += cmdf(`%s`, cfg.Image)

	return []Task{
		NewTask(cmd).ThrowDockerErrors(),
	}
}

type UpdateEnvVarsRole struct{}

func (s *UpdateEnvVarsRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(cmdf("scp %s %s@%s:%d", cfg.EnvFile, server.User, server.Host, server.Port)),
		NewTask(cmdf("sops --encrypt --in-place --age1 --age-recipient %s %s", cfg.CertResolver, cfg.EnvFile)),
		NewTask(cmdf("scp %s %s@%s:%d/%s", cfg.EnvFile, server.User, server.Host, server.Port, server.AppDir)),
	}
}

type StopTraefikServiceRole struct{}

func (s *StopTraefikServiceRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(cmdf("docker stop %s", cfg.ContainerName)),
		NewTask(cmdf("docker rm %s", cfg.ContainerName)),
	}
}

type PullDockerImageRole struct{}

func (s *PullDockerImageRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(cmdf("docker pull %s", cfg.Image)),
		NewTask(cmdf("docker tag %s %s", cfg.Image, cfg.ContainerName)),
	}
}

type BootstrapAppDirRole struct{}

func (s *BootstrapAppDirRole) Description() string {
	return "Bootstrap the application directory on the server if it doesn't exist"
}

func (s *BootstrapAppDirRole) BuildTasks(cfg Config, server Server) []Task {
	return []Task{
		NewTask(cmdf("mkdir -p %s", server.AppDir)),
		NewTask(cmdf("mkdir -p %s/letsencrypt", server.AppDir)),
	}
}

var Deploy = []Role{
	&BootstrapAppDirRole{},
	// &SyncEnvVarsRole{},
	&PullDockerImageRole{},
	// &StopTraefikServiceRole{},
	&DeployTraefikServiceRole{},
}
