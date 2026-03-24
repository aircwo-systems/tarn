package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

const rieContainerPort = "8080/tcp"

// ContainerInfo holds metadata about a running Lambda container.
type ContainerInfo struct {
	ID           string
	FunctionName string
	Runtime      types.Runtime
	CreatedAt    time.Time
	LastInvoked  time.Time
	HostPort     string // host port mapped to container's 8080
	State        string
}

// Engine manages Docker containers for Lambda execution.
type Engine struct {
	client     *client.Client
	cfg        *config.Config
	containers map[string]*ContainerInfo // functionName -> container
	mu         sync.RWMutex
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
		containers: make(map[string]*ContainerInfo),
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

// EnsureImage pulls the runtime image if not already present, blocking until ready.
func (e *Engine) EnsureImage(ctx context.Context, runtime types.Runtime) error {
	exists, err := e.ImageExists(ctx, runtime)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return e.PullImage(ctx, runtime)
}

// CreateContainer creates a new Lambda execution container with port mapping
// so the host can reach the RIE on port 8080 inside the container.
func (e *Engine) CreateContainer(ctx context.Context, fn *types.FunctionConfig, codeDir string, layerDirs []string) (*ContainerInfo, error) {
	img, ok := types.RuntimeImageMap[fn.Runtime]
	if !ok {
		return nil, fmt.Errorf("unsupported runtime: %s", fn.Runtime)
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

	env := []string{
		fmt.Sprintf("AWS_LAMBDA_FUNCTION_NAME=%s", fn.FunctionName),
		fmt.Sprintf("AWS_LAMBDA_FUNCTION_VERSION=%s", fn.Version),
		fmt.Sprintf("AWS_LAMBDA_FUNCTION_MEMORY_SIZE=%d", fn.MemorySize),
		fmt.Sprintf("AWS_REGION=%s", e.cfg.Region),
		fmt.Sprintf("AWS_DEFAULT_REGION=%s", e.cfg.Region),
		fmt.Sprintf("AWS_LAMBDA_LOG_GROUP_NAME=/aws/lambda/%s", fn.FunctionName),
		fmt.Sprintf("AWS_LAMBDA_LOG_STREAM_NAME=%s", time.Now().Format("2006/01/02")),
		fmt.Sprintf("_HANDLER=%s", fn.Handler),
		fmt.Sprintf("AWS_LAMBDA_FUNCTION_TIMEOUT=%d", fn.Timeout),
		// Point SDK calls back to OpenStack
		fmt.Sprintf("AWS_ENDPOINT_URL=http://host.docker.internal:%d", e.cfg.Port),
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
	}

	for k, v := range fn.Environment {
		if rewritten, ok := dbURLRewrites[k]; ok {
			env = append(env, fmt.Sprintf("%s=%s", k, rewritten))
		} else {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}

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
		binds = append(binds, fmt.Sprintf("%s:/opt/openstack/secrets-proxy:ro", secretsProxyPath))
		env = append(env,
			"PARAMETERS_SECRETS_EXTENSION_HTTP_PORT=2773",
			"AWS_SESSION_TOKEN=local-dev-token",
			"OPENSTACK_INTERNAL_LAMBDA=1",
		)
		bgCmds = append(bgCmds, "/opt/openstack/secrets-proxy &")
	}

	if dbProxyPath != "" {
		binds = append(binds, fmt.Sprintf("%s:/opt/openstack/db-proxy:ro", dbProxyPath))
		env = append(env,
			fmt.Sprintf("OPENSTACK_DB_UPSTREAM=%s", dbUpstream),
			fmt.Sprintf("OPENSTACK_DB_NAME=%s", dbName),
			fmt.Sprintf("OPENSTACK_DB_PROXY_PORT=%d", dbProxyPort),
		)
		bgCmds = append(bgCmds, "/opt/openstack/db-proxy &")
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
	}

	name := fmt.Sprintf("openstack-lambda-%s-%d", sanitizeName(fn.FunctionName), time.Now().UnixMilli())

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
	}

	e.mu.Lock()
	e.containers[fn.FunctionName] = info
	e.mu.Unlock()

	return info, nil
}

// StartContainer starts a container and resolves its mapped host port.
func (e *Engine) StartContainer(ctx context.Context, info *ContainerInfo) error {
	if err := e.client.ContainerStart(ctx, info.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Inspect to get the dynamically assigned host port.
	// Retry a few times because the port mapping may take a moment to appear.
	var hostPort string
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

		time.Sleep(200 * time.Millisecond)
	}

	if hostPort == "" {
		return fmt.Errorf("no port mapping found for container %s after retries", info.ID[:12])
	}

	info.HostPort = hostPort
	info.State = "running"

	log.Printf("[engine] container %s started for %s (port %s)", info.ID[:12], info.FunctionName, info.HostPort)
	return nil
}

// StopContainer stops a running container.
func (e *Engine) StopContainer(ctx context.Context, containerID string) error {
	timeout := 5
	return e.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// RemoveContainer removes a container and clears it from the tracked map.
func (e *Engine) RemoveContainer(ctx context.Context, containerID string) error {
	e.mu.Lock()
	for name, info := range e.containers {
		if info.ID == containerID {
			delete(e.containers, name)
			break
		}
	}
	e.mu.Unlock()

	return e.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// EvictContainer stops and removes a function's warm container.
func (e *Engine) EvictContainer(ctx context.Context, functionName string) {
	e.mu.RLock()
	info, ok := e.containers[functionName]
	e.mu.RUnlock()
	if !ok {
		return
	}
	_ = e.StopContainer(ctx, info.ID)
	_ = e.RemoveContainer(ctx, info.ID)
}

// GetContainer returns info about a function's container if it exists.
func (e *Engine) GetContainer(functionName string) (*ContainerInfo, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	info, ok := e.containers[functionName]
	return info, ok
}

// ContainerLogs retrieves stdout/stderr logs from a container.
// Docker multiplexes stdout/stderr with 8-byte headers; stdcopy demuxes them.
func (e *Engine) ContainerLogs(ctx context.Context, containerID string) (string, error) {
	reader, err := e.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	logs, err := readContainerLogStream(reader)
	if err != nil {
		// Fallback: some container configs use raw stream (no mux headers)
		var raw strings.Builder
		reader2, err2 := e.client.ContainerLogs(ctx, containerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
		})
		if err2 != nil {
			return "", err2
		}
		defer func() { _ = reader2.Close() }()
		_, _ = io.Copy(&raw, reader2)
		return raw.String(), nil
	}

	return logs, nil
}

func readContainerLogStream(reader io.Reader) (string, error) {
	// Preserve stdout/stderr interleaving by writing both streams to the same
	// destination buffer in the order frames are read.
	var combined bytes.Buffer
	if _, err := stdcopy.StdCopy(&combined, &combined, reader); err != nil {
		return "", err
	}
	return combined.String(), nil
}

// Cleanup stops and removes all managed containers.
func (e *Engine) Cleanup(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for name, info := range e.containers {
		_ = e.client.ContainerStop(ctx, info.ID, container.StopOptions{})
		_ = e.client.ContainerRemove(ctx, info.ID, container.RemoveOptions{Force: true})
		delete(e.containers, name)
	}
}

// Close releases the Docker client resources.
func (e *Engine) Close() error {
	return e.client.Close()
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

func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "-", ":", "-", " ", "-")
	return strings.ToLower(r.Replace(name))
}
