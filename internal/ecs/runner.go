// Package ecs runner.go implements types.TaskRunner: the concrete component
// that actually runs containers, the per-container log pump, and the
// per-account reconcile loop that keeps ECS services at DesiredCount. See
// docs/design/ecs-support.md section 2 ("Task runner").
//
// This file owns the container; internal/ecs/service.go owns the Task
// record. Every state transition on a Task/TaskContainer goes through the
// Service's exported Set*/NewTaskRecord/StopTaskRecord methods so the two
// stay consistent under concurrent callers (RunTask calls, the reconcile
// loop, and the per-container wait goroutines all mutate records at once).
package ecs

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/engine"
	"github.com/aircwo-systems/tarn/internal/logs"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

const (
	// stopContainerTimeout bounds Stop()'s attempt to stop every tracked
	// container when the runner itself is shutting down.
	stopContainerTimeout = 10 * time.Second
	// stopTimeoutGracePeriod bounds how long past the longest SIGTERM grace
	// stopContainerIDs waits. With all-default timeouts the context is
	// 5s + 5s = 10s, exactly the previous stopContainerTimeout behavior.
	stopTimeoutGracePeriod = 5 * time.Second
	maxRunTaskCount        = 10
)

var (
	// reconcileTickInterval is how often the reconcile loop re-checks every
	// service's RUNNING count against its DesiredCount. A package var (not
	// a const) so tests can shrink it instead of sleeping on the real
	// interval.
	reconcileTickInterval = 2 * time.Second

	// logDrainGracePeriod bounds how long the per-container wait goroutine
	// waits for the log pump to finish on its own after the container exits,
	// before forcing it closed. Docker's follow stream normally ends shortly
	// after the container stops, but this keeps cleanup from hanging forever
	// against a stream Docker never closes. A package var for the same
	// test-speed reason as reconcileTickInterval.
	logDrainGracePeriod = 2 * time.Second

	// stoppedTaskRetention keeps enough history for normal DescribeTasks calls
	// without allowing repeated launch failures to grow the JSON store forever.
	stoppedTaskRetention     = 5 * time.Minute
	serviceLaunchBackoffBase = 1 * time.Second
	serviceLaunchBackoffMax  = 1 * time.Minute

	// inactiveServiceRetention keeps a DeleteService tombstone (see
	// Service.tombstoneServiceLocked) describable long enough for Terraform's
	// post-destroy DescribeServices poll to observe status INACTIVE, without
	// keeping every deleted service's record forever. A package var for the
	// same test-speed reason as reconcileTickInterval.
	inactiveServiceRetention = 1 * time.Hour

	// serviceStabilityWindow is how long a service task must stay RUNNING
	// before onContainerFinished treats its exit as healthy (and resets
	// launch backoff) rather than as a crash. A package var for the same
	// test-speed reason as reconcileTickInterval.
	serviceStabilityWindow = 30 * time.Second
)

// taskEngine is the narrow slice of internal/engine's Docker primitives the
// runner needs, defined here (the consumer) rather than depended on as a
// concrete *engine.Engine, so it is fakeable with no Docker daemon. Every
// method mirrors one exported on *engine.Engine in internal/engine/task.go.
type taskEngine interface {
	EnsureImageRef(ctx context.Context, ref string) error
	CreateAndStartTaskContainer(ctx context.Context, spec engine.TaskContainerSpec) (*engine.TaskContainerHandle, error)
	WaitTaskContainer(ctx context.Context, containerID string) (int64, error)
	FollowContainerLogs(ctx context.Context, containerID string, onLine func(line string, stderr bool)) error
	ListContainersByLabel(ctx context.Context, selector map[string]string) ([]engine.TaskContainerSummary, error)
	RemoveTaskContainer(ctx context.Context, containerID string) error
	StopContainer(ctx context.Context, containerID string, timeoutSec int) error
	// InspectContainerHealth returns Docker's inspect Health.Status
	// ("starting"/"healthy"/"unhealthy"), or "" when the container defines
	// no HEALTHCHECK. Only called for containers whose ContainerDefinition
	// sets HealthCheck (see spawnHealthPoller in health.go).
	InspectContainerHealth(ctx context.Context, containerID string) (string, error)

	// EnsureTaskVolume, TaskVolumeExists, RemoveTaskVolume and
	// ListTaskVolumesByLabel back the ECS Volume/DockerVolumeConfiguration
	// resolution in resolveAndEnsureVolumes and finishTask's task-scoped
	// volume cleanup. Every method mirrors one exported on *engine.Engine.
	EnsureTaskVolume(ctx context.Context, name, driver string, driverOpts, labels map[string]string) error
	TaskVolumeExists(ctx context.Context, name string) (bool, error)
	RemoveTaskVolume(ctx context.Context, name string) error
	ListTaskVolumesByLabel(ctx context.Context, selector map[string]string) ([]engine.TaskVolumeSummary, error)
}

// logSink is the narrow logging dependency the runner needs. It matches
// *logs.Service's actual signatures exactly (no error return), so
// *logs.Service satisfies it with no adapter. Deliberately not
// logs.Service.IngestContainerLogs: that method hardcodes the
// "/aws/lambda/<name>" group prefix and runs a Lambda RIE line classifier
// that ECS containers never emit (see docs/design/ecs-support.md).
type logSink interface {
	CreateLogGroup(name string)
	PutLogEvents(groupName, streamName string, events []logs.LogEvent)
}

// runningTask is the runner's own in-memory bookkeeping for one in-flight
// task: the Docker container ID per container name, rebuilt from persisted
// RuntimeId values or Docker labels after a restart, and how many containers
// have yet to exit before the task record can flip to STOPPED.
type runningTask struct {
	mu             sync.Mutex
	remaining      int
	launchComplete bool
	stopRequested  bool
	serviceKey     string
	containerIDs   map[string]string // container name -> Docker container ID

	// The fields below are captured once at launch (cluster/task definition
	// are not otherwise available to finishTask/onContainerFinished) so the
	// RUNNING and STOPPED traces recorded from this task's lifecycle can
	// describe it without another store round trip.
	correlationID     string
	clusterName       string
	spanName          string // service name for service-owned tasks, else task definition family
	group             string
	taskDefinitionArn string
	// essential records, per container name, whether the task definition
	// marks it essential (nil/true means essential — AWS's default). Traces
	// only fail the task's status over an essential container's exit code.
	essential map[string]bool
	// stopTimeouts records, per container name, the task definition's
	// StopTimeout in seconds. Only positive values are kept; anything
	// absent means the engine default. Captured at launch so stops honor
	// it even if the definition changes later.
	stopTimeouts map[string]int
	// taskVolumeNames lists the Docker named volumes resolveAndEnsureVolumes
	// created with "task" scope for this task, so finishTask can remove them
	// once every container has stopped. Shared-scope volumes are never
	// listed here — they must outlive this task.
	taskVolumeNames []string
}

type launchBackoffState struct {
	failures  int
	nextRetry time.Time
}

// serviceGate is the per-service equivalent of the account-wide launchGate.
// DrainService takes its write lock for the duration of the drain, which
// waits out a launch already in flight for the same service, without blocking
// every other service's launches on the account for however long this one
// drain takes. Launches that arrive after the drain are rejected by checking
// the service record (DRAINING or deleted), not by state on the gate, so a
// service recreated under the same name can launch again.
type serviceGate struct {
	mu sync.RWMutex
}

// Runner is the concrete types.TaskRunner implementation: it creates and
// starts containers for RunTask, stops them for StopTask, and runs the
// reconcile loop that keeps ECS services at DesiredCount.
type Runner struct {
	cfg  *config.Config
	svc  *Service
	eng  taskEngine
	logs logSink

	// traceStore records the RUNNING/STOPPED traces for tasks this runner
	// launches. Nil until SetTraceStore is called (mirrors every other
	// service's SetTraceStore); trace recording is a no-op when nil.
	traceStore *tracesvc.Store

	// secretsResolver resolves container definition `secrets` entries into
	// environment values at launch. Nil until SetSecretsResolver is called; a
	// container definition with no `secrets` never consults it.
	secretsResolver SecretsResolver

	// ctx is the runner's own lifetime context. It is deliberately NOT the
	// ctx passed in to RunTask: that request context dies as soon as the API
	// response is written, long before the container it started has exited.
	// Stop cancels it immediately to abort any launch (image pull / container
	// create) still in flight.
	ctx    context.Context
	cancel context.CancelFunc
	// lifecycleCtx bounds the per-container wait and log-pump goroutines
	// spawned by spawnLifecycle. It is a separate context from ctx above:
	// Stop must keep it alive while it asks Docker to stop every tracked
	// container so WaitTaskContainer observes the container's real exit
	// code, only cancelling it afterward (bounded by the same wg-wait
	// timeout) to unstick anything still stuck. Cancelling it at the same
	// time as ctx — which used to be the case — made WaitTaskContainer
	// return "context canceled" immediately instead of the real exit code,
	// and let RemoveTaskContainer's force removal race the graceful stop.
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	// wg tracks every log-pump and wait goroutine currently in flight, so
	// Stop() can block until all of them have finished cleaning up.
	wg sync.WaitGroup

	reconcileDone     chan struct{}
	reconcileWG       sync.WaitGroup
	reconcileStopOnce sync.Once

	stopOnce  sync.Once
	stopped   atomic.Bool
	startOnce sync.Once
	// launchGate closes the gap between a task launch and its registration in
	// the shutdown snapshot. Stop takes the write lock only after preventing
	// new launches, so it cannot miss a container created by a launch in flight.
	launchGate sync.RWMutex

	mu    sync.Mutex
	tasks map[string]*runningTask // task ARN -> in-flight bookkeeping

	backoffMu     sync.Mutex
	launchBackoff map[string]launchBackoffState

	// serviceGatesMu guards serviceGates; see serviceGate.
	serviceGatesMu sync.Mutex
	serviceGates   map[string]*serviceGate
}

// NewRunner constructs a Runner. eng and sink are narrow interfaces so tests
// can supply fakes; production callers pass *engine.Engine and *logs.Service.
func NewRunner(cfg *config.Config, svc *Service, eng taskEngine, sink logSink) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	return &Runner{
		cfg:             cfg,
		svc:             svc,
		eng:             eng,
		logs:            sink,
		ctx:             ctx,
		cancel:          cancel,
		lifecycleCtx:    lifecycleCtx,
		lifecycleCancel: lifecycleCancel,
		reconcileDone:   make(chan struct{}),
		tasks:           make(map[string]*runningTask),
		launchBackoff:   make(map[string]launchBackoffState),
		serviceGates:    make(map[string]*serviceGate),
	}
}

// SetTraceStore wires the trace store this runner records ECS task
// lifecycle traces into. Safe to leave unset — trace recording is skipped
// entirely when traceStore is nil.
func (r *Runner) SetTraceStore(ts *tracesvc.Store) { r.traceStore = ts }

// Start recovers account-owned Docker task containers, then begins the
// reconcile loop. Recovery runs before the loop starts so reconciliation sees
// the recovered task/container view. Safe to call once; call Stop to halt it.
func (r *Runner) Start() {
	r.startOnce.Do(func() {
		if r.stopped.Load() {
			return
		}
		r.recoverTasks(context.Background())
		if r.stopped.Load() {
			return
		}
		r.reconcileWG.Add(1)
		go r.reconcileLoop()
	})
}

