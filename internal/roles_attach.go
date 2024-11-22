package ferry

import (
	"context"
	"fmt"
)

var Attach = []Role{
	&GetContainerNameRole{},
	&AttachContainerRole{},
}

type AttachContainerRole struct{}

func (s *AttachContainerRole) Description() string {
	return "Attach to running container in interactive shell"
}

func (s *AttachContainerRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	containerNameInput := ctx.Value(CtxKey("container_name"))
	if containerNameInput == nil {
		logger.Fatal("container_name not found in context")
	}
	containerName := containerNameInput.(string)

	return []Task{
		NewTask(fmt.Sprintf("docker exec -it %s /bin/bash", containerName)).SetInteractive(),
	}
}
