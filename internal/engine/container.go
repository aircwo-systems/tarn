package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const rieContainerPort = "8080/tcp"

// Docker labels applied to every Lambda execution container. The port label
// scopes ownership to one Tarn instance, so a startup sweep never touches
// containers belonging to another Tarn running against the same Docker daemon.
const (
	labelManaged       = "tarn.managed"
	labelManagedLambda = "lambda"
	labelPort          = "tarn.port"
	labelAccount       = "tarn.account"
	labelFunction      = "tarn.function"
)

// LambdaContainerName matches Docker names of Lambda containers created by
// Tarn (and its former name, OpenStack), labelled or not.
var LambdaContainerName = regexp.MustCompile(`^/(?:tarn|openstack)-lambda-.+-[0-9]{13}$`)

// lambdaImagePrefix is the registry path of every AWS Lambda base image.
const lambdaImagePrefix = "public.ecr.aws/lambda/"

// containerRemoveTimeout bounds a single Docker remove issued after the
// container has already been dropped from tracking.
const containerRemoveTimeout = 30 * time.Second

// lambdaOwnerLabels selects the Lambda containers owned by a Tarn instance
// listening on port.
func lambdaOwnerLabels(port int) map[string]string {
	return map[string]string{
		labelManaged: labelManagedLambda,
		labelPort:    strconv.Itoa(port),
	}
}

// lambdaContainerLabels labels a new Lambda container with its owner and,
// for inspection, the function and account it serves.
func lambdaContainerLabels(port int, accountID, functionName string) map[string]string {
	labels := lambdaOwnerLabels(port)
	labels[labelAccount] = accountID
	labels[labelFunction] = functionName
	return labels
}

// ContainerInfo holds metadata about a running Lambda container.
type ContainerInfo struct {
	ID           string
	FunctionName string
	Runtime      types.Runtime
	CreatedAt    time.Time
	LastInvoked  time.Time
	HostPort     string // host port mapped to container's 8080
	State        string
	// Busy is true while a container is servicing an invocation. The AWS RIE
	// handles one invocation at a time, so each container (an "execution
	// environment") is held exclusively for the duration of an invoke. Guarded
	// by Engine.mu.
	Busy bool
}

// Engine manages Docker containers for Lambda execution.
//
// To mirror AWS Lambda concurrency (and therefore Step Functions Map / parallel
// fan-out), each function is backed by a POOL of containers rather than a single
// one: every concurrent invocation gets its own warm container ("execution
// environment"), created on demand up to a per-function cap and reused when idle.
type Engine struct {
	client     *client.Client
	cfg        *config.Config
	containers map[string][]*ContainerInfo // functionName -> pool of containers
	mu         sync.RWMutex
	// imageKnown caches runtimes confirmed to have their image present locally,
	// avoiding a Docker ImageList call on every EnsureImage invocation.
	imageKnown sync.Map // types.Runtime → struct{}
	// pullMus holds a per-runtime mutex so concurrent EnsureImage calls for the
	// same runtime serialise rather than spawning duplicate Docker pulls.
	pullMus sync.Map // types.Runtime → *sync.Mutex

	// imageRefKnown and refPullMus mirror imageKnown/pullMus but key on an
	// arbitrary image reference rather than a types.Runtime. ECS task
	// containers name arbitrary images, which don't fit the closed runtime
	// set EnsureImage assumes. See EnsureImageRef in task.go.
	imageRefKnown sync.Map // string (image ref) → struct{}
	refPullMus    sync.Map // string (image ref) → *sync.Mutex
	// taskPayloadFiles tracks host-side files mounted into ECS task
	// containers for event payloads larger than one environment value.
	taskPayloadFiles sync.Map // container ID -> host path
}

// New creates a new container engine.
func New(cfg *config.Config) (*Engine, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Engine{
		client:     cli,
		cfg:        cfg,
		containers: make(map[string][]*ContainerInfo),
	}, nil
}

// Ping checks if the Docker daemon is reachable.
func (e *Engine) Ping(ctx context.Context) error {
	_, err := e.client.Ping(ctx)
	if err != nil {
		return fmt.Errorf("docker daemon unreachable: %w", err)
	}
	return nil
}

