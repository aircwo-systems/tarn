package lambda

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/engine"
	logssvc "github.com/aircwo-systems/tarn/internal/logs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Service implements Lambda business logic.
type Service struct {
	cfg     *config.Config
	store   *Store
	engine  *engine.Engine
	pool    *engine.WarmPool
	invoker *engine.Invoker
	logsSvc *logssvc.Service
	startMu sync.Map // per-function mutex to prevent duplicate cold starts

	logCursorMu sync.Mutex
	logCursors  map[string]int
	metricsMu   sync.RWMutex
	metrics     map[string]*FunctionMetrics
}

// FunctionMetrics tracks invoke-level function metrics used by the dashboard.
type FunctionMetrics struct {
	Invocations       int64     `json:"invocations"`
	MessagesProcessed int64     `json:"messagesProcessed"`
	LastInvokedAt     time.Time `json:"lastInvokedAt"`
}

const (
	coldStartInvokeRetryAttempts = 4
	coldStartInvokeRetryBackoff  = 150 * time.Millisecond

	// containerAcquireTimeout bounds how long an invoke waits for a free
	// execution environment when the function is at its concurrency cap.
	containerAcquireTimeout = 60 * time.Second
	// containerAcquirePoll is how often to re-check for a freed environment.
	containerAcquirePoll = 25 * time.Millisecond
)

// NewService creates a new Lambda service.
func NewService(cfg *config.Config, store *Store, eng *engine.Engine, pool *engine.WarmPool, logsSvc *logssvc.Service) *Service {
	return &Service{
		cfg:        cfg,
		store:      store,
		engine:     eng,
		pool:       pool,
		invoker:    engine.NewInvoker(),
		logsSvc:    logsSvc,
		logCursors: make(map[string]int),
		metrics:    make(map[string]*FunctionMetrics),
	}
}

// CreateFunction creates a new Lambda function.
// If a function with the same name already exists on disk (e.g. from a previous
// run that was not cleanly destroyed), it is overwritten.  Returning
// ResourceConflictException in that case causes the Terraform AWS provider v5
// to retry until timeout and taint the resource, which is unhelpful for a local
// emulator where disk state can outlive Terraform state.
func (s *Service) CreateFunction(ctx context.Context, fn *types.FunctionConfig, code []byte) (*types.FunctionConfig, error) {
	// Evict any warm container for a pre-existing function so the next invoke
	// picks up the new code and config.
	s.evictWarmContainer(fn.FunctionName)

	if fn.Timeout == 0 {
		fn.Timeout = s.cfg.LambdaDefaultTimeout
	}
	if fn.MemorySize == 0 {
		fn.MemorySize = s.cfg.LambdaDefaultMemory
	}
	if fn.Version == "" {
		fn.Version = "$LATEST"
	}

	fn.FunctionArn = fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", s.cfg.Region, s.cfg.AccountID, fn.FunctionName)
	// Skip the Pending→Active transition dance: return Active immediately so
	// Terraform's WaitUntilFunctionActiveV2 waiter completes on its first poll
	// rather than requiring two round-trips (~5s per function on default intervals).
	fn.State = types.FunctionStateActive
	fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	fn.LastModified = time.Now()

	if len(code) > 0 {
		hash, err := s.store.SaveCode(fn.FunctionName, code)
		if err != nil {
			return nil, fmt.Errorf("failed to save code: %w", err)
		}
		fn.CodeSHA256 = hash
		fn.CodeSize = int64(len(code))

		// Pre-extract so the first invoke is faster
		if _, err := s.store.ExtractCode(fn.FunctionName); err != nil {
			log.Printf("[lambda] warning: failed to pre-extract code for %s: %v", fn.FunctionName, err)
		}
	}

	if err := s.store.SaveFunction(fn); err != nil {
		return nil, fmt.Errorf("failed to save function: %w", err)
	}
	s.ensureFunctionMetrics(fn.FunctionName)
	if s.logsSvc != nil {
		s.logsSvc.CreateLogGroup(fmt.Sprintf("/aws/lambda/%s", fn.FunctionName))
		s.logsSvc.LogSystemEvent(logssvc.LevelINFO, fmt.Sprintf("Function created: %s (runtime: %s)", fn.FunctionName, fn.Runtime))
	}

	// Pull runtime image in background
	if s.engine != nil {
		go func() {
			pullCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := s.engine.EnsureImage(pullCtx, fn.Runtime); err != nil {
				log.Printf("[lambda] warning: failed to pull image for %s: %v", fn.Runtime, err)
			}
		}()
	}

	return fn, nil
}

