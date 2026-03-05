package hooks

import (
	"fmt"
	"os"
	osExec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
	"go.uber.org/zap"
)

// Env contains variables passed to hooks
type Env struct {
	FerryVersion   string
	FerryPerformer string
	FerryApp       string
	FerryServer    string
	FerryImage     string
	FerryRuntime   time.Duration // Elapsed time (post hooks only)
}

// Runner executes lifecycle hooks
type Runner struct {
	globalHooks map[config.HookType]config.Hook
	logger      *zap.Logger
}

// NewRunner creates a new hook runner
func NewRunner(globalHooks map[config.HookType]config.Hook, logger *zap.Logger) *Runner {
	if globalHooks == nil {
		globalHooks = make(map[config.HookType]config.Hook)
	}
	return &Runner{
		globalHooks: globalHooks,
		logger:      logger,
	}
}

// RunLocal executes hook on local machine (pre_build)
func (r *Runner) RunLocal(hookType config.HookType, appHooks map[config.HookType]config.Hook, env Env) error {
	// Run global hooks first
	if hook, ok := r.globalHooks[hookType]; ok {
		if err := r.executeLocalHook(hookType, hook, env); err != nil {
			return err
		}
	}

	// Run app-specific hooks
	if appHooks != nil {
		if hook, ok := appHooks[hookType]; ok {
			if err := r.executeLocalHook(hookType, hook, env); err != nil {
				return err
			}
		}
	}

	return nil
}

// RunRemote executes hook on remote server via SSH
func (r *Runner) RunRemote(executor exec.Executor, hookType config.HookType, appHooks map[config.HookType]config.Hook, env Env) error {
	// Run global hooks first
	if hook, ok := r.globalHooks[hookType]; ok {
		if err := r.executeRemoteHook(executor, hookType, hook, env); err != nil {
			return err
		}
	}

	// Run app-specific hooks
	if appHooks != nil {
		if hook, ok := appHooks[hookType]; ok {
			if err := r.executeRemoteHook(executor, hookType, hook, env); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *Runner) executeLocalHook(hookType config.HookType, hook config.Hook, env Env) error {
	envVars := r.buildEnvVars(env)

	// Check for script file first
	if hook.Script != "" {
		return r.runLocalScript(hookType, hook.Script, envVars)
	}

	// Check for .ferry/hooks/<hook-type> script
	hookScript := filepath.Join(".ferry", "hooks", string(hookType))
	if _, err := os.Stat(hookScript); err == nil {
		return r.runLocalScript(hookType, hookScript, envVars)
	}

	// Run inline commands
	for _, cmd := range hook.Commands {
		r.logger.Info("Running local hook", zap.String("hook", string(hookType)), zap.String("cmd", cmd))

		c := osExec.Command("sh", "-c", cmd)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Env = append(os.Environ(), envVars...)

		if err := c.Run(); err != nil {
			return fmt.Errorf("hook %s failed: %w", hookType, err)
		}
	}

	return nil
}

func (r *Runner) runLocalScript(hookType config.HookType, scriptPath string, envVars []string) error {
	r.logger.Info("Running local hook script", zap.String("hook", string(hookType)), zap.String("script", scriptPath))

	c := osExec.Command(scriptPath)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Env = append(os.Environ(), envVars...)

	if err := c.Run(); err != nil {
		return fmt.Errorf("hook script %s failed: %w", scriptPath, err)
	}
	return nil
}

func (r *Runner) executeRemoteHook(executor exec.Executor, hookType config.HookType, hook config.Hook, env Env) error {
	envExport := r.buildEnvExport(env)

	// Check for script file first
	if hook.Script != "" {
		return r.runRemoteScript(executor, hookType, hook.Script, envExport)
	}

	// Run inline commands
	for _, cmd := range hook.Commands {
		r.logger.Info("Running remote hook", zap.String("hook", string(hookType)), zap.String("cmd", cmd))

		fullCmd := fmt.Sprintf("%s%s", envExport, cmd)
		if _, err := executor.Run(fullCmd); err != nil {
			return fmt.Errorf("hook %s failed: %w", hookType, err)
		}
	}

	return nil
}

func (r *Runner) runRemoteScript(executor exec.Executor, hookType config.HookType, scriptPath string, envExport string) error {
	r.logger.Info("Running remote hook script", zap.String("hook", string(hookType)), zap.String("script", scriptPath))

	// Read local script and execute it remotely
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("failed to read hook script %s: %w", scriptPath, err)
	}

	// Execute script content remotely
	cmd := fmt.Sprintf("%scat <<'FERRY_HOOK_EOF' | sh\n%s\nFERRY_HOOK_EOF", envExport, string(content))
	if _, err := executor.Run(cmd); err != nil {
		return fmt.Errorf("hook script %s failed: %w", scriptPath, err)
	}

	return nil
}

func (r *Runner) buildEnvVars(env Env) []string {
	vars := []string{
		fmt.Sprintf("FERRY_VERSION=%s", env.FerryVersion),
		fmt.Sprintf("FERRY_PERFORMER=%s", env.FerryPerformer),
		fmt.Sprintf("FERRY_APP=%s", env.FerryApp),
		fmt.Sprintf("FERRY_SERVER=%s", env.FerryServer),
		fmt.Sprintf("FERRY_IMAGE=%s", env.FerryImage),
	}
	if env.FerryRuntime > 0 {
		vars = append(vars, fmt.Sprintf("FERRY_RUNTIME=%s", env.FerryRuntime.String()))
	}
	return vars
}

func (r *Runner) buildEnvExport(env Env) string {
	vars := r.buildEnvVars(env)
	exports := make([]string, len(vars))
	for i, v := range vars {
		exports[i] = fmt.Sprintf("export %s", v)
	}
	return strings.Join(exports, "; ") + "; "
}
