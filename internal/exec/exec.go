package exec

// Executor provides an interface for running commands on a remote server
type Executor interface {
	// Run executes a command and returns the output
	Run(cmd string) (string, error)
	// RunInteractive runs an interactive command with PTY
	RunInteractive(cmd string) error
	// Close closes the connection
	Close() error
}

// Result contains the result of a command execution
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}
