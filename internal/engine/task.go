package engine

// Docker primitives for long-lived, arbitrary-image ECS task containers.
//
// This file is a sibling to container.go's Lambda path, not an extension of
// it: task containers have no RIE, no code mount, no warm pool and no Busy
// flag. See docs/design/ecs-support.md section 2 ("Task runner") for the
// design this implements. T7 (internal/ecs/runner.go) composes these
// primitives into RunTask, StopTask and the reconcile loop; this file only
// provides the capability, not the policy of when to call it.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

// TaskContainerSpec describes a task container to create, independent of
// any types.FunctionConfig. Task definitions name arbitrary images and run
// their own entrypoint, so this is a narrower, explicit shape rather than
// something coerced out of the Lambda config.
type TaskContainerSpec struct {
	Image      string
	Name       string
	Command    []string
	Entrypoint []string
	Env        map[string]string
	// CPU is the ECS CPU allocation in CPU units. One vCPU is 1024 units.
	// Zero leaves Docker's CPU scheduling unchanged.
	CPU int64
	// Memory is the ECS hard memory limit in MiB. Zero leaves Docker's memory
	// limit unchanged.
	Memory int64
	// MemoryReservation is the ECS soft memory limit in MiB. Zero leaves
	// Docker's memory reservation unchanged.
	MemoryReservation int64
	// NetworkMode accepts the ECS modes bridge, host, none and awsvpc. An
	// empty value preserves the historical Docker default (bridge).
	NetworkMode string
	// Ports lists container ports to publish. Each is bound to an ephemeral
	// host port so multiple replicas of the same task definition never
	// collide; the assigned host ports are read back after start and
	// returned as NetworkBindings.
	Ports []int
	// Labels are applied to the container verbatim. The engine is shared
	// across accounts, so labels (e.g. tarn.account, tarn.task-arn) are the
	// only thing preventing cross-account collisions when T7 reconciles or
	// reaps orphans.
	Labels map[string]string
	// Region and EventPayload are supplied by the ECS runner. EventPayload is
	// exposed as EVENT_PAYLOAD while it fits safely in one environment value;
	// larger payloads are mounted as EVENT_PAYLOAD_FILE instead.
	Region       string
	EventPayload []byte
	// EventPayloadFile is populated internally by CreateAndStartTaskContainer
	// when EventPayload exceeds the environment-value limit.
	EventPayloadFile string
	// AccountID is the owning account's 12-digit ID, exposed to the container
	// as AWS_ACCESS_KEY_ID so SDK calls made from inside it are attributed to
	// this task's own account by Tarn's SigV4 account resolution
	// (internal/account), instead of falling through to the default account.
	// Left empty, CreateAndStartTaskContainer defaults it from the engine's
	// own config; buildTaskContainerEnv falls back to defaultTaskAccountID as
	// a last resort.
	AccountID string
	// CorrelationID is this task's trace correlation ID, exposed to the
	// container as TARN_CORRELATION_ID. Empty means no trace store is wired
	// up for the caller (or none was assigned), in which case the env var is
	// omitted entirely.
	CorrelationID string
}

// defaultTaskAccountID is used for AWS_ACCESS_KEY_ID when neither the spec
// nor the engine's own config carries an account ID. It matches
// config.Config's own zero-value default.
const defaultTaskAccountID = "000000000000"

// TaskContainerHandle is the result of creating and starting a task
// container: its Docker ID plus the host ports Docker actually assigned.
type TaskContainerHandle struct {
	ID              string
	Name            string
	NetworkBindings []types.NetworkBinding
}

// TaskContainerSummary is one container matched by ListContainersByLabel.
type TaskContainerSummary struct {
	ID     string
	Names  []string
	State  string
	Status string
	Labels map[string]string
}

// refPullMu returns the per-image-reference mutex, creating it lazily.
func (e *Engine) refPullMu(ref string) *sync.Mutex {
	mu := &sync.Mutex{}
	actual, _ := e.refPullMus.LoadOrStore(ref, mu)
	return actual.(*sync.Mutex)
}

// imageRefExists reports whether ref is already present in the local image
// list, matching any of its RepoTags exactly.
func (e *Engine) imageRefExists(ctx context.Context, ref string) (bool, error) {
	images, err := e.client.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == ref {
				return true, nil
			}
		}
	}
	return false, nil
}

