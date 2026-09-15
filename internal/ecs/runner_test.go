package ecs

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/engine"
	"github.com/aircwo-systems/tarn/internal/logs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// --- fakes -------------------------------------------------------------------

// exitSignal is delivered on a container's wait channel to unblock
// WaitTaskContainer, mimicking a real container exit.
type exitSignal struct {
	code int64
	err  error
}

// fakeEngine is a no-Docker stand-in for taskEngine. It records every call
// in order (globally, and per test assertions can filter by container ID)
// and lets a test control exactly when a container "exits" via its wait
// channel.
type fakeEngine struct {
	mu         sync.Mutex
	calls      []string
	nextID     int
	waitChans  map[string]chan exitSignal
	stoppedIDs map[string]bool
	created    []engine.TaskContainerSpec

	ensureErr     error
	ensureStarted chan struct{}
	ensureRelease chan struct{}
	ensureOnce    sync.Once
	createErr     error
	createErrAt   int
	listResult    []engine.TaskContainerSummary
	listErr       error
	listSelectors []map[string]string
	// delayedStops makes StopContainer leave the matching Docker summary in
	// RUNNING for this many list calls before the test releases the waiters.
	delayedStops map[string]int
	// stopExitCode is the exit code StopContainer delivers to a container's
	// wait channel, mimicking a graceful-stop exit (default 0). Tests that
	// need to tell a graceful exit apart from a context-canceled one set it
	// to something else (e.g. 143, SIGTERM's conventional code).
	stopExitCode int64
	// stopTimeouts records the SIGTERM grace (seconds) each StopContainer
	// call asked for, so tests can assert per-container StopTimeout
	// plumbing without a Docker daemon.
	stopTimeouts map[string]int
	// scriptLines, when set, are delivered to the FollowContainerLogs
	// callback (with their stream flags) before it blocks on ctx, letting
	// pumpLogs tests feed deterministic stdout/stderr output with no Docker.
	scriptLines []scriptLogLine
	// health maps a container ID to the Docker inspect Health.Status string
	// InspectContainerHealth returns for it ("", "starting", "healthy",
	// "unhealthy"). Missing entries return "" (no HEALTHCHECK defined).
	health    map[string]string
	healthErr error

	// volumeCreateCalls records every EnsureTaskVolume call, in order, so
	// tests can assert driver/driverOpts/labels were plumbed through without
	// a Docker daemon.
	volumeCreateCalls []fakeVolumeCreateCall
	// volumeExists seeds TaskVolumeExists's answer for a given name; a name
	// absent from the map reports false (not found), matching a fresh
	// Docker host with no such volume yet.
	volumeExists    map[string]bool
	volumeExistErr  error
	volumeCreateErr error
	// volumeRemoveCalls records every RemoveTaskVolume call, in order.
	volumeRemoveCalls []string
	volumeRemoveErr   error
	// listVolumesResult is returned verbatim by ListTaskVolumesByLabel,
	// letting startup-recovery tests seed pre-existing Docker volumes with
	// no daemon involved.
	listVolumesResult []engine.TaskVolumeSummary
	listVolumesErr    error
}

// fakeVolumeCreateCall records one EnsureTaskVolume invocation.
type fakeVolumeCreateCall struct {
	Name       string
	Driver     string
	DriverOpts map[string]string
	Labels     map[string]string
}

// setHealth sets the Docker health status InspectContainerHealth reports for
// containerID, safe to call concurrently with the poller goroutine.
func (f *fakeEngine) setHealth(containerID, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.health == nil {
		f.health = make(map[string]string)
	}
	f.health[containerID] = status
}

func (f *fakeEngine) InspectContainerHealth(ctx context.Context, containerID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.healthErr != nil {
		return "", f.healthErr
	}
	return f.health[containerID], nil
}

// scriptLogLine is one canned container log line for fakeEngine.
type scriptLogLine struct {
	line   string
	stderr bool
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		waitChans:    make(map[string]chan exitSignal),
		stoppedIDs:   make(map[string]bool),
		delayedStops: make(map[string]int),
	}
}

func (f *fakeEngine) record(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeEngine) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeEngine) EnsureImageRef(ctx context.Context, ref string) error {
	f.record("ensure:" + ref)
	if f.ensureStarted != nil {
		f.ensureOnce.Do(func() { close(f.ensureStarted) })
	}
	if f.ensureRelease != nil {
		select {
		case <-f.ensureRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.ensureErr
}

func (f *fakeEngine) CreateAndStartTaskContainer(ctx context.Context, spec engine.TaskContainerSpec) (*engine.TaskContainerHandle, error) {
	f.mu.Lock()
	if f.createErr != nil || (f.createErrAt > 0 && f.nextID+1 == f.createErrAt) {
		err := f.createErr
		if err == nil {
			err = fmt.Errorf("create failed at container %d", f.createErrAt)
		}
		f.mu.Unlock()
		return nil, err
	}
	f.nextID++
	id := fmt.Sprintf("container-%d", f.nextID)
	f.waitChans[id] = make(chan exitSignal, 1)
	f.created = append(f.created, spec)
	f.mu.Unlock()

	f.record("create:" + spec.Name)
	return &engine.TaskContainerHandle{ID: id, Name: spec.Name}, nil
}

func (f *fakeEngine) createdSpecs() []engine.TaskContainerSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]engine.TaskContainerSpec(nil), f.created...)
}

func (f *fakeEngine) WaitTaskContainer(ctx context.Context, containerID string) (int64, error) {
	f.mu.Lock()
	ch := f.waitChans[containerID]
	f.mu.Unlock()

	f.record("wait-start:" + containerID)
	select {
	case sig := <-ch:
		f.record("wait-done:" + containerID)
		return sig.code, sig.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (f *fakeEngine) FollowContainerLogs(ctx context.Context, containerID string, onLine func(line string, stderr bool)) error {
	f.record("logs-start:" + containerID)
	f.mu.Lock()
	lines := append([]scriptLogLine(nil), f.scriptLines...)
	f.mu.Unlock()
	for _, l := range lines {
		onLine(l.line, l.stderr)
	}
	<-ctx.Done()
	f.record("logs-done:" + containerID)
	return ctx.Err()
}

func (f *fakeEngine) ListContainersByLabel(ctx context.Context, selector map[string]string) ([]engine.TaskContainerSummary, error) {
	f.mu.Lock()
	f.listSelectors = append(f.listSelectors, cloneStringMap(selector))
	result := append([]engine.TaskContainerSummary(nil), f.listResult...)
	for i := range result {
		if remaining := f.delayedStops[result[i].ID]; remaining > 0 && f.stoppedIDs[result[i].ID] {
			result[i].State = "running"
			f.delayedStops[result[i].ID] = remaining - 1
			if remaining == 1 {
				for j := range f.listResult {
					if f.listResult[j].ID == result[i].ID {
						f.listResult[j].State = "exited"
					}
				}
				if ch := f.waitChans[result[i].ID]; ch != nil {
					select {
					case ch <- exitSignal{code: 0}:
					default:
					}
				}
			}
		}
	}
	err := f.listErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (f *fakeEngine) addExistingContainer(id string) {
	f.mu.Lock()
	f.waitChans[id] = make(chan exitSignal, 1)
	f.mu.Unlock()
}

func (f *fakeEngine) listSelectorsSnapshot() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]map[string]string, len(f.listSelectors))
	for i, selector := range f.listSelectors {
		out[i] = cloneStringMap(selector)
	}
	return out
}

func (f *fakeEngine) RemoveTaskContainer(ctx context.Context, containerID string) error {
	f.record("remove:" + containerID)
	return nil
}

func (f *fakeEngine) StopContainer(ctx context.Context, containerID string, timeoutSec int) error {
	f.record("stop:" + containerID)
	f.mu.Lock()
	if f.stopTimeouts == nil {
		f.stopTimeouts = make(map[string]int)
	}
	f.stopTimeouts[containerID] = timeoutSec
	f.stoppedIDs[containerID] = true
	if _, delayed := f.delayedStops[containerID]; delayed {
		f.mu.Unlock()
		return nil
	}
	ch := f.waitChans[containerID]
	code := f.stopExitCode
	f.mu.Unlock()
	if ch != nil {
		select {
		case ch <- exitSignal{code: code}:
		default:
		}
	}
	return nil
}

func (f *fakeEngine) EnsureTaskVolume(ctx context.Context, name, driver string, driverOpts, labels map[string]string) error {
	f.record("ensure-volume:" + name)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.volumeCreateErr != nil {
		return f.volumeCreateErr
	}
	f.volumeCreateCalls = append(f.volumeCreateCalls, fakeVolumeCreateCall{
		Name: name, Driver: driver, DriverOpts: driverOpts, Labels: labels,
	})
	if f.volumeExists == nil {
		f.volumeExists = make(map[string]bool)
	}
	f.volumeExists[name] = true
	return nil
}

func (f *fakeEngine) TaskVolumeExists(ctx context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.volumeExistErr != nil {
		return false, f.volumeExistErr
	}
	return f.volumeExists[name], nil
}