func (s *Service) promoteFunctionToActiveIfPending(name string) error {
	fn, err := s.store.GetFunction(name)
	if err != nil {
		return err
	}

	if fn.LastUpdateStatus == "" {
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	}

	if fn.State != types.FunctionStatePending {
		if fn.LastUpdateStatus == types.LastUpdateStatusSuccessful {
			return nil
		}
		return s.store.SaveFunction(fn)
	}

	fn.State = types.FunctionStateActive
	fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	return s.store.SaveFunction(fn)
}

// getFunctionMutex returns a per-function mutex (created lazily) to serialize cold starts.
func (s *Service) getFunctionMutex(name string) *sync.Mutex {
	v, _ := s.startMu.LoadOrStore(name, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// ActivatePendingFunctions promotes all persisted functions from Pending to Active
// on startup, so functions saved mid-create (before the 750ms timer fired) are
// immediately invokable and show as Active in the UI.
func (s *Service) ActivatePendingFunctions() {
	fns, err := s.store.ListFunctions()
	if err != nil {
		log.Printf("[lambda] warning: could not list functions for startup activation: %v", err)
		return
	}
	for _, fn := range fns {
		if fn.State == types.FunctionStatePending {
			if err := s.promoteFunctionToActiveIfPending(fn.FunctionName); err != nil {
				log.Printf("[lambda] warning: could not activate %s on startup: %v", fn.FunctionName, err)
			} else {
				log.Printf("[lambda] activated pending function %s on startup", fn.FunctionName)
			}
		}
	}
}

// GetFunction retrieves a function by name.
func (s *Service) GetFunction(name string) (*types.FunctionConfig, error) {
	fn, err := s.store.GetFunction(name)
	if err != nil {
		return nil, err
	}
	// older functions may not have the field populated
	if fn.LastUpdateStatus == "" {
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	}

	// If we are still in the pending state, return that value once and
	// transition the persisted configuration to active.  This ensures the
	// first observer sees "Pending" and subsequent callers observe the
	// expected AWS-like progression.
	if fn.State == types.FunctionStatePending {
		// copy before mutating so we don't modify the object returned to
		// the caller.
		original := *fn
		fn.State = types.FunctionStateActive
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
		// persist the updated state synchronously so the next read
		// immediately sees Active (an async save races with the next poll).
		_ = s.store.SaveFunction(fn)
		return &original, nil
	}

	return fn, nil
}

// ListFunctions returns all functions.
func (s *Service) ListFunctions() ([]*types.FunctionConfig, error) {
	return s.store.ListFunctions()
}

// DeleteFunction removes a function and its container.
func (s *Service) DeleteFunction(ctx context.Context, name string) error {
	s.evictWarmContainer(name)
	if err := s.store.DeleteFunction(name); err != nil {
		return err
	}
	s.deleteFunctionMetrics(name)
	if s.logsSvc != nil {
		s.logsSvc.LogSystemEvent(logssvc.LevelINFO, fmt.Sprintf("Function deleted: %s", name))
	}
	return nil
}

// GetEventExamples reads *.json files from the events/ directory inside a
// function's deployed zip and returns them keyed by filename (without extension).
// It handles both raw proxy events ({"httpMethod":...,"body":...}) and plain
// JSON objects. Returns nil when no events directory exists or code is absent.
func (s *Service) GetEventExamples(name string) map[string]json.RawMessage {
	extractDir, err := s.store.ExtractCode(name)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(extractDir, "events"))
	if err != nil {
		return nil
	}
	result := make(map[string]json.RawMessage)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(extractDir, "events", e.Name()))
		if err != nil || !json.Valid(data) {
			continue
		}
		result[strings.TrimSuffix(e.Name(), ".json")] = json.RawMessage(data)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// UpdateFunctionCode replaces function code.
func (s *Service) UpdateFunctionCode(ctx context.Context, name string, code []byte) (*types.FunctionConfig, error) {
	fn, err := s.store.GetFunction(name)
	if err != nil {
		return nil, err
	}

	hash, err := s.store.SaveCode(name, code)
	if err != nil {
		return nil, fmt.Errorf("failed to save code: %w", err)
	}

	fn.CodeSHA256 = hash
	fn.CodeSize = int64(len(code))
	fn.LastModified = time.Now()
	fn.LastUpdateStatus = types.LastUpdateStatusSuccessful

	// Pre-extract so the first invoke is faster; failure is non-fatal.
	if _, err := s.store.ExtractCode(name); err != nil {
		log.Printf("[lambda] warning: failed to pre-extract code for %s: %v", name, err)
	}

	// Evict warm container so next invoke uses new code
	s.evictWarmContainer(name)

	if err := s.store.SaveFunction(fn); err != nil {
		return nil, err
	}

	if s.logsSvc != nil {
		s.logsSvc.LogSystemEvent(logssvc.LevelINFO, fmt.Sprintf("Function code updated: %s", name))
	}
	return fn, nil
}

// TagResource adds or overwrites tags on a function.
func (s *Service) TagResource(name string, tags map[string]string) error {
	fn, err := s.store.GetFunction(name)
	if err != nil {
		return err
	}
	if fn.Tags == nil {
		fn.Tags = make(map[string]string)
	}
	for k, v := range tags {
		fn.Tags[k] = v
	}
	return s.store.SaveFunction(fn)
}

// UntagResource removes specified tag keys from a function.
func (s *Service) UntagResource(name string, tagKeys []string) error {
	fn, err := s.store.GetFunction(name)
	if err != nil {
		return err
	}
	if fn.Tags != nil {
		for _, k := range tagKeys {
			delete(fn.Tags, k)
		}
	}
	return s.store.SaveFunction(fn)
}

// --- Layer operations ---

// PublishLayerVersion publishes a new layer version.
func (s *Service) PublishLayerVersion(name, description string, runtimes []string, code []byte) (*types.LayerConfig, error) {
	version := s.store.NextLayerVersion(name)

	cfg := &types.LayerConfig{
		LayerName:          name,
		LayerArn:           fmt.Sprintf("arn:aws:lambda:%s:%s:layer:%s", s.cfg.Region, s.cfg.AccountID, name),
		VersionNumber:      version,
		Description:        description,
		CompatibleRuntimes: runtimes,
		CreatedDate:        time.Now().UTC().Format(time.RFC3339),
	}
	cfg.LayerVersionArn = fmt.Sprintf("%s:%d", cfg.LayerArn, version)

	hash, err := s.store.SaveLayer(name, version, code, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to save layer: %w", err)
	}
	cfg.CodeSHA256 = hash
	cfg.CodeSize = int64(len(code))

	return cfg, nil
}

// GetLayerVersion retrieves a specific layer version.
func (s *Service) GetLayerVersion(name string, version int64) (*types.LayerConfig, error) {
	return s.store.GetLayer(name, version)
}

// ListLayers returns all layers (latest version of each).
func (s *Service) ListLayers() ([]*types.LayerConfig, error) {
	return s.store.ListLayers()
}

// ListLayerVersions returns all versions for a layer.
func (s *Service) ListLayerVersions(name string) ([]*types.LayerConfig, error) {
	return s.store.ListLayerVersions(name)
}

// DeleteLayerVersion removes a specific layer version.
func (s *Service) DeleteLayerVersion(name string, version int64) error {
	return s.store.DeleteLayerVersion(name, version)
}

// ResolveLayerDirs extracts and returns paths for all layer ARNs on a function.
// External managed layers that are unavailable in the local emulator are skipped.
func (s *Service) ResolveLayerDirs(layers []string) ([]string, []string, error) {
	var dirs []string
	var skipped []string
	for _, arn := range layers {
		name, version, err := parseLayerArn(arn)
		if err != nil {
			return nil, nil, err
		}
		dir, err := s.store.ExtractLayer(name, version)
		if err != nil {
			if shouldSkipUnavailableExternalLayer(arn, s.cfg.AccountID, err) {
				skipped = append(skipped, arn)
				continue
			}
			return nil, nil, fmt.Errorf("failed to extract layer %s v%d: %w", name, version, err)
		}
		dirs = append(dirs, dir)
	}
	return dirs, skipped, nil
}

func shouldSkipUnavailableExternalLayer(layerARN, localAccountID string, err error) bool {
	layerARN = strings.TrimSpace(layerARN)
	if layerARN == "" || err == nil {
		return false
	}
	if !strings.HasPrefix(layerARN, "arn:aws:lambda:") || !strings.Contains(layerARN, ":layer:") {
		return false
	}
	parts := strings.Split(layerARN, ":")
	if len(parts) < 8 {
		return false
	}
	accountID := strings.TrimSpace(parts[4])
	if accountID == "" || accountID == localAccountID {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "layer code not found") || strings.Contains(msg, "no such file or directory")
}

// parseLayerArn extracts the layer name and version from an ARN like
// "arn:aws:lambda:us-east-1:000000000000:layer:myLayer:1"
func parseLayerArn(arn string) (string, int64, error) {
	parts := strings.Split(arn, ":")
	if len(parts) < 8 {
		return "", 0, fmt.Errorf("invalid layer ARN: %s", arn)
	}
	name := parts[len(parts)-2]
	version, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid layer version in ARN %s: %w", arn, err)
	}
	return name, version, nil
}