// EnsureImageRef ensures an arbitrary image reference is present locally,
// pulling it if missing. It mirrors EnsureImage's cache-then-mutex pattern
// but keys on the reference string instead of a types.Runtime, since ECS
// task definitions name arbitrary images that don't fit that closed set.
//
// Critical behaviour: if the image already exists locally, this NEVER
// attempts a pull. Local development usually builds the task image itself
// with no registry configured, so a doomed pull attempt must not become a
// fatal error when the image is already present. Only a missing image that
// also fails to pull is an error.
func (e *Engine) EnsureImageRef(ctx context.Context, ref string) error {
	if ref == "" {
		return fmt.Errorf("image reference must not be empty")
	}

	if _, ok := e.imageRefKnown.Load(ref); ok {
		return nil
	}

	mu := e.refPullMu(ref)
	mu.Lock()
	defer mu.Unlock()

	if _, ok := e.imageRefKnown.Load(ref); ok {
		return nil
	}

	exists, err := e.imageRefExists(ctx, ref)
	if err != nil {
		return err
	}
	if exists {
		e.imageRefKnown.Store(ref, struct{}{})
		return nil
	}

	log.Printf("[engine] image %s not found locally, pulling...", ref)
	reader, err := e.client.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("image %s not found locally and pull failed: %w", ref, err)
	}
	defer func() { _ = reader.Close() }()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("image %s not found locally and pull failed: %w", ref, err)
	}

	e.imageRefKnown.Store(ref, struct{}{})
	log.Printf("[engine] image %s pulled successfully", ref)
	return nil
}

// buildTaskContainerEnv assembles the container environment for a task
// container: the SDK-endpoint redirect that lets code inside the container
// reach Tarn's own service endpoints (mirroring CreateContainer's Lambda
// env around container.go:207), followed by the spec's own environment.
func buildTaskContainerEnv(spec TaskContainerSpec, tarnPort int) []string {
	accountID := spec.AccountID
	if accountID == "" {
		accountID = defaultTaskAccountID
	}

	values := make(map[string]string, len(spec.Env)+7)
	values["AWS_ENDPOINT_URL"] = fmt.Sprintf("http://host.docker.internal:%d", tarnPort)
	values["AWS_REGION"] = spec.Region
	values["AWS_DEFAULT_REGION"] = spec.Region
	values["AWS_ACCESS_KEY_ID"] = accountID
	values["AWS_SECRET_ACCESS_KEY"] = "test"
	if spec.CorrelationID != "" {
		values["TARN_CORRELATION_ID"] = spec.CorrelationID
	}
	for k, v := range spec.Env {
		values[k] = v
	}
	if len(spec.EventPayload) > 0 {
		if len(spec.EventPayload) <= maxTaskPayloadEnv {
			values["EVENT_PAYLOAD"] = string(spec.EventPayload)
		} else if spec.EventPayloadFile != "" {
			delete(values, "EVENT_PAYLOAD")
			values["EVENT_PAYLOAD_FILE"] = taskPayloadPath
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, fmt.Sprintf("%s=%s", key, values[key]))
	}
	return env
}

// buildTaskPortBindings builds the ExposedPorts/PortBindings pair for a
// task container: every requested container port is exposed and bound to
// an ephemeral host port (empty HostPort lets Docker assign one), so
// multiple replicas of the same task definition never collide.
func buildTaskPortBindings(ports []int) (nat.PortSet, nat.PortMap) {
	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	for _, p := range ports {
		natPort := nat.Port(fmt.Sprintf("%d/tcp", p))
		exposed[natPort] = struct{}{}
		bindings[natPort] = []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}}
	}
	return exposed, bindings
}

// decodeNetworkBindings converts Docker's post-start port map (as returned
// by ContainerInspect) into the wire-shape NetworkBinding list T7 records
// on TaskContainer.NetworkBindings. Entries with no assigned host port
// (not yet bound) or an unparsable host port are skipped.
func decodeNetworkBindings(ports nat.PortMap) []types.NetworkBinding {
	var out []types.NetworkBinding
	for port, bs := range ports {
		for _, b := range bs {
			hostPort, err := strconv.Atoi(b.HostPort)
			if err != nil {
				continue
			}
			out = append(out, types.NetworkBinding{
				ContainerPort: port.Int(),
				HostPort:      hostPort,
				Protocol:      port.Proto(),
				BindIP:        b.HostIP,
			})
		}
	}
	return out
}

const (
	ecsCPUUnitsPerVCPU = int64(1024)
	bytesPerMiB        = int64(1024 * 1024)
	taskHostGateway    = "host.docker.internal:host-gateway"
	maxTaskPayloadEnv  = types.TaskEventPayloadEnvMaxBytes
	taskPayloadPath    = "/tmp/tarn/event-payload.json"
)

