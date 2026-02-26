package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// ContainerInfo holds metadata about a running Lambda container.
type ContainerInfo struct {
	ID           string
	FunctionName string
	Runtime      types.Runtime
	CreatedAt    time.Time
	LastInvoked  time.Time
	Port         int
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

	reader, err := e.client.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", img, err)
	}
	defer reader.Close()

	// Consume the output to complete the pull
	_, err = io.Copy(io.Discard, reader)
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

// CreateContainer creates a new Lambda execution container.
func (e *Engine) CreateContainer(ctx context.Context, fn *types.FunctionConfig, codeDir string, layerDirs []string) (*ContainerInfo, error) {
	img, ok := types.RuntimeImageMap[fn.Runtime]
	if !ok {
		return nil, fmt.Errorf("unsupported runtime: %s", fn.Runtime)
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

	// Add user-defined environment variables
	for k, v := range fn.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Build bind mounts: code dir + layer dirs
	binds := []string{
		fmt.Sprintf("%s:/var/task:ro", codeDir),
	}
	for _, layerDir := range layerDirs {
		binds = append(binds, fmt.Sprintf("%s:/opt:ro", layerDir))
	}

	memoryBytes := int64(fn.MemorySize) * 1024 * 1024

	containerCfg := &container.Config{
		Image: img,
		Env:   env,
		Cmd:   []string{fn.Handler},
	}

	hostCfg := &container.HostConfig{
		Binds: binds,
		Resources: container.Resources{
			Memory: memoryBytes,
		},
		// Allow container to reach OpenStack API on host
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

// StartContainer starts a previously created container.
func (e *Engine) StartContainer(ctx context.Context, containerID string) error {
	return e.client.ContainerStart(ctx, containerID, container.StartOptions{})
}

// StopContainer stops a running container.
func (e *Engine) StopContainer(ctx context.Context, containerID string) error {
	timeout := 5
	return e.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// RemoveContainer removes a container.
func (e *Engine) RemoveContainer(ctx context.Context, containerID string) error {
	return e.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// GetContainer returns info about a function's container if it exists.
func (e *Engine) GetContainer(functionName string) (*ContainerInfo, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	info, ok := e.containers[functionName]
	return info, ok
}

// ContainerLogs retrieves logs from a container.
func (e *Engine) ContainerLogs(ctx context.Context, containerID string) (string, error) {
	reader, err := e.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", err
	}
	defer reader.Close()

	buf := new(strings.Builder)
	_, err = io.Copy(buf, reader)
	return buf.String(), err
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

func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "-", ":", "-", " ", "-")
	return strings.ToLower(r.Replace(name))
}
