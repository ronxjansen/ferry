package ferry

import (
	"context"
	"fmt"
	"strings"
)

var Run = []Role{
	&RunDockerCommandRole{},
}

func NewRunDockerCommandRole(args []string) []Role {
	return []Role{
		&RunDockerCommandRole{args: args},
	}
}

type RunDockerCommandRole struct {
	args []string
}

func (s *RunDockerCommandRole) Description() string {
	return "Run a command in a docker container on your VPS"
}

func (s *RunDockerCommandRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	command := strings.Join(s.args, " ")
	networkArgs := []string{}
	for _, network := range cfg.Networks {
		networkArgs = append(networkArgs, fmt.Sprintf("--network %s", network))
	}

	return []Task{
		NewTask(fmt.Sprintf("docker run --env-file %s/%s %s %s %s", server.AppDir, cfg.EnvFile, strings.Join(networkArgs, " "), cfg.Image, command)).ThrowDockerErrors().Stdout(),
	}
}
