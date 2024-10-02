package ferry

import (
	"bytes"
	"context"
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

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		for i := 0; i < len(roles); i++ {
			logger.Info("Running role", zap.String("role", roles[i].Description()))

			role := roles[i]
			tasks := role.BuildTasks(c.config, ctx, server)

			for in := 0; in < len(tasks); in++ {
				newCtx, nextIndex, err := c.runCmd(client, server, ctx, tasks[in])
				if err != nil {
					return err
				}
				ctx = newCtx
				in += nextIndex
			}
		}
	}

	return nil
}

func (c *CommandManager) runCmd(client *ssh.Client, server Server, ctx context.Context, task Task) (context.Context, int, error) {
	// we can only run one command in a session; if we want to run multiple commands in a session we need to combine commands into a single string seperated by ;. This impacts error handling, output handling and skipping logic.
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
		if task.PipeStdout {
			session.Stdout = os.Stdout
		}
		err = session.Run(commandStr)
	} else {
		// Run the command locally with SSH agent forwarding
		cmd := exec.Command("sh", "-c", commandStr)
		cmd.Env = append(os.Environ(),
			"SSH_AUTH_SOCK="+os.Getenv("SSH_AUTH_SOCK"),
			"DOCKER_HOST="+os.Getenv("DOCKER_HOST"),
			"DOCKER_CERT_PATH="+os.Getenv("DOCKER_CERT_PATH"),
			"DOCKER_TLS_VERIFY="+os.Getenv("DOCKER_TLS_VERIFY"),
		)
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
	errCtx, errResponse := task.ErrorHandler(ctx, output, err)

	// Each tasks can determine what the next task index should be to be executed
	nextCtx, nextIndex := task.NextIndex(errCtx, output, errResponse)

	if errResponse != nil {
		logger.Error(task.Name,
			zap.Int("nextIndex", nextIndex),
			zap.String("command", commandStr),
			zap.String("stdout", stdout.String()),
			zap.String("stderr", stderr.String()))

		return nextCtx, nextIndex, errResponse
	}

	// Each tasks can determine how to handle the output
	outputCtx, output := task.ResponseHandler(nextCtx, output)

	logger.Debug(task.Name, zap.String("command", commandStr), zap.Int("nextIndex", nextIndex), zap.String("output", string(output)))

	return outputCtx, nextIndex, errResponse
}
