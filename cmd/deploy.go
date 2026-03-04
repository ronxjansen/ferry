package cmd

import (
	"fmt"
	"os"
	osExec "os/exec"
	"strings"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
	"github.com/ronxjansen/ferry/internal/proxy"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	deployImage string
)

var deployCmd = &cobra.Command{
	Use:   "deploy <app>",
	Short: "Deploy an application",
	Long:  `Deploy an application to its configured server`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]

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

		logger.Info("Deploying app", zap.String("app", appName), zap.String("server", server.Name))

		executor, err := exec.NewSSHExecutor(*server)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %w", server.Name, err)
		}
		defer executor.Close()

		// Create proxy for labels
		p, err := proxy.New(cfg.Proxy)
		if err != nil {
			return err
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

		stagingContainer := fmt.Sprintf("%s-new", app.Name)
		finalContainer := app.Name

		// Clean up stale staging container from failed deploy
		executor.Run(fmt.Sprintf("docker stop %s 2>/dev/null", stagingContainer))
		executor.Run(fmt.Sprintf("docker rm %s 2>/dev/null", stagingContainer))

		logger.Info("Deploying container", zap.String("container", stagingContainer))

		// Create networks
		executor.Run(fmt.Sprintf("docker network create --attachable %s", p.Network()))
		for _, network := range app.Networks {
			if network != p.Network() {
				executor.Run(fmt.Sprintf("docker network create --attachable %s", network))
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
				executor.Run(fmt.Sprintf("docker network connect %s %s", network, stagingContainer))
			}
		}

		// Stop and remove old container to free the name
		executor.Run(fmt.Sprintf("docker stop %s 2>/dev/null", finalContainer))
		executor.Run(fmt.Sprintf("docker rm %s 2>/dev/null", finalContainer))

		// Rename staging to final
		if _, err := executor.Run(fmt.Sprintf("docker rename %s %s", stagingContainer, finalContainer)); err != nil {
			return fmt.Errorf("failed to rename container: %w", err)
		}

		// Cleanup
		executor.Run("docker container prune -f")
		executor.Run("docker image prune -f")

		logger.Info("Deployment complete", zap.String("app", appName), zap.String("container", finalContainer))
		return nil
	},
}


func buildDockerRunCommand(app *config.App, containerName string, p proxy.Proxy) string {
	parts := []string{
		"docker run -d",
		fmt.Sprintf("--name %s", containerName),
		fmt.Sprintf("--network %s", p.Network()),
		fmt.Sprintf("--network-alias %s", app.Name),
	}

	// Add environment variables from env file
	if app.EnvFile != "" {
		envVars := loadEnvFile(app.EnvFile)
		for _, env := range envVars {
			parts = append(parts, fmt.Sprintf("--env '%s'", env))
		}
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

	return strings.Join(parts, " ")
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

func init() {
	deployCmd.Flags().StringVarP(&deployImage, "image", "i", "", "Override the image to deploy")
	rootCmd.AddCommand(deployCmd)
}