// UpdateFunctionConfiguration updates a function's configuration without changing its code.
func (s *Service) UpdateFunctionConfiguration(ctx context.Context, name string, update *types.FunctionConfigUpdate) (*types.FunctionConfig, error) {
	fn, err := s.store.GetFunction(name)
	if err != nil {
		return nil, err
	}

	if update.Handler != "" {
		fn.Handler = update.Handler
	}
	if update.Description != nil {
		fn.Description = *update.Description
	}
	if update.Timeout != 0 {
		fn.Timeout = update.Timeout
	}
	if update.MemorySize != 0 {
		fn.MemorySize = update.MemorySize
	}
	if update.Role != "" {
		fn.Role = update.Role
	}
	if update.Runtime != "" {
		fn.Runtime = types.Runtime(update.Runtime)
	}
	if update.Environment != nil {
		fn.Environment = update.Environment.Variables
	}
	if update.Layers != nil {
		fn.Layers = update.Layers
	}
	if update.DeadLetterConfig != nil {
		fn.DeadLetterConfig = update.DeadLetterConfig
	}

	fn.LastModified = time.Now()
	fn.LastUpdateStatus = types.LastUpdateStatusSuccessful

	// Evict warm container since config changed
	s.evictWarmContainer(name)

	if err := s.store.SaveFunction(fn); err != nil {
		return nil, err
	}

	return fn, nil
}

