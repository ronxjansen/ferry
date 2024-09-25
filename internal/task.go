package ferry

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

type ResponseHandler func([]byte) []byte
type ErrorHandler func([]byte, error) error
type NextIndex func([]byte, error) int

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

func defaultResponseHandler(output []byte) []byte {
	return output
}

func defaultErrorHandler(output []byte, err error) error {
	return raiseOnExitError(err)
}

var defaultIncrement = 0

func defaultNextIndex(output []byte, err error) int {
	return defaultIncrement
}

func NewTask(command string) Task {
	return Task{
		Command:         command,
		Name:            command[:20],
		ResponseHandler: defaultResponseHandler,
		ErrorHandler:    defaultErrorHandler,
		NextIndex:       defaultNextIndex,
		Remote:          true,
	}
}

func (t Task) ThrowDockerErrors() Task {
	t.ErrorHandler = func(output []byte, err error) error {
		if dockerErr := throwDockerErrors(output); dockerErr != nil {
			return dockerErr
		}
		return raiseOnExitError(err)
	}
	return t
}

func (t Task) SetRemote(remote bool) Task {
	t.Remote = remote
	return t
}

func (t Task) WithResponseHandler(handler ResponseHandler) Task {
	originalHandler := t.ResponseHandler
	t.ResponseHandler = func(output []byte) []byte {
		return handler(originalHandler(output))
	}
	return t
}

func (t Task) WithErrorHandler(handler ErrorHandler) Task {
	originalHandler := t.ErrorHandler
	t.ErrorHandler = func(output []byte, err error) error {
		return handler(output, originalHandler(output, err))
	}
	return t
}

func (t Task) IgnoreError() Task {
	t.ErrorHandler = func(output []byte, err error) error {
		return nil
	}
	return t
}

func (t Task) SkipByOnError(increment int) Task {
	t.NextIndex = func(output []byte, err error) int {
		if err != nil {
			return increment
		}
		return defaultIncrement
	}
	return t
}

func (t Task) SkipByOnOutputMatch(increment int, match string) Task {
	t.NextIndex = func(output []byte, err error) int {
		if strings.Contains(string(output), match) {
			return increment
		}
		return defaultIncrement
	}
	return t
}
