// Package exec runs commands. Docker operations go through the local docker
// CLI against a per-server Docker context (transport, builds, PTYs are the
// CLI's problem); plain SSH via the system ssh binary remains for non-Docker
// host chores. Both come with the user's ~/.ssh/config, agent auth, and host
// key verification for free.
//
// Links to servers are assumed flaky: every SSH connection ferry spawns gets
// keepalives generous enough to ride out multi-minute stalls, and short
// docker control commands retry transparently on transient transport errors
// (a fresh connection typically works even while established flows are
// stalled).
package exec

import (
	"bytes"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
	"time"

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
	return StreamTo(os.Stdout, args...)
}

// StreamTo executes a local command, streaming stdout to w. Stderr goes to
// the terminal and its tail also lands in the returned error, so callers can
// classify failures (Transient) that exit codes alone don't reveal.
func StreamTo(w io.Writer, args ...string) error {
	cmd := osexec.Command(args[0], args[1:]...)
	tail := &tailBuffer{}
	cmd.Stdout = w
	cmd.Stderr = io.MultiWriter(os.Stderr, tail)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w\n%s", strings.Join(args, " "), err, tail.String())
	}
	return nil
}

// tailBuffer keeps the last few KB written to it.
type tailBuffer struct {
	buf []byte
}

const tailBufferSize = 4096

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailBufferSize {
		t.buf = t.buf[len(t.buf)-tailBufferSize:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	return strings.TrimRight(string(t.buf), "\n")
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
	return RunReader(bytes.NewReader(input), args...)
}

// RunReader executes a local command with stdin streamed from r (context
// tarballs, file content) and returns its combined output.
func RunReader(r io.Reader, args ...string) (string, error) {
	cmd := osexec.Command(args[0], args[1:]...)
	cmd.Stdin = r
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

// transientPatterns mark errors caused by the transport, not the command:
// stalled or reset connections, SSH transport death (exit 255), docker
// failing to dial the context, buildkit's gRPC layer giving up.
var transientPatterns = []string{
	"connection reset",
	"broken pipe",
	"operation timed out",
	"connection timed out",
	"i/o timeout",
	"unexpected eof",
	"error during connect",
	"code = unavailable",
	"client_loop",
	"connection closed by remote host",
	"kex_exchange_identification",
	"exit status 255", // ssh reserves 255 for transport failure
}

// Transient reports whether an error looks like a flaky-link failure worth
// retrying on a fresh connection.
func Transient(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, p := range transientPatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// retryDelays paces reconnection attempts. The diagnosed failure mode is an
// established-flow stall of ~1-2 minutes during which new connections work
// immediately, so the first retries come quickly.
var retryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

// sleep is a var so tests can run retries without waiting.
var sleep = time.Sleep

// RunRetry runs fn, retrying on Transient errors with backoff. Non-transient
// errors return immediately.
func RunRetry(fn func() (string, error)) (string, error) {
	out, err := fn()
	for _, delay := range retryDelays {
		if err == nil || !Transient(err) {
			return out, err
		}
		sleep(delay)
		out, err = fn()
	}
	return out, err
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

// Run captures combined output. Transient transport failures retry on a
// fresh connection; callers of non-idempotent commands (docker run, network
// create) must tolerate an "already exists" on the retry by re-checking
// state.
func (d *Docker) Run(args ...string) (string, error) {
	return RunRetry(func() (string, error) { return Run(d.Args(args...)...) })
}

// Stream streams output to the terminal. No retry: callers decide whether
// re-running an attached command is safe.
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

// keepaliveOpts keep SSH sessions alive through link stalls: 30s probes with
// 10 tolerated misses gives ~300s of stall tolerance before ssh gives up —
// comfortably past the 1-2 minute blackholes flaky middleboxes produce. Set
// on argv so they win over a stricter user ssh_config.
var keepaliveOpts = []string{
	"-o", "ServerAliveInterval=30",
	"-o", "ServerAliveCountMax=10",
	"-o", "TCPKeepAlive=yes",
}

func sshArgs(s *config.Server, extra ...string) []string {
	args := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	args = append(args, keepaliveOpts...)
	args = append(args, "-p", fmt.Sprint(s.Port))
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

// StreamTo executes a shell command on the host, streaming stdout to w.
func (h *Host) StreamTo(w io.Writer, command string) error {
	return StreamTo(w, append(sshArgs(h.Server), command)...)
}

// RunInput executes a shell command on the host with input on stdin.
func (h *Host) RunInput(input []byte, command string) (string, error) {
	return RunInput(input, append(sshArgs(h.Server), command)...)
}

// RunReader executes a shell command on the host with stdin streamed from r.
func (h *Host) RunReader(r io.Reader, command string) (string, error) {
	return RunReader(r, append(sshArgs(h.Server), command)...)
}

// Interactive opens an interactive session (or command) with a PTY.
func (h *Host) Interactive(command string) error {
	args := []string{"ssh", "-t"}
	args = append(args, keepaliveOpts...)
	args = append(args, "-p", fmt.Sprint(h.Server.Port), h.Server.Address())
	if command != "" {
		args = append(args, command)
	}
	return Interactive(args...)
}

// WriteFile ships content to a path on the host with the given mode, creating
// parent directories. Content goes over stdin, never through argv.
func (h *Host) WriteFile(path string, content []byte, mode string) error {
	command := fmt.Sprintf("mkdir -p $(dirname %q) && cat > %q && chmod %s %q", path, path, mode, path)
	if _, err := h.RunInput(content, command); err != nil {
		return fmt.Errorf("failed to write %s on %s: %w", path, h.Server.Name, err)
	}
	return nil
}