// Invoke executes a Lambda function.
func (s *Service) Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	fn, err := s.store.GetFunction(input.FunctionName)
	if err != nil {
		return nil, err
	}

	if fn.State == types.FunctionStatePending {
		if err := s.promoteFunctionToActiveIfPending(fn.FunctionName); err != nil {
			return nil, fmt.Errorf("failed to promote function %s to active: %w", fn.FunctionName, err)
		}
		fn, err = s.store.GetFunction(input.FunctionName)
		if err != nil {
			return nil, err
		}
	}

	if fn.State != types.FunctionStateActive {
		err := fmt.Errorf("function %s is not active (state: %s)", fn.FunctionName, fn.State)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, err.Error())
		return nil, err
	}
	if s.engine == nil || s.pool == nil {
		err := fmt.Errorf("lambda execution engine is not configured")
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, err.Error())
		return nil, err
	}

	// DryRun just validates the function exists and is active
	if input.InvocationType == "DryRun" {
		return &types.InvokeOutput{StatusCode: 204}, nil
	}

	// Ensure code is extracted
	codeDir, err := s.store.ExtractCode(fn.FunctionName)
	if err != nil {
		wrapped := fmt.Errorf("failed to extract code: %w", err)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
		return nil, wrapped
	}

	// Resolve layer directories
	var layerDirs []string
	if len(fn.Layers) > 0 {
		var skippedLayers []string
		layerDirs, skippedLayers, err = s.ResolveLayerDirs(fn.Layers)
		if err != nil {
			wrapped := fmt.Errorf("failed to resolve layers: %w", err)
			s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
			return nil, wrapped
		}
		for _, layerARN := range skippedLayers {
			s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelWARN, fmt.Sprintf("Skipping unavailable external layer %s; invocation continues without it", layerARN))
		}
	}

	// Ensure runtime image is available (block if needed)
	if err := s.engine.EnsureImage(ctx, fn.Runtime); err != nil {
		wrapped := fmt.Errorf("runtime image not available: %w", err)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
		return nil, wrapped
	}

	// Acquire a dedicated execution environment for this invocation. Concurrent
	// invokes each get their own warm container (created on demand up to the
	// per-function concurrency cap), mirroring AWS Lambda — so a Step Functions
	// Map / parallel fan-out runs truly in parallel instead of serializing on a
	// single container.
	info, coldStart, err := s.acquireContainer(ctx, fn, codeDir, layerDirs)
	if err != nil {
		return nil, err
	}
	// Return the environment to the pool when this invocation finishes.
	defer s.engine.Release(info)

	s.pool.Touch(fn.FunctionName)

	// Identify this invocation: a fresh request id plus the log group/stream the
	// container's output is ingested into (the stream name mirrors the truncated
	// container id used by ingestContainerLogs). These let callers deep-link to
	// the exact logs for this call.
	requestID := uuid.NewString()
	logGroup := fmt.Sprintf("/aws/lambda/%s", fn.FunctionName)
	logStream := info.ID
	if len(logStream) > 12 {
		logStream = logStream[:12]
	}

	// Set timeout deadline for the invocation
	invokeCtx, cancel := context.WithTimeout(ctx, time.Duration(fn.Timeout)*time.Second)
	defer cancel()

	invokeStart := time.Now()
	s.logInvocationBoundary(logGroup, logStream, fmt.Sprintf("START RequestId: %s Version: %s", requestID, fn.Version))

	output, err := s.invokeWithRetry(invokeCtx, info, input, coldStart)
	if err != nil {
		s.logInvocationBoundary(logGroup, logStream, fmt.Sprintf("END RequestId: %s", requestID))
		wrapped := fmt.Errorf("invocation failed: %w", err)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
		return nil, wrapped
	}

	s.logInvocationBoundary(logGroup, logStream, fmt.Sprintf("END RequestId: %s", requestID))
	s.logInvocationBoundary(logGroup, logStream, fmt.Sprintf("REPORT RequestId: %s\tDuration: %.2f ms", requestID, float64(time.Since(invokeStart).Microseconds())/1000.0))

	output.ExecutedVersion = fn.Version
	output.RequestID = requestID
	output.LogGroup = logGroup
	output.LogStream = logStream
	s.recordInvocation(fn.FunctionName, input.Payload)

	// For async invocations, return 202 immediately
	if input.InvocationType == "Event" {
		return &types.InvokeOutput{
			StatusCode:      202,
			ExecutedVersion: fn.Version,
			RequestID:       requestID,
			LogGroup:        logGroup,
			LogStream:       logStream,
		}, nil
	}

	return output, nil
}

