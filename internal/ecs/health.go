// health.go polls Docker's HEALTHCHECK result for containers that define one
// and reflects it onto the task record, replacing a service task whose
// essential container goes UNHEALTHY. See docs/design/ecs-support.md.
package ecs

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// healthPollInterval is how often a container's Docker health status is
// re-read. A package var so tests can shrink it instead of sleeping on the
// real interval.
var healthPollInterval = 500 * time.Millisecond

// spawnHealthPoller starts a background poller for one container's Docker
// HEALTHCHECK result, tracked by r.wg like every other lifecycle goroutine so
// Stop() waits for it to exit. It is a no-op when cd defines no HealthCheck.
// containerID is captured by value at call time. exited is
// spawnLifecycle's return channel for the same container: the poller
// selects on it (alongside r.lifecycleCtx.Done() and the engine reporting an
// inspect error) so it stops promptly once the container's own wait
// goroutine observes it exit, rather than only on Stop()'s lifecycleCtx
// cancellation — which Stop() doesn't fire until after waiting (bounded by
// stopContainerTimeout) for every r.wg goroutine, this poller included, to
// finish. Without exited, a poller with nothing else to wake it would tick
// for the full timeout after a normal exit.
func (r *Runner) spawnHealthPoller(taskArn, containerName, containerID string, rt *runningTask, exited <-chan struct{}) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(healthPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-r.lifecycleCtx.Done():
				return
			case <-exited:
				return
			case <-ticker.C:
			}

			dockerStatus, err := r.eng.InspectContainerHealth(r.lifecycleCtx, containerID)
			if err != nil {
				// Container likely exited/was removed; its own wait goroutine
				// (spawnLifecycle) owns recording that, not this poller.
				return
			}
			status := mapDockerHealthStatus(dockerStatus)
			if status == "" {
				continue
			}

			task, err := r.svc.SetContainerHealthStatus(taskArn, containerName, status)
			if err != nil {
				return
			}

			if status == types.HealthStatusUnhealthy {
				r.handleUnhealthyContainer(taskArn, containerName, rt)
				return
			}
			if task != nil && task.LastStatus == types.TaskStatusStopped {
				return
			}
		}
	}()
}

// mapDockerHealthStatus translates Docker's inspect Health.Status values
// into the AWS ECS HealthStatus vocabulary. "starting" (Docker's grace
// period before the first check) and any unrecognised value map to UNKNOWN,
// matching how ECS reports a container before its first successful check.
func mapDockerHealthStatus(dockerStatus string) string {
	switch dockerStatus {
	case "healthy":
		return types.HealthStatusHealthy
	case "unhealthy":
		return types.HealthStatusUnhealthy
	case "starting":
		return types.HealthStatusUnknown
	default:
		return ""
	}
}

// handleUnhealthyContainer stops (and, for a service-owned task, lets the
// reconcile loop replace) a task whose essential container just reported
// UNHEALTHY, matching AWS's stoppedReason for this case. A non-essential
// container going unhealthy is recorded (via SetContainerHealthStatus) but
// does not stop the task — AWS's essential-container rule, same as exit-code
// handling elsewhere in this package.
func (r *Runner) handleUnhealthyContainer(taskArn, containerName string, rt *runningTask) {
	if rt == nil || (rt.essential != nil && !rt.essential[containerName]) {
		return
	}
	if _, err := r.svc.StopTaskRecord(taskArn, "Task failed container health checks"); err != nil {
		log.Printf("[ecs] mark task %s stopped after health check failure: %v", taskArn, err)
		return
	}
	r.markRunningTaskStopping(rt)
	r.stopContainerIDs(context.Background(), rt, r.taskContainerIDs(rt), fmt.Sprintf("(health check failed %s)", taskArn))
}
