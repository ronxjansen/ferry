package exec

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ronxjansen/ferry/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

// SSHExecutor executes commands over SSH
type SSHExecutor struct {
	client *ssh.Client
	server config.Server
}

// NewSSHExecutor creates a new SSH executor for the given server
func NewSSHExecutor(server config.Server) (*SSHExecutor, error) {
	var authMethods []ssh.AuthMethod

	// Try key file first if specified
	if server.KeyFile != "" {
		keyPath := expandPath(server.KeyFile)
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file %s: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse key file %s: %w", keyPath, err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	// Fall back to ssh-agent
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		agentConn, err := net.Dial("unix", sock)
		if err == nil {
			agentClient := agent.NewClient(agentConn)
			authMethods = append(authMethods, ssh.PublicKeysCallback(agentClient.Signers))
		}
		// Don't fail if ssh-agent is unavailable, just skip it
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no authentication methods available: specify key_file or ensure ssh-agent is running")
	}

	sshConfig := &ssh.ClientConfig{
		User:            server.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: add proper host key verification
	}

	addr := fmt.Sprintf("%s:%d", server.Host, server.Port)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	return &SSHExecutor{
		client: client,
		server: server,
	}, nil
}

// Run executes a command and returns the combined output
func (e *SSHExecutor) Run(cmd string) (string, error) {
	session, err := e.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			return output, fmt.Errorf("command exited with status %d: %s", exitErr.ExitStatus(), output)
		}
		return output, fmt.Errorf("command failed: %w", err)
	}

	return output, nil
}

// RunInteractive runs an interactive command with PTY support
func (e *SSHExecutor) RunInteractive(cmd string) error {
	session, err := e.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Get file descriptor for stdin
	fd := int(os.Stdin.Fd())

	// Get current terminal size
	width, height, err := term.GetSize(fd)
	if err != nil {
		// Fallback to reasonable defaults
		width, height = 80, 24
	}

	// Put terminal into raw mode
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("failed to set raw terminal: %w", err)
	}
	defer term.Restore(fd, oldState)

	// Request PTY with actual terminal size
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", height, width, modes); err != nil {
		return fmt.Errorf("failed to request PTY: %w", err)
	}

	// Handle window resize
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	go func() {
		for range sigwinch {
			if w, h, err := term.GetSize(fd); err == nil {
				session.WindowChange(h, w)
			}
		}
	}()
	defer signal.Stop(sigwinch)

	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("interactive command failed: %w", err)
	}

	return nil
}

// Close closes the SSH connection
func (e *SSHExecutor) Close() error {
	return e.client.Close()
}

// expandPath expands ~ to the user's home directory
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}