type taskNetworkConfig struct {
	dockerMode   container.NetworkMode
	publishPorts bool
	hostGateway  bool
}

// resolveTaskNetworkMode translates the supported ECS modes into Docker
// settings. awsvpc intentionally keeps Docker's implicit default network: it
// is the behavior the local runner historically provided, while a real ENI is
// outside this Docker-backed implementation.
func resolveTaskNetworkMode(mode string, hasPorts bool) (taskNetworkConfig, error) {
	mode = strings.TrimSpace(mode)
	config := taskNetworkConfig{
		publishPorts: true,
		hostGateway:  true,
	}

	switch mode {
	case "":
		// Keep NetworkMode empty so Docker applies its normal default.
	case "bridge":
		config.dockerMode = container.NetworkMode("bridge")
	case "awsvpc":
		// The existing ECS runner used Docker's default bridge network.
	case "host":
		config.dockerMode = container.NetworkMode("host")
		config.publishPorts = false
	case "none":
		config.dockerMode = container.NetworkMode("none")
		config.publishPorts = false
		// Docker rejects ExtraHosts with a private (none) network.
		config.hostGateway = false
	default:
		return taskNetworkConfig{}, fmt.Errorf("unsupported ECS network mode %q", mode)
	}

	if hasPorts && !config.publishPorts {
		return taskNetworkConfig{}, fmt.Errorf("ECS network mode %q does not support port mappings", mode)
	}
	return config, nil
}

func ecsCPUUnitsToNanoCPUs(cpu int64) (int64, error) {
	if cpu < 0 {
		return 0, fmt.Errorf("CPU must not be negative")
	}
	if cpu == 0 {
		return 0, nil
	}
	if cpu > math.MaxInt64/1_000_000_000 {
		return 0, fmt.Errorf("CPU value %d is too large", cpu)
	}
	return cpu * 1_000_000_000 / ecsCPUUnitsPerVCPU, nil
}

func ecsMiBToBytes(value int64, name string) (int64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	if value == 0 {
		return 0, nil
	}
	if value > math.MaxInt64/bytesPerMiB {
		return 0, fmt.Errorf("%s value %d is too large", name, value)
	}
	return value * bytesPerMiB, nil
}

func buildTaskResources(spec TaskContainerSpec) (container.Resources, error) {
	nanoCPUs, err := ecsCPUUnitsToNanoCPUs(spec.CPU)
	if err != nil {
		return container.Resources{}, err
	}
	memory, err := ecsMiBToBytes(spec.Memory, "memory")
	if err != nil {
		return container.Resources{}, err
	}
	memoryReservation, err := ecsMiBToBytes(spec.MemoryReservation, "memory reservation")
	if err != nil {
		return container.Resources{}, err
	}
	if memory > 0 && memoryReservation > memory {
		return container.Resources{}, fmt.Errorf("memory reservation cannot exceed memory")
	}

	resources := container.Resources{
		Memory:            memory,
		MemoryReservation: memoryReservation,
	}
	if spec.CPU > 0 {
		// CPUShares preserves ECS's relative CPU weighting on Docker's
		// scheduler; NanoCPUs supplies the corresponding hard ceiling.
		resources.CPUShares = spec.CPU
		resources.NanoCPUs = nanoCPUs
	}
	return resources, nil
}

func buildTaskContainerConfig(spec TaskContainerSpec, tarnPort int) (*container.Config, *container.HostConfig, error) {
	network, err := resolveTaskNetworkMode(spec.NetworkMode, len(spec.Ports) > 0)
	if err != nil {
		return nil, nil, err
	}
	resources, err := buildTaskResources(spec)
	if err != nil {
		return nil, nil, err
	}

	var exposedPorts nat.PortSet
	var portBindings nat.PortMap
	if network.publishPorts {
		exposedPorts, portBindings = buildTaskPortBindings(spec.Ports)
	} else {
		exposedPorts = nat.PortSet{}
		portBindings = nat.PortMap{}
	}

	containerCfg := &container.Config{
		Image:        spec.Image,
		Env:          buildTaskContainerEnv(spec, tarnPort),
		Cmd:          spec.Command,
		Labels:       spec.Labels,
		ExposedPorts: exposedPorts,
	}
	if len(spec.Entrypoint) > 0 {
		containerCfg.Entrypoint = spec.Entrypoint
	}

	hostCfg := &container.HostConfig{
		NetworkMode:  network.dockerMode,
		PortBindings: portBindings,
		Resources:    resources,
	}
	if spec.EventPayloadFile != "" {
		hostCfg.Binds = []string{fmt.Sprintf("%s:%s:ro", spec.EventPayloadFile, taskPayloadPath)}
	}
	if network.hostGateway {
		hostCfg.ExtraHosts = []string{taskHostGateway}
	}
	return containerCfg, hostCfg, nil
}