// logInvocationBoundary writes an AWS-style START/END/REPORT marker line into a
// function's log stream so a deep-linked invocation can be located precisely
// within a (warm-reused) stream. It is best-effort and a no-op without logs.
func (s *Service) logInvocationBoundary(logGroup, logStream, message string) {
	if s.logsSvc == nil {
		return
	}
	s.logsSvc.CreateLogGroup(logGroup)
	s.logsSvc.PutLogEvents(logGroup, logStream, []logssvc.LogEvent{{
		Timestamp: time.Now().UTC(),
		Message:   message,
		Level:     logssvc.LevelINFO,
		Source:    logssvc.SourceRuntime,
	}})
}

// acquireContainer returns a ready, exclusively-held execution environment for
// the function. It first reuses any idle warm container; otherwise it cold-starts
// a new one, up to LambdaMaxConcurrency per function; if the cap is reached and
// all environments are busy, it waits (up to containerAcquireTimeout) for one to
// free up. Cold starts are serialized per function so the cap is enforced and
// Docker isn't stampeded, while invocations themselves run concurrently. The
// returned container is Busy and must be returned via engine.Release.
func (s *Service) acquireContainer(ctx context.Context, fn *types.FunctionConfig, codeDir string, layerDirs []string) (*engine.ContainerInfo, bool, error) {
	maxConc := s.cfg.LambdaMaxConcurrency
	if maxConc < 1 {
		maxConc = 1
	}
	fnMu := s.getFunctionMutex(fn.FunctionName)
	deadline := time.Now().Add(containerAcquireTimeout)

	for {
		// Fast path: reuse an idle warm container.
		if info, ok := s.engine.AcquireIdle(fn.FunctionName); ok {
			log.Printf("[lambda] warm invoke for %s (container %s)", fn.FunctionName, info.ID[:12])
			return info, false, nil
		}

		// Decide whether to cold-start, serialized per function so the cap holds.
		fnMu.Lock()
		// Re-check: another invoke may have released an environment meanwhile.
		if info, ok := s.engine.AcquireIdle(fn.FunctionName); ok {
			fnMu.Unlock()
			log.Printf("[lambda] warm invoke for %s (container %s)", fn.FunctionName, info.ID[:12])
			return info, false, nil
		}
		if s.engine.CountContainers(fn.FunctionName) < maxConc {
			info, cerr := s.coldStart(ctx, fn, codeDir, layerDirs)
			fnMu.Unlock()
			if cerr != nil {
				return nil, false, cerr
			}
			return info, true, nil
		}
		fnMu.Unlock()

		// At the concurrency cap with everything busy — wait for a release.
		if time.Now().After(deadline) {
			err := fmt.Errorf("no execution environment available for %s within %s (concurrency cap %d reached)", fn.FunctionName, containerAcquireTimeout, maxConc)
			s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, err.Error())
			return nil, false, err
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(containerAcquirePoll):
		}
	}
}