// PullImage pulls a Lambda runtime image if not already present.
func (e *Engine) PullImage(ctx context.Context, runtime types.Runtime) error {
	img, ok := types.RuntimeImageMap[runtime]
	if !ok {
		return fmt.Errorf("unsupported runtime: %s", runtime)
	}

	log.Printf("[engine] pulling image %s...", img)
	reader, err := e.client.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", img, err)
	}
	defer func() { _ = reader.Close() }()

	_, err = io.Copy(io.Discard, reader)
	if err == nil {
		log.Printf("[engine] image %s pulled successfully", img)
	}
	return err
}

// ImageExists checks if a runtime image is already available locally.
func (e *Engine) ImageExists(ctx context.Context, runtime types.Runtime) (bool, error) {
	img, ok := types.RuntimeImageMap[runtime]
	if !ok {
		return false, fmt.Errorf("unsupported runtime: %s", runtime)
	}

	images, err := e.client.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return false, err
	}

	for _, i := range images {
		for _, tag := range i.RepoTags {
			if tag == img {
				return true, nil
			}
		}
	}
	return false, nil
}

// runtimePullMu returns the per-runtime mutex, creating it lazily.
func (e *Engine) runtimePullMu(runtime types.Runtime) *sync.Mutex {
	mu := &sync.Mutex{}
	actual, _ := e.pullMus.LoadOrStore(runtime, mu)
	return actual.(*sync.Mutex)
}

// EnsureImage pulls the runtime image if not already present, blocking until ready.
// A per-runtime mutex prevents duplicate concurrent pulls; an in-memory cache
// skips the Docker ImageList RPC for runtimes already confirmed present.
func (e *Engine) EnsureImage(ctx context.Context, runtime types.Runtime) error {
	if _, ok := e.imageKnown.Load(runtime); ok {
		return nil
	}

	mu := e.runtimePullMu(runtime)
	mu.Lock()
	defer mu.Unlock()

	if _, ok := e.imageKnown.Load(runtime); ok {
		return nil
	}

	exists, err := e.ImageExists(ctx, runtime)
	if err != nil {
		return err
	}
	if exists {
		e.imageKnown.Store(runtime, struct{}{})
		return nil
	}
	if err := e.PullImage(ctx, runtime); err != nil {
		return err
	}
	e.imageKnown.Store(runtime, struct{}{})
	return nil
}

