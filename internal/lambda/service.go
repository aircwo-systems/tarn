package lambda

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/semaphore"
	"time"

	"github.com/google/uuid"

	"github.com/aircwo-systems/tarn/internal/asyncqueue"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/engine"
	logssvc "github.com/aircwo-systems/tarn/internal/logs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Service implements Lambda business logic.
type Service struct {
	cfg        *config.Config
	store      *Store
	engine     *engine.Engine
	pool       *engine.WarmPool
	invoker    *engine.Invoker
	logsSvc    *logssvc.Service
	startGates sync.Map // function name -> cancellable cold-start gate

	metricsMu sync.RWMutex
	metrics   map[string]*FunctionMetrics

	async *asyncqueue.Queue // asynchronous (Event) invokes
}

// FunctionMetrics tracks invoke-level function metrics used by the dashboard.
type FunctionMetrics struct {
	instance          string
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

	// asyncWorkers bounds how many asynchronous invokes run at once across
	// the account; asyncBacklog is how many may wait before Enqueue reports
	// throttling.
	asyncWorkers = 16
	asyncBacklog = 10000
	// asyncCloseTimeout is how long shutdown waits for queued and running
	// asynchronous invokes before cancelling them.
	asyncCloseTimeout = 30 * time.Second
)

type queuedInvocationKey struct{}

// NewService creates a new Lambda service.
func NewService(cfg *config.Config, store *Store, eng *engine.Engine, pool *engine.WarmPool, logsSvc *logssvc.Service) *Service {
	return &Service{
		cfg:     cfg,
		store:   store,
		engine:  eng,
		pool:    pool,
		invoker: engine.NewInvoker(),
		logsSvc: logsSvc,
		metrics: make(map[string]*FunctionMetrics),
		async:   asyncqueue.New("lambda", asyncWorkers, asyncBacklog),
	}
}

// Enqueue checks that the function can be invoked, then queues run to perform
// an asynchronous (Event) invoke and returns without waiting for it, as AWS
// does. run gets a context that the submitting request cannot cancel. A full
// backlog returns ErrTooManyRequests.
func (s *Service) Enqueue(functionName string, run func(ctx context.Context)) error {
	if _, err := s.invocableFunction(functionName); err != nil {
		return err
	}
	if err := s.async.Submit(func(ctx context.Context) { run(context.WithValue(ctx, queuedInvocationKey{}, true)) }); err != nil {
		return fmt.Errorf("asynchronous invoke of %s not queued: %v: %w", functionName, err, ErrTooManyRequests)
	}
	return nil
}

// DrainAsync waits for every queued asynchronous invoke to finish.
func (s *Service) DrainAsync() { s.async.Drain() }

// Close waits for queued asynchronous invokes, then stops the queue.
func (s *Service) Close() { s.async.Close(asyncCloseTimeout) }

// CreateFunction creates a new Lambda function.
// If a function with the same name already exists on disk (e.g. from a previous
// run that was not cleanly destroyed), it is overwritten.  Returning
// ResourceConflictException in that case causes the Terraform AWS provider v5
// to retry until timeout and taint the resource, which is unhelpful for a local
// emulator where disk state can outlive Terraform state.
func (s *Service) CreateFunction(ctx context.Context, fn *types.FunctionConfig, code []byte) (*types.FunctionConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, invalid("function configuration is required")
	}
	fn = cloneFunction(fn)
	if fn.Timeout == 0 {
		fn.Timeout = s.cfg.LambdaDefaultTimeout
	}
	if fn.MemorySize == 0 {
		fn.MemorySize = s.cfg.LambdaDefaultMemory
	}
	if len(fn.Architectures) == 0 {
		fn.Architectures = []string{"x86_64"}
	}
	fn.Version = "$LATEST"
	if fn.PackageType == "" {
		fn.PackageType = "Zip"
		if fn.ImageURI != "" {
			fn.PackageType = "Image"
		}
	}
	if err := validateConfiguration(fn); err != nil {
		return nil, err
	}
	fn.FunctionArn = fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", s.cfg.Region, s.cfg.AccountID, fn.FunctionName)
	fn.State = types.FunctionStateActive
	fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	fn.LastModified = time.Now()
	if fn.PackageType == "Image" && s.engine != nil {
		resolved, err := s.engine.ResolveImageReference(ctx, fn.ImageURI, fn.Architectures...)
		if err != nil {
			return nil, err
		}
		fn.ResolvedImageURI = resolved
	}
	result, err := s.store.commitDeployment(fn, code, true, func(record *functionRecord) {
		if s.engine != nil {
			s.engine.RetireLatestRevision(s.cfg.AccountID, record.FunctionName, record.Metadata.Deployment.ID+":"+record.Metadata.InstanceID+":"+record.Version)
		}
		s.ensureFunctionMetrics(record.FunctionName, record.Metadata.InstanceID)
	})
	if err != nil {
		return nil, err
	}
	if s.logsSvc != nil {
		s.logsSvc.CreateLogGroup(fmt.Sprintf("/aws/lambda/%s", result.FunctionName))
		s.logsSvc.LogSystemEvent(logssvc.LevelINFO, fmt.Sprintf("Function created: %s (runtime: %s)", result.FunctionName, result.Runtime))
	}
	if s.engine != nil {
		config := cloneFunction(result)
		go func() {
			pullCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := s.ensureFunctionImage(pullCtx, config); err != nil {
				log.Printf("[lambda] warning: failed to prepare image for %s: %v", config.FunctionName, err)
			}
		}()
	}
	return result, nil
}

