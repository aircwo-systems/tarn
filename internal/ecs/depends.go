// depends.go implements container dependsOn validation (RegisterTaskDefinition
// time) and the launch-time wait each dependent container performs before
// Runner.startContainer is called for it. See docs/design/ecs-support.md and
// the AWS ECS ContainerDependency shape this mirrors: START (dependency has
// been started), COMPLETE (dependency exited, any code), SUCCESS (dependency
// exited 0), HEALTHY (dependency's Docker health check reports healthy).
package ecs

import (
	"context"
	"fmt"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// dependsOnPollInterval is how often waitForDependency re-checks the
// dependency container's recorded state. A package var so tests can shrink
// it instead of sleeping on the real interval.
var dependsOnPollInterval = 25 * time.Millisecond

// dependsOnWaitTimeout bounds how long a dependent container waits for its
// dependency before giving up. In practice this is rarely reached: the
// launch context is cancelled immediately by Stop() (see launchContext /
// context.AfterFunc), so this is a backstop against a dependency that is
// simply never going to satisfy its condition (e.g. its own image pull hangs
// forever with no context cancellation in play).
var dependsOnWaitTimeout = 10 * time.Minute

// validateDependsOn checks every container definition's DependsOn entries
// for an unknown container name, an unknown condition, a self-dependency, and
// a dependency cycle. AWS rejects all of these at RegisterTaskDefinition time
// with a ClientException, so this does too — it is intentionally not part of
// invalidParameterError's InvalidParameterException family.
func validateDependsOn(cds []types.ContainerDefinition, names map[string]bool) *ServiceError {
	adjacency := make(map[string][]string, len(cds))
	for _, cd := range cds {
		for _, dep := range cd.DependsOn {
			if !names[dep.ContainerName] {
				return clientError("container %s depends on unknown container %s", cd.Name, dep.ContainerName)
			}
			if dep.ContainerName == cd.Name {
				return clientError("container %s cannot depend on itself", cd.Name)
			}
			switch dep.Condition {
			case types.ContainerConditionStart, types.ContainerConditionComplete,
				types.ContainerConditionSuccess, types.ContainerConditionHealthy:
			default:
				return clientError("container %s: unsupported dependsOn condition %q", cd.Name, dep.Condition)
			}
			adjacency[cd.Name] = append(adjacency[cd.Name], dep.ContainerName)
		}
	}
	if cycle := findDependencyCycle(adjacency); cycle != "" {
		return clientError("container dependency cycle detected: %s", cycle)
	}
	return nil
}

// findDependencyCycle runs a DFS over the dependsOn graph and returns a
// human-readable description of the first cycle found, or "" if the graph is
// acyclic.
func findDependencyCycle(adjacency map[string][]string) string {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(adjacency))
	var path []string

	var visit func(node string) string
	visit = func(node string) string {
		switch state[node] {
		case visiting:
			path = append(path, node)
			return joinCycle(path)
		case done:
			return ""
		}
		state[node] = visiting
		path = append(path, node)
		for _, dep := range adjacency[node] {
			if cycle := visit(dep); cycle != "" {
				return cycle
			}
		}
		path = path[:len(path)-1]
		state[node] = done
		return ""
	}

	for node := range adjacency {
		if state[node] == unvisited {
			if cycle := visit(node); cycle != "" {
				return cycle
			}
		}
	}
	return ""
}

func joinCycle(path []string) string {
	out := path[0]
	for _, p := range path[1:] {
		out += " -> " + p
	}
	return out
}

// waitForDependencies blocks until every DependsOn entry on cd is satisfied,
// polling the task record (not Docker directly — the record is what every
// other piece of container-status bookkeeping already updates). Returns nil
// immediately when cd has no dependencies.
func (r *Runner) waitForDependencies(ctx context.Context, taskArn string, cd types.ContainerDefinition) error {
	for _, dep := range cd.DependsOn {
		if err := r.waitForDependency(ctx, taskArn, dep); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) waitForDependency(ctx context.Context, taskArn string, dep types.ContainerDependency) error {
	ticker := time.NewTicker(dependsOnPollInterval)
	defer ticker.Stop()
	deadline := time.Now().Add(dependsOnWaitTimeout)

	for {
		task, err := r.svc.GetTask(taskArn)
		if err != nil {
			return fmt.Errorf("resolve dependency container %s: %w", dep.ContainerName, err)
		}
		container := findTaskContainer(task, dep.ContainerName)
		if container == nil {
			return fmt.Errorf("dependency container %s not found on task", dep.ContainerName)
		}

		switch dep.Condition {
		case types.ContainerConditionStart:
			if container.LastStatus != "" && container.LastStatus != types.TaskStatusProvisioning {
				return nil
			}
		case types.ContainerConditionComplete:
			if container.LastStatus == types.TaskStatusStopped {
				return nil
			}
		case types.ContainerConditionSuccess:
			if container.LastStatus == types.TaskStatusStopped {
				if container.ExitCode != nil && *container.ExitCode == 0 {
					return nil
				}
				return fmt.Errorf("dependency container %s did not exit successfully (condition SUCCESS)", dep.ContainerName)
			}
		case types.ContainerConditionHealthy:
			if container.HealthStatus == types.HealthStatusHealthy {
				return nil
			}
		default:
			// Unreachable in practice: validateDependsOn rejects unknown
			// conditions at RegisterTaskDefinition time.
			return fmt.Errorf("unsupported dependsOn condition %q", dep.Condition)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for dependency container %s (condition %s)", dep.ContainerName, dep.Condition)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func findTaskContainer(task *types.Task, name string) *types.TaskContainer {
	if task == nil {
		return nil
	}
	for i := range task.Containers {
		if task.Containers[i].Name == name {
			return &task.Containers[i]
		}
	}
	return nil
}
