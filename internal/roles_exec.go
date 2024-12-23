package ferry

import (
	"context"
	"fmt"
	"strings"
)

var Exec = []Role{
	&ExecDockerCommandRole{},
}

func NewExecDockerCommandRole(args []string) []Role {
	return []Role{
		&GetContainerNameRole{},
		&ExecDockerCommandRole{args: args},
	}
}

type ExecDockerCommandRole struct {
	args []string
}

func (s *ExecDockerCommandRole) Description() string {
	return "Run a command in a docker container on your VPS"
}

func (s *ExecDockerCommandRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	containerNameInput := ctx.Value(CtxKey("container_name"))
	if containerNameInput == nil {
		logger.Fatal("container_name not found in context")
	}
	containerName := containerNameInput.(string)

	command := strings.Join(s.args, " ")

	envCmd := buildEnvCmd(cfg.EnvFile)

	return []Task{
		NewTask(fmt.Sprintf("docker exec %s %s sh -c '%s'",
			envCmd,
			containerName,
			command)).ThrowDockerErrors().Stdout(),
	}
}