func (s *Service) promoteFunctionToActiveIfPending(name string) error {
	_, err := s.store.mutateFunction(name, func(record *functionRecord) error {
		if record.State == types.FunctionStatePending {
			record.State = types.FunctionStateActive
		}
		if record.LastUpdateStatus == "" || record.State == types.FunctionStateActive {
			record.LastUpdateStatus = types.LastUpdateStatusSuccessful
		}
		return nil
	})
	return err
}

func (s *Service) startGate(name string) *semaphore.Weighted {
	if v, ok := s.startGates.Load(name); ok {
		return v.(*semaphore.Weighted)
	}
	v, _ := s.startGates.LoadOrStore(name, semaphore.NewWeighted(1))
	return v.(*semaphore.Weighted)
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

// GetFunction retrieves a function by name (or ARN).
func (s *Service) GetFunction(reference string) (*types.FunctionConfig, error) {
	fn, err := s.store.GetFunction(reference)
	if err != nil {
		return nil, err
	}
	if fn.LastUpdateStatus == "" {
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
	}
	if fn.State == types.FunctionStatePending {
		if err := s.promoteFunctionToActiveIfPending(fn.FunctionName); err != nil {
			return nil, err
		}
	}
	return fn, nil
}

// ListFunctions returns all functions.
func (s *Service) ListFunctions() ([]*types.FunctionConfig, error) {
	return s.store.ListFunctions()
}

// DeleteFunction removes a function and its container.
func (s *Service) DeleteFunction(ctx context.Context, reference string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, qualifier, err := ParseFunctionReference(reference, "")
	if err != nil {
		return err
	}
	if qualifier != "" && qualifier != "$LATEST" {
		_, err = s.store.mutateFunction(name, func(record *functionRecord) error {
			if !numberedVersion(qualifier) || record.Metadata.Versions[qualifier] == nil {
				return fmt.Errorf("%w: version %s", ErrFunctionNotFound, qualifier)
			}
			for _, alias := range record.Metadata.Aliases {
				if alias.FunctionVersion == qualifier {
					return fmt.Errorf("%w: version is used by an alias", ErrResourceConflict)
				}
			}
			delete(record.Metadata.Versions, qualifier)
			record.afterCommit = func(*functionRecord) {
				if s.engine != nil {
					s.engine.RetireFunctionVersion(s.cfg.AccountID, name, qualifier)
				}
			}
			return nil
		})
		return err
	}
	if err := s.store.deleteFunction(name, func() {
		if s.engine != nil {
			s.engine.RetireFunction(s.cfg.AccountID, name)
		}
		s.deleteFunctionMetrics(name)
	}); err != nil {
		return err
	}
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
	lease, err := s.store.acquireDeployment(name, "")
	if err != nil {
		return nil
	}
	defer lease.release()
	extractDir := lease.CodeDir
	if extractDir == "" {
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
func (s *Service) UpdateFunctionCode(ctx context.Context, reference string, code []byte, architectures ...[]string) (*types.FunctionConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := mutableFunction(reference)
	if err != nil {
		return nil, err
	}
	if len(code) == 0 {
		return nil, invalid("function code is required")
	}
	result, err := s.store.mutateFunction(name, func(record *functionRecord) error {
		if record.PackageType == "Image" {
			return invalid("use ImageUri to update an image function")
		}
		if len(architectures) > 0 && architectures[0] != nil {
			if len(architectures[0]) != 1 {
				return invalid("Architectures must contain one architecture")
			}
			record.Architectures = append([]string{}, architectures[0]...)
		}
		if err := validateConfiguration(record.FunctionConfig); err != nil {
			return err
		}
		record.LastModified = time.Now()
		record.LastUpdateStatus = types.LastUpdateStatusSuccessful
		prepared, err := s.store.prepareDeploymentLocked(record.FunctionConfig, code, record.Metadata.Deployment)
		if err != nil {
			return err
		}
		record.Metadata.Deployment = prepared
		record.codeToMirror = append([]byte(nil), code...)
		return nil
	})
	if err == nil {
		s.retireLatestContainers(name)
	}
	return result, err
}

// TagResource adds or overwrites tags on a function (name or ARN).
func (s *Service) TagResource(reference string, tags map[string]string) error {
	name, err := bareFunction(reference)
	if err != nil {
		return err
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error {
		if record.Tags == nil {
			record.Tags = make(map[string]string)
		}
		for key, value := range tags {
			record.Tags[key] = value
		}
		return nil
	})
	return err
}

// UntagResource removes specified tag keys from a function (name or ARN).
func (s *Service) UntagResource(reference string, tagKeys []string) error {
	name, err := bareFunction(reference)
	if err != nil {
		return err
	}
	_, err = s.store.mutateFunction(name, func(record *functionRecord) error {
		for _, key := range tagKeys {
			delete(record.Tags, key)
		}
		return nil
	})
	return err
}

// --- Layer operations ---

// PublishLayerVersion publishes a new layer version.
func (s *Service) PublishLayerVersion(name, description string, runtimes []string, code []byte) (*types.LayerConfig, error) {
	return s.store.publishLayer(name, description, runtimes, code)
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
	if len(parts) != 8 || parts[0] != "arn" || parts[2] != "lambda" || parts[5] != "layer" {
		return "", 0, invalid("invalid layer ARN: " + arn)
	}
	name := parts[len(parts)-2]
	if err := validName(name, 140); err != nil {
		return "", 0, err
	}
	version, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || version < 1 {
		return "", 0, invalid("invalid layer version in ARN " + arn)
	}
	return name, version, nil
}

// UpdateFunctionConfiguration updates a function's configuration without changing its code.
func (s *Service) UpdateFunctionConfiguration(ctx context.Context, reference string, update *types.FunctionConfigUpdate) (*types.FunctionConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if update == nil {
		return nil, invalid("configuration update is required")
	}
	name, err := mutableFunction(reference)
	if err != nil {
		return nil, err
	}
	result, err := s.store.mutateFunction(name, func(record *functionRecord) error {
		fn := record.FunctionConfig
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
			fn.Environment = maps.Clone(update.Environment.Variables)
		}
		if update.Layers != nil {
			fn.Layers = append([]string{}, update.Layers...)
		}
		if update.DeadLetterConfig != nil {
			value := *update.DeadLetterConfig
			fn.DeadLetterConfig = &value
		}
		if update.KMSKeyArn != nil {
			fn.KMSKeyArn = *update.KMSKeyArn
		}
		if update.ImageConfig != nil {
			fn.ImageConfig = cloneFunction(&types.FunctionConfig{ImageConfig: update.ImageConfig}).ImageConfig
		}
		if err := validateConfiguration(fn); err != nil {
			return err
		}
		fn.LastModified = time.Now()
		fn.LastUpdateStatus = types.LastUpdateStatusSuccessful
		_, err := s.store.prepareCurrentLocked(record, false)
		return err
	})
	if err == nil {
		s.retireLatestContainers(name)
	}
	return result, err
}

// invocableFunction returns the function's configuration if it exists, is
// Active (promoting it from Pending) and an execution engine is configured.
func (s *Service) invocableFunction(name string) (*types.FunctionConfig, error) {
	fn, err := s.store.GetFunction(name)
	if err != nil {
		return nil, err
	}

	if fn.State == types.FunctionStatePending {
		if err := s.promoteFunctionToActiveIfPending(fn.FunctionName); err != nil {
			return nil, fmt.Errorf("failed to promote function %s to active: %w", fn.FunctionName, err)
		}
		fn, err = s.store.GetFunction(name)
		if err != nil {
			return nil, err
		}
	}

	if fn.State != types.FunctionStateActive {
		err := fmt.Errorf("function %s is not active (state: %s): %w", fn.FunctionName, fn.State, ErrFunctionNotReady)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, err.Error())
		return nil, err
	}
	if s.engine == nil || s.pool == nil {
		err := fmt.Errorf("lambda execution engine is not configured")
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, err.Error())
		return nil, err
	}
	return fn, nil
}