// buildLambdaContainerEnv assembles the container environment for a Lambda
// execution container: Lambda runtime metadata, the SDK-endpoint redirect
// back to Tarn, and AWS_ACCESS_KEY_ID set to accountID (the function's own
// account) so SDK calls made from inside the container are attributed to
// that account by Tarn's SigV4 account resolution (internal/account),
// instead of falling through to the default account. fn.Environment is
// appended last so a user-set variable (including AWS_ACCESS_KEY_ID itself)
// overrides any of the above — Docker resolves duplicate env keys by taking
// the last occurrence.
func buildLambdaContainerEnv(fn *types.FunctionConfig, accountID, region string, port int, dbURLRewrites map[string]string) []string {
	env := []string{
		fmt.Sprintf("AWS_LAMBDA_FUNCTION_NAME=%s", fn.FunctionName),
		fmt.Sprintf("AWS_LAMBDA_FUNCTION_VERSION=%s", fn.Version),
		fmt.Sprintf("AWS_LAMBDA_FUNCTION_MEMORY_SIZE=%d", fn.MemorySize),
		fmt.Sprintf("AWS_REGION=%s", region),
		fmt.Sprintf("AWS_DEFAULT_REGION=%s", region),
		fmt.Sprintf("AWS_LAMBDA_LOG_GROUP_NAME=/aws/lambda/%s", fn.FunctionName),
		fmt.Sprintf("AWS_LAMBDA_LOG_STREAM_NAME=%s", time.Now().Format("2006/01/02")),
		fmt.Sprintf("_HANDLER=%s", fn.Handler),
		fmt.Sprintf("AWS_LAMBDA_FUNCTION_TIMEOUT=%d", fn.Timeout),
		// Point SDK calls back to Tarn
		fmt.Sprintf("AWS_ENDPOINT_URL=http://host.docker.internal:%d", port),
		fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", accountID),
		"AWS_SECRET_ACCESS_KEY=test",
	}

	for k, v := range fn.Environment {
		if rewritten, ok := dbURLRewrites[k]; ok {
			env = append(env, fmt.Sprintf("%s=%s", k, rewritten))
		} else {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return env
}

// CreateContainer creates a new Lambda execution container with port mapping
// so the host can reach the RIE on port 8080 inside the container.
func (e *Engine) CreateContainer(ctx context.Context, fn *types.FunctionConfig, codeDir string, layerDirs []string, accountID string) (*ContainerInfo, error) {
	img, ok := types.RuntimeImageMap[fn.Runtime]
	if !ok {
		return nil, fmt.Errorf("unsupported runtime: %s", fn.Runtime)
	}
	if accountID == "" {
		if e.cfg != nil && e.cfg.AccountID != "" {
			accountID = e.cfg.AccountID
		} else {
			accountID = defaultTaskAccountID
		}
	}

	// Scan function environment for PostgreSQL URLs so db-proxy can observe them.
	// Build a rewrite map: original key -> rewritten URL pointing to localhost:15432.
	const dbProxyPort = 15432
	dbProxyPath := e.findDBProxy()
	dbURLRewrites := map[string]string{}
	var dbUpstream, dbName string
	if dbProxyPath != "" {
		for k, v := range fn.Environment {
			if newURL, upstream, name, ok := rewritePostgresURL(v, dbProxyPort); ok {
				dbURLRewrites[k] = newURL
				if dbUpstream == "" {
					dbUpstream = upstream
					dbName = name
				}
			}
		}
		if dbUpstream == "" {
			dbProxyPath = "" // no DB URLs found — skip injection
		}
	}

	env := buildLambdaContainerEnv(fn, accountID, e.cfg.Region, e.cfg.Port, dbURLRewrites)

	binds := []string{
		fmt.Sprintf("%s:/var/task:ro", codeDir),
	}
	for _, layerDir := range layerDirs {
		binds = append(binds, fmt.Sprintf("%s:/opt:ro", layerDir))
	}

	// Inject sidecar binaries alongside the Lambda runtime. Neither binary
	// registers with the Extensions API, so they must not live under /opt/extensions.
	//
	//   secrets-proxy: mimics the AWS Parameters and Secrets extension HTTP API.
	//   db-proxy: transparent TCP proxy for PostgreSQL — observational only.
	var bgCmds []string

	secretsProxyPath := e.findSecretsProxy()
	if secretsProxyPath != "" {
		binds = append(binds, fmt.Sprintf("%s:/opt/tarn/secrets-proxy:ro", secretsProxyPath))
		env = append(env,
			"PARAMETERS_SECRETS_EXTENSION_HTTP_PORT=2773",
			"AWS_SESSION_TOKEN=local-dev-token",
			"TARN_INTERNAL_LAMBDA=1",
		)
		bgCmds = append(bgCmds, "/opt/tarn/secrets-proxy &")
	}

	if dbProxyPath != "" {
		binds = append(binds, fmt.Sprintf("%s:/opt/tarn/db-proxy:ro", dbProxyPath))
		env = append(env,
			fmt.Sprintf("TARN_DB_UPSTREAM=%s", dbUpstream),
			fmt.Sprintf("TARN_DB_NAME=%s", dbName),
			fmt.Sprintf("TARN_DB_PROXY_PORT=%d", dbProxyPort),
		)
		bgCmds = append(bgCmds, "/opt/tarn/db-proxy &")
	}

	var entrypoint []string
	if len(bgCmds) > 0 {
		cmd := strings.Join(bgCmds, " ") + " exec /lambda-entrypoint.sh " + fn.Handler
		entrypoint = []string{"/bin/sh", "-c", cmd}
	}

	memoryBytes := int64(fn.MemorySize) * 1024 * 1024

	exposedPorts := nat.PortSet{
		nat.Port(rieContainerPort): struct{}{},
	}

	containerCfg := &container.Config{
		Image:        img,
		Env:          env,
		ExposedPorts: exposedPorts,
		Cmd:          []string{fn.Handler},
		Labels:       lambdaContainerLabels(e.cfg.Port, accountID, fn.FunctionName),
	}
	if len(entrypoint) > 0 {
		containerCfg.Entrypoint = entrypoint
	}

	hostCfg := &container.HostConfig{
		Binds: binds,
		Resources: container.Resources{
			Memory: memoryBytes,
		},
		PortBindings: nat.PortMap{
			nat.Port(rieContainerPort): []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: ""}, // let Docker assign a free port
			},
		},
		// Lambda provides writable /tmp (512MB by default)
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					SizeBytes: 512 * 1024 * 1024, // 512 MB like real Lambda
				},
			},
		},
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		// Warm containers live for minutes under sustained load; cap Docker's
		// on-disk log so it can't grow without bound. Tarn ingests output
		// incrementally after each invoke, so rotation loses nothing it needs.
		LogConfig: container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "2"},
		},
	}

	name := fmt.Sprintf("tarn-lambda-%s-%d", sanitizeName(fn.FunctionName), time.Now().UnixMilli())

	resp, err := e.client.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, name)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	info := &ContainerInfo{
		ID:           resp.ID,
		FunctionName: fn.FunctionName,
		Runtime:      fn.Runtime,
		CreatedAt:    time.Now(),
		LastInvoked:  time.Now(),
		State:        "created",
		// Created on the cold-start path of an invoke, so it belongs to that
		// invocation until released. Counting it (Busy) immediately also makes it
		// part of the pool size for concurrency-cap checks.
		Busy: true,
	}

	e.mu.Lock()
	e.containers[fn.FunctionName] = append(e.containers[fn.FunctionName], info)
	e.mu.Unlock()

	return info, nil
}

