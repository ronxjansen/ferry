package deps

import (
	"fmt"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
	"go.uber.org/zap"
)

// Resolver handles service dependencies
type Resolver struct {
	cfg    *config.Config
	logger *zap.Logger
}

// NewResolver creates a new dependency resolver
func NewResolver(cfg *config.Config, logger *zap.Logger) *Resolver {
	return &Resolver{
		cfg:    cfg,
		logger: logger,
	}
}

// WaitForDependencies waits for all dependencies of an app to be satisfied
func (r *Resolver) WaitForDependencies(executor exec.Executor, app *config.App, timeout time.Duration) error {
	for _, dep := range app.DependsOn {
		if err := r.WaitForDependency(executor, dep, timeout); err != nil {
			return fmt.Errorf("dependency %s not satisfied: %w", dep.Service, err)
		}
	}
	return nil
}

// WaitForDependency waits for a single dependency condition to be satisfied
func (r *Resolver) WaitForDependency(executor exec.Executor, dep config.Dependency, timeout time.Duration) error {
	r.logger.Info("Waiting for dependency",
		zap.String("service", dep.Service),
		zap.String("condition", string(dep.Condition)))

	deadline := time.Now().Add(timeout)
	checkInterval := 2 * time.Second

	for time.Now().Before(deadline) {
		satisfied, err := r.checkCondition(executor, dep)
		if err != nil {
			r.logger.Debug("Dependency check error", zap.Error(err))
		}
		if satisfied {
			r.logger.Info("Dependency satisfied", zap.String("service", dep.Service))
			return nil
		}
		time.Sleep(checkInterval)
	}

	return fmt.Errorf("timeout waiting for %s to satisfy condition %s", dep.Service, dep.Condition)
}

func (r *Resolver) checkCondition(executor exec.Executor, dep config.Dependency) (bool, error) {
	switch dep.Condition {
	case config.ConditionStarted:
		return r.isServiceStarted(executor, dep.Service)
	case config.ConditionHealthy:
		return r.isServiceHealthy(executor, dep.Service)
	case config.ConditionCompleted:
		return r.isServiceCompleted(executor, dep.Service)
	default:
		return false, fmt.Errorf("unknown condition: %s", dep.Condition)
	}
}

func (r *Resolver) isServiceStarted(executor exec.Executor, service string) (bool, error) {
	cmd := fmt.Sprintf("docker ps --filter name=^%s$ --format '{{.Names}}'", service)
	output, err := executor.Run(cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == service, nil
}

func (r *Resolver) isServiceHealthy(executor exec.Executor, service string) (bool, error) {
	cmd := fmt.Sprintf("docker inspect --format='{{.State.Health.Status}}' %s 2>/dev/null", service)
	output, err := executor.Run(cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "healthy", nil
}

func (r *Resolver) isServiceCompleted(executor exec.Executor, service string) (bool, error) {
	// Check if container exists and has exited
	stateCmd := fmt.Sprintf("docker inspect --format='{{.State.Status}}' %s 2>/dev/null", service)
	stateOutput, err := executor.Run(stateCmd)
	if err != nil {
		return false, err
	}

	if strings.TrimSpace(stateOutput) != "exited" {
		return false, nil
	}

	// Check exit code
	exitCmd := fmt.Sprintf("docker inspect --format='{{.State.ExitCode}}' %s 2>/dev/null", service)
	exitOutput, err := executor.Run(exitCmd)
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(exitOutput) == "0", nil
}

// ResolveDependencyOrder returns apps in topological order for deploy --all
func (r *Resolver) ResolveDependencyOrder(appNames []string) ([]string, error) {
	// Build adjacency list and in-degree map
	graph := make(map[string][]string)      // app -> apps that depend on it
	inDegree := make(map[string]int)        // app -> number of dependencies

	// Initialize
	appSet := make(map[string]bool)
	for _, name := range appNames {
		appSet[name] = true
		inDegree[name] = 0
		graph[name] = []string{}
	}

	// Build graph from dependencies
	for _, name := range appNames {
		app, err := r.cfg.GetApp(name)
		if err != nil {
			return nil, err
		}

		for _, dep := range app.DependsOn {
			if !appSet[dep.Service] {
				return nil, fmt.Errorf("app %s depends on %s which is not in the deployment list", name, dep.Service)
			}
			graph[dep.Service] = append(graph[dep.Service], name)
			inDegree[name]++
		}
	}

	// Kahn's algorithm for topological sort
	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	var result []string
	for len(queue) > 0 {
		// Pop from queue
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// Reduce in-degree for dependents
		for _, dependent := range graph[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(result) != len(appNames) {
		return nil, fmt.Errorf("circular dependency detected in app configuration")
	}

	return result, nil
}
