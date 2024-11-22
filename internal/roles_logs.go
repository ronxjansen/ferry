package ferry

import (
	"context"
	"fmt"
)

var Logs = []Role{
	&GetContainerNameRole{},
	&ShowDockerLogsRole{},
}

type ShowDockerLogsRole struct{}

func (s *ShowDockerLogsRole) Description() string {
	return "Show logs for the docker service"
}

func (s *ShowDockerLogsRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	containerNameInput := ctx.Value(CtxKey("container_name"))
	if containerNameInput == nil {
		logger.Fatal("container_name not found in context")
	}
	containerName := containerNameInput.(string)

	return []Task{
		NewTask(fmt.Sprintf("docker logs -f --tail=100 %s", containerName)).Stdout(),
	}
}

type GetContainerNameRole struct{}

func (s *GetContainerNameRole) Description() string {
	return "Get the app name"
}

// Get the container name (either app_name+blue or app_name+green) from the context, using the app_name
func (s *GetContainerNameRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		// Get the current container name (either app_name-blue or app_name-green) that is running
		NewTask(cmdf(`docker ps --format '{{.Names}}' | grep "%s-" | head -n1 | xargs`, cfg.ContainerName)).
			ThrowDockerErrors().
			PersistOutput(CtxKey("container_name")),
	}
}