// AcquireIdle returns a ready, idle container for the function and marks it Busy,
// so the caller has exclusive use of it for one invocation. Returns ok=false when
// every existing container is busy (or none exist).
func (e *Engine) AcquireIdle(functionName string) (*ContainerInfo, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, info := range e.containers[functionName] {
		if !info.Busy && info.State == "running" {
			info.Busy = true
			info.LastInvoked = time.Now()
			return info, true
		}
	}
	return nil, false
}

// Release returns a container to the pool so another invocation can reuse it.
func (e *Engine) Release(info *ContainerInfo) {
	if info == nil {
		return
	}
	e.mu.Lock()
	info.Busy = false
	info.LastInvoked = time.Now()
	e.mu.Unlock()
}

// CountContainers returns the current pool size for a function (running plus
// just-created). Used to enforce the per-function concurrency cap.
func (e *Engine) CountContainers(functionName string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.containers[functionName])
}

// StartContainer starts a container and resolves its mapped host port.
func (e *Engine) StartContainer(ctx context.Context, info *ContainerInfo) error {
	if err := e.client.ContainerStart(ctx, info.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Inspect to get the dynamically assigned host port.
	// Use exponential backoff starting at 10ms — on fast local Docker the
	// mapping appears in the first inspect call with no delay.
	var hostPort string
	backoff := 10 * time.Millisecond
	for attempt := 0; attempt < 10; attempt++ {
		inspect, err := e.client.ContainerInspect(ctx, info.ID)
		if err != nil {
			return fmt.Errorf("failed to inspect container: %w", err)
		}

		// Check if the container exited
		if inspect.State != nil && inspect.State.Status == "exited" {
			return fmt.Errorf("container exited immediately (exit code %d)", inspect.State.ExitCode)
		}

		// Try to find the port mapping under any key containing "8080"
		for port, bindings := range inspect.NetworkSettings.Ports {
			if strings.Contains(string(port), "8080") && len(bindings) > 0 {
				hostPort = bindings[0].HostPort
				break
			}
		}
		if hostPort != "" {
			break
		}

		time.Sleep(backoff)
		if backoff < 200*time.Millisecond {
			backoff *= 2
		}
	}

	if hostPort == "" {
		return fmt.Errorf("no port mapping found for container %s after retries", info.ID[:12])
	}

	info.HostPort = hostPort
	info.State = "running"

	log.Printf("[engine] container %s started for %s (port %s)", info.ID[:12], info.FunctionName, info.HostPort)
	return nil
}

// DefaultStopTimeoutSec is the SIGTERM grace period used when stopping a
// container that defines no StopTimeout of its own.
const DefaultStopTimeoutSec = 5

// ResolveStopTimeoutSec normalizes a container StopTimeout value: positive
// values pass through, anything else means the engine default.
func ResolveStopTimeoutSec(v int) int {
	if v <= 0 {
		return DefaultStopTimeoutSec
	}
	return v
}

// StopContainer stops a running container, waiting up to timeoutSec after
// SIGTERM before SIGKILL. A non-positive timeoutSec selects the default.
func (e *Engine) StopContainer(ctx context.Context, containerID string, timeoutSec int) error {
	timeout := ResolveStopTimeoutSec(timeoutSec)
	return e.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// RemoveContainer removes a single container (by ID) and drops it from its
// function's pool. The Docker removal ignores ctx cancellation: once the
// container is untracked, an aborted remove would orphan it for good (for
// example when the invoking HTTP client disconnects during a cold start).
func (e *Engine) RemoveContainer(ctx context.Context, containerID string) error {
	e.mu.Lock()
	for name, pool := range e.containers {
		for i, info := range pool {
			if info.ID == containerID {
				e.containers[name] = append(pool[:i], pool[i+1:]...)
				if len(e.containers[name]) == 0 {
					delete(e.containers, name)
				}
				break
			}
		}
	}
	e.mu.Unlock()

	removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), containerRemoveTimeout)
	defer cancel()
	return e.client.ContainerRemove(removeCtx, containerID, container.RemoveOptions{Force: true})
}