// Stop halts the reconcile loop, then stops every container the runner
// currently tracks, then waits for their wait/log-pump goroutines to finish
// recording state and removing the containers. This ordering is enforced
// internally — not merely documented — specifically so a caller cannot stop
// tasks before the loop, which would otherwise observe the running count
// drop below DesiredCount and restart everything just killed. Stop is safe
// to call more than once; only the first call does anything.
func (r *Runner) Stop() {
	r.stopOnce.Do(func() {
		r.stopped.Store(true)
		// Cancel image pulls and container creates that are still in flight. The
		// launch gate below then waits for those operations to unwind before it
		// snapshots IDs, so this remains bounded by the engine's context handling.
		r.cancel()

		// 1. Halt the reconcile loop and wait for it to actually exit before
		// touching a single container.
		r.reconcileStopOnce.Do(func() { close(r.reconcileDone) })
		r.reconcileWG.Wait()

		// 2. Wait for any RunTask/reconcile launch to finish its critical section,
		// then stop every container the runner is currently tracking.
		r.launchGate.Lock()
		r.mu.Lock()
		stops := make(map[*runningTask][]string)
		var taskArns []string
		for taskArn, rt := range r.tasks {
			taskArns = append(taskArns, taskArn)
			r.markRunningTaskStopping(rt)
			rt.mu.Lock()
			for _, id := range rt.containerIDs {
				stops[rt] = append(stops[rt], id)
			}
			rt.mu.Unlock()
		}
		r.mu.Unlock()
		r.launchGate.Unlock()
		for _, taskArn := range taskArns {
			if _, err := r.svc.StopTaskRecord(taskArn, "ECS runner shutting down"); err != nil {
				log.Printf("[ecs] mark task %s stopped during shutdown: %v", taskArn, err)
			}
		}

		for rt, ids := range stops {
			r.stopContainerIDs(context.Background(), rt, ids, "during shutdown")
		}

		// 3. Wait for every wait/log-pump goroutine to observe the real
		// exit, record it, drain logs and remove the container. lifecycleCtx
		// is deliberately still live here: the wait goroutines are reading
		// it (not ctx, cancelled in step 0) so StopContainer's graceful stop
		// above has a chance to be observed as a real exit code rather than
		// "context canceled".
		done := make(chan struct{})
		go func() {
			r.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(stopContainerTimeout):
			log.Printf("[ecs] timed out waiting for task lifecycle cleanup")
		}

		// Now that every lifecycle goroutine has either finished or been
		// given its bounded chance to, cancel lifecycleCtx too. This is what
		// unsticks a goroutine still stuck past the timeout above (e.g. the
		// engine's graceful stop silently failed to exit the container) —
		// harmless for everything else, which has already exited.
		r.lifecycleCancel()
	})
}

// --- RunTask / StopTask (types.TaskRunner) ----------------------------------

// RunTask resolves the task definition, then launches Count (default 1)
// independent task instances. Each instance's containers are created from
// the task definition layered with Overrides; failures for one instance are
// reported in Failures rather than aborting the others.
func (r *Runner) RunTask(ctx context.Context, in *types.RunTaskInput) (*types.RunTaskOutput, error) {
	if in == nil {
		return nil, invalidParameterError("RunTask input is required")
	}
	if r.stopped.Load() {
		return nil, clientError("ECS runner is shutting down")
	}

	cluster, err := r.svc.ResolveCluster(in.Cluster)
	if err != nil {
		return nil, err
	}
	td, err := r.svc.resolveTaskDefinition(in.TaskDefinition)
	if err != nil {
		return nil, err
	}
	if err := validateTaskDefinition(td); err != nil {
		return nil, invalidParameterError("%v", err)
	}
	if err := validateOverrides(td, in.Overrides); err != nil {
		return nil, err
	}
	if err := validateTags(in.Tags); err != nil {
		return nil, err
	}

	// RunTask's PropagateTags is TASK_DEFINITION or NONE (no SERVICE option —
	// that's CreateService-only, since a standalone RunTask has no owning
	// service to copy tags from). Explicit tags win over a propagated key of
	// the same name.
	var tags []types.Tag
	if strings.EqualFold(in.PropagateTags, "TASK_DEFINITION") {
		tags = mergeTags(td.Tags, in.Tags)
	} else {
		tags = cloneTags(in.Tags)
	}

	count := in.Count
	if count < 0 || count > maxRunTaskCount {
		return nil, invalidParameterError("Count must be between 0 and %d", maxRunTaskCount)
	}
	if count <= 0 {
		count = 1
	}

	out := &types.RunTaskOutput{}
	for i := 0; i < count; i++ {
		// serviceName is empty here: RunTask always launches standalone
		// tasks. Service-owned tasks are launched by the reconcile loop via
		// launchTask directly, with the real service name for labeling.
		task, err := r.launchTaskWithPayload(ctx, cluster, td, in.Overrides, in.EventPayload, in.LaunchType, in.Group, "", in.CorrelationID, tags)
		if err != nil {
			out.Failures = append(out.Failures, types.Failure{
				Reason: "TaskFailedToStart",
				Detail: err.Error(),
			})
			continue
		}
		out.Tasks = append(out.Tasks, *task)
	}
	return out, nil
}

// StopTask stops a task's containers and marks its record stopped with
// reason. It does not itself read the exit code, drain logs or remove the
// container: StopContainer causes the container's already-running wait
// goroutine (spawned by launchTask) to observe the exit, which is where
// that cleanup sequence actually happens — the same path a natural exit
// takes, so there is exactly one place that does it.
func (r *Runner) StopTask(ctx context.Context, cluster, taskArn, reason string) error {
	task, err := r.svc.GetTask(taskArn)
	if err != nil {
		return err
	}

	if strings.TrimSpace(cluster) != "" {
		c, cerr := r.svc.ResolveCluster(cluster)
		if cerr == nil && task.ClusterArn != c.ClusterArn {
			return clientError("task %s is not in cluster %s", taskArn, cluster)
		}
	}

	if _, err := r.svc.StopTaskRecord(task.TaskArn, reason); err != nil {
		return err
	}

	r.mu.Lock()
	rt, ok := r.tasks[task.TaskArn]
	r.mu.Unlock()
	if !ok {
		// Nothing locally tracked (already exited and cleaned up, or this
		// runner process didn't launch it); the record is already marked.
		return nil
	}

	r.markRunningTaskStopping(rt)
	ids := make([]string, 0, len(rt.containerIDs))
	rt.mu.Lock()
	for _, id := range rt.containerIDs {
		ids = append(ids, id)
	}
	rt.mu.Unlock()

	r.stopContainerIDs(ctx, rt, ids, fmt.Sprintf("(task %s)", task.TaskArn))
	return nil
}

