package ferry

import (
	"context"
	"fmt"
)

type Role interface {
	BuildTasks(cfg Config, ctx context.Context, server Server) []Task
}

func cmdf(cmd string, args ...any) string {
	return fmt.Sprintf(cmd+" ", args...)
}

type PrepareDeployRole struct{}

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

func (s *DeployTraefikServiceRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	appName := ctx.Value(CtxKey("app_name")).(string)
	oldName := ctx.Value(CtxKey("old_name")).(string)

	// to be safe stop the app_name container if it is running
	cmd := cmdf(`docker run -d --name %s`, appName)
	cmd += cmdf(`--network traefik-network`)
	cmd += cmdf(`--env-file %s/%s`, server.AppDir, cfg.EnvFile)
	for _, volume := range server.Volumes {
		cmd += cmdf(`--volume %s`, volume)
	}
	cmd += cmdf(`--publish $port:%d`, cfg.Port)
	cmd += cmdf(`--label "traefik.enable=true"`)
	cmd += cmdf(`--label "traefik.http.routers.%s.rule=Host('%s')"`, appName, cfg.Domain)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.server.port=%d"`, appName, cfg.Port)
	cmd += cmdf(`--label "traefik.http.routers.%s.service=%s"`, appName)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.server.port=%d"`, appName, cfg.Port)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.path=%s"`, appName, cfg.HealthCheck.Path)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.interval=%s"`, appName, cfg.HealthCheck.Interval)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.timeout=%s"`, appName, cfg.HealthCheck.Timeout)
	cmd += cmdf(`--label "traefik.http.services.%s.loadbalancer.healthcheck.success_status_code=%d"`, appName, cfg.HealthCheck.SuccessStatusCode)
	cmd += cmdf(`%s`, cfg.Image)

	updateEnvVars := &UpdateEnvVarsRole{}
	updateEnvVarsTasks := updateEnvVars.BuildTasks(cfg, ctx, server)

	tasks := []Task{
		NewTask(cmd).ThrowDockerErrors(),

		// wait for the service to be healthy
		// NewTask(cmdf(`while ! curl -s -o /dev/null -w "%{http_code}" http://%s:%d | grep -q "200"; do sleep 1; done`, cfg.Domain, cfg.Port)).ThrowOnOutputMatch(1, "000"),

		NewTask(cmdf(`docker stop %s || true`, oldName)).ThrowDockerErrors(),
		NewTask(cmdf(`docker rm %s || true`, oldName)).ThrowDockerErrors(),

		// some more cleanup
		NewTask(`docker container prune -f`).ThrowDockerErrors(),
		NewTask(`docker image prune -f`).ThrowDockerErrors(),
	}

	return append(updateEnvVarsTasks, tasks...)
}

type UpdateEnvVarsRole struct{}

func (s *UpdateEnvVarsRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf("scp -A %s %s@%s:%s", cfg.EnvFile, server.User, server.Host, server.AppDir)).SetRemote(false),
	}
}

type StopTraefikServiceRole struct{}

func (s *StopTraefikServiceRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf("docker stop %s", cfg.ContainerName)),
		NewTask(cmdf("docker rm %s", cfg.ContainerName)),
	}
}

type PullDockerImageRole struct{}

func (s *PullDockerImageRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf("docker pull %s", cfg.Image)),
		NewTask(cmdf("docker tag %s %s", cfg.Image, cfg.ContainerName)),
	}
}

type BootstrapAppDirRole struct{}

func (s *BootstrapAppDirRole) Description() string {
	return "Bootstrap the application directory on the server if it doesn't exist"
}

func (s *BootstrapAppDirRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf("mkdir -p %s", server.AppDir)),
		NewTask(cmdf("mkdir -p %s/letsencrypt", server.AppDir)),
	}
}

var Deploy = []Role{
	&BootstrapAppDirRole{},
	// &SyncEnvVarsRole{},
	&PullDockerImageRole{},
	&PrepareDeployRole{},
	&DeployTraefikServiceRole{},
}