// CreateAndStartTaskContainer creates and starts an ad-hoc container from an
// explicit spec: no RIE, no code mount, the image's own entrypoint (unless
// overridden). It publishes each of spec.Ports to an ephemeral host port,
// starts the container, and reads the actually-assigned host ports back
// from Docker rather than trusting the request. AutoRemove is deliberately
// left off: the caller (T7) must read the exit code and drain logs before
// removing the container via RemoveTaskContainer.
func (e *Engine) CreateAndStartTaskContainer(ctx context.Context, spec TaskContainerSpec) (*TaskContainerHandle, error) {
	if spec.Image == "" {
		return nil, fmt.Errorf("task container spec requires an image")
	}
	if spec.Region == "" && e.cfg != nil {
		spec.Region = e.cfg.Region
	}
	if spec.AccountID == "" && e.cfg != nil {
		spec.AccountID = e.cfg.AccountID
	}
	cleanupPayload := func() {}
	if len(spec.EventPayload) > maxTaskPayloadEnv {
		path, cleanup, payloadErr := e.writeTaskPayload(spec.EventPayload)
		if payloadErr != nil {
			return nil, payloadErr
		}
		spec.EventPayloadFile = path
		cleanupPayload = cleanup
	}
	keepPayload := false
	defer func() {
		if !keepPayload {
			cleanupPayload()
		}
	}()

	tarnPort := 0
	if e.cfg != nil {
		tarnPort = e.cfg.Port
	}
	containerCfg, hostCfg, err := buildTaskContainerConfig(spec, tarnPort)
	if err != nil {
		return nil, fmt.Errorf("invalid task container spec: %w", err)
	}

	name := spec.Name
	if name == "" {
		name = fmt.Sprintf("tarn-task-%d", time.Now().UnixNano())
	}

	resp, err := e.client.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, name)
	if err != nil {
		return nil, fmt.Errorf("failed to create task container: %w", err)
	}

	if err := e.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = e.client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("failed to start task container: %w", err)
	}

	var bindings []types.NetworkBinding
	if len(spec.Ports) > 0 {
		// Read the dynamically assigned host ports back from Docker, same
		// backoff shape as StartContainer's RIE port resolution: on fast
		// local Docker the mapping is usually present on the first inspect.
		backoff := 10 * time.Millisecond
		for attempt := 0; attempt < 10; attempt++ {
			inspect, ierr := e.client.ContainerInspect(ctx, resp.ID)
			if ierr != nil {
				_ = e.client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
				return nil, fmt.Errorf("failed to inspect task container: %w", ierr)
			}
			if inspect.State != nil && inspect.State.Status == "exited" {
				_ = e.client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
				return nil, fmt.Errorf("task container exited immediately (exit code %d)", inspect.State.ExitCode)
			}
			bindings = decodeNetworkBindings(inspect.NetworkSettings.Ports)
			if len(bindings) >= len(spec.Ports) {
				break
			}
			time.Sleep(backoff)
			if backoff < 200*time.Millisecond {
				backoff *= 2
			}
		}
	}

	if spec.EventPayloadFile != "" {
		e.taskPayloadFiles.Store(resp.ID, spec.EventPayloadFile)
		keepPayload = true
	}
	return &TaskContainerHandle{ID: resp.ID, Name: name, NetworkBindings: bindings}, nil
}

func (e *Engine) writeTaskPayload(payload []byte) (string, func(), error) {
	dir := ""
	if e.cfg != nil {
		dir = e.cfg.DataDir
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", nil, fmt.Errorf("create task payload directory: %w", err)
		}
	} else {
		dir = os.TempDir()
	}
	file, err := os.CreateTemp(dir, ".tarn-ecs-payload-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create task event payload file: %w", err)
	}
	path := filepath.Clean(file.Name())
	cleanup := func() { _ = os.Remove(path) }
	// The task may run as a non-root user. The bind-mounted file therefore
	// needs read permission for users other than the Tarn process that created
	// it; the containing data directory remains private on the host.
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("set task event payload permissions: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write task event payload: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close task event payload: %w", err)
	}
	return path, cleanup, nil
}

