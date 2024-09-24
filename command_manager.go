package main

import (
	"bytes"
	"context"
	"fmt"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type Task struct {
	Command string
	Handler func(context.Context, []byte, error, bool) (context.Context, error)
	Remote  bool
}

type Handler func(context.Context, []byte, error) (context.Context, error)

func NewTask(command string, options ...func(*Task)) Task {
	task := Task{
		Command: command,
		Handler: func(ctx context.Context, output []byte, err error, isExitError bool) (context.Context, error) {
			return ctx, nil
		},
		Remote: true,
	}

	for _, option := range options {
		option(&task)
	}

	return task
}

// WithHandler sets a custom handler for the task
type HandlerFunc func(context.Context, []byte, error, bool) (context.Context, error)

func WithHandler(handler HandlerFunc) func(*Task) {
	return func(t *Task) {
		t.Handler = handler
	}
}

func WithRemote(remote bool) func(*Task) {
	return func(t *Task) {
		t.Remote = remote
	}
}

type nextTaskKey string

func SkipNextTasks(skipNextTasks int) HandlerFunc {
	return func(ctx context.Context, output []byte, err error, isExitError bool) (context.Context, error) {
		if err != nil {
			return ctx, err
		}

		nextTask := ctx.Value(nextTaskKey("nextTask")).(int)
		return context.WithValue(ctx, nextTaskKey("nextTask"), int(nextTask+skipNextTasks)), nil
	}
}

func IgnoreError() HandlerFunc {
	return func(ctx context.Context, output []byte, err error, isExitError bool) (context.Context, error) {
		return ctx, nil
	}
}

type CommandManager struct {
	config Config
	cancel context.CancelFunc
	ctx    context.Context
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

		c.ctx, c.cancel = context.WithCancel(context.Background())
		c.ctx = context.WithValue(c.ctx, nextTaskKey("nextTask"), 0)

		for i := 0; i < len(roles); i++ {
			nextTask := c.ctx.Value(nextTaskKey("nextTask")).(int)
			if i < nextTask {
				continue
			}

			role := roles[i]
			tasks := role.BuildTasks(c.config, server)

			for _, task := range tasks {
				c.ctx, err = c.runCmd(client, server, task)
				if err != nil {
					return err
				}

				nextTask = c.ctx.Value(nextTaskKey("nextTask")).(int)
				if nextTask > i+1 {
					i = nextTask - 1 // -1 because the loop will increment i
				}
			}
			return nil
		}
		return nil
	}

	return nil
}

func (c *CommandManager) runCmd(client *ssh.Client, server Server, task Task) (context.Context, error) {
	session, err := client.NewSession()
	if err != nil {
		logger.Fatal("error creating SSH session: %v", zap.Error(err), zap.String("server", server.Host))
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	commandStr := task.Command

	err = session.Run(commandStr)
	output := stdout.Bytes()
	if len(stderr.Bytes()) > 0 {
		output = append(output, '\n')
		output = append(output, stderr.Bytes()...)
	}

	exitError, isExitError := err.(*ssh.ExitError)

	if err != nil && !isExitError {
		logger.Error("SSH error",
			zap.String("command", commandStr),
			zap.String("server", server.Host),
			zap.Error(err))
		return c.ctx, err
	}

	c.ctx, err = task.Handler(c.ctx, output, err, isExitError)

	if err != nil {
		logger.Error("Command failed",
			zap.String("command", commandStr),
			zap.Int("exit_code", exitError.ExitStatus()),
			zap.String("stdout", stdout.String()),
			zap.String("stderr", stderr.String()))

		return c.ctx, err
	}

	logger.Debug("Command output", zap.String("command", commandStr), zap.String("output", string(output)), zap.Error(err), zap.String("error", fmt.Sprintf("%v", err)))

	return c.ctx, nil
}
