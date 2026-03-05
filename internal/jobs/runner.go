package jobs

import (
	"fmt"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
	"go.uber.org/zap"
)

// Runner executes one-shot job containers
type Runner struct {
	logger *zap.Logger
}

// NewRunner creates a new job runner
func NewRunner(logger *zap.Logger) *Runner {
	return &Runner{
		logger: logger,
	}
}

// Run starts a job container and waits for completion
func (r *Runner) Run(executor exec.Executor, app *config.App, timeout time.Duration) error {
	containerName := app.Name

	// Remove any existing container with the same name
	executor.Run(fmt.Sprintf("docker rm -f %s 2>/dev/null", containerName))

	r.logger.Info("Running job", zap.String("job", app.Name), zap.String("image", app.Image))

	// Build docker run command for job
	runCmd := r.buildJobRunCommand(app, containerName)

	if _, err := executor.Run(runCmd); err != nil {
		return fmt.Errorf("failed to start job container: %w", err)
	}

	// Wait for job to complete
	return r.waitForCompletion(executor, containerName, timeout)
}

func (r *Runner) buildJobRunCommand(app *config.App, containerName string) string {
	parts := []string{
		"docker run -d",
		fmt.Sprintf("--name %s", containerName),
	}

	// Add networks
	for _, network := range app.Networks {
		parts = append(parts, fmt.Sprintf("--network %s", network))
	}

	// Add environment variables from env map
	for key, value := range app.Env {
		parts = append(parts, fmt.Sprintf("--env '%s=%s'", key, value))
	}

	// Add volumes
	for _, vol := range app.Volumes {
		parts = append(parts, fmt.Sprintf("--volume %s", vol))
	}

	// Add image
	parts = append(parts, app.Image)

	// Add command if specified
	if len(app.Command) > 0 {
		for _, cmd := range app.Command {
			parts = append(parts, fmt.Sprintf("'%s'", cmd))
		}
	}

	return strings.Join(parts, " ")
}

func (r *Runner) waitForCompletion(executor exec.Executor, containerName string, timeout time.Duration) error {
	r.logger.Info("Waiting for job to complete", zap.String("job", containerName))

	deadline := time.Now().Add(timeout)
	checkInterval := 2 * time.Second

	for time.Now().Before(deadline) {
		// Check container status
		statusCmd := fmt.Sprintf("docker inspect --format='{{.State.Status}}' %s 2>/dev/null", containerName)
		statusOutput, err := executor.Run(statusCmd)
		if err != nil {
			return fmt.Errorf("failed to check job status: %w", err)
		}

		status := strings.TrimSpace(statusOutput)

		if status == "exited" {
			// Check exit code
			exitCmd := fmt.Sprintf("docker inspect --format='{{.State.ExitCode}}' %s 2>/dev/null", containerName)
			exitOutput, err := executor.Run(exitCmd)
			if err != nil {
				return fmt.Errorf("failed to get job exit code: %w", err)
			}

			exitCode := strings.TrimSpace(exitOutput)
			if exitCode == "0" {
				r.logger.Info("Job completed successfully", zap.String("job", containerName))
				return nil
			}

			// Get logs for debugging
			logsCmd := fmt.Sprintf("docker logs --tail 50 %s 2>&1", containerName)
			logs, _ := executor.Run(logsCmd)

			return fmt.Errorf("job %s failed with exit code %s\nLogs:\n%s", containerName, exitCode, logs)
		}

		time.Sleep(checkInterval)
	}

	return fmt.Errorf("timeout waiting for job %s to complete", containerName)
}

// GetExitCode returns the exit code of a completed job
func (r *Runner) GetExitCode(executor exec.Executor, containerName string) (int, error) {
	exitCmd := fmt.Sprintf("docker inspect --format='{{.State.ExitCode}}' %s 2>/dev/null", containerName)
	exitOutput, err := executor.Run(exitCmd)
	if err != nil {
		return -1, fmt.Errorf("failed to get exit code: %w", err)
	}

	var exitCode int
	_, err = fmt.Sscanf(strings.TrimSpace(exitOutput), "%d", &exitCode)
	if err != nil {
		return -1, fmt.Errorf("failed to parse exit code: %w", err)
	}

	return exitCode, nil
}