// WaitTaskContainer blocks until the container transitions to a
// non-running state and returns its exit code. This is the one-shot
// RunTask result path: the exit code is the result of the task, so a real
// exit must be distinguished from ctx being cancelled out from under the
// wait (which returns ctx.Err(), not a misleading exit code of 0).
func (e *Engine) WaitTaskContainer(ctx context.Context, containerID string) (int64, error) {
	statusCh, errCh := e.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case err := <-errCh:
		if err != nil {
			return 0, fmt.Errorf("failed waiting for container: %w", err)
		}
		return 0, nil
	case status := <-statusCh:
		if status.Error != nil && status.Error.Message != "" {
			return status.StatusCode, fmt.Errorf("container wait error: %s", status.Error.Message)
		}
		return status.StatusCode, nil
	}
}

// scanDemuxedLines decodes a Docker-multiplexed stdout/stderr stream and
// delivers each line to onLine as it is produced, blocking until muxed
// reaches EOF or errors. The stderr flag tells which stream a line came
// from: Docker multiplexes the two, and downstream classifiers (e.g. ECS log
// levels) cannot recover that signal from the line content alone. It is
// factored out of FollowContainerLogs so the decoding can be unit tested
// against a fake multiplexed reader with no Docker daemon involved, the
// same way readContainerLogStream is tested by
// TestReadContainerLogStreamPreservesInterleaving in container_test.go —
// this is the streaming counterpart of that one-shot helper, built on the
// same stdcopy demux rather than a reimplementation of it.
func scanDemuxedLines(muxed io.Reader, onLine func(line string, stderr bool)) error {
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	copyDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(outW, errW, muxed)
		copyDone <- err
		_ = outW.CloseWithError(err)
		_ = errW.CloseWithError(err)
	}()

	scan := func(r *io.PipeReader, stderr bool) {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			onLine(scanner.Text(), stderr)
		}
	}

	// Drain stderr in the background while stdout is scanned on the caller
	// goroutine, so at least one stream's callback ordering stays exactly
	// as before. Cross-stream interleaving at delivery is arrival-ordered,
	// which matches how consumers timestamp lines on receipt.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scan(errR, true)
	}()
	scan(outR, false)
	wg.Wait()

	return <-copyDone
}

// FollowContainerLogs streams a running container's logs with follow
// enabled, delivering decoded lines to onLine until the container stops or
// ctx is cancelled. It does not format, classify or parse lines — T7
// decides where they go (CreateLogGroup/PutLogEvents per the design doc).
func (e *Engine) FollowContainerLogs(ctx context.Context, containerID string, onLine func(line string, stderr bool)) error {
	reader, err := e.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	done := make(chan error, 1)
	go func() { done <- scanDemuxedLines(reader, onLine) }()

	select {
	case <-ctx.Done():
		_ = reader.Close() // unblocks the in-flight read so the goroutine exits
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// buildLabelFilterArgs builds the Docker filters.Args for matching
// containers on an exact label selector (key=value for every entry).
func buildLabelFilterArgs(selector map[string]string) filters.Args {
	f := filters.NewArgs()
	for k, v := range selector {
		f.Add("label", fmt.Sprintf("%s=%s", k, v))
	}
	return f
}

// ListContainersByLabel lists all containers (running or stopped) whose
// labels match every key/value pair in selector. T7 uses this both to
// reconcile a service's running task count and to reap orphaned task
// containers left over from a previous run at startup.
func (e *Engine) ListContainersByLabel(ctx context.Context, selector map[string]string) ([]TaskContainerSummary, error) {
	summaries, err := e.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: buildLabelFilterArgs(selector),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers by label: %w", err)
	}

	out := make([]TaskContainerSummary, 0, len(summaries))
	for _, c := range summaries {
		out = append(out, TaskContainerSummary{
			ID:     c.ID,
			Names:  c.Names,
			State:  c.State,
			Status: c.Status,
			Labels: c.Labels,
		})
	}
	return out, nil
}

// RemoveTaskContainer removes a task container by ID. It does not stop the
// container first — the caller is expected to have already waited for
// exit (WaitTaskContainer) or issued StopContainer — and containers are
// never created with AutoRemove, so this is the only way a task container
// disappears. That ordering lets T7 read the exit code and drain logs
// before the container is gone.
func (e *Engine) RemoveTaskContainer(ctx context.Context, containerID string) error {
	defer e.removeTaskPayloadFile(containerID)
	return e.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

func (e *Engine) removeTaskPayloadFile(containerID string) {
	if path, ok := e.taskPayloadFiles.LoadAndDelete(containerID); ok {
		_ = os.Remove(path.(string))
	}
}