func (f *fakeEngine) RemoveTaskVolume(ctx context.Context, name string) error {
	f.record("remove-volume:" + name)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.volumeRemoveErr != nil {
		return f.volumeRemoveErr
	}
	f.volumeRemoveCalls = append(f.volumeRemoveCalls, name)
	delete(f.volumeExists, name)
	return nil
}

func (f *fakeEngine) ListTaskVolumesByLabel(ctx context.Context, selector map[string]string) ([]engine.TaskVolumeSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listVolumesErr != nil {
		return nil, f.listVolumesErr
	}
	var out []engine.TaskVolumeSummary
	for _, v := range f.listVolumesResult {
		match := true
		for k, want := range selector {
			if v.Labels[k] != want {
				match = false
				break
			}
		}
		if match {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeEngine) volumeCreateCallsSnapshot() []fakeVolumeCreateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeVolumeCreateCall(nil), f.volumeCreateCalls...)
}

func (f *fakeEngine) volumeRemoveCallsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.volumeRemoveCalls...)
}

// finish delivers an exit signal to a container's wait channel directly,
// for tests that need a specific exit code/error rather than the
// StopContainer-triggered graceful 0.
func (f *fakeEngine) finish(containerID string, code int64, err error) {
	f.mu.Lock()
	ch := f.waitChans[containerID]
	f.mu.Unlock()
	ch <- exitSignal{code: code, err: err}
}

// fakeLogSink is a no-op logSink that records what it was given.
type fakeLogSink struct {
	mu     sync.Mutex
	groups []string
	events []logs.LogEvent
}

func (f *fakeLogSink) CreateLogGroup(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups = append(f.groups, name)
}

func (f *fakeLogSink) PutLogEvents(groupName, streamName string, events []logs.LogEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, events...)
}

// --- test setup ---------------------------------------------------------------

func testConfig() *config.Config {
	return &config.Config{Region: "us-east-1", AccountID: "000000000000"}
}

func newTestRunner(t *testing.T) (*Runner, *Service, *fakeEngine, *fakeLogSink) {
	t.Helper()
	cfg := testConfig()
	svc := NewService(cfg, NewStore(cfg))
	if err := svc.Init(); err != nil {
		t.Fatalf("svc.Init: %v", err)
	}
	eng := newFakeEngine()
	sink := &fakeLogSink{}
	r := NewRunner(cfg, svc, eng, sink)
	return r, svc, eng, sink
}