// Invoke executes a Lambda function and waits for the result. Callers that
// want AWS's asynchronous Event behaviour should use Enqueue.
func (s *Service) Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, invalid("invocation input is required")
	}
	if input.InvocationType != "" && input.InvocationType != "RequestResponse" && input.InvocationType != "Event" && input.InvocationType != "DryRun" {
		return nil, invalid("invalid invocation type")
	}
	if input.LogType != "" && input.LogType != "None" && input.LogType != "Tail" {
		return nil, invalid("invalid log type")
	}
	name, qualifier, err := ParseFunctionReference(input.FunctionName, input.Qualifier)
	if err != nil {
		return nil, err
	}
	reference := name
	if qualifier != "" {
		reference += ":" + qualifier
	}
	fn, err := s.invocableFunction(reference)
	if err != nil {
		return nil, err
	}
	if input.InvocationType == "DryRun" {
		return &types.InvokeOutput{StatusCode: 204, ExecutedVersion: fn.Version}, nil
	}
	if input.InvocationType == "Event" {
		queued := *input
		queued.FunctionName = reference
		queued.Qualifier = ""
		queued.InvocationType = "RequestResponse"
		queued.Payload = append([]byte(nil), input.Payload...)
		if err := s.Enqueue(reference, func(ctx context.Context) {
			if _, err := s.Invoke(ctx, &queued); err != nil {
				s.logFunctionRuntimeEvent(name, logssvc.LevelERROR, err.Error())
			}
		}); err != nil {
			return nil, err
		}
		return &types.InvokeOutput{StatusCode: 202, ExecutedVersion: fn.Version}, nil
	}
	if fn.PackageType == "Image" && fn.ResolvedImageURI == "" {
		if err := s.freezeLegacyImage(ctx, reference); err != nil {
			return nil, err
		}
	}
	lease, err := s.store.acquireDeployment(reference, "", func(lease *deploymentLease) { s.ensureFunctionMetrics(lease.Config.FunctionName, lease.InstanceID) })
	if err != nil {
		return nil, err
	}
	defer lease.release()
	fn = lease.Config
	if fn.State != types.FunctionStateActive {
		return nil, fmt.Errorf("%w: %s", ErrFunctionNotReady, fn.FunctionName)
	}
	localInput := *input
	localInput.FunctionName = fn.FunctionName
	input = &localInput
	codeDir, layerDirs := lease.CodeDir, lease.LayerDirs
	for _, arn := range lease.SkippedLayers {
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelWARN, "Skipping unavailable external layer "+arn+"; invocation continues without it")
	}
	info, coldStart, err := s.acquireContainer(ctx, fn, codeDir, layerDirs, lease.Revision)
	if err != nil {
		return nil, err
	}
	// Return the environment to the pool when this invocation finishes. The
	// closure reads info at return time because a dead warm container may be
	// replaced below.
	defer func() { s.engine.Release(info) }()

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
	if err != nil && !coldStart && isTransientRIEInvokeError(err) {
		// The warm container's runtime is gone (JVM crash, OOM kill, Docker
		// restart). Drop it from the pool so later invokes don't keep hitting
		// the dead port, then retry once on a fresh environment.
		output, err = s.recoverDeadWarmContainer(invokeCtx, &info, err, warmRecovery{
			remove: func(dead *engine.ContainerInfo) {
				s.ingestContainerLogs(fn.FunctionName, dead)
				_ = s.engine.RemoveContainer(context.Background(), dead.ID)
			},
			reacquire: func() (*engine.ContainerInfo, bool, error) {
				return s.acquireContainer(invokeCtx, fn, codeDir, layerDirs, lease.Revision)
			},
			invoke: func(next *engine.ContainerInfo, cold bool) (*types.InvokeOutput, error) {
				return s.invokeWithRetry(invokeCtx, next, input, cold)
			},
		})
	}
	if info != nil && shortID(info.ID) != logStream {
		logStream = shortID(info.ID)
		s.logInvocationBoundary(logGroup, logStream, fmt.Sprintf("START RequestId: %s Version: %s", requestID, fn.Version))
	}
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
	s.recordInvocationInstance(fn.FunctionName, lease.InstanceID, input.Payload)

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
func (s *Service) acquireContainer(ctx context.Context, fn *types.FunctionConfig, codeDir string, layerDirs []string, revision string) (*engine.ContainerInfo, bool, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, containerAcquireTimeout)
	defer cancel()
	gate := s.startGate(fn.FunctionName)
	queued, _ := ctx.Value(queuedInvocationKey{}).(bool)
	for {
		if err := acquireCtx.Err(); err != nil {
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
			return nil, false, fmt.Errorf("%w: acquisition timed out for %s", ErrTooManyRequests, fn.FunctionName)
		}
		var available <-chan struct{}
		if queued {
			available = s.engine.Availability(s.cfg.AccountID, fn.FunctionName)
		}
		limit, reserved, err := s.functionConcurrency(fn.FunctionName)
		if err != nil {
			return nil, false, err
		}
		if limit == 0 || (reserved && s.engine.BusyContainers(s.cfg.AccountID, fn.FunctionName) >= limit) {
			if queued {
				select {
				case <-acquireCtx.Done():
				case <-available:
				}
				continue
			}
			return nil, false, fmt.Errorf("%w: reserved concurrency limit reached for %s", ErrTooManyRequests, fn.FunctionName)
		}
		if info, ok := s.engine.AcquireIdleRevision(s.cfg.AccountID, fn.FunctionName, revision, limit); ok {
			return info, false, nil
		}
		if err := gate.Acquire(acquireCtx, 1); err != nil {
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
			return nil, false, fmt.Errorf("%w: acquisition timed out for %s", ErrTooManyRequests, fn.FunctionName)
		}
		limit, reserved, err = s.functionConcurrency(fn.FunctionName)
		if err != nil {
			gate.Release(1)
			return nil, false, err
		}
		if limit == 0 || (reserved && s.engine.BusyContainers(s.cfg.AccountID, fn.FunctionName) >= limit) {
			gate.Release(1)
			if queued {
				select {
				case <-acquireCtx.Done():
				case <-available:
				}
				continue
			}
			return nil, false, fmt.Errorf("%w: reserved concurrency limit reached for %s", ErrTooManyRequests, fn.FunctionName)
		}
		available = s.engine.Availability(s.cfg.AccountID, fn.FunctionName)
		if info, ok := s.engine.AcquireIdleRevision(s.cfg.AccountID, fn.FunctionName, revision, limit); ok {
			gate.Release(1)
			return info, false, nil
		}
		if s.engine.CountContainers(s.cfg.AccountID, fn.FunctionName) >= limit {
			s.engine.DiscardIdleRevision(s.cfg.AccountID, fn.FunctionName, revision)
		}
		if s.engine.CountContainers(s.cfg.AccountID, fn.FunctionName) < limit {
			info, err := s.coldStart(acquireCtx, fn, codeDir, layerDirs, revision)
			gate.Release(1)
			if err != nil {
				if ctx.Err() != nil {
					return nil, false, ctx.Err()
				}
				if acquireCtx.Err() != nil {
					return nil, false, fmt.Errorf("%w: acquisition timed out for %s", ErrTooManyRequests, fn.FunctionName)
				}
				return nil, false, err
			}
			return info, true, nil
		}
		gate.Release(1)
		select {
		case <-acquireCtx.Done():
		case <-available:
		}
	}
}

