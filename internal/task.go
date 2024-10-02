package ferry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type ResponseHandler func(ctx context.Context, output []byte) (context.Context, []byte)
type ErrorHandler func(ctx context.Context, output []byte, err error) (context.Context, error)
type NextIndex func(ctx context.Context, output []byte, err error) (context.Context, int)

type Task struct {
	// Name is the name of the task. It's used in logs to identify the task.
	Name string
	// The command to run.
	Command string
	// ResponseHandler will determine how the app should react to the command output
	ResponseHandler ResponseHandler
	// ErrorHandler will determine how the app should react to the command error
	ErrorHandler ErrorHandler
	// Remote determines if the task should be run on a remote server or locally.
	Remote bool
	// GetNextTaskIndex is a function that will be called to determine the next task index.
	NextIndex NextIndex

	PipeStdout bool
}

func raiseOnExitError(err error) error {
	// we actually want to raise exit errors, as errors
	exitError, isExitError := err.(*ssh.ExitError)

	if isExitError {
		return fmt.Errorf("received exit code %d", exitError.ExitStatus())
	}

	return err
}

func throwDockerErrors(output []byte) error {
	outputStr := string(output)
	if strings.Contains(outputStr, "Error:") || strings.Contains(outputStr, "error:") {
		return fmt.Errorf("docker command received exit code %s", outputStr)
	}

	return nil
}

func defaultResponseHandler(ctx context.Context, output []byte) (context.Context, []byte) {
	return ctx, output
}

func defaultErrorHandler(ctx context.Context, output []byte, err error) (context.Context, error) {
	return ctx, raiseOnExitError(err)
}

var defaultIncrement = 0

func defaultNextIndex(ctx context.Context, output []byte, err error) (context.Context, int) {
	return ctx, defaultIncrement
}

func NewTask(command string) Task {
	name := ""
	if len(command) > 20 {
		name = command[:20]
	} else {
		name = command
	}
	return Task{
		Command:         command,
		Name:            name,
		ResponseHandler: defaultResponseHandler,
		ErrorHandler:    defaultErrorHandler,
		NextIndex:       defaultNextIndex,
		Remote:          true,
		PipeStdout:      false,
	}
}

func (t Task) ThrowDockerErrors() Task {
	t.ErrorHandler = func(ctx context.Context, output []byte, err error) (context.Context, error) {
		if dockerErr := throwDockerErrors(output); dockerErr != nil {
			return ctx, dockerErr
		}
		return ctx, raiseOnExitError(err)
	}
	return t
}

func (t Task) SetRemote(remote bool) Task {
	t.Remote = remote
	return t
}

func (t Task) WithResponseHandler(handler ResponseHandler) Task {
	originalHandler := t.ResponseHandler
	t.ResponseHandler = func(ctx context.Context, output []byte) (context.Context, []byte) {
		ctx, out := originalHandler(ctx, output)
		return handler(ctx, out)
	}
	return t
}

func (t Task) WithErrorHandler(handler ErrorHandler) Task {
	originalHandler := t.ErrorHandler
	t.ErrorHandler = func(ctx context.Context, output []byte, err error) (context.Context, error) {
		ctx, err = originalHandler(ctx, output, err)
		return handler(ctx, output, err)
	}
	return t
}

func (t Task) IgnoreError() Task {
	t.ErrorHandler = func(ctx context.Context, output []byte, err error) (context.Context, error) {
		return ctx, nil
	}
	return t
}

func (t Task) SkipByOnError(increment int) Task {
	t.NextIndex = func(ctx context.Context, output []byte, err error) (context.Context, int) {
		if err != nil {
			return ctx, increment
		}
		return ctx, defaultIncrement
	}
	return t
}

func (t Task) SkipByOnOutputMatch(increment int, match string) Task {
	t.ErrorHandler = func(ctx context.Context, output []byte, err error) (context.Context, error) {
		return ctx, nil
	}
	t.NextIndex = func(ctx context.Context, output []byte, err error) (context.Context, int) {
		if strings.Contains(string(output), match) {
			return ctx, increment
		}
		return ctx, defaultIncrement
	}
	return t
}

func (t Task) Stdout() Task {
	t.PipeStdout = true
	return t
}

func (t Task) Wait(delay int) Task {
	time.Sleep(time.Duration(delay) * time.Second)
	return t
}

type CtxKey string

func (t Task) PersistOutput(name CtxKey) Task {
	originalHandler := t.ResponseHandler
	t.ResponseHandler = func(ctx context.Context, output []byte) (context.Context, []byte) {
		ctx, out := originalHandler(ctx, output)
		ctx = context.WithValue(ctx, name, strings.Trim(string(out), "\n"))
		return ctx, out
	}
	return t
}
