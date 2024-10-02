package ferry

import (
	"context"
	"fmt"
	"strconv"
)

type Role interface {
	BuildTasks(cfg Config, ctx context.Context, server Server) []Task
	Description() string
}

func cmdf(cmd string, args ...any) string {
	return fmt.Sprintf(cmd+" ", args...)
}

var Deploy = []Role{
	&BootstrapAppDirRole{},
	&PullDockerImageRole{},
	&PrepareDeployRole{},
	&PrepareDockerRole{},
	&UpdateEnvVarsRole{},
	&DeployTraefikServiceRole{},
	&CleanUpDeployRole{},
}

type PrepareDockerRole struct{}

func (s *PrepareDockerRole) Description() string {
	return "Create Docker prerequisites"
}

func (s *PrepareDockerRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	tasks := []Task{}

	for _, network := range cfg.Networks {
		tasks = append(tasks, NewTask(cmdf(`docker network create --attachable %s`, network)).IgnoreError())
		tasks = append(tasks, NewTask(cmdf(`docker network connect %s traefik`, network)).IgnoreError())
	}

	return tasks
}

type PrepareDeployRole struct{}

func (s *PrepareDeployRole) Description() string {
	return "Prepare docker based deploy"
}

func (s *PrepareDeployRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		// increment port if in use, and persist the new value in the context
		NewTask(cmdf(`port=%d; while docker ps -a --filter "name=%s" --format "{{.Ports}}" | grep -q ":$port->"; do ((port++)); done; export port; echo $port`, cfg.Port, cfg.ContainerName)).PersistOutput(CtxKey("port")),

		// create an app_name with either -blue or -green suffix, which is yet unused, and persist the new value in the context
		NewTask(cmdf(`app_name="%s"; if docker ps -a --format '{{.Names}}' | grep -q "${app_name}-blue"; then echo "%s-green"; else echo "%s-blue"; fi;`, cfg.ContainerName, cfg.ContainerName, cfg.ContainerName)).ThrowDockerErrors().PersistOutput(CtxKey("app_name")),

		// create an old name based on the app_name
		NewTask(cmdf(`app_name="%s"; if docker ps -a --format '{{.Names}}' | grep -q "${app_name}-blue"; then echo "%s-blue"; else echo "%s-green"; fi;`, cfg.ContainerName, cfg.ContainerName, cfg.ContainerName)).PersistOutput(CtxKey("old_name")),
	}
}

type DeployTraefikServiceRole struct{}

func (s *DeployTraefikServiceRole) Description() string {
	return "Deploy Traefik service"
}

func (s *DeployTraefikServiceRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	appName := ctx.Value(CtxKey("app_name")).(string)
	portIntStr := ctx.Value(CtxKey("port")).(string)
	portInt, err := strconv.Atoi(portIntStr)
	if err != nil {
		return []Task{}
	}

	// to be safe stop the app_name container if it is running
	cmd := cmdf(`docker run -d --name %s`, appName)
	for _, network := range cfg.Networks {
		cmd += cmdf(`--network %s`, network)
	}
	cmd += cmdf(`--network-alias %s`, cfg.ContainerName)
	cmd += cmdf(`--env-file %s/%s`, server.AppDir, cfg.EnvFile)

	for _, volume := range server.Volumes {
		cmd += cmdf(`--volume %s`, volume)
	}

	cmd += cmdf(`--label "traefik.enable=true"`)

	if cfg.Type == "app" {
		cmd += cmdf(`--publish %d:%d`, portInt, cfg.Port)
		// cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.path=%s"`, appName, cfg.Health.Path)
		// cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.interval=%s"`, appName, cfg.Health.Interval)
		// cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.timeout=%s"`, appName, cfg.Health.Timeout)
		// cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.status=%d"`, appName, cfg.Health.SuccessStatusCode)
		cmd += cmdf(`--label "traefik.http.routers.%s.rule=Host(%s)"`, appName, fmt.Sprintf("\\`%s\\`", cfg.Domain))
		cmd += cmdf(`--label "traefik.http.routers.%s.entrypoints=websecure"`, appName)
		cmd += cmdf(`--label "traefik.http.routers.%s.tls.certresolver=myresolver"`, appName)
	}

	if cfg.Type == "postgres" {
		cmd += cmdf(`--label "traefik.http.routers.%s.rule=Host(%s)"`, appName, fmt.Sprintf("\\`%s\\`", "postgres.localhost"))
		cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.server.port=%d"`, appName, cfg.Port)
	}

	cmd += cmdf(`%s`, cfg.Image)

	return []Task{
		NewTask(cmd).ThrowDockerErrors(),
	}
}

type CleanUpDeployRole struct{}

func (s *CleanUpDeployRole) Description() string {
	return "Clean up deploy"
}

func (s *CleanUpDeployRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	oldName := ctx.Value(CtxKey("old_name")).(string)

	return []Task{
		NewTask(cmdf(`docker stop %s || true`, oldName)).ThrowDockerErrors(),
		NewTask(cmdf(`docker rm %s || true`, oldName)).ThrowDockerErrors(),
		// some more cleanup
		NewTask(`docker container prune -f`).ThrowDockerErrors(),
		NewTask(`docker image prune -f`).ThrowDockerErrors(),
	}
}

type UpdateEnvVarsRole struct{}

func (s *UpdateEnvVarsRole) Description() string {
	return "Update environment variables"
}

func (s *UpdateEnvVarsRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf("scp -A %s %s@%s:%s", cfg.EnvFile, server.User, server.Host, server.AppDir)).SetRemote(false),
	}
}

type StopTraefikServiceRole struct{}

func (s *StopTraefikServiceRole) Description() string {
	return "Stop Traefik service"
}

func (s *StopTraefikServiceRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf("docker stop %s", cfg.ContainerName)),
		NewTask(cmdf("docker rm %s", cfg.ContainerName)),
	}
}

type PullDockerImageRole struct{}

func (s *PullDockerImageRole) Description() string {
	return "Pull Docker image"
}

func (s *PullDockerImageRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf("docker pull %s", cfg.Image)),
		NewTask(cmdf("docker tag %s %s", cfg.Image, cfg.ContainerName)),
	}
}

type BootstrapAppDirRole struct{}

func (s *BootstrapAppDirRole) Description() string {
	return "Create app directory"
}

func (s *BootstrapAppDirRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf("mkdir -p %s", server.AppDir)),
		NewTask(cmdf("mkdir -p %s/data", server.AppDir)),
		NewTask(cmdf("mkdir -p %s/letsencrypt", server.AppDir)),
	}
}