func (s *Service) functionConcurrency(name string) (int, bool, error) {
	value, err := s.GetFunctionConcurrency(name)
	if err != nil {
		return 0, false, err
	}
	limit := max(s.cfg.LambdaMaxConcurrency, 1)
	if value != nil {
		return min(limit, *value), true, nil
	}
	return limit, false, nil
}

// coldStart creates, starts and readies a new container for the function and
// returns it Busy (already counted in the pool). Caller must hold the
// per-function mutex.
func (s *Service) coldStart(ctx context.Context, fn *types.FunctionConfig, codeDir string, layerDirs []string, revision string) (*engine.ContainerInfo, error) {
	log.Printf("[lambda] cold start for %s", fn.FunctionName)
	if s.logsSvc != nil {
		s.logsSvc.LogSystemEvent(logssvc.LevelINFO, fmt.Sprintf("Cold start: %s", fn.FunctionName))
	}
	if err := s.ensureFunctionImage(ctx, fn); err != nil {
		return nil, fmt.Errorf("runtime image not available: %w", err)
	}

	id, _, _ := strings.Cut(revision, ":")
	release, err := s.store.retainDeployment(fn.FunctionName, id)
	if err != nil {
		return nil, err
	}
	info, err := s.engine.CreateContainer(ctx, fn, codeDir, layerDirs, s.cfg.AccountID, engine.LambdaContainerOptions{Revision: revision, OnRemove: release})
	if err != nil {
		release()
		wrapped := fmt.Errorf("failed to create container: %w", err)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
		return nil, wrapped
	}
	s.retireLatestContainers(fn.FunctionName)

	if err := s.engine.StartContainer(ctx, info); err != nil {
		s.engine.DiscardContainerAsync(info)
		wrapped := fmt.Errorf("failed to start container: %w", err)
		s.logFunctionRuntimeEvent(fn.FunctionName, logssvc.LevelERROR, wrapped.Error())
		return nil, wrapped
	}

	// Wait for the RIE to be ready. Capture startup logs before removing a failed
	// container (important for runtimes like Java/Spring Boot whose crash output
	// would otherwise be lost). Only this container is removed, not the pool.
	if err := s.invoker.WaitForReady(ctx, info.HostPort); err != nil {
		s.ingestContainerLogs(fn.FunctionName, info)
		s.engine.DiscardContainerAsync(info)
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
		output, err := s.invoker.Invoke(ctx, info.HostPort, input)
		newLogs := s.ingestContainerLogs(input.FunctionName, info)
		if err == nil {
			if input.LogType == "Tail" {
				output.LogResult = tailLogResult(newLogs)
			}
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

// warmRecovery holds the steps used to replace a dead warm container.
type warmRecovery struct {
	remove    func(dead *engine.ContainerInfo)
	reacquire func() (*engine.ContainerInfo, bool, error)
	invoke    func(info *engine.ContainerInfo, coldStart bool) (*types.InvokeOutput, error)
}

// recoverDeadWarmContainer removes the dead container *info, acquires a
// replacement (stored back into *info so the caller releases the right one)
// and retries the invocation once.
func (s *Service) recoverDeadWarmContainer(ctx context.Context, info **engine.ContainerInfo, cause error, r warmRecovery) (*types.InvokeOutput, error) {
	dead := *info
	log.Printf("[lambda] warm container %s for %s unreachable (%v); removing and retrying on a fresh environment", shortID(dead.ID), dead.FunctionName, cause)
	if s.logsSvc != nil {
		s.logsSvc.LogSystemEvent(logssvc.LevelWARN, fmt.Sprintf("Removed unreachable container for %s: %v", dead.FunctionName, cause))
	}
	r.remove(dead)
	// Nothing left to release for the removed container.
	*info = nil

	if err := ctx.Err(); err != nil {
		return nil, cause
	}
	next, coldStart, err := r.reacquire()
	if err != nil {
		return nil, fmt.Errorf("%w (replacement container failed: %v)", cause, err)
	}
	*info = next
	return r.invoke(next, coldStart)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
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

func (s *Service) ensureFunctionMetrics(name, instance string) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	if metric, exists := s.metrics[name]; !exists || metric.instance != instance {
		s.metrics[name] = &FunctionMetrics{instance: instance}
	}
}

func (s *Service) deleteFunctionMetrics(name string) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	delete(s.metrics, name)
}

func (s *Service) recordInvocationInstance(name, instance string, payload []byte) {
	processed := countProcessedMessages(payload)
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	metric, exists := s.metrics[name]
	if !exists || metric.instance != instance {
		return
	}

	metric.Invocations++
	metric.MessagesProcessed += processed
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

// ingestContainerLogs moves output the container wrote since the last ingest
// into the function's log group and returns it.
func (s *Service) ingestContainerLogs(functionName string, info *engine.ContainerInfo) string {
	if s.logsSvc == nil || s.engine == nil || info == nil {
		return ""
	}

	logCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	since := info.LogCursor()

	newLogs, last, err := s.engine.ContainerLogsSince(logCtx, info.ID, since)
	if err != nil || newLogs == "" {
		return ""
	}

	info.SetLogCursor(last)

	streamName := info.ID
	if len(streamName) > 12 {
		streamName = streamName[:12]
	}
	s.logsSvc.IngestContainerLogs(functionName, streamName, newLogs)
	return newLogs
}

// tailLogResult encodes an invocation's log output the way Invoke returns it
// for LogType Tail: the last 4 KB, base64 encoded.
func tailLogResult(logs string) string {
	const maxTailBytes = 4096
	if len(logs) > maxTailBytes {
		logs = logs[len(logs)-maxTailBytes:]
	}
	return base64.StdEncoding.EncodeToString([]byte(logs))
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