// coldStart creates, starts and readies a new container for the function and
// returns it Busy (already counted in the pool). Caller must hold the
// per-function mutex.
func (s *Service) coldStart(ctx context.Context, fn *types.FunctionConfig, codeDir string, layerDirs []string) (*engine.ContainerInfo, error) {
	log.Printf("[lambda] cold start for %s", fn.FunctionName)
	if s.logsSvc != nil {
		s.logsSvc.LogSystemEvent(logssvc.LevelINFO, fmt.Sprintf("Cold start: %s", fn.FunctionName))
	}

	info, err := s.engine.CreateContainer(ctx, fn, codeDir, layerDirs)
	if err != nil {
		wrapped := fmt.Errorf("failed to create container: %w", err)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
		return nil, wrapped
	}

	if err := s.engine.StartContainer(ctx, info); err != nil {
		_ = s.engine.RemoveContainer(ctx, info.ID)
		wrapped := fmt.Errorf("failed to start container: %w", err)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
		return nil, wrapped
	}

	// Wait for the RIE to be ready. Capture startup logs before removing a failed
	// container (important for runtimes like Java/Spring Boot whose crash output
	// would otherwise be lost). Only this container is removed, not the pool.
	if err := s.invoker.WaitForReady(ctx, info.HostPort); err != nil {
		s.ingestContainerLogs(fn.FunctionName, info)
		_ = s.engine.RemoveContainer(ctx, info.ID)
		if s.logsSvc != nil {
			s.logsSvc.LogSystemEvent(logssvc.LevelERROR, fmt.Sprintf("Container failed to start: %s: %v", fn.FunctionName, err))
		}
		wrapped := fmt.Errorf("container not ready: %w", err)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
		return nil, wrapped
	}
	return info, nil
}