func registerSingleContainerTaskDef(t *testing.T, svc *Service, family string) *types.TaskDefinition {
	t.Helper()
	out, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: family,
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:    "app",
				Image:   "example/app:latest",
				Command: []string{"orig-cmd"},
				Environment: []types.KeyValuePair{
					{Name: "A", Value: "1"},
					{Name: "B", Value: "2"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	return out.TaskDefinition
}

func TestRunnerMapsTaskResourcesAndNetworkModeToEngine(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family:      "resource-mapping",
		Cpu:         "512",
		Memory:      "256",
		NetworkMode: types.NetworkModeHost,
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "app", Image: "example/app:latest"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if got := out.Tasks[0].Containers[0].ContainerID; got != "container-1" {
		t.Fatalf("persisted runtime ID = %q, want container-1", got)
	}

	specs := eng.createdSpecs()
	if len(specs) != 1 {
		t.Fatalf("created specs = %d, want 1: %+v", len(specs), specs)
	}
	spec := specs[0]
	if spec.CPU != 512 || spec.Memory != 256 || spec.MemoryReservation != 0 {
		t.Fatalf("resource mapping = CPU %d, memory %d, reservation %d; want 512, 256, 0", spec.CPU, spec.Memory, spec.MemoryReservation)
	}
	if spec.NetworkMode != types.NetworkModeHost {
		t.Fatalf("network mode = %q, want %q", spec.NetworkMode, types.NetworkModeHost)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestRunnerResolvesTaskScopedSharedVolumeAcrossContainers verifies a bare
// task-definition volume (no Host, no DockerVolumeConfiguration) mounted by
// two containers resolves to the same Docker named-volume bind on both,
// scoped to this task only — matching AWS's "volume shared within one task"
// semantics for a plain Volume entry with only mountPoints referencing it.
func TestRunnerResolvesTaskScopedSharedVolumeAcrossContainers(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "shared-volume",
		Volumes: []types.Volume{
			{Name: "shared"},
		},
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:        "writer",
				Image:       "example/writer:latest",
				MountPoints: []types.MountPoint{{SourceVolume: "shared", ContainerPath: "/data"}},
			},
			{
				Name:        "reader",
				Image:       "example/reader:latest",
				MountPoints: []types.MountPoint{{SourceVolume: "shared", ContainerPath: "/mnt", ReadOnly: true}},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	taskArn := out.Tasks[0].TaskArn
	wantVolume := "tarn-ecs-task-" + taskIDFromRef(taskArn) + "-shared"

	specs := eng.createdSpecs()
	if len(specs) != 2 {
		t.Fatalf("created specs = %d, want 2: %+v", len(specs), specs)
	}
	byImage := map[string]engine.TaskContainerSpec{}
	for _, s := range specs {
		byImage[s.Image] = s
	}
	writer, ok := byImage["example/writer:latest"]
	if !ok {
		t.Fatalf("no spec for writer container: %+v", specs)
	}
	reader, ok := byImage["example/reader:latest"]
	if !ok {
		t.Fatalf("no spec for reader container: %+v", specs)
	}
	if len(writer.Binds) != 1 || writer.Binds[0] != wantVolume+":/data" {
		t.Fatalf("writer binds = %v, want [%s:/data]", writer.Binds, wantVolume)
	}
	if len(reader.Binds) != 1 || reader.Binds[0] != wantVolume+":/mnt:ro" {
		t.Fatalf("reader binds = %v, want [%s:/mnt:ro]", reader.Binds, wantVolume)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestRunnerResolvesHostVolumeToDirectBindMount verifies a task-definition
// volume with Host.SourcePath set binds that exact host path, and that a
// shared-scope dockerVolumeConfiguration volume gets a stable name derived
// from the account ID (not the task), so two tasks referencing it share the
// same underlying Docker volume.
func TestRunnerResolvesHostVolumeToDirectBindMount(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "host-and-shared-volume",
		Volumes: []types.Volume{
			{Name: "hostvol", Host: &types.HostVolumeProperties{SourcePath: "/host/data"}},
			{Name: "sharedvol", DockerVolumeConfiguration: &types.DockerVolumeConfiguration{Scope: "shared", Autoprovision: true}},
		},
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:  "app",
				Image: "example/app:latest",
				MountPoints: []types.MountPoint{
					{SourceVolume: "hostvol", ContainerPath: "/data"},
					{SourceVolume: "sharedvol", ContainerPath: "/shared"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	if _, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family}); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	specs := eng.createdSpecs()
	if len(specs) != 1 {
		t.Fatalf("created specs = %d, want 1: %+v", len(specs), specs)
	}
	binds := specs[0].Binds
	wantHost := "/host/data:/data"
	wantShared := "tarn-ecs-shared-" + r.cfg.AccountID + "-sharedvol:/shared"
	got := map[string]bool{wantHost: false, wantShared: false}
	for _, b := range binds {
		if _, ok := got[b]; ok {
			got[b] = true
		}
	}
	for want, seen := range got {
		if !seen {
			t.Fatalf("binds = %v, missing expected bind %q", binds, want)
		}
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestRunnerCreatesDockerVolumeWithDriverOptsAndLabels verifies a task-scoped
// dockerVolumeConfiguration's Driver/DriverOpts/Labels reach
// EnsureTaskVolume verbatim, merged with Tarn's own tarn.* labels
// (account/task-arn/volume-scope) so the created volume is identifiable for
// startup recovery.
func TestRunnerCreatesDockerVolumeWithDriverOptsAndLabels(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "volume-driver-opts",
		Volumes: []types.Volume{
			{
				Name: "data",
				DockerVolumeConfiguration: &types.DockerVolumeConfiguration{
					Driver:     "local",
					DriverOpts: map[string]string{"type": "tmpfs"},
					Labels:     map[string]string{"team": "platform"},
				},
			},
		},
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:        "app",
				Image:       "example/app:latest",
				MountPoints: []types.MountPoint{{SourceVolume: "data", ContainerPath: "/data"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("RunTask returned %d tasks, failures=%+v", len(out.Tasks), out.Failures)
	}
	taskArn := out.Tasks[0].TaskArn
	wantName := "tarn-ecs-task-" + taskIDFromRef(taskArn) + "-data"

	calls := eng.volumeCreateCallsSnapshot()
	if len(calls) != 1 {
		t.Fatalf("volume create calls = %d, want 1: %+v", len(calls), calls)
	}
	call := calls[0]
	if call.Name != wantName {
		t.Fatalf("volume name = %q, want %q", call.Name, wantName)
	}
	if call.Driver != "local" {
		t.Fatalf("driver = %q, want local", call.Driver)
	}
	if call.DriverOpts["type"] != "tmpfs" {
		t.Fatalf("driverOpts = %+v, want type=tmpfs", call.DriverOpts)
	}
	if call.Labels["team"] != "platform" {
		t.Fatalf("labels missing user label: %+v", call.Labels)
	}
	if call.Labels[labelAccount] != r.cfg.AccountID || call.Labels[labelTaskArn] != taskArn || call.Labels[labelVolumeScope] != volumeScopeTask {
		t.Fatalf("labels missing tarn.* identification: %+v", call.Labels)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestRunnerFailsLaunchWhenSharedVolumeMissingAndAutoprovisionFalse verifies
// a shared-scope volume with autoprovision=false that doesn't already exist
// on the Docker host fails the task launch with a clear reason, instead of
// RunTask silently creating a "shared" volume ECS itself would have refused
// to create.
func TestRunnerFailsLaunchWhenSharedVolumeMissingAndAutoprovisionFalse(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "volume-no-autoprovision",
		Volumes: []types.Volume{
			{Name: "data", DockerVolumeConfiguration: &types.DockerVolumeConfiguration{Scope: "shared", Autoprovision: false}},
		},
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:        "app",
				Image:       "example/app:latest",
				MountPoints: []types.MountPoint{{SourceVolume: "data", ContainerPath: "/data"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 0 || len(out.Failures) != 1 {
		t.Fatalf("expected one failure and no tasks, got tasks=%+v failures=%+v", out.Tasks, out.Failures)
	}
	if !strings.Contains(out.Failures[0].Detail, "autoprovision is false") {
		t.Fatalf("failure detail = %q, want mention of autoprovision", out.Failures[0].Detail)
	}
	if len(eng.volumeCreateCallsSnapshot()) != 0 {
		t.Fatalf("expected no volume to be created, got %+v", eng.volumeCreateCallsSnapshot())
	}

	r.Stop()
}

// TestRunnerRemovesTaskScopedVolumeOnStopButNotShared verifies finishTask
// removes a task-scoped Docker volume once every container has stopped, but
// never touches a shared-scope volume used by the same task.
func TestRunnerRemovesTaskScopedVolumeOnStopButNotShared(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "volume-remove-on-stop",
		Volumes: []types.Volume{
			{Name: "scratch"},
			{Name: "sharedvol", DockerVolumeConfiguration: &types.DockerVolumeConfiguration{Scope: "shared", Autoprovision: true}},
		},
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:  "app",
				Image: "example/app:latest",
				MountPoints: []types.MountPoint{
					{SourceVolume: "scratch", ContainerPath: "/scratch"},
					{SourceVolume: "sharedvol", ContainerPath: "/shared"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: tdOut.TaskDefinition.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("RunTask returned %d tasks, failures=%+v", len(out.Tasks), out.Failures)
	}
	taskArn := out.Tasks[0].TaskArn
	wantScratchVolume := "tarn-ecs-task-" + taskIDFromRef(taskArn) + "-scratch"
	wantSharedVolume := "tarn-ecs-shared-" + r.cfg.AccountID + "-sharedvol"

	finishAllContainers(eng, 0, nil)

	deadline := time.Now().Add(2 * time.Second)
	for {
		task, err := svc.GetTask(taskArn)
		if err == nil && task.LastStatus == types.TaskStatusStopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not reach STOPPED in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	removed := eng.volumeRemoveCallsSnapshot()
	if len(removed) != 1 || removed[0] != wantScratchVolume {
		t.Fatalf("volume remove calls = %v, want exactly [%s]", removed, wantScratchVolume)
	}
	for _, name := range removed {
		if name == wantSharedVolume {
			t.Fatalf("shared volume %s must never be removed automatically", wantSharedVolume)
		}
	}

	r.Stop()
}

// TestRecoverOrphanTaskVolumesRemovesOnlyStoppedOrUnknownTasks verifies
// startup recovery's orphan volume sweep removes a task-scoped volume
// belonging to a task the store no longer has (or already STOPPED), while
// leaving alone a volume belonging to a task that's still running, and never
// considering a shared-scope volume in the first place (selectorForAccountTaskVolumes
// only ever asks the engine for task-scoped labels).
func TestRecoverOrphanTaskVolumesRemovesOnlyStoppedOrUnknownTasks(t *testing.T) {
	r, _, eng, _ := newTestRunner(t)
	accountID := r.cfg.AccountID

	runningTaskArn := "arn:aws:ecs:us-east-1:000000000000:task/default/running-task"
	goneTaskArn := "arn:aws:ecs:us-east-1:000000000000:task/default/gone-task"

	eng.listVolumesResult = []engine.TaskVolumeSummary{
		{
			Name:   "tarn-ecs-task-running-scratch",
			Labels: taskVolumeLabels(accountID, runningTaskArn, volumeScopeTask),
		},
		{
			Name:   "tarn-ecs-task-gone-scratch",
			Labels: taskVolumeLabels(accountID, goneTaskArn, volumeScopeTask),
		},
	}

	tasksByARN := map[string]*types.Task{
		runningTaskArn: {TaskArn: runningTaskArn, LastStatus: types.TaskStatusRunning},
		// goneTaskArn deliberately absent, simulating a task the store has
		// already pruned.
	}

	r.recoverOrphanTaskVolumes(context.Background(), tasksByARN)

	removed := eng.volumeRemoveCallsSnapshot()
	if len(removed) != 1 || removed[0] != "tarn-ecs-task-gone-scratch" {
		t.Fatalf("removed volumes = %v, want exactly [tarn-ecs-task-gone-scratch]", removed)
	}

	r.Stop()
}

// --- override layering ---------------------------------------------------------

func TestMergeContainerEnvReplacesAndAdds(t *testing.T) {
	base := []types.KeyValuePair{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}}
	override := &types.ContainerOverride{
		Environment: []types.KeyValuePair{{Name: "B", Value: "20"}, {Name: "C", Value: "3"}},
	}

	got := mergeContainerEnv(base, override)

	want := map[string]string{"A": "1", "B": "20", "C": "3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, got[k], v)
		}
	}
}

func TestMergeContainerEnvNilOverrideKeepsBase(t *testing.T) {
	base := []types.KeyValuePair{{Name: "A", Value: "1"}}
	got := mergeContainerEnv(base, nil)
	if got["A"] != "1" || len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestResolveCommandOverrideReplacesEntirely(t *testing.T) {
	cd := types.ContainerDefinition{Command: []string{"orig", "args"}}
	override := &types.ContainerOverride{Command: []string{"new"}}

	got := resolveCommand(cd, override)

	if len(got) != 1 || got[0] != "new" {
		t.Fatalf("got %v, want [new]", got)
	}
}

func TestResolveCommandNoOverrideKeepsDefinition(t *testing.T) {
	cd := types.ContainerDefinition{Command: []string{"orig"}}
	got := resolveCommand(cd, nil)
	if len(got) != 1 || got[0] != "orig" {
		t.Fatalf("got %v, want [orig]", got)
	}
}

func TestValidateOverridesRejectsUnknownContainer(t *testing.T) {
	td := &types.TaskDefinition{
		Family:               "fam",
		ContainerDefinitions: []types.ContainerDefinition{{Name: "app"}},
	}
	overrides := &types.TaskOverride{
		ContainerOverrides: []types.ContainerOverride{{Name: "not-app"}},
	}

	if err := validateOverrides(td, overrides); err == nil {
		t.Fatal("expected error for override naming an unknown container")
	}
}

func TestValidateOverridesAcceptsKnownContainer(t *testing.T) {
	td := &types.TaskDefinition{
		Family:               "fam",
		ContainerDefinitions: []types.ContainerDefinition{{Name: "app"}},
	}
	overrides := &types.TaskOverride{
		ContainerOverrides: []types.ContainerOverride{{Name: "app", Command: []string{"x"}}},
	}
	if err := validateOverrides(td, overrides); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- labels and selectors -------------------------------------------------------

func TestTaskLabelsOmitsServiceWhenStandalone(t *testing.T) {
	labels := taskLabels("acct", "cluster1", "", "arn:task/1")
	if _, ok := labels[labelService]; ok {
		t.Fatalf("expected tarn.service to be absent for a standalone task, got %v", labels)
	}
	if labels[labelAccount] != "acct" || labels[labelCluster] != "cluster1" || labels[labelTaskArn] != "arn:task/1" {
		t.Fatalf("unexpected labels: %v", labels)
	}
}

func TestTaskLabelsIncludesServiceWhenSet(t *testing.T) {
	labels := taskLabels("acct", "cluster1", "svcA", "arn:task/1")
	if labels[labelService] != "svcA" {
		t.Fatalf("expected tarn.service=svcA, got %v", labels)
	}
}

func TestSelectorForServiceIsAScopedSubsetOfSelectorForAccount(t *testing.T) {
	acct := selectorForAccount("acct")
	svc := selectorForService("acct", "cluster1", "svcA")
	for k, v := range acct {
		if svc[k] != v {
			t.Fatalf("selectorForService missing account-level key %s", k)
		}
	}
	if len(svc) <= len(acct) {
		t.Fatalf("expected selectorForService to be more specific than selectorForAccount")
	}
}

// --- Group convention for service-owned tasks -----------------------------------

func TestServiceOwnedTaskGetsGroupConventionAndIsFoundByListTasks(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-a")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcA",
		TaskDefinition: td.Family,
		DesiredCount:   1,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	task, err := r.launchTask(context.Background(), cluster, td, nil, "", serviceGroup("svcA"), "svcA", nil)
	if err != nil {
		t.Fatalf("launchTask: %v", err)
	}
	if task.Group != "service:svcA" {
		t.Fatalf("task.Group = %q, want service:svcA", task.Group)
	}

	out, err := svc.ListTasks(&types.ListTasksInput{ServiceName: "svcA"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(out.TaskArns) != 1 || out.TaskArns[0] != task.TaskArn {
		t.Fatalf("ListTasks with ServiceName filter = %v, want [%s]", out.TaskArns, task.TaskArn)
	}

	// clean up the background goroutines this test started.
	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// finishAllContainers sends an exit signal to every container the fake
// engine has created so far, unblocking their wait goroutines.
func finishAllContainers(eng *fakeEngine, code int64, err error) {
	eng.mu.Lock()
	ids := make([]string, 0, len(eng.waitChans))
	for id := range eng.waitChans {
		ids = append(ids, id)
	}
	eng.mu.Unlock()
	for _, id := range ids {
		select {
		case eng.waitChans[id] <- exitSignal{code: code, err: err}:
		default:
		}
	}
}

// --- exit code nil vs zero -------------------------------------------------------

func TestExitCodeZeroIsNotCollapsedToNil(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 20 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-zero")
	cluster, _ := svc.ResolveCluster("")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d (failures: %v)", len(out.Tasks), out.Failures)
	}
	taskArn := out.Tasks[0].TaskArn

	eng.mu.Lock()
	var containerID string
	for id := range eng.waitChans {
		containerID = id
	}
	eng.mu.Unlock()
	eng.finish(containerID, 0, nil)

	waitForContainerStopped(t, svc, taskArn, "app")
	waitForTaskDesiredStopped(t, svc, taskArn)

	task, err := svc.GetTask(taskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	c := task.Containers[0]
	if c.ExitCode == nil {
		t.Fatal("expected non-nil ExitCode for a real exit 0, got nil")
	}
	if *c.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", *c.ExitCode)
	}
	if task.LastStatus != types.TaskStatusStopped {
		t.Fatalf("LastStatus = %s, want STOPPED", task.LastStatus)
	}
	if task.DesiredStatus != types.TaskDesiredStatusStopped {
		t.Fatalf("DesiredStatus = %s, want STOPPED after natural exit", task.DesiredStatus)
	}
	_ = cluster
	r.Stop()
}

func TestExitCodeNilWhenWaitErrors(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-err")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	taskArn := out.Tasks[0].TaskArn

	eng.mu.Lock()
	var containerID string
	for id := range eng.waitChans {
		containerID = id
	}
	eng.mu.Unlock()
	eng.finish(containerID, 0, fmt.Errorf("wait failed"))

	waitForContainerStopped(t, svc, taskArn, "app")

	task, err := svc.GetTask(taskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Containers[0].ExitCode != nil {
		t.Fatalf("expected nil ExitCode on wait error, got %v", *task.Containers[0].ExitCode)
	}
	r.Stop()
}

func waitForContainerStopped(t *testing.T, svc *Service, taskArn, containerName string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		task, err := svc.GetTask(taskArn)
		if err == nil {
			for _, c := range task.Containers {
				if c.Name == containerName && c.LastStatus == types.TaskStatusStopped {
					return
				}
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for container %s on task %s to reach STOPPED", containerName, taskArn)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func waitForTaskDesiredStopped(t *testing.T, svc *Service, taskArn string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		task, err := svc.GetTask(taskArn)
		if err == nil && task.DesiredStatus == types.TaskDesiredStatusStopped {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for task %s to reach desired STOPPED", taskArn)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// --- cleanup ordering ------------------------------------------------------------

func TestCleanupOrderingReadExitDrainThenRemove(t *testing.T) {
	orig := logDrainGracePeriod
	logDrainGracePeriod = 20 * time.Millisecond
	defer func() { logDrainGracePeriod = orig }()

	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-order")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	taskArn := out.Tasks[0].TaskArn

	eng.mu.Lock()
	var containerID string
	for id := range eng.waitChans {
		containerID = id
	}
	eng.mu.Unlock()
	eng.finish(containerID, 3, nil)

	waitForContainerStopped(t, svc, taskArn, "app")
	waitForCall(t, eng, "remove:"+containerID)

	calls := eng.callLog()
	waitDoneIdx := indexOf(calls, "wait-done:"+containerID)
	logsDoneIdx := indexOf(calls, "logs-done:"+containerID)
	removeIdx := indexOf(calls, "remove:"+containerID)

	if waitDoneIdx < 0 || logsDoneIdx < 0 || removeIdx < 0 {
		t.Fatalf("missing expected calls in log: %v", calls)
	}
	if waitDoneIdx >= removeIdx {
		t.Fatalf("expected wait-done before remove, got %v", calls)
	}
	if logsDoneIdx >= removeIdx {
		t.Fatalf("expected logs-done (drain) before remove, got %v", calls)
	}

	r.Stop()
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func waitForCall(t *testing.T, eng *fakeEngine, call string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		for _, c := range eng.callLog() {
			if c == call {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for call %q", call)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// --- reconcile scaling -----------------------------------------------------------

func TestReconcileRollsServiceTaskAfterTaskDefinitionUpdate(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 20 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	oldTD := registerSingleContainerTaskDef(t, svc, "fam-rollout")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	created, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcRollout",
		TaskDefinition: oldTD.Family,
		DesiredCount:   1,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	// Establish one old-definition replica.
	r.reconcileService(context.Background(), cluster, created.Service)
	oldTasks := svc.store.ListTasks(cluster.ClusterArn)
	if len(oldTasks) != 1 {
		t.Fatalf("old service tasks = %d, want 1", len(oldTasks))
	}
	oldTask := oldTasks[0]

	newTD := registerSingleContainerTaskDef(t, svc, "fam-rollout")
	updated, err := svc.UpdateService(&types.UpdateServiceInput{
		Service:        created.Service.ServiceName,
		TaskDefinition: newTD.TaskDefinitionArn,
	})
	if err != nil {
		t.Fatalf("UpdateService: %v", err)
	}
	service := updated.Service
	eng.listResult = []engine.TaskContainerSummary{{ID: "container-1", State: "running"}}

	// The first rollout pass only stops the old replica. It must not launch
	// a replacement while that replica is still present.
	r.reconcileService(context.Background(), cluster, service)
	if got := countPrefix(eng.callLog(), "stop:"); got != 1 {
		t.Fatalf("stop calls after update = %d, want 1: %v", got, eng.callLog())
	}
	if got := countPrefix(eng.callLog(), "create:"); got != 1 {
		t.Fatalf("create calls after update = %d, want 1: %v", got, eng.callLog())
	}
	stopping, err := svc.GetTask(oldTask.TaskArn)
	if err != nil {
		t.Fatalf("GetTask old task: %v", err)
	}
	if stopping.DesiredStatus != types.TaskDesiredStatusStopped {
		t.Fatalf("old task desired status = %q, want STOPPED", stopping.DesiredStatus)
	}

	// A second pass cannot stop or launch again while the old container is
	// still visible. This is the one-at-a-time rollout bound.
	r.reconcileService(context.Background(), cluster, service)
	if got := countPrefix(eng.callLog(), "stop:"); got != 1 {
		t.Fatalf("duplicate old-task stop calls = %d, want 1: %v", got, eng.callLog())
	}
	if got := countPrefix(eng.callLog(), "create:"); got != 1 {
		t.Fatalf("premature replacement creates = %d, want 1: %v", got, eng.callLog())
	}

	waitForContainerStopped(t, svc, oldTask.TaskArn, "app")
	waitForCall(t, eng, "remove:container-1")
	eng.listResult = []engine.TaskContainerSummary{{ID: "container-1", State: "exited"}}
	r.reconcileService(context.Background(), cluster, service)
	if got := countPrefix(eng.callLog(), "create:"); got != 2 {
		t.Fatalf("replacement creates after old task stopped = %d, want 2: %v", got, eng.callLog())
	}
	currentTasks := svc.store.ListTasks(cluster.ClusterArn)
	if len(currentTasks) != 2 {
		t.Fatalf("service task records = %d, want old plus replacement", len(currentTasks))
	}
	var replacement *types.Task
	for _, task := range currentTasks {
		if task.TaskArn != oldTask.TaskArn {
			replacement = task
		}
	}
	if replacement == nil || replacement.TaskDefinitionArn != newTD.TaskDefinitionArn {
		t.Fatalf("replacement task = %+v, want definition %s", replacement, newTD.TaskDefinitionArn)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func TestReconcileRolloutStopsOnlyOneOldTaskWhenDefinitionsAreMixed(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	oldTD := registerSingleContainerTaskDef(t, svc, "fam-mixed-rollout")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	created, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcMixedRollout",
		TaskDefinition: oldTD.Family,
		DesiredCount:   2,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	r.reconcileService(context.Background(), cluster, created.Service)

	newTD := registerSingleContainerTaskDef(t, svc, "fam-mixed-rollout")
	updated, err := svc.UpdateService(&types.UpdateServiceInput{
		Service:        created.Service.ServiceName,
		TaskDefinition: newTD.TaskDefinitionArn,
	})
	if err != nil {
		t.Fatalf("UpdateService: %v", err)
	}
	service := updated.Service
	newTask, err := r.launchTask(context.Background(), cluster, newTD, nil, service.LaunchType, serviceGroup(service.ServiceName), service.ServiceName, nil)
	if err != nil {
		t.Fatalf("launchTask new definition: %v", err)
	}
	eng.listResult = []engine.TaskContainerSummary{
		{ID: "container-1", State: "running"},
		{ID: "container-2", State: "running"},
		{ID: "container-3", State: "running"},
	}

	r.reconcileService(context.Background(), cluster, service)
	if got := countPrefix(eng.callLog(), "stop:"); got != 1 {
		t.Fatalf("mixed rollout stop calls = %d, want 1: %v", got, eng.callLog())
	}
	oldTasks := svc.store.ListTasks(cluster.ClusterArn)
	oldStopping := 0
	for _, task := range oldTasks {
		if task.TaskDefinitionArn == oldTD.TaskDefinitionArn && task.DesiredStatus == types.TaskDesiredStatusStopped {
			oldStopping++
		}
		if task.TaskArn == newTask.TaskArn && task.DesiredStatus == types.TaskDesiredStatusStopped {
			t.Fatalf("new-definition task was selected for stop: %+v", task)
		}
	}
	if oldStopping != 1 {
		t.Fatalf("old tasks marked stopped = %d, want 1", oldStopping)
	}

	// The second old task waits until the first old task finishes.
	r.reconcileService(context.Background(), cluster, service)
	if got := countPrefix(eng.callLog(), "stop:"); got != 1 {
		t.Fatalf("mixed rollout stopped multiple old tasks = %d, want 1: %v", got, eng.callLog())
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func TestReconcileSameTaskDefinitionDoesNotRollServiceTasks(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-noop-rollout")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	created, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcNoopRollout",
		TaskDefinition: td.Family,
		DesiredCount:   1,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	r.reconcileService(context.Background(), cluster, created.Service)
	eng.listResult = []engine.TaskContainerSummary{{ID: "container-1", State: "running"}}

	updated, err := svc.UpdateService(&types.UpdateServiceInput{
		Service:        created.Service.ServiceName,
		TaskDefinition: td.TaskDefinitionArn,
	})
	if err != nil {
		t.Fatalf("UpdateService with same definition: %v", err)
	}
	r.reconcileService(context.Background(), cluster, updated.Service)

	if got := countPrefix(eng.callLog(), "stop:"); got != 0 {
		t.Fatalf("same-definition stop calls = %d, want 0: %v", got, eng.callLog())
	}
	if got := countPrefix(eng.callLog(), "create:"); got != 1 {
		t.Fatalf("same-definition create calls = %d, want 1: %v", got, eng.callLog())
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func TestReconcileScalesUpToDesiredCount(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-scaleup")
	cluster, _ := svc.ResolveCluster("")

	svcOut, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcUp",
		TaskDefinition: td.Family,
		DesiredCount:   3,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	eng.listResult = nil // no containers currently running

	r.reconcileService(context.Background(), cluster, svcOut.Service)

	creates := countPrefix(eng.callLog(), "create:")
	if creates != 3 {
		t.Fatalf("expected 3 creates, got %d (calls: %v)", creates, eng.callLog())
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func TestReconcileScalesDownToDesiredCount(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-scaledown")
	cluster, _ := svc.ResolveCluster("")

	svcOut, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcDown",
		TaskDefinition: td.Family,
		DesiredCount:   3,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	group := serviceGroup("svcDown")
	var taskArns []string
	for i := 0; i < 3; i++ {
		task, err := r.launchTask(context.Background(), cluster, td, nil, "", group, "svcDown", nil)
		if err != nil {
			t.Fatalf("launchTask: %v", err)
		}
		taskArns = append(taskArns, task.TaskArn)
	}

	// Simulate the docker-level view showing all 3 running.
	eng.listResult = []engine.TaskContainerSummary{
		{ID: "x1", State: "running"},
		{ID: "x2", State: "running"},
		{ID: "x3", State: "running"},
	}

	desired := 1
	updated, err := svc.UpdateService(&types.UpdateServiceInput{
		Service:      svcOut.Service.ServiceName,
		DesiredCount: &desired,
	})
	if err != nil {
		t.Fatalf("UpdateService desired count: %v", err)
	}
	r.reconcileService(context.Background(), cluster, updated.Service)

	stopCount := countPrefix(eng.callLog(), "stop:")
	if stopCount != 2 {
		t.Fatalf("expected 2 stop calls to scale 3->1, got %d (calls: %v)", stopCount, eng.callLog())
	}

	// The lowest-ARN tasks (deterministic ordering) should be the ones
	// stopped; the task record's DesiredStatus reflects that.
	stoppedRecords := 0
	for _, arn := range taskArns {
		task, err := svc.GetTask(arn)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if task.DesiredStatus == types.TaskDesiredStatusStopped {
			stoppedRecords++
		}
	}
	if stoppedRecords != 2 {
		t.Fatalf("expected 2 tasks marked desired-stopped, got %d", stoppedRecords)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestReconcileOnceIgnoresTombstonedService guards the runner half of the
// DeleteService tombstone fix: reconcileOnce iterates every service in the
// store (not just ones it already knows are active), so an INACTIVE
// tombstone left behind by DeleteService must not be reconciled toward its
// old DesiredCount — that would relaunch tasks for a "deleted" service.
func TestReconcileOnceIgnoresTombstonedService(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-tombstone-reconcile")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcTombstoned",
		TaskDefinition: td.Family,
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, err := svc.DeleteService(&types.DeleteServiceInput{Service: "svcTombstoned"}); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}

	// A tombstone still carries DesiredCount 0, so give reconcileOnce a
	// reason to launch if it mistakenly treated the record as live: bump
	// DesiredCount directly in the store the way a stale in-flight update
	// could race a delete.
	tombstone, err := svc.store.GetService(cluster.ClusterArn, "svcTombstoned")
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	tombstone.DesiredCount = 2
	if err := svc.store.SaveService(tombstone); err != nil {
		t.Fatalf("SaveService: %v", err)
	}

	eng.listResult = nil
	r.reconcileOnce(context.Background())

	if creates := countPrefix(eng.callLog(), "create:"); creates != 0 {
		t.Fatalf("expected no task launches for a tombstoned service, got %d creates (calls: %v)", creates, eng.callLog())
	}
	r.Stop()
}

func TestReconcileScaleDownDoesNotCountTaskDrainingFromDocker(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-scaledown-delay")
	cluster, _ := svc.ResolveCluster("")
	svcOut, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcDownDelay",
		TaskDefinition: td.Family,
		DesiredCount:   2,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	group := serviceGroup("svcDownDelay")
	first, err := r.launchTask(context.Background(), cluster, td, nil, "", group, "svcDownDelay", nil)
	if err != nil {
		t.Fatalf("launch first task: %v", err)
	}
	second, err := r.launchTask(context.Background(), cluster, td, nil, "", group, "svcDownDelay", nil)
	if err != nil {
		t.Fatalf("launch second task: %v", err)
	}
	eng.listResult = []engine.TaskContainerSummary{
		{ID: "container-1", State: "running"},
		{ID: "container-2", State: "running"},
	}
	eng.mu.Lock()
	eng.delayedStops["container-1"] = 1
	eng.delayedStops["container-2"] = 1
	eng.mu.Unlock()

	desired := 1
	updated, err := svc.UpdateService(&types.UpdateServiceInput{
		Service:      svcOut.Service.ServiceName,
		DesiredCount: &desired,
	})
	if err != nil {
		t.Fatalf("UpdateService: %v", err)
	}
	r.reconcileService(context.Background(), cluster, updated.Service)
	r.reconcileService(context.Background(), cluster, updated.Service)

	if got := countPrefix(eng.callLog(), "stop:"); got != 1 {
		t.Fatalf("scale-down stop calls while first task drains = %d, want 1: %v", got, eng.callLog())
	}
	firstRecord, err := svc.GetTask(first.TaskArn)
	if err != nil {
		t.Fatalf("GetTask first: %v", err)
	}
	secondRecord, err := svc.GetTask(second.TaskArn)
	if err != nil {
		t.Fatalf("GetTask second: %v", err)
	}
	stopped := 0
	if firstRecord.DesiredStatus == types.TaskDesiredStatusStopped {
		stopped++
	}
	if secondRecord.DesiredStatus == types.TaskDesiredStatusStopped {
		stopped++
	}
	if stopped != 1 {
		t.Fatalf("reconcile stopped %d healthy tasks while one task drains: first=%+v second=%+v", stopped, firstRecord, secondRecord)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func TestPartialTaskLaunchStopsContainersAlreadyStarted(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	_, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "fam-partial-launch",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "one", Image: "example/one:latest"},
			{Name: "two", Image: "example/two:latest"},
			{Name: "three", Image: "example/three:latest"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	eng.createErrAt = 3

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: "fam-partial-launch"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 0 || len(out.Failures) != 1 {
		t.Fatalf("unexpected partial launch result: %+v", out)
	}
	for _, id := range []string{"container-1", "container-2"} {
		if indexOf(eng.callLog(), "stop:"+id) < 0 {
			t.Fatalf("started container %s was not stopped after launch failure: %v", id, eng.callLog())
		}
	}
	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func TestStopTaskRacingWithLaunchStopsContainerCreatedAfterRequest(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-stop-race")
	eng.ensureStarted = make(chan struct{})
	eng.ensureRelease = make(chan struct{})

	runDone := make(chan *types.RunTaskOutput, 1)
	go func() {
		out, _ := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
		runDone <- out
	}()
	select {
	case <-eng.ensureStarted:
	case <-time.After(time.Second):
		t.Fatal("launch did not reach EnsureImageRef")
	}

	deadline := time.After(time.Second)
	var task *types.Task
	for task == nil {
		tasks := svc.store.ListTasks("")
		if len(tasks) > 0 {
			task = tasks[0]
			break
		}
		select {
		case <-deadline:
			t.Fatal("task record was not created")
		case <-time.After(time.Millisecond):
		}
	}
	if err := r.StopTask(context.Background(), "", task.TaskArn, "race stop"); err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	close(eng.ensureRelease)

	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("RunTask did not finish after releasing launch")
	}
	recorded, err := svc.GetTask(task.TaskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if recorded.DesiredStatus != types.TaskDesiredStatusStopped || recorded.LastStatus == types.TaskStatusRunning {
		t.Fatalf("task became running after StopTask: %+v", recorded)
	}
	if indexOf(eng.callLog(), "stop:container-1") < 0 {
		t.Fatalf("container created after StopTask was not stopped: %v", eng.callLog())
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

func countPrefix(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

// --- shutdown ordering and goroutine hygiene -------------------------------------

func TestStopHaltsReconcileLoopBeforeStoppingTasksAndLeaksNoGoroutines(t *testing.T) {
	origTick := reconcileTickInterval
	reconcileTickInterval = 5 * time.Millisecond
	defer func() { reconcileTickInterval = origTick }()

	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-shutdown")

	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcShutdown",
		TaskDefinition: td.Family,
		DesiredCount:   1,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	// The fake never reports any containers as running via
	// ListContainersByLabel (decoupled from bookkeeping), so every tick the
	// loop believes it needs to launch another replica. This is exactly the
	// runaway that halting the loop before stopping tasks must prevent.
	eng.listResult = nil

	r.Start()
	time.Sleep(40 * time.Millisecond) // allow several ticks to fire

	r.Stop()

	countAtStopReturn := countPrefix(eng.callLog(), "create:")
	if countAtStopReturn == 0 {
		t.Fatal("expected the reconcile loop to have launched at least one task before Stop")
	}

	// If the loop were still ticking after Stop returned, more creates
	// would appear here. Stop's internal ordering (halt the loop, wait for
	// it, only then stop tasks) is what prevents that.
	time.Sleep(40 * time.Millisecond)
	countAfterWait := countPrefix(eng.callLog(), "create:")
	if countAfterWait != countAtStopReturn {
		t.Fatalf("reconcile loop kept running after Stop: creates went from %d to %d", countAtStopReturn, countAfterWait)
	}

	// Stop() only returns once every pump/wait goroutine has finished
	// (r.wg.Wait()), so every tracked container must have been removed.
	creates := countAtStopReturn
	removes := countPrefix(eng.callLog(), "remove:")
	if removes != creates {
		t.Fatalf("expected every created container (%d) to be removed by the time Stop returned, got %d removes", creates, removes)
	}

	r.mu.Lock()
	remaining := len(r.tasks)
	r.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected no tracked tasks after Stop, got %d", remaining)
	}
}

// --- StopTask -----------------------------------------------------------------

func TestStopTaskMarksRecordAndStopsContainer(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-stoptask")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	taskArn := out.Tasks[0].TaskArn

	if err := r.StopTask(context.Background(), "", taskArn, "manual stop"); err != nil {
		t.Fatalf("StopTask: %v", err)
	}

	waitForContainerStopped(t, svc, taskArn, "app")

	task, err := svc.GetTask(taskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.DesiredStatus != types.TaskDesiredStatusStopped {
		t.Fatalf("DesiredStatus = %s, want STOPPED", task.DesiredStatus)
	}
	if task.StoppedReason != "manual stop" {
		t.Fatalf("StoppedReason = %q, want %q", task.StoppedReason, "manual stop")
	}

	stopCalled := false
	for _, c := range eng.callLog() {
		if len(c) >= 5 && c[:5] == "stop:" {
			stopCalled = true
		}
	}
	if !stopCalled {
		t.Fatal("expected StopContainer to have been called")
	}

	r.Stop()
}

// --- Count > 1 -------------------------------------------------------------------

func TestRunTaskLaunchesCountIndependentTasks(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-count")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family, Count: 3})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(out.Tasks))
	}
	seen := map[string]bool{}
	for _, task := range out.Tasks {
		if seen[task.TaskArn] {
			t.Fatalf("duplicate task ARN %s", task.TaskArn)
		}
		seen[task.TaskArn] = true
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestRunTaskPropagatesTagsFromTaskDefinition guards RunTask's
// PropagateTags=TASK_DEFINITION path: tags from the task definition are
// copied onto the launched task, merged with (not overridden by) any
// explicit RunTask tags of the same key.
func TestRunTaskPropagatesTagsFromTaskDefinition(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	tdOut, err := svc.RegisterTaskDefinition(&types.RegisterTaskDefinitionInput{
		Family: "fam-propagate-tags",
		ContainerDefinitions: []types.ContainerDefinition{
			{Name: "app", Image: "example/app:latest"},
		},
		Tags: []types.Tag{{Key: "env", Value: "prod"}, {Key: "owner", Value: "team-a"}},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{
		TaskDefinition: tdOut.TaskDefinition.Family,
		PropagateTags:  "TASK_DEFINITION",
		Tags:           []types.Tag{{Key: "owner", Value: "explicit-wins"}},
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(out.Tasks))
	}

	task, err := svc.GetTask(out.Tasks[0].TaskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	byKey := map[string]string{}
	for _, tag := range task.Tags {
		byKey[tag.Key] = tag.Value
	}
	if byKey["env"] != "prod" {
		t.Fatalf("expected propagated env=prod tag, got %+v", task.Tags)
	}
	if byKey["owner"] != "explicit-wins" {
		t.Fatalf("expected explicit RunTask tag to win over propagated, got %+v", task.Tags)
	}

	// Without PropagateTags, only explicit RunTask tags land on the task.
	out2, err := r.RunTask(context.Background(), &types.RunTaskInput{
		TaskDefinition: tdOut.TaskDefinition.Family,
		Tags:           []types.Tag{{Key: "solo", Value: "yes"}},
	})
	if err != nil {
		t.Fatalf("RunTask (no propagate): %v", err)
	}
	task2, err := svc.GetTask(out2.Tasks[0].TaskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(task2.Tags) != 1 || task2.Tags[0].Key != "solo" {
		t.Fatalf("expected only explicit tag without PropagateTags, got %+v", task2.Tags)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestServicePropagatesTagsToLaunchedTasks guards CreateService's
// PropagateTags=SERVICE path: the reconcile loop's launched tasks inherit
// the service's tags.
func TestServicePropagatesTagsToLaunchedTasks(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-service-propagate-tags")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}

	createOut, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "propagate-svc",
		TaskDefinition: td.Family,
		DesiredCount:   1,
		PropagateTags:  "SERVICE",
		Tags:           []types.Tag{{Key: "env", Value: "prod"}},
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	r.reconcileService(context.Background(), cluster, createOut.Service)

	tasks := svc.store.ListTasks(cluster.ClusterArn)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 launched task, got %d", len(tasks))
	}
	if len(tasks[0].Tags) != 1 || tasks[0].Tags[0].Key != "env" || tasks[0].Tags[0].Value != "prod" {
		t.Fatalf("expected launched task to inherit service tags, got %+v", tasks[0].Tags)
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// --- startup recovery ----------------------------------------------------------

func TestRunnerRecoversPersistedRuntimeIDAndLifecycle(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 20 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-recover")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	task, err := svc.NewTaskRecord(cluster, td, nil, types.LaunchTypeFargate, "")
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}
	if _, err := svc.SetContainerID(task.TaskArn, "app", "persisted-runtime"); err != nil {
		t.Fatalf("SetContainerID: %v", err)
	}
	if _, err := svc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
		t.Fatalf("SetTaskStatus: %v", err)
	}

	eng.addExistingContainer("persisted-runtime")
	eng.listResult = []engine.TaskContainerSummary{{
		ID:    "persisted-runtime",
		State: "running",
		Labels: map[string]string{
			labelAccount: testConfig().AccountID,
			labelTaskArn: task.TaskArn,
		},
	}}

	r.Start()
	waitForCall(t, eng, "wait-start:persisted-runtime")

	recovered, err := svc.GetTask(task.TaskArn)
	if err != nil {
		t.Fatalf("GetTask after recovery: %v", err)
	}
	if recovered.Containers[0].ContainerID != "persisted-runtime" {
		t.Fatalf("recovered runtime ID = %q, want persisted-runtime", recovered.Containers[0].ContainerID)
	}
	if recovered.LastStatus != types.TaskStatusRunning {
		t.Fatalf("recovered task status = %q, want RUNNING", recovered.LastStatus)
	}

	r.mu.Lock()
	rt, ok := r.tasks[task.TaskArn]
	r.mu.Unlock()
	if !ok {
		t.Fatal("expected recovered task in runner bookkeeping")
	}
	rt.mu.Lock()
	gotID := rt.containerIDs["app"]
	rt.mu.Unlock()
	if gotID != "persisted-runtime" {
		t.Fatalf("recovered bookkeeping runtime ID = %q, want persisted-runtime", gotID)
	}

	eng.finish("persisted-runtime", 0, nil)
	waitForTaskDesiredStopped(t, svc, task.TaskArn)
	waitForCall(t, eng, "remove:persisted-runtime")
	r.Stop()
}

func TestRunnerRecoveryMarksMissingContainerStoppedWithoutDeletingTask(t *testing.T) {
	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-missing")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	task, err := svc.NewTaskRecord(cluster, td, nil, types.LaunchTypeFargate, "")
	if err != nil {
		t.Fatalf("NewTaskRecord: %v", err)
	}
	if _, err := svc.SetContainerID(task.TaskArn, "app", "missing-runtime"); err != nil {
		t.Fatalf("SetContainerID: %v", err)
	}
	if _, err := svc.SetTaskStatus(task.TaskArn, types.TaskStatusRunning); err != nil {
		t.Fatalf("SetTaskStatus: %v", err)
	}

	eng.listResult = nil
	r.Start()
	r.Stop()

	recovered, err := svc.GetTask(task.TaskArn)
	if err != nil {
		t.Fatalf("missing task record after recovery: %v", err)
	}
	if recovered.LastStatus != types.TaskStatusStopped {
		t.Fatalf("missing task status = %q, want STOPPED", recovered.LastStatus)
	}
	if recovered.DesiredStatus != types.TaskDesiredStatusStopped {
		t.Fatalf("missing task desired status = %q, want STOPPED", recovered.DesiredStatus)
	}
	container := recovered.Containers[0]
	if container.LastStatus != types.TaskStatusStopped {
		t.Fatalf("missing container status = %q, want STOPPED", container.LastStatus)
	}
	if container.Reason != "container missing during ECS runner recovery" {
		t.Fatalf("missing container reason = %q", container.Reason)
	}
	if countPrefix(eng.callLog(), "remove:") != 0 {
		t.Fatalf("missing task recovery removed a container: %v", eng.callLog())
	}
}

func TestRunnerRecoveryCleansUpAccountOrphanContainers(t *testing.T) {
	r, _, eng, _ := newTestRunner(t)
	eng.listResult = []engine.TaskContainerSummary{
		{
			ID:    "orphan-running",
			State: "running",
			Labels: map[string]string{
				labelAccount: testConfig().AccountID,
				labelTaskArn: "arn:aws:ecs:us-east-1:000000000000:task/default/gone",
			},
		},
		{
			ID:    "orphan-stopped",
			State: "exited",
			Labels: map[string]string{
				labelAccount: testConfig().AccountID,
				labelTaskArn: "arn:aws:ecs:us-east-1:000000000000:task/default/gone-stopped",
			},
		},
	}

	r.Start()
	r.Stop()

	calls := eng.callLog()
	if indexOf(calls, "stop:orphan-running") < 0 {
		t.Fatalf("expected running orphan to be stopped, calls: %v", calls)
	}
	for _, id := range []string{"orphan-running", "orphan-stopped"} {
		if indexOf(calls, "remove:"+id) < 0 {
			t.Fatalf("expected orphan %s to be removed, calls: %v", id, calls)
		}
	}
	if indexOf(calls, "stop:orphan-running") > indexOf(calls, "remove:orphan-running") {
		t.Fatalf("expected running orphan stop before remove, calls: %v", calls)
	}

	selectors := eng.listSelectorsSnapshot()
	if len(selectors) != 1 || selectors[0][labelAccount] != testConfig().AccountID {
		t.Fatalf("startup recovery selector = %v, want account-scoped selector", selectors)
	}
}

// --- graceful shutdown vs. lifecycle context -------------------------------------

// TestStopRecordsRealExitCodeInsteadOfContextCanceled guards against Stop
// cancelling the lifecycle wait context before asking Docker to gracefully
// stop the container: the wait goroutine's WaitTaskContainer must observe
// the container's real exit (StopContainer's graceful signal, here 143) and
// not "context canceled", and StopContainer must be called before
// RemoveTaskContainer so the container isn't force-removed while the
// graceful stop is still racing it.
func TestStopRecordsRealExitCodeInsteadOfContextCanceled(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	eng.stopExitCode = 143
	td := registerSingleContainerTaskDef(t, svc, "fam-graceful-stop")

	out, err := r.RunTask(context.Background(), &types.RunTaskInput{TaskDefinition: td.Family})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	taskArn := out.Tasks[0].TaskArn

	r.Stop()

	task, err := svc.GetTask(taskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	c := task.Containers[0]
	if c.ExitCode == nil {
		t.Fatalf("expected a real exit code recorded during shutdown, got nil (reason=%q)", c.Reason)
	}
	if *c.ExitCode != 143 {
		t.Fatalf("ExitCode = %d, want 143 (graceful stop signal, not context-canceled)", *c.ExitCode)
	}

	calls := eng.callLog()
	stopIdx := indexOf(calls, "stop:container-1")
	removeIdx := indexOf(calls, "remove:container-1")
	if stopIdx < 0 || removeIdx < 0 {
		t.Fatalf("missing expected calls: %v", calls)
	}
	if stopIdx >= removeIdx {
		t.Fatalf("expected graceful stop before force-remove, calls: %v", calls)
	}
}

// --- per-service drain gate --------------------------------------------------------

// TestDrainServiceDoesNotBlockOnAnUnrelatedServicesLaunch guards against
// DrainService taking the account-wide launchGate: a launch for one service
// stuck in EnsureImageRef (e.g. a slow image pull) must not stall
// DrainService for a different service, which used to share that single
// account-wide lock.
func TestDrainServiceDoesNotBlockOnAnUnrelatedServicesLaunch(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	tdA := registerSingleContainerTaskDef(t, svc, "fam-drain-gate-a")
	tdB := registerSingleContainerTaskDef(t, svc, "fam-drain-gate-b")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcGateA",
		TaskDefinition: tdA.Family,
		DesiredCount:   1,
	}); err != nil {
		t.Fatalf("CreateService A: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcGateB",
		TaskDefinition: tdB.Family,
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService B: %v", err)
	}

	eng.ensureStarted = make(chan struct{})
	eng.ensureRelease = make(chan struct{})

	launchDone := make(chan struct{})
	go func() {
		_, _ = r.launchTask(context.Background(), cluster, tdA, nil, "", serviceGroup("svcGateA"), "svcGateA", nil)
		close(launchDone)
	}()
	select {
	case <-eng.ensureStarted:
	case <-time.After(time.Second):
		t.Fatal("service A launch did not reach EnsureImageRef")
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := r.DrainService(drainCtx, cluster, "svcGateB"); err != nil {
		t.Fatalf("DrainService for unrelated service B blocked on service A's in-flight launch: %v", err)
	}

	close(eng.ensureRelease)
	select {
	case <-launchDone:
	case <-time.After(time.Second):
		t.Fatal("service A launch did not finish after release")
	}

	finishAllContainers(eng, 0, nil)
	r.Stop()
}

// TestDrainServiceRejectsLaunchAfterItConcludes guards the other half of the
// per-service gate: once DrainService has returned, a launch for that same
// service must still be rejected, even though the gate's lock itself was
// released when the function returned.
func TestDrainServiceRejectsLaunchAfterItConcludes(t *testing.T) {
	r, svc, _, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-drain-reject")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcRejectAfterDrain",
		TaskDefinition: td.Family,
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	// Mimic DeleteService(Force): the DRAINING status is persisted before the
	// drainer runs.
	record, err := svc.store.GetService(cluster.ClusterArn, "svcRejectAfterDrain")
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	record.Status = types.ServiceStatusDraining
	if err := svc.store.SaveService(record); err != nil {
		t.Fatalf("SaveService: %v", err)
	}

	if err := r.DrainService(context.Background(), cluster, "svcRejectAfterDrain"); err != nil {
		t.Fatalf("DrainService: %v", err)
	}

	if _, err := r.launchTask(context.Background(), cluster, td, nil, "", serviceGroup("svcRejectAfterDrain"), "svcRejectAfterDrain", nil); err == nil {
		t.Fatal("expected launch for a service already drained to be rejected")
	}

	// A service recreated under the same name after deletion must be able to
	// launch again.
	if err := svc.store.DeleteService(cluster.ClusterArn, "svcRejectAfterDrain"); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcRejectAfterDrain",
		TaskDefinition: td.Family,
		DesiredCount:   1,
	}); err != nil {
		t.Fatalf("recreate CreateService: %v", err)
	}
	if _, err := r.launchTask(context.Background(), cluster, td, nil, "", serviceGroup("svcRejectAfterDrain"), "svcRejectAfterDrain", nil); err != nil {
		t.Fatalf("launch for recreated service rejected: %v", err)
	}

	r.Stop()
}

// TestDrainServiceStopsTrackedTaskAndWaitsForRealExitCode guards two related
// bugs in DrainService: (1) DeleteService always marks a service's tasks
// DesiredStatus STOPPED before calling the drainer, so a loop here that skips
// tasks whose DesiredStatus is already STOPPED is dead code that never
// actually stops a tracked task's container through this runner's own
// bookkeeping; and (2) DrainService used to force every task straight to
// STOPPED as soon as Docker reported no live container, racing the lifecycle
// goroutine that records the container's real exit code — so DrainService
// could return before that exit code was actually recorded.
func TestDrainServiceStopsTrackedTaskAndWaitsForRealExitCode(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-drain-dead-loop")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcDrainDeadLoop",
		TaskDefinition: td.Family,
		DesiredCount:   1,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	task, err := r.launchTask(context.Background(), cluster, td, nil, "", serviceGroup("svcDrainDeadLoop"), "svcDrainDeadLoop", nil)
	if err != nil {
		t.Fatalf("launchTask: %v", err)
	}

	// Mimic DeleteService(Force): it marks every active task's DesiredStatus
	// STOPPED and persists it before ever invoking the drainer.
	if _, err := svc.StopTaskRecord(task.TaskArn, "service force-deleted"); err != nil {
		t.Fatalf("StopTaskRecord: %v", err)
	}

	eng.listResult = []engine.TaskContainerSummary{{ID: "container-1", State: "running"}}
	eng.mu.Lock()
	eng.delayedStops["container-1"] = 1
	eng.mu.Unlock()

	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.DrainService(drainCtx, cluster, "svcDrainDeadLoop"); err != nil {
		t.Fatalf("DrainService: %v", err)
	}

	if indexOf(eng.callLog(), "stop:container-1") < 0 {
		t.Fatalf("DrainService never asked Docker to stop the tracked container: %v", eng.callLog())
	}

	finalTask, err := svc.GetTask(task.TaskArn)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if finalTask.LastStatus != types.TaskStatusStopped {
		t.Fatalf("task LastStatus = %s, want STOPPED once DrainService returns", finalTask.LastStatus)
	}
	c := finalTask.Containers[0]
	if c.ExitCode == nil {
		t.Fatalf("DrainService returned before the container's real exit code was recorded (container LastStatus=%s) — STOPPED status and exit code raced", c.LastStatus)
	}

	r.Stop()
}

// --- crash-loop backoff -----------------------------------------------------------

// TestServiceLaunchBackoffGrowsOnRepeatedInstantCrashes guards against
// resetting backoff as soon as a service's containers start: a service whose
// container crashes immediately after start must see growing retry delays,
// not relaunch on every reconcile tick forever.
func TestServiceLaunchBackoffGrowsOnRepeatedInstantCrashes(t *testing.T) {
	origGrace := logDrainGracePeriod
	logDrainGracePeriod = 10 * time.Millisecond
	defer func() { logDrainGracePeriod = origGrace }()

	r, svc, eng, _ := newTestRunner(t)
	td := registerSingleContainerTaskDef(t, svc, "fam-crashloop")
	cluster, err := svc.ResolveCluster("")
	if err != nil {
		t.Fatalf("ResolveCluster: %v", err)
	}
	if _, err := svc.CreateService(&types.CreateServiceInput{
		ServiceName:    "svcCrashLoop",
		TaskDefinition: td.Family,
		DesiredCount:   0,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	key := serviceKey(cluster.ClusterArn, "svcCrashLoop")

	var delays []time.Duration
	for i := 0; i < 3; i++ {
		before := time.Now()
		task, err := r.launchTask(context.Background(), cluster, td, nil, "", serviceGroup("svcCrashLoop"), "svcCrashLoop", nil)
		if err != nil {
			t.Fatalf("launchTask %d: %v", i, err)
		}
		containerID := task.Containers[0].ContainerID
		if containerID == "" {
			t.Fatalf("iteration %d: launched task has no recorded container ID", i)
		}
		eng.finish(containerID, 1, nil)
		waitForTaskDesiredStopped(t, svc, task.TaskArn)

		r.backoffMu.Lock()
		state := r.launchBackoff[key]
		r.backoffMu.Unlock()
		if state.failures != i+1 {
			t.Fatalf("iteration %d: failures = %d, want %d", i, state.failures, i+1)
		}
		delays = append(delays, state.nextRetry.Sub(before))
	}

	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] {
			t.Fatalf("expected strictly growing backoff delays, got %v", delays)
		}
	}
}

// --- container log levels ----------------------------------------------------

// TestPumpLogsMarksStderrLinesAsError verifies the stderr fix: plain
// containers carry no level metadata, so anything they write to stderr (e.g.
// Node's console.error) must surface as ERROR instead of being keyword-filed
// as INFO. Explicit tokens still win over the stream default.
func TestPumpLogsMarksStderrLinesAsError(t *testing.T) {
	r, _, eng, sink := newTestRunner(t)
	eng.scriptLines = []scriptLogLine{
		{line: "serving on :8080", stderr: false},
		{line: "publish attempt 1 failed: HTTP 503", stderr: true},
		{line: "WARN retrying in 100ms", stderr: true},
		{line: "boom: unexpected ERROR talking to queue", stderr: false},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.pumpLogs(ctx, "/ecs/test", "stream-1", "container-1")
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sink.mu.Lock()
		n := len(sink.events)
		sink.mu.Unlock()
		if n >= 4 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatalf("timed out waiting for pumped events, got %d", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 4 {
		t.Fatalf("events = %d, want 4", len(sink.events))
	}
	wantLevels := []logs.LogLevel{logs.LevelINFO, logs.LevelERROR, logs.LevelWARN, logs.LevelERROR}
	for i, want := range wantLevels {
		if sink.events[i].Level != want {
			t.Errorf("event %d (%q): level = %s, want %s", i, sink.events[i].Message, sink.events[i].Level, want)
		}
	}
}

func TestLogContainerExit(t *testing.T) {
	newRunner := func(t *testing.T) (*Runner, *fakeLogSink) {
		t.Helper()
		r, _, _, sink := newTestRunner(t)
		return r, sink
	}
	essentialRT := func() *runningTask {
		return &runningTask{essential: map[string]bool{"app": true, "sidecar": false}}
	}
	code3 := int64(3)
	code0 := int64(0)

	t.Run("nonzero essential exit logs ERROR with code", func(t *testing.T) {
		r, sink := newRunner(t)
		r.logContainerExit("/ecs/g", "s", "app", &code3, "", essentialRT())
		sink.mu.Lock()
		defer sink.mu.Unlock()
		if len(sink.events) != 1 {
			t.Fatalf("events = %d, want 1", len(sink.events))
		}
		ev := sink.events[0]
		if ev.Level != logs.LevelERROR {
			t.Errorf("level = %s, want ERROR", ev.Level)
		}
		if !strings.Contains(ev.Message, `"app"`) || !strings.Contains(ev.Message, "3") {
			t.Errorf("message should name the container and code, got %q", ev.Message)
		}
	})

	t.Run("missing exit code logs ERROR with reason", func(t *testing.T) {
		r, sink := newRunner(t)
		r.logContainerExit("/ecs/g", "s", "app", nil, "container gone", essentialRT())
		sink.mu.Lock()
		defer sink.mu.Unlock()
		if len(sink.events) != 1 {
			t.Fatalf("events = %d, want 1", len(sink.events))
		}
		if sink.events[0].Level != logs.LevelERROR {
			t.Errorf("level = %s, want ERROR", sink.events[0].Level)
		}
		if !strings.Contains(sink.events[0].Message, "container gone") {
			t.Errorf("message should carry the reason, got %q", sink.events[0].Message)
		}
	})

	t.Run("clean exit stays silent", func(t *testing.T) {
		r, sink := newRunner(t)
		r.logContainerExit("/ecs/g", "s", "app", &code0, "", essentialRT())
		sink.mu.Lock()
		defer sink.mu.Unlock()
		if len(sink.events) != 0 {
			t.Fatalf("events = %d, want 0", len(sink.events))
		}
	})

	t.Run("requested stop stays silent", func(t *testing.T) {
		r, sink := newRunner(t)
		rt := essentialRT()
		rt.stopRequested = true
		r.logContainerExit("/ecs/g", "s", "app", &code3, "", rt)
		sink.mu.Lock()
		defer sink.mu.Unlock()
		if len(sink.events) != 0 {
			t.Fatalf("events = %d, want 0", len(sink.events))
		}
	})

	t.Run("non-essential crash stays silent", func(t *testing.T) {
		r, sink := newRunner(t)
		r.logContainerExit("/ecs/g", "s", "sidecar", &code3, "", essentialRT())
		sink.mu.Lock()
		defer sink.mu.Unlock()
		if len(sink.events) != 0 {
			t.Fatalf("events = %d, want 0", len(sink.events))
		}
	})
}
