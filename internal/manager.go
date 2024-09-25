package ferry

import (
	"bytes"
	"os"
	"os/exec"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type CommandManager struct {
	config Config
}

func NewCommandManager(config Config) *CommandManager {
	return &CommandManager{
		config: config,
	}
}

func (c *CommandManager) Run(roles []Role) error {
	for _, server := range c.config.Servers {
		client, err := NewSSHClient(server)
		if err != nil {
			logger.Fatal("Failed to create SSH client", zap.Error(err), zap.String("server", server.Host))
		}

		for i := 0; i < len(roles); i++ {
			role := roles[i]
			tasks := role.BuildTasks(c.config, server)

			for in := 0; in < len(tasks); in++ {
				nextIndex, err := c.runCmd(client, server, tasks[in])
				if err != nil {
					return err
				}
				in += nextIndex
			}
		}
	}

	return nil
}

func (c *CommandManager) runCmd(client *ssh.Client, server Server, task Task) (int, error) {
	session, err := client.NewSession()
	if err != nil {
		logger.Fatal("error creating SSH session: %v", zap.Error(err), zap.String("server", server.Host))
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	commandStr := task.Command

	if task.Remote {
		err = session.Run(commandStr)
	} else {
		// Run the command locally with SSH agent forwarding
		cmd := exec.Command("sh", "-c", commandStr)
		cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+os.Getenv("SSH_AUTH_SOCK"))
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
	}

	output := stdout.Bytes()
	if len(stderr.Bytes()) > 0 {
		output = append(output, '\n')
		output = append(output, stderr.Bytes()...)
	}

	// Each tasks can determine how to handle the error
	errResponse := task.ErrorHandler(output, err)

	// Each tasks can determine what the next task index should be to be executed
	nextIndex := task.NextIndex(output, errResponse)

	if errResponse != nil {
		logger.Error(task.Name,
			zap.Int("nextIndex", nextIndex),
			zap.String("command", commandStr),
			zap.String("stdout", stdout.String()),
			zap.String("stderr", stderr.String()))

		return nextIndex, errResponse
	}

	// Each tasks can determine how to handle the output
	output = task.ResponseHandler(output)

	logger.Debug(task.Name, zap.String("command", commandStr), zap.Int("nextIndex", nextIndex), zap.String("output", string(output)))

	return nextIndex, errResponse
}
