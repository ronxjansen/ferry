package cmd

import (
	"fmt"
	"os"
	"os/user"
	osExec "os/exec"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/deps"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/hooks"
	"github.com/ronxjansen/ferry/internal/jobs"
	"github.com/ronxjansen/ferry/internal/proxy"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const Version = "0.1.0"

var (
	deployImage   string
	deployAll     bool
	deployTimeout time.Duration
)

var deployCmd = &cobra.Command{
	Use:   "deploy [app]",
	Short: "Deploy an application",
	Long:  `Deploy an application to its configured server. Use --all to deploy all apps in dependency order.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if deployAll {
			return deployAllApps()
		}

		if len(args) == 0 {
			return fmt.Errorf("app name required (or use --all to deploy all apps)")
		}

		appName := args[0]
		return deploySingleApp(appName)
	},
}

func deployAllApps() error {
	resolver := deps.NewResolver(cfg, logger)

	// Get all app names and resolve order
	appNames := cfg.AppNames()
	ordered, err := resolver.ResolveDependencyOrder(appNames)
	if err != nil {
		return fmt.Errorf("failed to resolve dependency order: %w", err)
	}

	logger.Info("Deploying all apps in order", zap.Strings("order", ordered))

	for _, appName := range ordered {
		if err := deploySingleApp(appName); err != nil {
			return fmt.Errorf("failed to deploy %s: %w", appName, err)
		}
	}

	logger.Info("All apps deployed successfully")
	return nil
}

func deploySingleApp(appName string) error {
	startTime := time.Now()

	app, err := cfg.GetApp(appName)
	if err != nil {
		return fmt.Errorf("app '%s' not found. Available apps: %v", appName, cfg.AppNames())
	}

	// Override image if specified
	if deployImage != "" {
		app.Image = deployImage
	}

	server, err := app.GetServer(cfg)
	if err != nil {
		return err
	}

	logger.Info("Deploying app",
		zap.String("app", appName),
		zap.String("type", string(app.Type)),
		zap.String("server", server.Name))

	// Create hook runner and environment
	hookRunner := hooks.NewRunner(cfg.Hooks, logger)
	hookEnv := hooks.Env{
		FerryVersion:   Version,
		FerryPerformer: getCurrentUser(),
		FerryApp:       appName,
		FerryServer:    server.Name,
		FerryImage:     app.Image,
	}

	// Run pre-build hook (local)
	if err := hookRunner.RunLocal(config.HookPreBuild, app.Hooks, hookEnv); err != nil {
		return fmt.Errorf("pre-build hook failed: %w", err)
	}

	executor, err := exec.NewSSHExecutor(*server)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
	}
	defer executor.Close()

	// Wait for dependencies
	if len(app.DependsOn) > 0 {
		resolver := deps.NewResolver(cfg, logger)
		if err := resolver.WaitForDependencies(executor, app, deployTimeout); err != nil {
			return fmt.Errorf("dependency wait failed: %w", err)
		}
	}

	// Create app directory
	appDir := fmt.Sprintf("~/%s", app.Name)
	executor.Run(fmt.Sprintf("mkdir -p %s", appDir))

	// Pull or build image
	if app.DeployMethod == "build" {
		if err := buildAndPushImage(executor, app, server.Host, server.User, appDir); err != nil {
			return fmt.Errorf("failed to build image: %w", err)
		}
	} else {
		logger.Info("Pulling image", zap.String("image", app.Image))
		if _, err := executor.Run(fmt.Sprintf("docker pull %s", app.Image)); err != nil {
			return fmt.Errorf("failed to pull image: %w", err)
		}
	}

	// Run pre-deploy hook (remote)
	if err := hookRunner.RunRemote(executor, config.HookPreDeploy, app.Hooks, hookEnv); err != nil {
		return fmt.Errorf("pre-deploy hook failed: %w", err)
	}

	// Deploy based on app type
	switch app.Type {
	case config.AppTypeJob:
		jobRunner := jobs.NewRunner(logger)
		if err := jobRunner.Run(executor, app, deployTimeout); err != nil {
			return fmt.Errorf("job execution failed: %w", err)
		}
	case config.AppTypeService:
		if err := deployService(executor, app); err != nil {
			return fmt.Errorf("service deployment failed: %w", err)
		}
	default: // AppTypeApp
		p, err := proxy.New(cfg.Proxy)
		if err != nil {
			return err
		}
		if err := deployApp(executor, app, p); err != nil {
			return fmt.Errorf("app deployment failed: %w", err)
		}
	}

	// Run post-deploy hook (remote)
	hookEnv.FerryRuntime = time.Since(startTime)
	if err := hookRunner.RunRemote(executor, config.HookPostDeploy, app.Hooks, hookEnv); err != nil {
		return fmt.Errorf("post-deploy hook failed: %w", err)
	}

	// Wait for health check and run post-app-boot hook (for app and service types)
	if app.Type != config.AppTypeJob {
		if err := waitForHealthy(executor, app.Name, deployTimeout); err != nil {
			logger.Warn("Health check did not pass", zap.Error(err))
		} else {
			// Run post-app-boot hook (remote)
			hookEnv.FerryRuntime = time.Since(startTime)
			if err := hookRunner.RunRemote(executor, config.HookPostAppBoot, app.Hooks, hookEnv); err != nil {
				return fmt.Errorf("post-app-boot hook failed: %w", err)
			}
		}
	}

	// Cleanup
	executor.Run("docker container prune -f")
	executor.Run("docker image prune -f")

	logger.Info("Deployment complete",
		zap.String("app", appName),
		zap.Duration("duration", time.Since(startTime)))
	return nil
}

func deployApp(executor *exec.SSHExecutor, app *config.App, p proxy.Proxy) error {
	stagingContainer := fmt.Sprintf("%s-new", app.Name)
	finalContainer := app.Name

	// Clean up stale staging container from failed deploy
	executor.Run(fmt.Sprintf("docker stop %s 2>/dev/null", stagingContainer))
	executor.Run(fmt.Sprintf("docker rm %s 2>/dev/null", stagingContainer))

	logger.Info("Deploying container", zap.String("container", stagingContainer))

	// Create networks
	executor.Run(fmt.Sprintf("docker network create --attachable %s 2>/dev/null", p.Network()))
	for _, network := range app.Networks {
		if network != p.Network() {
			executor.Run(fmt.Sprintf("docker network create --attachable %s 2>/dev/null", network))
		}
	}

	// Run staging container
	runCmd := buildDockerRunCommand(app, stagingContainer, p)
	if _, err := executor.Run(runCmd); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Connect to additional networks
	for _, network := range app.Networks {
		if network != p.Network() {
			executor.Run(fmt.Sprintf("docker network connect %s %s 2>/dev/null", network, stagingContainer))
		}
	}

	// Stop and remove old container to free the name
	executor.Run(fmt.Sprintf("docker stop %s 2>/dev/null", finalContainer))
	executor.Run(fmt.Sprintf("docker rm %s 2>/dev/null", finalContainer))

	// Rename staging to final
	if _, err := executor.Run(fmt.Sprintf("docker rename %s %s", stagingContainer, finalContainer)); err != nil {
		return fmt.Errorf("failed to rename container: %w", err)
	}

	return nil
}

func deployService(executor *exec.SSHExecutor, app *config.App) error {
	stagingContainer := fmt.Sprintf("%s-new", app.Name)
	finalContainer := app.Name

	// Clean up stale staging container from failed deploy
	executor.Run(fmt.Sprintf("docker stop %s 2>/dev/null", stagingContainer))
	executor.Run(fmt.Sprintf("docker rm %s 2>/dev/null", stagingContainer))

	logger.Info("Deploying service", zap.String("container", stagingContainer))

	// Create networks for service
	for _, network := range app.Networks {
		executor.Run(fmt.Sprintf("docker network create --attachable %s 2>/dev/null", network))
	}

	// Run staging container (no proxy labels)
	runCmd := buildServiceRunCommand(app, stagingContainer)
	if _, err := executor.Run(runCmd); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Connect to additional networks
	for i, network := range app.Networks {
		if i > 0 { // First network is already connected via --network
			executor.Run(fmt.Sprintf("docker network connect %s %s 2>/dev/null", network, stagingContainer))
		}
	}

	// Stop and remove old container to free the name
	executor.Run(fmt.Sprintf("docker stop %s 2>/dev/null", finalContainer))
	executor.Run(fmt.Sprintf("docker rm %s 2>/dev/null", finalContainer))

	// Rename staging to final
	if _, err := executor.Run(fmt.Sprintf("docker rename %s %s", stagingContainer, finalContainer)); err != nil {
		return fmt.Errorf("failed to rename container: %w", err)
	}

	return nil
}

func buildDockerRunCommand(app *config.App, containerName string, p proxy.Proxy) string {
	parts := []string{
		"docker run -d",
		fmt.Sprintf("--name %s", containerName),
		fmt.Sprintf("--network %s", p.Network()),
		fmt.Sprintf("--network-alias %s", app.Name),
	}

	// Add health check if using custom test command
	parts = append(parts, buildHealthCheckArgs(app)...)

	// Add environment variables from env file
	if app.EnvFile != "" {
		envVars := loadEnvFile(app.EnvFile)
		for _, env := range envVars {
			parts = append(parts, fmt.Sprintf("--env '%s'", env))
		}
	}

	// Add inline environment variables
	for key, value := range app.Env {
		parts = append(parts, fmt.Sprintf("--env '%s=%s'", key, value))
	}

	// Add volumes
	for _, vol := range app.Volumes {
		parts = append(parts, fmt.Sprintf("--volume %s", vol))
	}

	// Add proxy labels
	for _, label := range p.DockerLabels(*app) {
		parts = append(parts, fmt.Sprintf("--label '%s'", label))
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

func buildServiceRunCommand(app *config.App, containerName string) string {
	parts := []string{
		"docker run -d",
		fmt.Sprintf("--name %s", containerName),
	}

	// Add first network if available
	if len(app.Networks) > 0 {
		parts = append(parts, fmt.Sprintf("--network %s", app.Networks[0]))
	}

	parts = append(parts, fmt.Sprintf("--network-alias %s", app.Name))

	// Add health check if using custom test command
	parts = append(parts, buildHealthCheckArgs(app)...)

	// Add environment variables from env file
	if app.EnvFile != "" {
		envVars := loadEnvFile(app.EnvFile)
		for _, env := range envVars {
			parts = append(parts, fmt.Sprintf("--env '%s'", env))
		}
	}

	// Add inline environment variables
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

func buildHealthCheckArgs(app *config.App) []string {
	var parts []string

	if len(app.Health.Test) > 0 {
		// Convert test array to docker health check format
		// e.g., ["CMD-SHELL", "pg_isready -U app"] -> --health-cmd "pg_isready -U app"
		if len(app.Health.Test) >= 2 && (app.Health.Test[0] == "CMD-SHELL" || app.Health.Test[0] == "CMD") {
			cmd := strings.Join(app.Health.Test[1:], " ")
			parts = append(parts, fmt.Sprintf("--health-cmd '%s'", cmd))
		}
	}

	if app.Health.Interval != "" {
		parts = append(parts, fmt.Sprintf("--health-interval %s", app.Health.Interval))
	}

	if app.Health.Timeout != "" {
		parts = append(parts, fmt.Sprintf("--health-timeout %s", app.Health.Timeout))
	}

	if app.Health.Retries > 0 {
		parts = append(parts, fmt.Sprintf("--health-retries %d", app.Health.Retries))
	}

	if app.Health.StartPeriod != "" {
		parts = append(parts, fmt.Sprintf("--health-start-period %s", app.Health.StartPeriod))
	}

	return parts
}

func waitForHealthy(executor *exec.SSHExecutor, containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	checkInterval := 2 * time.Second

	for time.Now().Before(deadline) {
		// Check if container has health check configured
		cmd := fmt.Sprintf("docker inspect --format='{{.State.Health.Status}}' %s 2>/dev/null", containerName)
		output, err := executor.Run(cmd)
		if err != nil {
			// No health check configured, consider it healthy
			return nil
		}

		status := strings.TrimSpace(output)
		if status == "healthy" {
			return nil
		}
		if status == "" {
			// No health check configured
			return nil
		}

		time.Sleep(checkInterval)
	}

	return fmt.Errorf("timeout waiting for container %s to become healthy", containerName)
}

func loadEnvFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var envVars []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		envVars = append(envVars, line)
	}
	return envVars
}

func buildAndPushImage(executor *exec.SSHExecutor, app *config.App, host, user, appDir string) error {
	logger.Info("Building image locally")

	// Build locally
	buildCmd := osExec.Command("sh", "-c",
		fmt.Sprintf("docker build --platform linux/amd64 %s -f %s -t %s",
			app.DockerContext, app.DockerFile, app.Name))
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("local build failed: %w", err)
	}

	// Save to tar
	imageFile := "image.tar"
	saveCmd := osExec.Command("sh", "-c",
		fmt.Sprintf("docker save -o %s %s", imageFile, app.Name))
	saveCmd.Stdout = os.Stdout
	saveCmd.Stderr = os.Stderr
	if err := saveCmd.Run(); err != nil {
		return fmt.Errorf("failed to save image: %w", err)
	}
	defer os.Remove(imageFile)

	// Copy to server
	scpCmd := osExec.Command("sh", "-c",
		fmt.Sprintf("scp %s %s@%s:%s/%s", imageFile, user, host, appDir, imageFile))
	scpCmd.Stdout = os.Stdout
	scpCmd.Stderr = os.Stderr
	if err := scpCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy image: %w", err)
	}

	// Load on server
	if _, err := executor.Run(fmt.Sprintf("docker load -i %s/%s", appDir, imageFile)); err != nil {
		return fmt.Errorf("failed to load image: %w", err)
	}

	// Tag the image
	if _, err := executor.Run(fmt.Sprintf("docker tag %s %s", app.Name, app.Image)); err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}

	// Cleanup
	executor.Run(fmt.Sprintf("rm %s/%s", appDir, imageFile))

	return nil
}

func getCurrentUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "unknown"
}

func init() {
	deployCmd.Flags().StringVarP(&deployImage, "image", "i", "", "Override the image to deploy")
	deployCmd.Flags().BoolVar(&deployAll, "all", false, "Deploy all apps in dependency order")
	deployCmd.Flags().DurationVar(&deployTimeout, "timeout", 10*time.Minute, "Timeout for dependencies and jobs")
	rootCmd.AddCommand(deployCmd)
}
