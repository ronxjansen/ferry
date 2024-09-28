package ferry

import (
	"context"
	"fmt"
)

var Logs = []Role{
	&ShowDockerLogsRole{},
}

type ShowDockerLogsRole struct{}

func (s *ShowDockerLogsRole) Description() string {
	return "Show logs for the docker service"
}

func (s *ShowDockerLogsRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(fmt.Sprintf("docker logs -f --tail=100 %s", cfg.ContainerName)).Stdout(),
	}
}