func (s *Service) invokeWithRetry(ctx context.Context, info *engine.ContainerInfo, input *types.InvokeInput, coldStart bool) (*types.InvokeOutput, error) {
	attempts := 1
	if coldStart {
		attempts += coldStartInvokeRetryAttempts
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		output, err := s.invoker.InvokeWithLogs(ctx, s.engine, info, input)
		s.ingestContainerLogs(input.FunctionName, info)
		if err == nil {
			return output, nil
		}

		if !coldStart || !isTransientRIEInvokeError(err) || attempt == attempts {
			return nil, err
		}

		backoff := time.Duration(attempt) * coldStartInvokeRetryBackoff
		log.Printf("[lambda] transient invoke error for %s (attempt %d/%d): %v; retrying in %s", input.FunctionName, attempt, attempts, err, backoff)

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return nil, fmt.Errorf("invoke retry loop exhausted for %s", input.FunctionName)
}

func isTransientRIEInvokeError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "rie invocation failed") {
		return false
	}

	transientTokens := []string{
		"connection refused",
		"connection reset by peer",
		"use of closed network connection",
		"server closed idle connection",
		"broken pipe",
		"unexpected eof",
		" eof",
	}
	for _, token := range transientTokens {
		if strings.Contains(msg, token) {
			return true
		}
	}

	return false
}

// GetFunctionMetrics returns invocation metrics for the given function.
func (s *Service) GetFunctionMetrics(name string) FunctionMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	metric, exists := s.metrics[name]
	if !exists {
		return FunctionMetrics{}
	}
	return *metric
}

func (s *Service) ensureFunctionMetrics(name string) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	if _, exists := s.metrics[name]; !exists {
		s.metrics[name] = &FunctionMetrics{}
	}
}

func (s *Service) deleteFunctionMetrics(name string) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	delete(s.metrics, name)
}

func (s *Service) recordInvocation(name string, payload []byte) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	metric, exists := s.metrics[name]
	if !exists {
		metric = &FunctionMetrics{}
		s.metrics[name] = metric
	}

	metric.Invocations++
	metric.MessagesProcessed += countProcessedMessages(payload)
	metric.LastInvokedAt = time.Now().UTC()
}

func countProcessedMessages(payload []byte) int64 {
	if len(payload) == 0 {
		return 1
	}

	var envelope struct {
		Records *[]json.RawMessage `json:"Records"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && envelope.Records != nil {
		return int64(len(*envelope.Records))
	}

	return 1
}

func (s *Service) evictWarmContainer(functionName string) {
	if s.engine == nil {
		return
	}
	if info, ok := s.engine.GetContainer(functionName); ok && info != nil {
		s.clearLogCursor(info.ID)
	}
	s.engine.EvictContainerAsync(functionName)
}

func (s *Service) ingestContainerLogs(functionName string, info *engine.ContainerInfo) {
	if s.logsSvc == nil || s.engine == nil || info == nil {
		return
	}

	logCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rawLogs, err := s.engine.ContainerLogs(logCtx, info.ID)
	if err != nil || rawLogs == "" {
		return
	}

	newLogs := s.consumeNewContainerLogs(info.ID, rawLogs)
	if newLogs == "" {
		return
	}

	streamName := info.ID
	if len(streamName) > 12 {
		streamName = streamName[:12]
	}
	s.logsSvc.IngestContainerLogs(functionName, streamName, newLogs)
}

func (s *Service) logFunctionRuntimeEvent(functionName string, level logssvc.LogLevel, message string) {
	if s.logsSvc == nil {
		return
	}
	functionName = strings.TrimSpace(functionName)
	message = strings.TrimSpace(message)
	if functionName == "" || message == "" {
		return
	}

	groupName := fmt.Sprintf("/aws/lambda/%s", functionName)
	s.logsSvc.CreateLogGroup(groupName)
	s.logsSvc.PutLogEvents(groupName, "tarn", []logssvc.LogEvent{{
		Timestamp: time.Now().UTC(),
		Message:   message,
		Level:     level,
		Source:    logssvc.SourceRuntime,
	}})
	s.logsSvc.LogSystemEvent(level, fmt.Sprintf("%s: %s", functionName, message))
}

func (s *Service) consumeNewContainerLogs(containerID, rawLogs string) string {
	s.logCursorMu.Lock()
	defer s.logCursorMu.Unlock()

	offset := s.logCursors[containerID]
	if offset < 0 || offset > len(rawLogs) {
		offset = 0
	}

	newLogs := rawLogs[offset:]
	s.logCursors[containerID] = len(rawLogs)
	return newLogs
}

func (s *Service) clearLogCursor(containerID string) {
	s.logCursorMu.Lock()
	defer s.logCursorMu.Unlock()
	delete(s.logCursors, containerID)
}