// DrainService marks every task owned by a service stopped and waits until no
// matching Docker container remains live. It is the runner half of
// ForceDeleteService: the service record is kept in DRAINING while this runs,
// so a restart cannot re-adopt the service's containers as active work.
//
// It uses a per-service gate (serviceGate), not the account-wide launchGate:
// launchGate is reserved for Stop, which really does need every launch on
// the account halted. A single service's drain must not block launches for
// every other service too — that used to be exactly what happened, so a
// slow image pull for an unrelated service could stall a force-delete's HTTP
// call for as long as the pull took.
func (r *Runner) DrainService(ctx context.Context, cluster *types.Cluster, serviceName string) error {
	if cluster == nil || strings.TrimSpace(serviceName) == "" {
		return fmt.Errorf("cluster and service name are required to drain ECS service")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := serviceKey(cluster.ClusterArn, serviceName)
	gate := r.serviceGate(key)
	gate.mu.Lock()
	defer gate.mu.Unlock()

	group := serviceGroup(serviceName)
	// DeleteService already marks every active task's DesiredStatus STOPPED
	// (and persists it) before calling the drainer, so the old check here
	// ("skip if DesiredStatus is already STOPPED") skipped every task and
	// never actually asked Docker to stop anything through this loop — the
	// container-listing loop below did that instead, but by calling
	// StopContainer directly on whatever Docker reported, bypassing this
	// runner's own runningTask bookkeeping entirely. Mark rt.stopRequested
	// and use the same stopContainerIDs path StopTask uses for whichever
	// tasks this runner still tracks; tasks with no tracked runningTask
	// (e.g. inherited from a crashed process) are covered by the label sweep
	// below regardless.
	for _, task := range r.serviceTasks(cluster.ClusterArn, group) {
		if task.LastStatus == types.TaskStatusStopped {
			continue
		}
		r.mu.Lock()
		rt, tracked := r.tasks[task.TaskArn]
		r.mu.Unlock()
		if !tracked {
			continue
		}
		r.markRunningTaskStopping(rt)
		r.stopContainerIDs(ctx, rt, r.taskContainerIDs(rt), fmt.Sprintf("(drain service %s)", serviceName))
	}

	selector := selectorForService(r.cfg.AccountID, cluster.ClusterName, serviceName)
	for {
		summaries, err := r.eng.ListContainersByLabel(ctx, selector)
		if err != nil {
			return fmt.Errorf("list service containers while draining: %w", err)
		}
		live := make([]engine.TaskContainerSummary, 0, len(summaries))
		for _, summary := range summaries {
			if taskContainerSummaryIsLive(summary) {
				live = append(live, summary)
				continue
			}
			// A service container without a live lifecycle goroutine still needs
			// explicit removal; tracked containers will harmlessly race their
			// normal cleanup path.
			if err := r.eng.RemoveTaskContainer(ctx, summary.ID); err != nil {
				log.Printf("[ecs] drain service %s: remove container %s: %v", serviceName, summary.ID, err)
			}
		}
		if len(live) == 0 {
			return r.waitServiceTasksStopped(ctx, cluster.ClusterArn, group)
		}

		for _, summary := range live {
			if err := r.eng.StopContainer(ctx, summary.ID, 0); err != nil {
				log.Printf("[ecs] drain service %s: stop container %s: %v", serviceName, summary.ID, err)
			}
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// waitServiceTasksStopped brings every one of a drained service's task
// records to STOPPED once Docker reports no live container left. A task this
// runner still tracks (rt in r.tasks) has a lifecycle goroutine that is the
// only place allowed to record its real exit code before flipping it to
// STOPPED (see onContainerFinished/finishTask); forcing STOPPED here first
// would race that goroutine and could leave a container's exit code
// unrecorded. So tracked tasks are waited out, bounded by ctx, instead of
// forced. A task with no tracked runningTask has no such goroutine coming —
// it was never launched by this process, or already fully cleaned up — so it
// is safe, and necessary, to force directly.
func (r *Runner) waitServiceTasksStopped(ctx context.Context, clusterArn, group string) error {
	for _, task := range r.serviceTasks(clusterArn, group) {
		if task.LastStatus == types.TaskStatusStopped {
			continue
		}
		r.mu.Lock()
		_, tracked := r.tasks[task.TaskArn]
		r.mu.Unlock()
		if !tracked {
			if _, err := r.svc.SetTaskStatus(task.TaskArn, types.TaskStatusStopped); err != nil {
				return err
			}
			continue
		}
		if err := r.waitUntilTaskUntracked(ctx, task.TaskArn); err != nil {
			return err
		}
	}
	return nil
}

// waitUntilTaskUntracked blocks until taskArn is no longer in r.tasks (i.e.
// its lifecycle goroutine has run finishTask) or ctx is done.
func (r *Runner) waitUntilTaskUntracked(ctx context.Context, taskArn string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		r.mu.Lock()
		_, tracked := r.tasks[taskArn]
		r.mu.Unlock()
		if !tracked {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// serviceGate returns the per-service drain/launch gate for key, creating it
// on first use.
func (r *Runner) serviceGate(key string) *serviceGate {
	r.serviceGatesMu.Lock()
	defer r.serviceGatesMu.Unlock()
	gate, ok := r.serviceGates[key]
	if !ok {
		gate = &serviceGate{}
		r.serviceGates[key] = gate
	}
	return gate
}

// --- Task launch -------------------------------------------------------------

// launchTask creates a task record, starts every one of its containers, and
// spawns their log pump and wait goroutines. group is the AWS-facing
// Task.Group value (serviceGroup(name) for service-owned tasks; whatever
// the caller supplied otherwise). serviceName is used purely for the
// tarn.service Docker label and is empty for standalone RunTask tasks.
func (r *Runner) launchTask(ctx context.Context, cluster *types.Cluster, td *types.TaskDefinition, overrides *types.TaskOverride, launchType, group, serviceName string, tags []types.Tag) (*types.Task, error) {
	return r.launchTaskWithPayload(ctx, cluster, td, overrides, nil, launchType, group, serviceName, "", tags)
}

func (r *Runner) launchTaskWithPayload(ctx context.Context, cluster *types.Cluster, td *types.TaskDefinition, overrides *types.TaskOverride, eventPayload []byte, launchType, group, serviceName, correlationID string, tags []types.Tag) (*types.Task, error) {
	r.launchGate.RLock()
	defer r.launchGate.RUnlock()
	if r.stopped.Load() {
		return nil, clientError("ECS runner is shutting down")
	}
	// Service-owned launches additionally hold the per-service gate (see
	// serviceGate) for their critical section, so a concurrent DrainService
	// for this same service waits this launch out. DeleteService persists
	// DRAINING before it calls the drainer, so a launch that acquires the gate
	// afterwards sees a non-ACTIVE (or already deleted) record and is rejected.
	if serviceName != "" {
		gate := r.serviceGate(serviceKey(cluster.ClusterArn, serviceName))
		gate.mu.RLock()
		defer gate.mu.RUnlock()
		current, err := r.svc.store.GetService(cluster.ClusterArn, serviceName)
		if err != nil || current.Status != types.ServiceStatusActive {
			return nil, clientError("service %s is not active", serviceName)
		}
	}
	launchCtx, releaseLaunchCtx := r.launchContext(ctx)
	defer releaseLaunchCtx()

	if err := validateTaskDefinition(td); err != nil {
		return nil, invalidParameterError("%v", err)
	}
	if err := validateOverrides(td, overrides); err != nil {
		return nil, err
	}
	resources, err := resolveTaskContainerResources(applyResourceOverrides(td, overrides))
	if err != nil {
		return nil, invalidParameterError("%v", err)
	}

	task, err := r.svc.NewTaskRecord(cluster, td, overrides, launchType, group)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(correlationID) == "" {
		correlationID = tracesvc.NewCorrelationID()
	}
	if _, cErr := r.svc.SetTaskCorrelationID(task.TaskArn, correlationID); cErr != nil {
		log.Printf("[ecs] record correlation id for %s: %v", task.TaskArn, cErr)
	}
	if len(tags) > 0 {
		if _, tErr := r.svc.SetTaskTags(task.TaskArn, tags); tErr != nil {
			log.Printf("[ecs] record tags for %s: %v", task.TaskArn, tErr)
		}
	}

	spanName := serviceName
	if spanName == "" {
		spanName = td.Family
	}
	essential := make(map[string]bool, len(td.ContainerDefinitions))
	for _, cd := range td.ContainerDefinitions {
		essential[cd.Name] = cd.Essential == nil || *cd.Essential
	}

	rt := &runningTask{
		containerIDs:      make(map[string]string),
		correlationID:     correlationID,
		clusterName:       cluster.ClusterName,
		spanName:          spanName,
		group:             group,
		taskDefinitionArn: td.TaskDefinitionArn,
		essential:         essential,
		stopTimeouts:      containerStopTimeouts(td),
	}
	if serviceName != "" {
		rt.serviceKey = serviceKey(cluster.ClusterArn, serviceName)
	}
	r.mu.Lock()
	r.tasks[task.TaskArn] = rt
	r.mu.Unlock()
	if current, getErr := r.svc.GetTask(task.TaskArn); getErr == nil && current.DesiredStatus == types.TaskDesiredStatusStopped {
		rt.mu.Lock()
		rt.stopRequested = true
		rt.mu.Unlock()
	}

	taskVolumeNames, volErr := r.resolveAndEnsureVolumes(launchCtx, td, task.TaskArn)
	rt.taskVolumeNames = taskVolumeNames
	if volErr != nil {
		_, _ = r.svc.StopTaskRecord(task.TaskArn, volErr.Error())
		r.markRunningTaskStopping(rt)
		r.setTaskStatusAfterLaunchFailure(task.TaskArn, rt)
		r.completeLaunch(task.TaskArn, rt)
		return nil, volErr
	}

	if err := r.startContainers(launchCtx, cluster, task, td, overrides, eventPayload, serviceName, resources, rt); err != nil {
		_, _ = r.svc.StopTaskRecord(task.TaskArn, err.Error())
		r.markRunningTaskStopping(rt)
		r.setTaskStatusAfterLaunchFailure(task.TaskArn, rt)
		r.completeLaunch(task.TaskArn, rt)
		r.stopContainerIDs(context.Background(), rt, r.taskContainerIDs(rt), fmt.Sprintf("(failed launch %s)", task.TaskArn))
		return nil, err
	}

	remaining, stopping := r.completeLaunch(task.TaskArn, rt)
	if stopping || r.stopped.Load() {
		if remaining > 0 {
			_, _ = r.svc.SetTaskStatus(task.TaskArn, types.TaskStatusStopping)
			r.stopContainerIDs(context.Background(), rt, r.taskContainerIDs(rt), fmt.Sprintf("(stopped during launch %s)", task.TaskArn))
		}
	} else if remaining > 0 {
		if runningTaskRec, allowed, err := r.svc.SetTaskRunningIfDesired(task.TaskArn); err != nil {
			log.Printf("[ecs] set task %s running: %v", task.TaskArn, err)
		} else if !allowed {
			r.markRunningTaskStopping(rt)
			_, _ = r.svc.SetTaskStatus(task.TaskArn, types.TaskStatusStopping)
			r.stopContainerIDs(context.Background(), rt, r.taskContainerIDs(rt), fmt.Sprintf("(stopped during launch %s)", task.TaskArn))
		} else {
			r.recordTaskRunningTrace(rt, runningTaskRec)
		}
	}

	refreshed, err := r.svc.GetTask(task.TaskArn)
	if err != nil {
		return task, nil
	}
	return refreshed, nil
}

// startContainers starts every container in td.ContainerDefinitions,
// honoring each container's DependsOn conditions. Containers with no
// dependency on one another start concurrently; a dependent container first
// waits (bounded by ctx) for its dependencies' conditions via
// waitForDependencies before startContainer is called for it. On the first
// failure the failing container's record is marked STOPPED and its error is
// returned — the caller's usual failure cleanup (stopContainerIDs, sweeping
// everything rt has recorded so far) handles every container already
// started by a sibling goroutine.
func (r *Runner) startContainers(ctx context.Context, cluster *types.Cluster, task *types.Task, td *types.TaskDefinition, overrides *types.TaskOverride, eventPayload []byte, serviceName string, resources map[string]taskContainerResources, rt *runningTask) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(td.ContainerDefinitions))
	for _, cd := range td.ContainerDefinitions {
		cd := cd
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.waitForDependencies(ctx, task.TaskArn, cd); err != nil {
				errCh <- fmt.Errorf("container %s: %w", cd.Name, err)
				return
			}
			if err := r.startContainer(ctx, cluster, task, td, cd, overrides, eventPayload, serviceName, resources[cd.Name], rt); err != nil {
				_, _ = r.svc.SetContainerStatus(task.TaskArn, cd.Name, types.TaskStatusStopped)
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// startContainer ensures the image, applies overrides, creates and starts
// the container, records its network bindings and RUNNING status, and
// spawns its log pump / wait goroutines.
func (r *Runner) startContainer(ctx context.Context, cluster *types.Cluster, task *types.Task, td *types.TaskDefinition, cd types.ContainerDefinition, overrides *types.TaskOverride, eventPayload []byte, serviceName string, resources taskContainerResources, rt *runningTask) error {
	rt.mu.Lock()
	startedBeforeStop := rt.stopRequested || r.stopped.Load()
	rt.mu.Unlock()
	if startedBeforeStop {
		return clientError("task %s was stopped before container %s could start", task.TaskArn, cd.Name)
	}

	secretEnv, err := r.resolveContainerSecrets(cd)
	if err != nil {
		return err
	}

	if err := r.eng.EnsureImageRef(ctx, cd.Image); err != nil {
		return fmt.Errorf("image %s: %w", cd.Image, err)
	}

	override := containerOverrideFor(overrides, cd.Name)
	env := mergeContainerEnv(cd.Environment, override)
	// Secrets win over a same-named Environment entry — see the comment on
	// types.ContainerDefinition.Secrets for why.
	for name, value := range secretEnv {
		env[name] = value
	}
	cmd := resolveCommand(cd, override)

	ports := make([]int, 0, len(cd.PortMappings))
	var fixedHostPorts map[int]int
	for _, pm := range cd.PortMappings {
		ports = append(ports, pm.ContainerPort)
		if pm.HostPort != 0 {
			// Task definitions that pin a host port (rather than leaving it 0
			// for Tarn to assign ephemerally) get bound to exactly that port.
			// If it's already taken, Docker's create/start error propagates up
			// through this function and becomes the task's StoppedReason.
			if fixedHostPorts == nil {
				fixedHostPorts = make(map[int]int, len(cd.PortMappings))
			}
			fixedHostPorts[pm.ContainerPort] = pm.HostPort
		}
	}

	labels := taskLabels(r.cfg.AccountID, cluster.ClusterName, serviceName, task.TaskArn)

	binds := resolveContainerMounts(td, task.TaskArn, r.cfg.AccountID, cd)
	volumesFrom := resolveVolumesFrom(task.TaskArn, cd)

	var capAdd, capDrop []string
	var initEnabled bool
	var shmSize int64
	var tmpfs map[string]string
	if cd.LinuxParameters != nil {
		if cd.LinuxParameters.InitProcessEnabled != nil {
			initEnabled = *cd.LinuxParameters.InitProcessEnabled
		}
		if cd.LinuxParameters.Capabilities != nil {
			capAdd = cd.LinuxParameters.Capabilities.Add
			capDrop = cd.LinuxParameters.Capabilities.Drop
		}
		if cd.LinuxParameters.SharedMemorySize > 0 {
			shmSize = int64(cd.LinuxParameters.SharedMemorySize) * bytesPerMiBRunner
		}
		if len(cd.LinuxParameters.Tmpfs) > 0 {
			tmpfs = make(map[string]string, len(cd.LinuxParameters.Tmpfs))
			for _, tf := range cd.LinuxParameters.Tmpfs {
				opts := "rw"
				if tf.Size > 0 {
					opts = fmt.Sprintf("rw,size=%dm", tf.Size)
				}
				if len(tf.MountOptions) > 0 {
					opts = opts + "," + strings.Join(tf.MountOptions, ",")
				}
				tmpfs[tf.ContainerPath] = opts
			}
		}
	}
	var readonlyRootFS, privileged, interactive, pseudoTTY bool
	if cd.ReadonlyRootFilesystem != nil {
		readonlyRootFS = *cd.ReadonlyRootFilesystem
	}
	if cd.Privileged != nil {
		privileged = *cd.Privileged
	}
	if cd.Interactive != nil {
		interactive = *cd.Interactive
	}
	if cd.PseudoTerminal != nil {
		pseudoTTY = *cd.PseudoTerminal
	}
	extraHosts := make([]string, 0, len(cd.ExtraHosts))
	for _, h := range cd.ExtraHosts {
		if h.Hostname == "" || h.IpAddress == "" {
			continue
		}
		extraHosts = append(extraHosts, fmt.Sprintf("%s:%s", h.Hostname, h.IpAddress))
	}

	handle, err := r.eng.CreateAndStartTaskContainer(ctx, engine.TaskContainerSpec{
		Image:                  cd.Image,
		Name:                   containerDockerName(task.TaskArn, cd.Name),
		Command:                cmd,
		Entrypoint:             cd.EntryPoint,
		Env:                    env,
		CPU:                    resources.cpu,
		Memory:                 resources.memory,
		MemoryReservation:      resources.memoryReservation,
		NetworkMode:            td.NetworkMode,
		Ports:                  ports,
		FixedHostPorts:         fixedHostPorts,
		Labels:                 labels,
		Region:                 r.cfg.Region,
		EventPayload:           append([]byte(nil), eventPayload...),
		AccountID:              r.cfg.AccountID,
		CorrelationID:          rt.correlationID,
		WorkingDirectory:       cd.WorkingDirectory,
		User:                   cd.User,
		StopTimeout:            cd.StopTimeout,
		Ulimits:                cd.Ulimits,
		DockerLabels:           cd.DockerLabels,
		ReadonlyRootFilesystem: readonlyRootFS,
		Privileged:             privileged,
		InitProcessEnabled:     initEnabled,
		CapAdd:                 capAdd,
		CapDrop:                capDrop,
		ShmSize:                shmSize,
		Tmpfs:                  tmpfs,
		Hostname:               cd.Hostname,
		DNSServers:             cd.DnsServers,
		ExtraHosts:             extraHosts,
		Interactive:            interactive,
		PseudoTerminal:         pseudoTTY,
		Binds:                  binds,
		VolumesFrom:            volumesFrom,
		HealthCheck:            cd.HealthCheck,
	})
	if err != nil {
		return fmt.Errorf("start container %s: %w", cd.Name, err)
	}
	if _, err := r.svc.SetContainerID(task.TaskArn, cd.Name, handle.ID); err != nil {
		log.Printf("[ecs] record runtime ID for %s/%s: %v", task.TaskArn, cd.Name, err)
	}

	rt.mu.Lock()
	rt.containerIDs[cd.Name] = handle.ID
	rt.remaining++
	stopping := rt.stopRequested || r.stopped.Load()
	rt.mu.Unlock()

	if _, err := r.svc.SetContainerNetworkBindings(task.TaskArn, cd.Name, handle.NetworkBindings); err != nil {
		log.Printf("[ecs] record network bindings for %s/%s: %v", task.TaskArn, cd.Name, err)
	}
	containerStatus := types.TaskStatusRunning
	if stopping {
		containerStatus = types.TaskStatusStopping
	}
	if _, err := r.svc.SetContainerStatus(task.TaskArn, cd.Name, containerStatus); err != nil {
		log.Printf("[ecs] record container status for %s/%s: %v", task.TaskArn, cd.Name, err)
	}

	logGroup := resolveLogGroup(td, cd)
	streamName := taskIDFromRef(task.TaskArn) + "/" + cd.Name
	exited := r.spawnLifecycle(task.TaskArn, cd.Name, handle.ID, logGroup, streamName, rt)
	if cd.HealthCheck != nil && !stopping {
		r.spawnHealthPoller(task.TaskArn, cd.Name, handle.ID, rt, exited)
	}
	if stopping {
		r.stopContainerIDs(context.Background(), rt, []string{handle.ID}, fmt.Sprintf("(stopped during launch %s)", task.TaskArn))
	}
	return nil
}

func (r *Runner) launchContext(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	launchCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	return launchCtx, func() {
		stop()
		cancel()
	}
}

func (r *Runner) markRunningTaskStopping(rt *runningTask) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	rt.stopRequested = true
	rt.mu.Unlock()
}

func (r *Runner) taskContainerIDs(rt *runningTask) []string {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	ids := make([]string, 0, len(rt.containerIDs))
	for _, id := range rt.containerIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (r *Runner) setTaskStatusAfterLaunchFailure(taskArn string, rt *runningTask) {
	if len(r.taskContainerIDs(rt)) > 0 {
		if _, err := r.svc.SetTaskStatus(taskArn, types.TaskStatusStopping); err != nil {
			log.Printf("[ecs] set failed task %s stopping: %v", taskArn, err)
		}
		return
	}
	if _, err := r.svc.SetTaskStatus(taskArn, types.TaskStatusStopped); err != nil {
		log.Printf("[ecs] set failed task %s stopped: %v", taskArn, err)
	}
}

// completeLaunch makes lifecycle completion meaningful only after every
// expected container has either been started or the launch has failed. A fast
// container can exit while the remaining definitions are still being created;
// without this gate that exit would delete the task bookkeeping too early and
// make later containers impossible to stop.
func (r *Runner) completeLaunch(taskArn string, rt *runningTask) (int, bool) {
	rt.mu.Lock()
	rt.launchComplete = true
	remaining := rt.remaining
	stopping := rt.stopRequested
	rt.mu.Unlock()
	if remaining <= 0 {
		r.finishTask(taskArn, rt)
	}
	return remaining, stopping
}

func (r *Runner) finishTask(taskArn string, rt *runningTask) {
	if _, err := r.svc.SetTaskStatus(taskArn, types.TaskStatusStopped); err != nil {
		log.Printf("[ecs] record task %s stopped: %v", taskArn, err)
	}
	if _, err := r.svc.SetTaskDesiredStatus(taskArn, types.TaskDesiredStatusStopped); err != nil {
		log.Printf("[ecs] record task %s desired-stopped: %v", taskArn, err)
	}
	r.recordTaskStoppedTrace(taskArn, rt)
	r.mu.Lock()
	if current, ok := r.tasks[taskArn]; ok && current == rt {
		delete(r.tasks, taskArn)
	}
	r.mu.Unlock()

	// Every container has already been removed by this point (spawnLifecycle
	// removes its own container before calling onContainerFinished, which is
	// what drives finishTask once rt.remaining reaches 0), so task-scoped
	// volumes are safe to remove now. Best-effort: never blocks STOPPED.
	if len(rt.taskVolumeNames) > 0 {
		r.removeTaskVolumes(context.Background(), taskArn, rt.taskVolumeNames)
	}
}

// recordTaskRunningTrace records the trace for a task reaching RUNNING. It is
// nil-safe: a no-op with no trace store wired up.
func (r *Runner) recordTaskRunningTrace(rt *runningTask, task *types.Task) {
	if r.traceStore == nil || rt == nil || task == nil {
		return
	}
	meta := ecsTraceMeta(rt, task)
	startedAt := task.CreatedAt
	if task.StartedAt != nil {
		startedAt = *task.StartedAt
	}
	r.traceStore.Add(&tracesvc.Trace{
		ID:            uuid.NewString()[:8],
		CorrelationID: rt.correlationID,
		StartedAt:     startedAt,
		DurationMs:    0,
		Status:        200,
		Method:        "ECS",
		Path:          ecsTracePath(rt),
		Spans: []tracesvc.Span{{
			Kind:   "ecs",
			Name:   rt.spanName,
			Status: "ok",
			Meta:   meta,
		}},
	})
}

// recordTaskStoppedTrace records the trace for a task reaching STOPPED. It
// is nil-safe: a no-op with no trace store wired up. Status is "error" when
// any essential container (per the task definition captured on rt) exited
// non-zero or never reported an exit code (e.g. it crashed before Docker
// could report one); non-essential containers never fail the task's status,
// mirroring AWS's essential-container rule. A task that was asked to stop
// (StopTask, scale-down, drain, shutdown) is never an error: SIGTERM/SIGKILL
// exit codes there are the expected outcome, not a crash.
func (r *Runner) recordTaskStoppedTrace(taskArn string, rt *runningTask) {
	if r.traceStore == nil || rt == nil {
		return
	}
	task, err := r.svc.GetTask(taskArn)
	if err != nil {
		return
	}

	meta := ecsTraceMeta(rt, task)
	if task.StoppedReason != "" {
		meta["stoppedReason"] = task.StoppedReason
	}

	rt.mu.Lock()
	stopRequested := rt.stopRequested
	rt.mu.Unlock()
	stopRequested = stopRequested || r.stopped.Load()
	if stopRequested {
		meta["stopRequested"] = "true"
	}

	status := "ok"
	for _, c := range task.Containers {
		if rt.essential != nil && !rt.essential[c.Name] {
			continue
		}
		if !stopRequested && (c.ExitCode == nil || *c.ExitCode != 0) {
			status = "error"
		}
		if c.ExitCode != nil {
			meta["exitCode"] = strconv.FormatInt(*c.ExitCode, 10)
		}
		if c.Reason != "" {
			if _, ok := meta["stoppedReason"]; !ok {
				meta["stoppedReason"] = c.Reason
			}
		}
	}

	var durationMs int64
	startedAt := task.CreatedAt
	if task.StartedAt != nil {
		startedAt = *task.StartedAt
	}
	if task.StartedAt != nil && task.StoppedAt != nil {
		durationMs = task.StoppedAt.Sub(*task.StartedAt).Milliseconds()
	}

	r.traceStore.Add(&tracesvc.Trace{
		ID:            uuid.NewString()[:8],
		CorrelationID: rt.correlationID,
		StartedAt:     startedAt,
		DurationMs:    durationMs,
		Status:        ecsTraceHTTPStatus(status),
		Method:        "ECS",
		Path:          ecsTracePath(rt),
		Spans: []tracesvc.Span{{
			Kind:       "ecs",
			Name:       rt.spanName,
			DurationMs: durationMs,
			Status:     status,
			Meta:       meta,
		}},
	})
}

// ecsTraceMeta builds the Meta fields common to both the RUNNING and STOPPED
// traces for a task.
func ecsTraceMeta(rt *runningTask, task *types.Task) map[string]string {
	meta := map[string]string{
		"cluster":           rt.clusterName,
		"taskArn":           task.TaskArn,
		"taskDefinitionArn": rt.taskDefinitionArn,
		"correlationId":     rt.correlationID,
	}
	if rt.group != "" {
		meta["group"] = rt.group
	}
	if ports := ecsHostPortsMeta(task.Containers); ports != "" {
		meta["hostPorts"] = ports
	}
	return meta
}

// ecsHostPortsMeta joins every bound host port across a task's containers,
// for the "hostPorts" trace Meta key. Empty when nothing is bound yet (or
// the task uses no port mappings).
func ecsHostPortsMeta(containers []types.TaskContainer) string {
	var ports []string
	for _, c := range containers {
		for _, nb := range c.NetworkBindings {
			ports = append(ports, strconv.Itoa(nb.HostPort))
		}
	}
	return strings.Join(ports, ",")
}

// ecsTracePath is the trace Path for a task's RUNNING/STOPPED traces:
// /ecs/<cluster>/<service-or-family>.
func ecsTracePath(rt *runningTask) string {
	return fmt.Sprintf("/ecs/%s/%s", rt.clusterName, rt.spanName)
}

// ecsTraceHTTPStatus maps a span's ok/error status to the HTTP-style status
// code trace.Trace.Status uses, matching every other service's convention
// (200 for ok, 500 for error).
func ecsTraceHTTPStatus(status string) int {
	if status == "error" {
		return 500
	}
	return 200
}

func (r *Runner) stopContainerIDs(ctx context.Context, rt *runningTask, ids []string, detail string) {
	if len(ids) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Resolve the SIGTERM grace per container from the snapshot taken at
	// launch; unknown containers fall back to the engine default. Resolved
	// values go to the engine so recordings (and tests) see real seconds.
	timeouts := make(map[string]int, len(ids))
	maxSec := 0
	if rt != nil {
		rt.mu.Lock()
		names := make(map[string]string, len(rt.containerIDs))
		for name, id := range rt.containerIDs {
			names[id] = name
		}
		snapshot := rt.stopTimeouts
		rt.mu.Unlock()
		for _, id := range ids {
			if secs, ok := snapshot[names[id]]; ok && secs > 0 {
				timeouts[id] = secs
			}
		}
	}
	for _, id := range ids {
		if _, ok := timeouts[id]; !ok {
			timeouts[id] = engine.DefaultStopTimeoutSec
		}
		if timeouts[id] > maxSec {
			maxSec = timeouts[id]
		}
	}
	stopCtx, cancel := context.WithTimeout(ctx, time.Duration(maxSec)*time.Second+stopTimeoutGracePeriod)
	defer cancel()

	unique := make(map[string]struct{}, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, seen := unique[id]; seen {
			continue
		}
		unique[id] = struct{}{}
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.eng.StopContainer(stopCtx, id, timeouts[id]); err != nil {
				log.Printf("[ecs] stop container %s %s: %v", id, detail, err)
			}
		}()
	}
	wg.Wait()
}

// containerStopTimeouts snapshots per-container SIGTERM grace periods
// (seconds) from a task definition. Containers without a positive
// StopTimeout are absent: callers fall back to the engine default. A nil
// definition (e.g. unresolvable during recovery) yields no overrides.
func containerStopTimeouts(td *types.TaskDefinition) map[string]int {
	out := make(map[string]int)
	if td == nil {
		return out
	}
	for _, cd := range td.ContainerDefinitions {
		if cd.StopTimeout > 0 {
			out[cd.Name] = cd.StopTimeout
		}
	}
	return out
}

// recoverTasks reconnects persisted active task records to Docker containers
// left behind by an earlier Runner. The account label scopes this scan so one
// account never adopts or removes another account's containers.
//
// Recovery is deliberately best effort. A Docker listing failure leaves the
// persisted task records untouched and lets the normal reconcile loop retry
// service work later. Once a listing succeeds, a task with no matching
// container is marked stopped but remains in the store, while containers with
// no live task record are stopped and removed as orphans.
func (r *Runner) recoverTasks(ctx context.Context) {
	if isNilTaskEngine(r.eng) {
		return
	}
	summaries, err := r.eng.ListContainersByLabel(ctx, selectorForAccount(r.cfg.AccountID))
	if err != nil {
		log.Printf("[ecs] startup recovery: list account containers: %v", err)
		return
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })

	persisted := r.svc.store.ListTasks("")
	tasksByARN := make(map[string]*types.Task, len(persisted))
	for _, task := range persisted {
		tasksByARN[task.TaskArn] = task
	}

	matched := make(map[string]map[string]engine.TaskContainerSummary, len(persisted))
	matchedIDs := make(map[string]bool, len(summaries))
	for _, summary := range summaries {
		taskARN := strings.TrimSpace(summary.Labels[labelTaskArn])
		task, ok := tasksByARN[taskARN]
		if !ok || task.LastStatus == types.TaskStatusStopped {
			continue
		}

		containerName := recoveredContainerName(task, summary, matched[taskARN])
		if containerName == "" {
			log.Printf("[ecs] startup recovery: cannot match container %s to task %s", summary.ID, taskARN)
			continue
		}
		if matched[taskARN] == nil {
			matched[taskARN] = make(map[string]engine.TaskContainerSummary)
		}
		if _, duplicate := matched[taskARN][containerName]; duplicate {
			log.Printf("[ecs] startup recovery: duplicate container %s for task %s container %s", summary.ID, taskARN, containerName)
			continue
		}
		matched[taskARN][containerName] = summary
		matchedIDs[summary.ID] = true
	}

	for _, task := range persisted {
		r.recoverTask(ctx, task, matched[task.TaskArn])
	}

	for _, summary := range summaries {
		if matchedIDs[summary.ID] {
			continue
		}
		r.cleanupOrphanContainer(ctx, summary)
	}

	r.recoverOrphanTaskVolumes(ctx, tasksByARN)
}

// recoverOrphanTaskVolumes removes task-scoped Docker volumes left behind by
// a Tarn restart that lost track of the task that owned them: a crash
// between "task fully stopped" and "finishTask's removeTaskVolumes call"
// would otherwise leak that volume forever, since nothing else ever
// revisits it. A volume is orphaned when its tarn.task-arn label names a
// task the store no longer has, or one already STOPPED — a still-running or
// still-launching task's volume is left alone. A volume with no
// tarn.task-arn label at all is skipped rather than guessed at. Shared-scope
// volumes are never considered here (selectorForAccountTaskVolumes only
// matches "task" scope), matching the "never remove shared volumes" rule
// finishTask itself follows.
func (r *Runner) recoverOrphanTaskVolumes(ctx context.Context, tasksByARN map[string]*types.Task) {
	if isNilTaskEngine(r.eng) {
		return
	}
	volumes, err := r.eng.ListTaskVolumesByLabel(ctx, selectorForAccountTaskVolumes(r.cfg.AccountID))
	if err != nil {
		log.Printf("[ecs] startup recovery: list task volumes: %v", err)
		return
	}
	for _, vol := range volumes {
		taskArn := strings.TrimSpace(vol.Labels[labelTaskArn])
		if taskArn == "" {
			continue
		}
		if task, ok := tasksByARN[taskArn]; ok && task.LastStatus != types.TaskStatusStopped {
			continue
		}
		if err := r.eng.RemoveTaskVolume(ctx, vol.Name); err != nil {
			log.Printf("[ecs] startup recovery: remove orphan volume %s: %v", vol.Name, err)
		}
	}
}

func isNilTaskEngine(eng taskEngine) bool {
	if eng == nil {
		return true
	}
	v := reflect.ValueOf(eng)
	return v.Kind() == reflect.Ptr && v.IsNil()
}

// recoveredContainerName maps a Docker summary to one persisted ECS
// container. RuntimeId is preferred because it survives a container rename;
// the deterministic Tarn name handles older state written before RuntimeId
// persistence was added. A single-container task is the final fallback for
// old containers whose summary omits Names.
func recoveredContainerName(task *types.Task, summary engine.TaskContainerSummary, assigned map[string]engine.TaskContainerSummary) string {
	if task == nil {
		return ""
	}
	for _, container := range task.Containers {
		if container.ContainerID == summary.ID && !containerAlreadyAssigned(assigned, container.Name) {
			return container.Name
		}
	}

	for _, name := range summary.Names {
		name = strings.TrimPrefix(name, "/")
		for _, container := range task.Containers {
			if name == containerDockerName(task.TaskArn, container.Name) && !containerAlreadyAssigned(assigned, container.Name) {
				return container.Name
			}
		}
	}

	if len(task.Containers) == 1 && assigned == nil {
		return task.Containers[0].Name
	}
	return ""
}

func containerAlreadyAssigned(assigned map[string]engine.TaskContainerSummary, name string) bool {
	if assigned == nil {
		return false
	}
	_, ok := assigned[name]
	return ok
}

// recoverTask installs lifecycle bookkeeping for every matching container on
// one persisted task. Missing containers become STOPPED records, but the task
// itself stays in the store. Service-owned tasks can then be replaced by the
// normal DesiredCount reconciliation pass.
func (r *Runner) recoverTask(ctx context.Context, task *types.Task, summaries map[string]engine.TaskContainerSummary) {
	if task == nil || task.LastStatus == types.TaskStatusStopped {
		return
	}

	td, err := r.svc.resolveTaskDefinition(task.TaskDefinitionArn)
	if err != nil {
		log.Printf("[ecs] startup recovery: resolve task definition for %s: %v", task.TaskArn, err)
	}

	rt := &runningTask{containerIDs: make(map[string]string), launchComplete: true}
	rt.stopTimeouts = containerStopTimeouts(td)
	if strings.HasPrefix(task.Group, "service:") {
		rt.serviceKey = serviceKey(task.ClusterArn, strings.TrimPrefix(task.Group, "service:"))
	}
	var stopIDs []string
	missing := 0
	hasRunning := false
	for _, container := range task.Containers {
		summary, ok := summaries[container.Name]
		if !ok {
			missing++
			_ = r.markMissingContainer(task.TaskArn, container.Name)
			continue
		}

		rt.containerIDs[container.Name] = summary.ID
		rt.remaining++
		if container.ContainerID != summary.ID {
			if _, err := r.svc.SetContainerID(task.TaskArn, container.Name, summary.ID); err != nil {
				log.Printf("[ecs] startup recovery: record runtime ID for %s/%s: %v", task.TaskArn, container.Name, err)
			}
		}
		if summary.State == "running" {
			hasRunning = true
			if task.DesiredStatus != types.TaskDesiredStatusStopped {
				if _, err := r.svc.SetContainerStatus(task.TaskArn, container.Name, types.TaskStatusRunning); err != nil {
					log.Printf("[ecs] startup recovery: record running status for %s/%s: %v", task.TaskArn, container.Name, err)
				}
			}
		}
		if summary.State == "running" && task.DesiredStatus == types.TaskDesiredStatusStopped {
			stopIDs = append(stopIDs, summary.ID)
		}
	}

	if missing > 0 {
		reason := "container missing during ECS runner recovery"
		if _, err := r.svc.StopTaskRecord(task.TaskArn, reason); err != nil {
			log.Printf("[ecs] startup recovery: mark task %s desired-stopped: %v", task.TaskArn, err)
		}
		for _, summary := range summaries {
			if summary.State != "running" || containsString(stopIDs, summary.ID) {
				continue
			}
			stopIDs = append(stopIDs, summary.ID)
		}
	}

	if rt.remaining == 0 {
		if _, err := r.svc.SetTaskStatus(task.TaskArn, types.TaskStatusStopped); err != nil {
			log.Printf("[ecs] startup recovery: mark task %s stopped: %v", task.TaskArn, err)
		}
		if _, err := r.svc.SetTaskDesiredStatus(task.TaskArn, types.TaskDesiredStatusStopped); err != nil {
			log.Printf("[ecs] startup recovery: mark task %s desired-stopped: %v", task.TaskArn, err)
		}
		return
	}
	if missing > 0 {
		if _, err := r.svc.SetTaskStatus(task.TaskArn, types.TaskStatusStopping); err != nil {
			log.Printf("[ecs] startup recovery: mark task %s stopping: %v", task.TaskArn, err)
		}
	} else if hasRunning && task.DesiredStatus != types.TaskDesiredStatusStopped {
		if _, err := r.svc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
			log.Printf("[ecs] startup recovery: mark task %s running: %v", task.TaskArn, err)
		}
	}

	r.mu.Lock()
	r.tasks[task.TaskArn] = rt
	r.mu.Unlock()

	for _, container := range task.Containers {
		summary, ok := summaries[container.Name]
		if !ok {
			continue
		}
		logGroup := recoveredLogGroup(td, container.Name, task.TaskDefinitionArn)
		streamName := taskIDFromRef(task.TaskArn) + "/" + container.Name
		r.spawnLifecycle(task.TaskArn, container.Name, summary.ID, logGroup, streamName, rt)
	}

	stopCtx, cancel := context.WithTimeout(ctx, stopContainerTimeout)
	defer cancel()
	for _, id := range stopIDs {
		if err := r.eng.StopContainer(stopCtx, id, 0); err != nil {
			log.Printf("[ecs] startup recovery: stop desired-stopped container %s: %v", id, err)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (r *Runner) markMissingContainer(taskArn, containerName string) error {
	if _, err := r.svc.SetContainerStatus(taskArn, containerName, types.TaskStatusStopped); err != nil {
		return err
	}
	_, err := r.svc.SetContainerExitCode(taskArn, containerName, nil, "container missing during ECS runner recovery")
	return err
}

func recoveredLogGroup(td *types.TaskDefinition, containerName, taskDefinitionARN string) string {
	if td != nil {
		for _, container := range td.ContainerDefinitions {
			if container.Name == containerName {
				return resolveLogGroup(td, container)
			}
		}
	}
	family, _, _, err := parseTaskDefinitionRef(taskDefinitionARN)
	if err == nil && family != "" {
		return "/ecs/" + family
	}
	return "/ecs/recovered"
}

func (r *Runner) cleanupOrphanContainer(ctx context.Context, summary engine.TaskContainerSummary) {
	if summary.ID == "" {
		return
	}
	if summary.State == "running" {
		if err := r.eng.StopContainer(ctx, summary.ID, 0); err != nil {
			log.Printf("[ecs] startup recovery: stop orphan container %s: %v", summary.ID, err)
		}
	}
	if err := r.eng.RemoveTaskContainer(ctx, summary.ID); err != nil {
		log.Printf("[ecs] startup recovery: remove orphan container %s: %v", summary.ID, err)
	}
}

// spawnLifecycle starts the log pump and wait goroutine for one container.
// Cleanup ordering on exit is: read the exit code, set STOPPED, drain the
// remaining log stream, then remove the container — never removed before
// its logs are drained, and never removed before the exit code is read.
// spawnLifecycle's return channel is closed as soon as WaitTaskContainer
// returns (the container is no longer running), letting a caller like
// spawnHealthPoller stop polling promptly instead of only on Stop()'s
// lifecycleCtx cancellation — which Stop() itself doesn't fire until after
// waiting (bounded by stopContainerTimeout) for every r.wg goroutine,
// including that poller, to finish. Without this signal a health poller
// with nothing else to wake it keeps ticking for the full timeout.
func (r *Runner) spawnLifecycle(taskArn, containerName, containerID, logGroup, streamName string, rt *runningTask) <-chan struct{} {
	pumpCtx, pumpCancel := context.WithCancel(r.lifecycleCtx)
	pumpDone := make(chan struct{})
	exited := make(chan struct{})

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer close(pumpDone)
		r.pumpLogs(pumpCtx, logGroup, streamName, containerID)
	}()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		// lifecycleCtx, not ctx: see the field comment. Using ctx here was
		// the shutdown bug — Stop cancels ctx before it has finished asking
		// Docker to gracefully stop this container, so WaitTaskContainer
		// would return "context canceled" instead of the real exit code.
		exitCode, waitErr := r.eng.WaitTaskContainer(r.lifecycleCtx, containerID)
		close(exited)
		var ec *int64
		reason := ""
		if waitErr == nil {
			v := exitCode
			ec = &v
		} else {
			reason = waitErr.Error()
		}
		if _, err := r.svc.SetContainerExitCode(taskArn, containerName, ec, reason); err != nil {
			log.Printf("[ecs] record exit code for %s/%s: %v", taskArn, containerName, err)
		}
		if _, err := r.svc.SetContainerStatus(taskArn, containerName, types.TaskStatusStopped); err != nil {
			log.Printf("[ecs] record container status for %s/%s: %v", taskArn, containerName, err)
		}
		r.logContainerExit(logGroup, streamName, containerName, ec, reason, rt)

		// Drain: let the pump finish on its own for a bounded grace period
		// (Docker's follow stream normally ends shortly after exit), then
		// force it closed so this never hangs against a stream Docker
		// doesn't close.
		select {
		case <-pumpDone:
		case <-time.After(logDrainGracePeriod):
		}
		pumpCancel()
		<-pumpDone

		_ = r.eng.RemoveTaskContainer(context.Background(), containerID)

		rt.mu.Lock()
		delete(rt.containerIDs, containerName)
		rt.mu.Unlock()

		r.onContainerFinished(taskArn)
	}()

	return exited
}

// logContainerExit records an ERROR event when a container stops
// unexpectedly: a nonzero (or missing) exit code on an essential container
// nobody asked to stop. It mirrors the trace-span rule in recordTaskTrace
// so logs and traces agree on what "failed" means. Expected stops (StopTask,
// runner shutdown) and non-essential sidecars stay silent — their exit codes
// are the expected outcome, not a crash.
func (r *Runner) logContainerExit(logGroup, streamName, containerName string, ec *int64, reason string, rt *runningTask) {
	rt.mu.Lock()
	stopRequested := rt.stopRequested
	essential := rt.essential == nil || rt.essential[containerName]
	rt.mu.Unlock()
	if stopRequested || r.stopped.Load() || !essential {
		return
	}
	if ec != nil && *ec == 0 {
		return
	}
	msg := fmt.Sprintf("container %q stopped without an exit code", containerName)
	if ec != nil {
		msg = fmt.Sprintf("container %q exited with code %d", containerName, *ec)
	}
	if reason != "" {
		msg += ": " + reason
	}
	r.logs.CreateLogGroup(logGroup)
	r.logs.PutLogEvents(logGroup, streamName, []logs.LogEvent{{
		Timestamp:  time.Now().UTC(),
		Message:    msg,
		Level:      logs.LevelERROR,
		Source:     logs.SourceRuntime,
		StreamName: streamName,
	}})
}

// onContainerFinished decrements the task's remaining-container count and,
// once every container has exited, flips the task record to STOPPED and
// drops the runner's local bookkeeping for it. Multi-container tasks are
// treated uniformly here: the task is STOPPED only when *all* its
// containers have exited, not when the first (possibly non-essential) one
// does — a deliberate simplification of AWS's "essential container" rule,
// which this runner does not implement.
func (r *Runner) onContainerFinished(taskArn string) {
	r.mu.Lock()
	rt, ok := r.tasks[taskArn]
	r.mu.Unlock()
	if !ok {
		return
	}

	rt.mu.Lock()
	rt.remaining--
	done := rt.launchComplete && rt.remaining <= 0
	rt.mu.Unlock()
	if !done {
		return
	}
	if rt.serviceKey != "" {
		if current, getErr := r.svc.GetTask(taskArn); getErr == nil && current.DesiredStatus == types.TaskDesiredStatusRunning {
			if taskRanStabilityWindow(current) {
				// The task stayed up at least serviceStabilityWindow before
				// exiting: treat it as a healthy instance reaching a normal
				// end rather than a crash, and let the service launch its
				// replacement without delay. Backoff is intentionally not
				// reset any earlier than this (see recordServiceLaunchSuccess's
				// caller in reconcileService) — a container that starts and
				// immediately crashes must keep growing the retry delay
				// instead of being relaunched every reconcile tick forever.
				r.recordServiceLaunchSuccess(rt.serviceKey)
			} else {
				r.recordServiceLaunchFailure(rt.serviceKey, time.Now())
			}
		}
	}
	r.finishTask(taskArn, rt)
}

// taskRanStabilityWindow reports whether task has been running long enough
// (serviceStabilityWindow) to be considered a healthy instance rather than a
// crash-looping one.
func taskRanStabilityWindow(task *types.Task) bool {
	if task == nil || task.StartedAt == nil {
		return false
	}
	return time.Since(*task.StartedAt) >= serviceStabilityWindow
}

// pumpLogs follows a container's log stream and writes each line into
// logGroup/streamName via CreateLogGroup + PutLogEvents. It never calls
// logsSvc.IngestContainerLogs — that method hardcodes the "/aws/lambda/"
// group prefix and classifies RIE-emulator lines ECS containers never
// produce (see docs/design/ecs-support.md).
func (r *Runner) pumpLogs(ctx context.Context, logGroup, streamName, containerID string) {
	r.logs.CreateLogGroup(logGroup)
	err := r.eng.FollowContainerLogs(ctx, containerID, func(line string, stderr bool) {
		level := logs.DetectLevel(line)
		if stderr && level == logs.LevelINFO {
			// Plain containers carry no level metadata: anything they write
			// to stderr is error output (e.g. Node's console.error), but
			// keyword detection alone would file "failed: HTTP 503" as INFO.
			// Only the unclassified default is upgraded — an explicit WARN
			// or DEBUG token stays as detected.
			level = logs.LevelERROR
		}
		r.logs.PutLogEvents(logGroup, streamName, []logs.LogEvent{{
			Timestamp:  time.Now().UTC(),
			Message:    line,
			Level:      level,
			Source:     logs.SourceOutput,
			StreamName: streamName,
		}})
	})
	if err != nil && ctx.Err() == nil {
		log.Printf("[ecs] log pump for container %s ended: %v", containerID, err)
	}
}

// --- Reconcile loop ----------------------------------------------------------

// reconcileLoop ticks reconcileTickInterval, reconciling every account's ECS
// services to their DesiredCount, until Stop closes reconcileDone.
func (r *Runner) reconcileLoop() {
	defer r.reconcileWG.Done()

	ticker := time.NewTicker(reconcileTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.reconcileDone:
			return
		case <-ticker.C:
			r.reconcileOnce(r.ctx)
		}
	}
}

// reconcileOnce reconciles every ACTIVE service in every cluster this
// runner's store knows about. Factored out of the ticker loop so tests can
// drive a single pass deterministically with no wall-clock waiting.
func (r *Runner) reconcileOnce(ctx context.Context) {
	if _, err := r.svc.store.PruneStoppedTasks(time.Now().UTC().Add(-stoppedTaskRetention)); err != nil {
		log.Printf("[ecs] reconcile: prune stopped tasks: %v", err)
	}
	if _, err := r.svc.store.PruneInactiveServices(time.Now().UTC().Add(-inactiveServiceRetention)); err != nil {
		log.Printf("[ecs] reconcile: prune inactive services: %v", err)
	}
	for _, cluster := range r.svc.store.ListClusters() {
		for _, svc := range r.svc.store.ListServices(cluster.ClusterArn) {
			if svc.Status != types.ServiceStatusActive {
				continue
			}
			r.reconcileService(ctx, cluster, svc)
		}
	}
}

// reconcileService counts RUNNING containers matching svc's label selector,
// keeps service task records aligned with DesiredCount, and performs a
// bounded one-task-at-a-time replacement when the service definition changes.
// A replacement is not launched until the old task has stopped and its
// container is no longer visible. This keeps repeated reconcile passes from
// launching duplicate replacements while Docker is still converging.
func (r *Runner) reconcileService(ctx context.Context, cluster *types.Cluster, svc *types.ECSService) {
	if cluster == nil || svc == nil {
		return
	}
	// Store writes clone service records. Refresh by identity so an API
	// UpdateService that happens between ticker iterations is observed even
	// when the caller still holds the pointer returned by CreateService.
	fresh, err := r.svc.store.GetService(cluster.ClusterArn, svc.ServiceName)
	if err != nil {
		log.Printf("[ecs] reconcile: load service %s: %v", svc.ServiceName, err)
		return
	}
	svc = fresh

	selector := selectorForService(r.cfg.AccountID, cluster.ClusterName, svc.ServiceName)
	summaries, err := r.eng.ListContainersByLabel(ctx, selector)
	if err != nil {
		log.Printf("[ecs] reconcile: list containers for service %s: %v", svc.ServiceName, err)
		return
	}
	running := 0
	for _, c := range summaries {
		if c.State == "running" {
			running++
		}
	}

	group := serviceGroup(svc.ServiceName)
	defer r.recordServiceCounts(cluster, svc.ServiceName, group, running)

	tasks := r.serviceTasks(cluster.ClusterArn, group)
	activeTasks := serviceActiveTasks(tasks)

	// A task that is already being stopped is the in-flight rollout gate. If
	// desired count has also been reduced, continue honoring scale-down for
	// the remaining active records, but never count the draining old task as a
	// reason to stop a healthy replacement.
	if r.oldTaskStillVisible(tasks, svc.TaskDefinitionArn, summaries) {
		if len(activeTasks) > svc.DesiredCount {
			r.stopServiceTasks(ctx, cluster, svc, activeTasks, len(activeTasks)-svc.DesiredCount)
		}
		return
	}

	// Scale-down takes precedence over rollout. It keeps the existing
	// DesiredCount behavior when an update also lowers the count, and it
	// prefers old-definition tasks so a newer task is not discarded.
	// Docker still lists a container as running during its stop timeout, but a
	// task whose desired status is STOPPED no longer represents service
	// capacity. Count task records that are still active so one draining task
	// cannot cause the last healthy task to be stopped on the next tick.
	capacity := len(activeTasks)
	if capacity > svc.DesiredCount {
		r.stopServiceTasks(ctx, cluster, svc, activeTasks, capacity-svc.DesiredCount)
		return
	}

	// Stop exactly one active old-definition task. StopTask marks its desired
	// status before asking Docker to stop it, which makes the operation
	// idempotent across subsequent reconcile passes. The next pass launches
	// the current definition only after this task is no longer active.
	for _, task := range activeTasks {
		if task.TaskDefinitionArn != svc.TaskDefinitionArn {
			if err := r.StopTask(ctx, cluster.ClusterArn, task.TaskArn, "rolling deployment to new task definition"); err != nil {
				log.Printf("[ecs] reconcile: stop old task %s for service %s: %v", task.TaskArn, svc.ServiceName, err)
			}
			return
		}
	}

	// Count task records as well as Docker's view. A newly launched task is
	// persisted before this method returns, so a stale Docker listing cannot
	// cause another replacement launch on the next pass.
	if len(activeTasks) >= svc.DesiredCount {
		return
	}
	if r.stopped.Load() {
		return
	}
	diff := svc.DesiredCount - len(activeTasks)
	launchKey := serviceKey(cluster.ClusterArn, svc.ServiceName)
	td, err := r.svc.resolveTaskDefinition(svc.TaskDefinitionArn)
	if err != nil {
		log.Printf("[ecs] reconcile: resolve task definition for service %s: %v", svc.ServiceName, err)
		r.recordServiceLaunchFailure(launchKey, time.Now())
		return
	}
	// PropagateTags on a service is SERVICE, TASK_DEFINITION, or NONE; copy
	// the corresponding tags onto every task this reconcile pass launches.
	var launchTags []types.Tag
	switch strings.ToUpper(svc.PropagateTags) {
	case "SERVICE":
		launchTags = cloneTags(svc.Tags)
	case "TASK_DEFINITION":
		launchTags = cloneTags(td.Tags)
	}
	for i := 0; i < diff; i++ {
		if !r.serviceLaunchAllowed(launchKey, time.Now()) {
			return
		}
		if _, err := r.launchTask(ctx, cluster, td, nil, svc.LaunchType, group, svc.ServiceName, launchTags); err != nil {
			log.Printf("[ecs] reconcile: start task for service %s: %v", svc.ServiceName, err)
			r.recordServiceLaunchFailure(launchKey, time.Now())
			return
		}
		// Backoff is deliberately not reset here. A container starting
		// successfully says nothing about whether it stays up; resetting on
		// start let a service that crashes immediately relaunch every
		// reconcile tick forever. onContainerFinished resets it once the
		// task has actually stayed running for serviceStabilityWindow.
	}
}

func (r *Runner) serviceLaunchAllowed(key string, now time.Time) bool {
	r.backoffMu.Lock()
	defer r.backoffMu.Unlock()
	state, ok := r.launchBackoff[key]
	return !ok || !now.Before(state.nextRetry)
}

func (r *Runner) recordServiceLaunchFailure(key string, now time.Time) {
	if key == "" {
		return
	}
	r.backoffMu.Lock()
	defer r.backoffMu.Unlock()
	state := r.launchBackoff[key]
	state.failures++
	delay := serviceLaunchBackoffBase
	for i := 1; i < state.failures && delay < serviceLaunchBackoffMax; i++ {
		delay *= 2
	}
	if delay > serviceLaunchBackoffMax {
		delay = serviceLaunchBackoffMax
	}
	state.nextRetry = now.Add(delay)
	r.launchBackoff[key] = state
}

func (r *Runner) recordServiceLaunchSuccess(key string) {
	if key == "" {
		return
	}
	r.backoffMu.Lock()
	delete(r.launchBackoff, key)
	r.backoffMu.Unlock()
}

// recordServiceCounts records Docker's observed running count and the task
// records that are still provisioning or pending. The running count is kept
// as the Docker observation rather than inferred from records, so the API
// reflects the same source the reconciler uses for container visibility.
func (r *Runner) recordServiceCounts(cluster *types.Cluster, serviceName, group string, running int) {
	pending := 0
	for _, t := range r.svc.store.ListTasks(cluster.ClusterArn) {
		if t.Group != group {
			continue
		}
		if t.LastStatus == types.TaskStatusProvisioning || t.LastStatus == types.TaskStatusPending {
			pending++
		}
	}
	if _, err := r.svc.SetServiceCounts(cluster, serviceName, running, pending); err != nil {
		log.Printf("[ecs] reconcile: update counts for service %s: %v", serviceName, err)
	}
}

// serviceTasks returns all task records owned by one service, ordered by
// TaskArn by the store. Keeping this query record-based lets reconciliation
// see tasks that Docker has not reported yet.
//
// Record-based counting is not strictly safer in every direction, though: a
// labelled Docker container with no matching task record (RemoveTaskContainer
// failed and the STOPPED record was later pruned by stoppedTaskRetention, for
// instance) is invisible to this query and to every caller built on top of
// it. Nothing here reaps that container — only recoverTasks, at the next
// process startup, sweeps account-labelled containers with no live record
// and removes them as orphans.
func (r *Runner) serviceTasks(clusterArn, group string) []*types.Task {
	var out []*types.Task
	for _, t := range r.svc.store.ListTasks(clusterArn) {
		if t.Group == group {
			out = append(out, t)
		}
	}
	return out
}

// serviceActiveTasks excludes tasks that have already reached STOPPED or
// whose desired status was set to STOPPED by StopTask. The latter remains in
// the store while Docker drains, but must not be selected twice.
func serviceActiveTasks(tasks []*types.Task) []*types.Task {
	var out []*types.Task
	for _, task := range tasks {
		if task == nil || task.LastStatus == types.TaskStatusStopped || task.DesiredStatus == types.TaskDesiredStatusStopped {
			continue
		}
		out = append(out, task)
	}
	return out
}

// stopServiceTasks stops up to count active service tasks. Old definitions
// are ordered before the current definition so a scale-down concurrent with
// an update helps the rollout instead of removing a healthy replacement.
func (r *Runner) stopServiceTasks(ctx context.Context, cluster *types.Cluster, svc *types.ECSService, tasks []*types.Task, count int) {
	if count <= 0 {
		return
	}
	var old, current []*types.Task
	for _, task := range tasks {
		if task.TaskDefinitionArn != svc.TaskDefinitionArn {
			old = append(old, task)
		} else {
			current = append(current, task)
		}
	}
	candidates := append(old, current...)
	for i := 0; i < count && i < len(candidates); i++ {
		if err := r.StopTask(ctx, cluster.ClusterArn, candidates[i].TaskArn, "scaling down to desired count"); err != nil {
			log.Printf("[ecs] reconcile: stop task %s: %v", candidates[i].TaskArn, err)
		}
	}
}

// oldTaskStillVisible reports whether a previous-definition task has already
// been asked to stop but still has a live Docker container or an in-flight
// lifecycle goroutine. It is the rollout gate that prevents a second old task
// from being stopped before the first one has finished.
func (r *Runner) oldTaskStillVisible(tasks []*types.Task, currentDefinition string, summaries []engine.TaskContainerSummary) bool {
	for _, task := range tasks {
		if task == nil || task.TaskDefinitionArn == currentDefinition || task.LastStatus == types.TaskStatusStopped || task.DesiredStatus != types.TaskDesiredStatusStopped {
			continue
		}
		if r.taskHasLiveContainer(task, summaries) {
			return true
		}
	}
	return false
}

func (r *Runner) taskHasLiveContainer(task *types.Task, summaries []engine.TaskContainerSummary) bool {
	if task == nil {
		return false
	}

	containerIDs := make(map[string]struct{}, len(task.Containers))
	for _, container := range task.Containers {
		if container.ContainerID != "" {
			containerIDs[container.ContainerID] = struct{}{}
		}
	}
	terminalIDs := make(map[string]struct{}, len(containerIDs))
	reported := false
	for _, summary := range summaries {
		matchesTask := summary.Labels[labelTaskArn] == task.TaskArn
		_, matchesRuntimeID := containerIDs[summary.ID]
		if !matchesTask && !matchesRuntimeID {
			continue
		}
		reported = true
		if taskContainerSummaryIsLive(summary) {
			return true
		}
		if summary.ID != "" {
			terminalIDs[summary.ID] = struct{}{}
		}
	}

	// A terminal summary is enough to prove that the reported container is
	// no longer live. Do not let an unrelated log-drain goroutine delay the
	// replacement. For multi-container tasks, an ID absent from the Docker
	// listing remains unknown and must still be checked against bookkeeping.
	if reported {
		allKnownIDsTerminal := true
		for id := range containerIDs {
			if _, ok := terminalIDs[id]; !ok {
				allKnownIDsTerminal = false
				break
			}
		}
		if allKnownIDsTerminal {
			return false
		}
	}

	r.mu.Lock()
	rt, ok := r.tasks[task.TaskArn]
	r.mu.Unlock()
	if !ok {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.containerIDs) > 0
}

func taskContainerSummaryIsLive(summary engine.TaskContainerSummary) bool {
	switch strings.ToLower(strings.TrimSpace(summary.State)) {
	case "exited", "dead", "removed":
		return false
	default:
		// Docker can report created, restarting, paused, or removing while a
		// stop is in flight. Treat all of those as present until the engine
		// reports a terminal state.
		return true
	}
}

// --- Overrides and small helpers --------------------------------------------

// validateOverrides rejects a RunTask/reconcile request naming a container
// override that does not exist in the task definition. Silently ignoring
// such an override would drop an EventBridge event payload with no signal
// to the caller (T9 delivers events exactly this way), so this errors
// instead.
func validateOverrides(td *types.TaskDefinition, overrides *types.TaskOverride) error {
	if overrides == nil {
		return nil
	}
	names := make(map[string]bool, len(td.ContainerDefinitions))
	for _, cd := range td.ContainerDefinitions {
		names[cd.Name] = true
	}
	for _, co := range overrides.ContainerOverrides {
		if !names[co.Name] {
			return invalidParameterError("container override %q does not match any container in task definition %s", co.Name, td.Family)
		}
	}
	return nil
}

// containerOverrideFor returns the override entry for containerName, or nil
// if none was supplied.
func containerOverrideFor(overrides *types.TaskOverride, containerName string) *types.ContainerOverride {
	if overrides == nil {
		return nil
	}
	for i := range overrides.ContainerOverrides {
		if overrides.ContainerOverrides[i].Name == containerName {
			return &overrides.ContainerOverrides[i]
		}
	}
	return nil
}

// mergeContainerEnv layers override environment entries on top of the task
// definition's base environment without mutating either: override entries
// replace matching names and add new ones.
func mergeContainerEnv(base []types.KeyValuePair, override *types.ContainerOverride) map[string]string {
	env := make(map[string]string, len(base))
	for _, kv := range base {
		env[kv.Name] = kv.Value
	}
	if override != nil {
		for _, kv := range override.Environment {
			env[kv.Name] = kv.Value
		}
	}
	return env
}

// resolveCommand returns the override's command when it supplies one,
// otherwise the task definition's own — an override command replaces the
// definition's command entirely, it does not merge with it.
func resolveCommand(cd types.ContainerDefinition, override *types.ContainerOverride) []string {
	if override != nil && len(override.Command) > 0 {
		return override.Command
	}
	return cd.Command
}

// containerDockerName builds the Docker container name for one task
// container: stable, unique per task, and human-recognisable in `docker ps`.
func containerDockerName(taskArn, containerName string) string {
	return "tarn-ecs-" + taskIDFromRef(taskArn) + "-" + containerName
}

// bytesPerMiBRunner mirrors internal/engine's bytesPerMiB, kept as a
// separate constant here so this package doesn't need to import an
// unexported engine constant.
const bytesPerMiBRunner = int64(1024 * 1024)

// resolveContainerMounts converts a container definition's MountPoints into
// Docker bind specs ("source:target[:ro]"). SourceVolume is resolved
// against the task definition's Volumes list:
//   - Host.SourcePath set: bind-mounts that host path directly.
//   - DockerVolumeConfiguration or a bare volume (neither Host nor
//     DockerVolumeConfiguration set): a Docker named volume. "shared" scope
//     gets a stable per-account+volume-name name so it survives across
//     tasks; anything else (including bare volumes, which AWS shares only
//     within one task) gets a name unique to this task, letting every
//     container in the task reference the same named volume while Docker
//     auto-creates it on first use.
//   - EfsVolumeConfiguration-only volumes are store/echo only (no local EFS
//     emulation) and are skipped here.
func resolveContainerMounts(td *types.TaskDefinition, taskArn, accountID string, cd types.ContainerDefinition) []string {
	if len(cd.MountPoints) == 0 {
		return nil
	}
	volumes := make(map[string]types.Volume, len(td.Volumes))
	for _, v := range td.Volumes {
		volumes[v.Name] = v
	}
	var binds []string
	for _, mp := range cd.MountPoints {
		vol, ok := volumes[mp.SourceVolume]
		if !ok || mp.ContainerPath == "" {
			continue
		}
		source, isNamedVolume := volumeDockerName(vol, taskArn, accountID)
		if source == "" && !isNamedVolume {
			// EFS-only volume, or an unresolvable one: not applied locally.
			continue
		}
		bind := source + ":" + mp.ContainerPath
		if mp.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}
	return binds
}

// volumeDockerName returns the Docker bind-mount source for a task
// definition Volume entry: an absolute host path for a Host.SourcePath
// volume (isNamedVolume false — no Docker named volume is created for a
// plain bind mount), or a deterministic Docker named-volume name otherwise
// (isNamedVolume true). An EFS-only volume (no Host, no
// DockerVolumeConfiguration) returns ("", false): it is store/echo only,
// with no local EFS emulation.
//
// Naming: "shared" scope gets a name stable across every task in this
// account ("tarn-ecs-shared-<account>-<volume>"), so repeated tasks
// referencing it share the same underlying Docker volume the way AWS's EFS
// shared scope would. Everything else (a bare volume with neither Host nor
// DockerVolumeConfiguration, or an explicit "task" scope) gets a name unique
// to this task ("tarn-ecs-task-<taskID>-<volume>"), matching AWS's "shared
// only within one task" default.
func volumeDockerName(v types.Volume, taskArn, accountID string) (name string, isNamedVolume bool) {
	if v.Host != nil && v.Host.SourcePath != "" {
		return v.Host.SourcePath, false
	}
	if v.EfsVolumeConfiguration != nil && v.DockerVolumeConfiguration == nil {
		return "", false
	}
	if v.DockerVolumeConfiguration != nil && v.DockerVolumeConfiguration.Scope == volumeScopeShared {
		return "tarn-ecs-shared-" + accountID + "-" + v.Name, true
	}
	return "tarn-ecs-task-" + taskIDFromRef(taskArn) + "-" + v.Name, true
}

// resolveAndEnsureVolumes validates and creates the Docker named volumes a
// task definition's Volumes need before any of its containers start.
// Host-only and EFS-only volumes need nothing done here (resolved directly
// by resolveContainerMounts, or left store/echo only for EFS). A "shared"
// scope volume with autoprovision=false must already exist — a missing one
// fails the whole launch with a clear StoppedReason rather than silently
// creating a volume ECS itself would have refused to. Every other
// Docker-volume-configured or bare volume is created if missing (Docker's
// own create-by-name is idempotent) carrying Tarn's own tarn.* labels; the
// "task"-scoped names created are returned so the caller can record them on
// rt.taskVolumeNames for finishTask to remove once the task stops.
func (r *Runner) resolveAndEnsureVolumes(ctx context.Context, td *types.TaskDefinition, taskArn string) ([]string, error) {
	var taskScoped []string
	accountID := r.cfg.AccountID
	for _, v := range td.Volumes {
		name, isNamedVolume := volumeDockerName(v, taskArn, accountID)
		if !isNamedVolume {
			continue
		}

		scope := volumeScopeTask
		autoprovision := true
		var driver string
		var driverOpts, dockerLabels map[string]string
		if v.DockerVolumeConfiguration != nil {
			driver = v.DockerVolumeConfiguration.Driver
			driverOpts = v.DockerVolumeConfiguration.DriverOpts
			dockerLabels = v.DockerVolumeConfiguration.Labels
			if v.DockerVolumeConfiguration.Scope == volumeScopeShared {
				scope = volumeScopeShared
				autoprovision = v.DockerVolumeConfiguration.Autoprovision
			}
		}

		if scope == volumeScopeShared && !autoprovision {
			exists, err := r.eng.TaskVolumeExists(ctx, name)
			if err != nil {
				return taskScoped, fmt.Errorf("volume %q: check shared Docker volume %q: %w", v.Name, name, err)
			}
			if !exists {
				return taskScoped, clientError("volume %q: shared Docker volume %q does not exist and autoprovision is false", v.Name, name)
			}
			continue
		}

		ownTaskArn := ""
		if scope == volumeScopeTask {
			ownTaskArn = taskArn
		}
		labels := make(map[string]string, len(dockerLabels)+2)
		for k, val := range dockerLabels {
			labels[k] = val
		}
		for k, val := range taskVolumeLabels(accountID, ownTaskArn, scope) {
			labels[k] = val
		}
		if err := r.eng.EnsureTaskVolume(ctx, name, driver, driverOpts, labels); err != nil {
			return taskScoped, fmt.Errorf("volume %q: %w", v.Name, err)
		}
		if scope == volumeScopeTask {
			taskScoped = append(taskScoped, name)
		}
	}
	return taskScoped, nil
}

// removeTaskVolumes best-effort removes every task-scoped Docker volume rt
// recorded at launch. Errors are logged, never surfaced: a volume Docker
// still considers busy (e.g. a slow container teardown race) or one that's
// already gone must not block the task from being recorded STOPPED, and
// there's no caller left to report the error to.
func (r *Runner) removeTaskVolumes(ctx context.Context, taskArn string, names []string) {
	for _, name := range names {
		if err := r.eng.RemoveTaskVolume(ctx, name); err != nil {
			log.Printf("[ecs] remove task volume %s for %s: %v", name, taskArn, err)
		}
	}
}

// resolveVolumesFrom converts a container definition's VolumesFrom into
// Docker's "container:[ro|rw]" HostConfig.VolumesFrom form, referencing
// other containers of the same task by their deterministic Docker name.
// This does not wait for the source container to exist first; a task
// definition relying on VolumesFrom should also declare the equivalent
// DependsOn so container start order matches.
func resolveVolumesFrom(taskArn string, cd types.ContainerDefinition) []string {
	if len(cd.VolumesFrom) == 0 {
		return nil
	}
	out := make([]string, 0, len(cd.VolumesFrom))
	for _, vf := range cd.VolumesFrom {
		if vf.SourceContainer == "" {
			continue
		}
		ref := containerDockerName(taskArn, vf.SourceContainer)
		if vf.ReadOnly {
			ref += ":ro"
		}
		out = append(out, ref)
	}
	return out
}

// resolveLogGroup picks the CloudWatch-style log group a container's output
// goes to: the task definition's own awslogs-group option when set,
// otherwise "/ecs/<family>".
func resolveLogGroup(td *types.TaskDefinition, cd types.ContainerDefinition) string {
	if cd.LogConfiguration != nil {
		if g := cd.LogConfiguration.Options["awslogs-group"]; g != "" {
			return g
		}
	}
	return "/ecs/" + td.Family
}