// SweepOrphanedLambdaContainers force-removes Lambda containers left behind
// by a previous run of this Tarn instance (crash, kill -9, closed terminal).
// Call at startup, before any invoke creates a container, so every match is
// an orphan. Returns the number removed.
func (e *Engine) SweepOrphanedLambdaContainers(ctx context.Context) (int, error) {
	orphans, err := e.ListContainersByLabel(ctx, lambdaOwnerLabels(e.cfg.Port))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, c := range orphans {
		if err := e.client.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			log.Printf("[engine] failed to remove orphaned Lambda container %s: %v", shortID(c.ID), err)
			continue
		}
		removed++
	}
	return removed, nil
}

// SweepLegacyLambdaContainers removes stopped Lambda containers created by
// Tarn versions that predate ownership labels. Running ones are left alone:
// without labels they may belong to another Tarn instance still using them.
// Returns the names of the containers removed.
func (e *Engine) SweepLegacyLambdaContainers(ctx context.Context) ([]string, error) {
	all, err := e.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	var removed []string
	for _, c := range all {
		name, ok := legacyStoppedLambdaName(c)
		if !ok {
			continue
		}
		if err := e.client.ContainerRemove(ctx, c.ID, container.RemoveOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
			log.Printf("[engine] failed to remove stopped Lambda container %s: %v", name, err)
			continue
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// legacyStoppedLambdaName reports whether c is a stopped, unlabelled Lambda
// container from an older Tarn, returning its name without the leading slash.
func legacyStoppedLambdaName(c container.Summary) (string, bool) {
	if _, labelled := c.Labels[labelManaged]; labelled {
		return "", false
	}
	if c.State != "exited" && c.State != "dead" && c.State != "created" {
		return "", false
	}
	if !strings.HasPrefix(c.Image, lambdaImagePrefix) {
		return "", false
	}
	for _, name := range c.Names {
		if LambdaContainerName.MatchString(name) {
			return strings.TrimPrefix(name, "/"), true
		}
	}
	return "", false
}

// EvictContainer stops and removes ALL of a function's containers synchronously.
func (e *Engine) EvictContainer(ctx context.Context, functionName string) {
	e.mu.Lock()
	pool := e.containers[functionName]
	delete(e.containers, functionName)
	e.mu.Unlock()
	for _, info := range pool {
		_ = e.StopContainer(ctx, info.ID, 0)
		_ = e.client.ContainerRemove(ctx, info.ID, container.RemoveOptions{Force: true})
	}
}

// EvictContainerAsync drops a function's whole pool from tracking immediately (so
// no subsequent invoke reuses it), then stops and removes the Docker containers in
// the background. Use on the API handler path to avoid blocking Terraform
// operations while Docker performs graceful container shutdown.
func (e *Engine) EvictContainerAsync(functionName string) {
	e.mu.Lock()
	pool := e.containers[functionName]
	delete(e.containers, functionName)
	e.mu.Unlock()
	if len(pool) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stopTimeout := 5
		for _, info := range pool {
			_ = e.client.ContainerStop(ctx, info.ID, container.StopOptions{Timeout: &stopTimeout})
			_ = e.client.ContainerRemove(ctx, info.ID, container.RemoveOptions{Force: true})
		}
	}()
}

// GetContainer returns any one container from a function's pool, if any exist.
// Used for log/trace lookups that just need a representative container.
func (e *Engine) GetContainer(functionName string) (*ContainerInfo, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	pool := e.containers[functionName]
	if len(pool) == 0 {
		return nil, false
	}
	return pool[0], true
}

// ContainerLogsSince returns the container's stdout/stderr written after since
// (all output when since is zero), with Docker's timestamps stripped, and the
// timestamp of the last line returned. Passing that timestamp back on the next
// call reads only new output, so each read costs the new output rather than
// the container's whole log history.
func (e *Engine) ContainerLogsSince(ctx context.Context, containerID string, since time.Time) (string, time.Time, error) {
	opts := container.LogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true}
	if !since.IsZero() {
		opts.Since = since.Format(time.RFC3339Nano)
	}
	reader, err := e.client.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return "", since, err
	}
	defer func() { _ = reader.Close() }()

	return logFramesAfter(reader, since)
}

