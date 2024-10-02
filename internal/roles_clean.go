package ferry

import "context"

var CleanCmnds = []Role{
	&CleanAppServiceRole{},
}

type CleanAppServiceRole struct{}

func (s *CleanAppServiceRole) Description() string {
	return "Remove Ferry related resources"
}

func (s *CleanAppServiceRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	return []Task{
		NewTask(cmdf(`docker stop traefik`)).IgnoreError(),
		NewTask(cmdf(`docker rm traefik`)).IgnoreError(),
		NewTask(cmdf(`docker network rm traefik-network`)).IgnoreError(),
		NewTask(cmdf(`docker system prune -a -f`)),
		NewTask(cmdf(`docker volume prune -f`)),
		NewTask(cmdf(`sudo rm -rf ~/ferry`)),
	}
}
