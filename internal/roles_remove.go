package ferry

import "context"

var RemoveCmnds = []Role{
	&RemoveAppServiceRole{},
}

type RemoveAppServiceRole struct{}

func (s *RemoveAppServiceRole) Description() string {
	return "Remove app service"
}

func (s *RemoveAppServiceRole) BuildTasks(cfg Config, ctx context.Context, server Server) []Task {
	tasks := []Task{}

	tasks = append(tasks, NewTask(cmdf(`docker stop %s-blue`, cfg.ContainerName)).IgnoreError())
	tasks = append(tasks, NewTask(cmdf(`docker stop %s-green`, cfg.ContainerName)).IgnoreError())

	for _, network := range cfg.Networks {
		tasks = append(tasks, NewTask(cmdf(`docker network rm %s`, network)).IgnoreError())
	}

	tasks = append(tasks, NewTask(cmdf(`docker system prune -a -f`)))
	tasks = append(tasks, NewTask(cmdf(`docker volume prune -f`)))

	tasks = append(tasks, NewTask(cmdf(`sudo rm -rf %s`, server.AppDir)))
	return tasks
}