// logFramesAfter reads Docker's multiplexed log stream, where every frame is
// one log entry prefixed with its RFC3339Nano timestamp and a space. It keeps
// entries strictly newer than since (Docker's own since filter is inclusive),
// strips each entry's timestamp, and preserves stdout/stderr interleaving.
// Working per frame matters for lines over 16 KB: Docker stores those as
// several partial entries, each with its own timestamp, and they must be
// rejoined without the timestamps in between. Entries without a parseable
// timestamp are kept whole.
func logFramesAfter(r io.Reader, since time.Time) (string, time.Time, error) {
	var b strings.Builder
	last := since
	var header [8]byte
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			if err == io.EOF {
				return b.String(), last, nil
			}
			return b.String(), last, err
		}
		frame := make([]byte, binary.BigEndian.Uint32(header[4:]))
		if _, err := io.ReadFull(r, frame); err != nil {
			return b.String(), last, err
		}

		stamp, entry, ok := bytes.Cut(frame, []byte(" "))
		ts, err := time.Parse(time.RFC3339Nano, string(stamp))
		if !ok || err != nil {
			b.Write(frame)
			continue
		}
		if !ts.After(since) {
			continue
		}
		b.Write(entry)
		if ts.After(last) {
			last = ts
		}
	}
}

// Cleanup stops and removes all managed containers across every function pool.
// Containers are untracked under the lock, then stopped and removed in
// parallel so shutdown time doesn't grow with pool size.
func (e *Engine) Cleanup(ctx context.Context) {
	e.mu.Lock()
	var all []*ContainerInfo
	for name, pool := range e.containers {
		all = append(all, pool...)
		delete(e.containers, name)
	}
	e.mu.Unlock()

	var wg sync.WaitGroup
	for _, info := range all {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = e.StopContainer(ctx, id, 0)
			if err := e.client.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
				log.Printf("[engine] failed to remove Lambda container %s: %v", shortID(id), err)
			}
		}(info.ID)
	}
	wg.Wait()
	e.cleanupTaskPayloadFiles()
}

// Close releases the Docker client resources.
func (e *Engine) Close() error {
	e.cleanupTaskPayloadFiles()
	return e.client.Close()
}

func (e *Engine) cleanupTaskPayloadFiles() {
	e.taskPayloadFiles.Range(func(key, value any) bool {
		e.taskPayloadFiles.Delete(key)
		_ = os.Remove(value.(string))
		return true
	})
}

// findSecretsProxy locates the secrets-proxy-linux binary.
// It searches in the build directory (relative to the working directory) and
// next to the running executable.
func (e *Engine) findSecretsProxy() string {
	candidates := []string{
		"./build/secrets-proxy-linux",
	}

	// Also check relative to the executable
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "secrets-proxy-linux"))
	}

	for _, path := range candidates {
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}

	return ""
}

// findDBProxy locates the db-proxy-linux binary using the same strategy as findSecretsProxy.
func (e *Engine) findDBProxy() string {
	candidates := []string{
		"./build/db-proxy-linux",
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "db-proxy-linux"))
	}
	for _, path := range candidates {
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	return ""
}

// rewritePostgresURL rewrites a PostgreSQL connection URL to route through the local
// db-proxy at the given port, returning the new URL, the original host:port (upstream),
// and the database name. Returns ok=false if the value is not a postgres URL.
func rewritePostgresURL(rawURL string, proxyPort int) (newURL, upstream, dbName string, ok bool) {
	// Strip JDBC prefix so the rest of the URL is a standard postgres:// URL.
	jdbcPrefix := ""
	parseURL := rawURL
	if strings.HasPrefix(rawURL, "jdbc:") {
		jdbcPrefix = "jdbc:"
		parseURL = rawURL[len("jdbc:"):]
	}

	u, err := url.Parse(parseURL)
	if err != nil || (u.Scheme != "postgresql" && u.Scheme != "postgres") {
		return "", "", "", false
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	upstream = host + ":" + port
	dbName = strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		dbName = upstream
	}
	u.Host = fmt.Sprintf("localhost:%d", proxyPort)
	return jdbcPrefix + u.String(), upstream, dbName, true
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "-", ":", "-", " ", "-")
	return strings.ToLower(r.Replace(name))
}
