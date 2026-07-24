// Package exec runs commands. Docker operations go through the local docker
// CLI against a per-server Docker context (transport, builds, PTYs are the
// CLI's problem); plain SSH via the system ssh binary remains for non-Docker
// host chores. Both come with the user's ~/.ssh/config, agent auth, and host
// key verification for free.
package exec

import (
	"bytes"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"

	"github.com/ronxjansen/ferry/internal/config"
)

// Run executes a local command and returns its combined output.
func Run(args ...string) (string, error) {
	cmd := osexec.Command(args[0], args[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := strings.TrimRight(out.String(), "\n")
	if err != nil {
		return output, fmt.Errorf("%s failed: %w\n%s", strings.Join(args, " "), err, output)
	}
	return output, nil
}

// Stream executes a local command, streaming output to the terminal.
func Stream(args ...string) error {
	cmd := osexec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Interactive executes a local command wired to the terminal, stdin included.
func Interactive(args ...string) error {
	cmd := osexec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunInput executes a local command with input on stdin (for secrets that
// must not appear in argv) and returns its combined output.
func RunInput(input []byte, args ...string) (string, error) {
	cmd := osexec.Command(args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := strings.TrimRight(out.String(), "\n")
	if err != nil {
		return output, fmt.Errorf("%s failed: %w\n%s", strings.Join(args, " "), err, output)
	}
	return output, nil
}

// Docker runs docker CLI invocations against one server's context.
type Docker struct {
	Context string
	Server  *config.Server
}

// Args prefixes docker args with the context selection.
func (d *Docker) Args(args ...string) []string {
	return append([]string{"docker", "--context", d.Context}, args...)
}

// Run captures combined output.
func (d *Docker) Run(args ...string) (string, error) {
	return Run(d.Args(args...)...)
}

// Stream streams output to the terminal.
func (d *Docker) Stream(args ...string) error {
	return Stream(d.Args(args...)...)
}

// Interactive wires the command to the terminal (for exec -it, logs -f).
func (d *Docker) Interactive(args ...string) error {
	return Interactive(d.Args(args...)...)
}

// Host runs plain SSH commands on a server for non-Docker chores:
// provisioning, mkdir, shipping env files, hooks, the lock dir, the audit log.
type Host struct {
	Server *config.Server
}

func sshArgs(s *config.Server, extra ...string) []string {
	args := []string{"ssh", "-o", "BatchMode=yes", "-p", fmt.Sprint(s.Port)}
	args = append(args, extra...)
	return append(args, s.Address())
}

// Run executes a shell command on the host and captures combined output.
func (h *Host) Run(command string) (string, error) {
	return Run(append(sshArgs(h.Server), command)...)
}

// Stream executes a shell command on the host, streaming output.
func (h *Host) Stream(command string) error {
	return Stream(append(sshArgs(h.Server), command)...)
}

// Interactive opens an interactive session (or command) with a PTY.
func (h *Host) Interactive(command string) error {
	args := []string{"ssh", "-t", "-p", fmt.Sprint(h.Server.Port), h.Server.Address()}
	if command != "" {
		args = append(args, command)
	}
	return Interactive(args...)
}

// WriteFile ships content to a path on the host with the given mode, creating
// parent directories. Content goes over stdin, never through argv.
func (h *Host) WriteFile(path string, content []byte, mode string) error {
	cmd := osexec.Command("ssh", "-o", "BatchMode=yes", "-p", fmt.Sprint(h.Server.Port), h.Server.Address(),
		fmt.Sprintf("mkdir -p $(dirname %q) && cat > %q && chmod %s %q", path, path, mode, path))
	cmd.Stdin = bytes.NewReader(content)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write %s on %s: %w\n%s", path, h.Server.Name, err, out.String())
	}
	return nil
}
