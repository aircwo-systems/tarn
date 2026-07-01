// Package stepfunctions is the service shell around the pure interpreter core.
// It owns persistence, executes state machines asynchronously, adapts Tarn's
// Lambda service to the interpreter's TaskExecutor seam, and records traces.
package stepfunctions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/stepfunctions/asl"
	"github.com/aircwo-systems/tarn/internal/stepfunctions/interpreter"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// LambdaInterface is the slice of Tarn's Lambda service the Task executor needs.
type LambdaInterface interface {
	Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error)
}

// ServiceError is an AWS-shaped API failure.
type ServiceError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *ServiceError) Error() string { return e.Message }

// StatusCode returns the HTTP status for this error (defaults to 400).
func (e *ServiceError) StatusCode() int {
	if e == nil || e.HTTPStatus == 0 {
		return 400
	}
	return e.HTTPStatus
}

func validationError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "ValidationException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func invalidDefinitionError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "InvalidDefinition", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func internalError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "InternalException", Message: fmt.Sprintf(format, args...), HTTPStatus: 500}
}

func stateMachineNotFound(arn string) *ServiceError {
	return &ServiceError{Code: "StateMachineDoesNotExist", Message: "State Machine Does Not Exist: '" + arn + "'", HTTPStatus: 400}
}

func executionNotFound(arn string) *ServiceError {
	return &ServiceError{Code: "ExecutionDoesNotExist", Message: "Execution Does Not Exist: '" + arn + "'", HTTPStatus: 400}
}

func stateMachineExists(arn string) *ServiceError {
	return &ServiceError{Code: "StateMachineAlreadyExists", Message: "State Machine Already Exists: '" + arn + "'", HTTPStatus: 400}
}

func executionExists(arn string) *ServiceError {
	return &ServiceError{Code: "ExecutionAlreadyExists", Message: "Execution Already Exists: '" + arn + "'", HTTPStatus: 400}
}

// runHandle tracks an in-flight execution so it can be cancelled (StopExecution)
// and so GetExecutionHistory can read live events before the final store write.
type runHandle struct {
	mu     sync.Mutex
	events []types.HistoryEvent
	cancel context.CancelFunc
}

// Service manages state machines and their executions.
type Service struct {
	cfg        *config.Config
	store      *Store
	lambda     LambdaInterface
	traceStore *tracesvc.Store

	mu      sync.Mutex
	running map[string]*runHandle // key: execution ARN
	wg      sync.WaitGroup
}

// NewService creates a Service. A nil store is replaced with a fresh one.
func NewService(cfg *config.Config, store *Store, lambda LambdaInterface) *Service {
	if store == nil {
		store = NewStore(cfg)
	}
	return &Service{
		cfg:     cfg,
		store:   store,
		lambda:  lambda,
		running: make(map[string]*runHandle),
	}
}

// SetTraceStore wires the trace store used to record executions.
func (s *Service) SetTraceStore(ts *tracesvc.Store) { s.traceStore = ts }

// Init restores persisted state.
func (s *Service) Init() error { return s.store.Init() }

// Start is a no-op: the service has no background workers (executions run as
// per-call goroutines). It exists for lifecycle symmetry with other services.
func (s *Service) Start() {}

// Stop cancels every in-flight execution and waits for them to finish.
func (s *Service) Stop() {
	s.mu.Lock()
	for _, h := range s.running {
		h.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// ---- Control plane ----

// CreateStateMachine validates the ASL definition and stores a new machine.
func (s *Service) CreateStateMachine(name, definition, roleArn, smType string, tags map[string]string) (*types.StateMachine, error) {
	if strings.TrimSpace(name) == "" {
		return nil, validationError("state machine name is required")
	}
	if smType == "" {
		smType = types.StateMachineTypeStandard
	}
	if smType != types.StateMachineTypeStandard {
		return nil, validationError("only STANDARD state machines are supported (got %q)", smType)
	}
	if err := s.ValidateStateMachineDefinition(definition); err != nil {
		return nil, err
	}
	arn := s.stateMachineARN(name)
	if _, err := s.store.GetStateMachine(arn); err == nil {
		return nil, stateMachineExists(arn)
	}
	sm := &types.StateMachine{
		Name:       name,
		Arn:        arn,
		Definition: definition,
		RoleArn:    roleArn,
		Type:       smType,
		Status:     types.StateMachineStatusActive,
		Tags:       tags,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.store.SaveStateMachine(sm); err != nil {
		return nil, internalError("save state machine: %v", err)
	}
	return sm, nil
}

// DescribeStateMachine returns the machine with the given ARN.
func (s *Service) DescribeStateMachine(arn string) (*types.StateMachine, error) {
	sm, err := s.store.GetStateMachine(arn)
	if err != nil {
		return nil, stateMachineNotFound(arn)
	}
	return sm, nil
}

// ValidateStateMachineDefinition checks whether a definition is valid without storing a state machine.
func (s *Service) ValidateStateMachineDefinition(definition string) error {
	if strings.TrimSpace(definition) == "" {
		return invalidDefinitionError("state machine definition is required")
	}
	if _, err := asl.Parse(definition); err != nil {
		return invalidDefinitionError("%s", err.Error())
	}
	return nil
}

// UpdateStateMachine replaces the definition and/or role of an existing machine.
func (s *Service) UpdateStateMachine(arn, definition, roleArn string) (*types.StateMachine, error) {
	sm, err := s.store.GetStateMachine(arn)
	if err != nil {
		return nil, stateMachineNotFound(arn)
	}
	if definition != "" {
		if _, perr := asl.Parse(definition); perr != nil {
			return nil, invalidDefinitionError("%s", perr.Error())
		}
		sm.Definition = definition
	}
	if roleArn != "" {
		sm.RoleArn = roleArn
	}
	if err := s.store.SaveStateMachine(sm); err != nil {
		return nil, internalError("update state machine: %v", err)
	}
	return sm, nil
}

// DeleteStateMachine removes a machine. Deleting a missing machine is not an
// error, matching AWS's delete semantics.
func (s *Service) DeleteStateMachine(arn string) error {
	_ = s.store.DeleteStateMachine(arn)
	return nil
}

// ListStateMachines returns all machines.
func (s *Service) ListStateMachines() []*types.StateMachine {
	return s.store.ListStateMachines()
}

// TagResource adds or overwrites tags on a machine.
func (s *Service) TagResource(arn string, tags map[string]string) error {
	sm, err := s.store.GetStateMachine(arn)
	if err != nil {
		return stateMachineNotFound(arn)
	}
	if sm.Tags == nil {
		sm.Tags = make(map[string]string, len(tags))
	}
	for k, v := range tags {
		sm.Tags[k] = v
	}
	return s.store.SaveStateMachine(sm)
}

// UntagResource removes tags from a machine.
func (s *Service) UntagResource(arn string, keys []string) error {
	sm, err := s.store.GetStateMachine(arn)
	if err != nil {
		return stateMachineNotFound(arn)
	}
	for _, k := range keys {
		delete(sm.Tags, k)
	}
	return s.store.SaveStateMachine(sm)
}

// ListTagsForResource returns a machine's tags.
func (s *Service) ListTagsForResource(arn string) (map[string]string, error) {
	sm, err := s.store.GetStateMachine(arn)
	if err != nil {
		return nil, stateMachineNotFound(arn)
	}
	return sm.Tags, nil
}

// ---- Execution plane ----

// StartExecution records a RUNNING execution and runs the interpreter in a
// background goroutine. It returns immediately, mirroring AWS semantics.
func (s *Service) StartExecution(stateMachineArn, name, input string) (*types.Execution, error) {
	sm, err := s.store.GetStateMachine(stateMachineArn)
	if err != nil {
		return nil, stateMachineNotFound(stateMachineArn)
	}
	machine, perr := asl.Parse(sm.Definition)
	if perr != nil {
		return nil, invalidDefinitionError("%s", perr.Error())
	}
	if strings.TrimSpace(name) == "" {
		name = uuid.NewString()
	}
	if strings.TrimSpace(input) == "" {
		input = "{}"
	}
	if !json.Valid([]byte(input)) {
		return nil, &ServiceError{Code: "InvalidExecutionInput", Message: "execution input is not valid JSON", HTTPStatus: 400}
	}
	execArn := s.executionARN(sm.Name, name)
	if _, err := s.store.GetExecution(execArn); err == nil {
		return nil, executionExists(execArn)
	}
	exec := &types.Execution{
		Arn:             execArn,
		Name:            name,
		StateMachineArn: stateMachineArn,
		Status:          types.ExecutionStatusRunning,
		Input:           input,
		StartDate:       time.Now().UTC(),
	}
	if err := s.store.SaveExecution(exec); err != nil {
		return nil, internalError("save execution: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	h := &runHandle{cancel: cancel}
	s.mu.Lock()
	s.running[execArn] = h
	s.mu.Unlock()

	s.wg.Add(1)
	go s.runExecution(ctx, h, execArn, sm, machine, input)

	return cloneExecution(exec), nil
}

// runExecution drives one execution to a terminal state and persists the result.
func (s *Service) runExecution(ctx context.Context, h *runHandle, execArn string, sm *types.StateMachine, machine *asl.Machine, input string) {
	defer s.wg.Done()

	start := time.Now()
	var correlationID, traceID string
	if s.traceStore != nil {
		correlationID = tracesvc.NewCorrelationID()
		traceID = uuid.NewString()[:8]
	}

	run := interpreter.Run{
		Machine:  machine,
		Input:    json.RawMessage(input),
		Executor: &taskExecutor{cfg: s.cfg, lambda: s.lambda},
		Clock:    interpreter.SystemClock,
		Emit: func(ev types.HistoryEvent) {
			h.mu.Lock()
			h.events = append(h.events, ev)
			h.mu.Unlock()
		},
	}
	output, runErr := run.Execute(ctx)

	h.mu.Lock()
	events := append([]types.HistoryEvent(nil), h.events...)
	h.mu.Unlock()

	exec, err := s.store.GetExecution(execArn)
	if err != nil {
		s.clearRunning(execArn)
		return
	}
	now := time.Now().UTC()
	exec.StopDate = &now
	exec.History = events
	exec.TraceID = traceID
	applyTerminalStatus(exec, output, runErr)
	_ = s.store.SaveExecution(exec)

	s.clearRunning(execArn)

	if s.traceStore != nil {
		s.emitTrace(traceID, correlationID, sm, exec, start, events)
	}
}

func (s *Service) clearRunning(execArn string) {
	s.mu.Lock()
	delete(s.running, execArn)
	s.mu.Unlock()
}

// applyTerminalStatus maps the interpreter's result onto an execution's terminal
// fields.
func applyTerminalStatus(exec *types.Execution, output json.RawMessage, runErr error) {
	switch {
	case runErr == nil:
		exec.Status = types.ExecutionStatusSucceeded
		exec.Output = string(output)
		return
	case errors.Is(runErr, interpreter.ErrAborted):
		exec.Status = types.ExecutionStatusAborted
		return
	}
	var se *interpreter.StateError
	if errors.As(runErr, &se) {
		exec.Error = se.Name
		exec.Cause = se.Cause
		if se.Name == interpreter.ErrTimeout {
			exec.Status = types.ExecutionStatusTimedOut
			return
		}
	}
	exec.Status = types.ExecutionStatusFailed
}

// DescribeExecution returns the execution with the given ARN.
func (s *Service) DescribeExecution(arn string) (*types.Execution, error) {
	ex, err := s.store.GetExecution(arn)
	if err != nil {
		return nil, executionNotFound(arn)
	}
	return ex, nil
}

// StopExecution cancels a running execution. It is a no-op for executions that
// have already finished.
func (s *Service) StopExecution(arn string) (*types.Execution, error) {
	ex, err := s.store.GetExecution(arn)
	if err != nil {
		return nil, executionNotFound(arn)
	}
	s.mu.Lock()
	h, running := s.running[arn]
	s.mu.Unlock()
	if running {
		h.cancel()
	}
	return ex, nil
}

// ListExecutions returns executions, optionally filtered by state machine ARN
// and/or status.
func (s *Service) ListExecutions(stateMachineArn, statusFilter string) ([]*types.Execution, error) {
	all := s.store.ListExecutions()
	out := make([]*types.Execution, 0, len(all))
	for _, ex := range all {
		if stateMachineArn != "" && ex.StateMachineArn != stateMachineArn {
			continue
		}
		if statusFilter != "" && ex.Status != statusFilter {
			continue
		}
		out = append(out, ex)
	}
	return out, nil
}

// GetExecutionHistory returns an execution's events. For a running execution the
// live (in-progress) events are returned.
func (s *Service) GetExecutionHistory(arn string) ([]types.HistoryEvent, error) {
	s.mu.Lock()
	h, running := s.running[arn]
	s.mu.Unlock()
	if running {
		h.mu.Lock()
		events := append([]types.HistoryEvent(nil), h.events...)
		h.mu.Unlock()
		return events, nil
	}
	ex, err := s.store.GetExecution(arn)
	if err != nil {
		return nil, executionNotFound(arn)
	}
	return ex.History, nil
}

// ---- ARNs ----

func (s *Service) stateMachineARN(name string) string {
	return fmt.Sprintf("arn:aws:states:%s:%s:stateMachine:%s", s.cfg.Region, s.cfg.AccountID, name)
}

func (s *Service) executionARN(machineName, execName string) string {
	return fmt.Sprintf("arn:aws:states:%s:%s:execution:%s:%s", s.cfg.Region, s.cfg.AccountID, machineName, execName)
}

// ---- Trace emission ----

func (s *Service) emitTrace(traceID, correlationID string, sm *types.StateMachine, exec *types.Execution, start time.Time, events []types.HistoryEvent) {
	if traceID == "" {
		traceID = uuid.NewString()[:8]
	}
	status := 200
	rootStatus := "ok"
	if exec.Status != types.ExecutionStatusSucceeded {
		status = 500
		rootStatus = "error"
	}
	spans := []tracesvc.Span{{
		Kind:       "stepfunctions",
		Name:       sm.Name,
		DurationMs: time.Since(start).Milliseconds(),
		Status:     rootStatus,
		Meta:       map[string]string{"execution": exec.Name, "status": exec.Status},
	}}
	for _, ev := range events {
		if !strings.HasSuffix(ev.Type, "StateEntered") {
			continue
		}
		name, _ := ev.Details["name"].(string)
		spans = append(spans, tracesvc.Span{
			Kind:   "stepfunctions",
			Name:   name,
			Status: "ok",
			Meta:   map[string]string{"event": ev.Type},
		})
	}
	s.traceStore.Add(&tracesvc.Trace{
		ID:            traceID,
		CorrelationID: correlationID,
		StartedAt:     start,
		DurationMs:    time.Since(start).Milliseconds(),
		Status:        status,
		Method:        "STEPFUNCTIONS",
		Path:          "/execution/" + exec.Name,
		Spans:         spans,
	})
}

// ---- Task executor (routing) ----

// taskExecutor routes Task-state invocations to the correct integration.
// It is the dependency-inversion boundary between the interpreter and Tarn's
// concrete services. Currently supports:
//   - bare Lambda ARN / :::lambda:invoke → runLambda
//   - arn:aws:states:::http:invoke       → runHTTP
type taskExecutor struct {
	cfg    *config.Config
	lambda LambdaInterface
}

// httpInvokeResource is the AWS optimised-integration ARN for HTTP tasks.
const httpInvokeResource = "arn:aws:states:::http:invoke"

// httpBodyCap is the maximum number of bytes read from an HTTP response body.
// Mirrors AWS's 256 KB effective state limit.
const httpBodyCap = 256 * 1024

// httpDefaultTimeout is the per-request timeout used when the Task does not
// specify TimeoutSeconds.
const httpDefaultTimeout = 10 * time.Second

func (e *taskExecutor) RunTask(ctx context.Context, req interpreter.TaskRequest) (interpreter.TaskResult, error) {
	switch {
	case isLambdaResource(req.Resource):
		return e.runLambda(ctx, req)
	case req.Resource == httpInvokeResource:
		return e.runHTTP(ctx, req)
	default:
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrTaskFailed,
			Cause: "unsupported Task resource: " + req.Resource,
		}
	}
}

// isLambdaResource returns true for bare Lambda function ARNs and the
// :::lambda:invoke optimised-integration ARN.
func isLambdaResource(resource string) bool {
	if strings.Contains(resource, ":lambda:invoke") {
		return true
	}
	if strings.HasPrefix(resource, "arn:aws:lambda:") {
		return true
	}
	// Bare function name (no arn: prefix at all)
	if !strings.HasPrefix(resource, "arn:") {
		return true
	}
	return false
}

// runLambda handles bare Lambda ARN and :::lambda:invoke resources.
func (e *taskExecutor) runLambda(ctx context.Context, req interpreter.TaskRequest) (interpreter.TaskResult, error) {
	fn, payload, err := resolveLambdaTarget(req)
	if err != nil {
		return interpreter.TaskResult{}, err
	}
	if e.lambda == nil {
		return interpreter.TaskResult{}, &interpreter.StateError{Name: interpreter.ErrTaskFailed, Cause: "lambda service unavailable"}
	}
	out, ierr := e.lambda.Invoke(ctx, &types.InvokeInput{
		FunctionName:   fn,
		Payload:        payload,
		InvocationType: "RequestResponse",
	})
	if ierr != nil {
		return interpreter.TaskResult{}, &interpreter.StateError{Name: interpreter.ErrTaskFailed, Cause: ierr.Error()}
	}
	// Carry log refs regardless of whether the function errored.
	result := interpreter.TaskResult{
		Output:    out.Payload,
		LogGroup:  out.LogGroup,
		LogStream: out.LogStream,
		RequestID: out.RequestID,
	}
	if out.FunctionError != "" {
		cause := out.FunctionError
		if len(out.Payload) > 0 {
			cause = string(out.Payload)
		}
		return result, &interpreter.StateError{Name: interpreter.ErrTaskFailed, Cause: cause}
	}
	return result, nil
}

// httpParams is the AWS http:invoke Parameters shape.
type httpParams struct {
	ApiEndpoint     string            `json:"ApiEndpoint"`
	Method          string            `json:"Method"`
	Headers         map[string]string `json:"Headers"`
	QueryParameters map[string]string `json:"QueryParameters"`
	RequestBody     json.RawMessage   `json:"RequestBody"`
	// Authentication is accepted but not validated; Tarn uses its own account
	// credential mechanism for Tarn-hosted endpoints (see D in the design doc).
	Authentication map[string]any `json:"Authentication"`
}

// httpTaskResult is the AWS http:invoke result shape.
type httpTaskResult struct {
	StatusCode   int               `json:"StatusCode"`
	Headers      map[string]string `json:"Headers"`
	ResponseBody any               `json:"ResponseBody"`
}

// runHTTP executes an arn:aws:states:::http:invoke Task.
//
// SSRF guard: only loopback addresses and the configured Tarn endpoint host are
// allowed as targets. All other hosts are rejected with a clear StateError so an
// ASL definition cannot be used to exfiltrate data from the server process.
//
// Multi-account: when the target host is a Tarn-internal surface, the executing
// account's SigV4 credential header is attached so multi-account routing resolves
// the correct namespace — matching the pattern used by the worker Lambdas today.
func (e *taskExecutor) runHTTP(ctx context.Context, req interpreter.TaskRequest) (interpreter.TaskResult, error) {
	var p httpParams
	if err := json.Unmarshal(req.Payload, &p); err != nil {
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrTaskFailed,
			Cause: "http:invoke: invalid Parameters: " + err.Error(),
		}
	}
	if p.ApiEndpoint == "" {
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrTaskFailed,
			Cause: "http:invoke: ApiEndpoint is required",
		}
	}
	if p.Method == "" {
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrTaskFailed,
			Cause: "http:invoke: Method is required",
		}
	}

	// Parse and validate the target URL.
	target, parseErr := url.Parse(p.ApiEndpoint)
	if parseErr != nil || target.Host == "" {
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrHTTPStatusCode,
			Cause: "http:invoke: invalid ApiEndpoint: " + p.ApiEndpoint,
		}
	}

	// SSRF guard: only allow loopback or the Tarn endpoint host.
	if !e.isTarnHost(target.Host) {
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrTaskFailed,
			Cause: "http:invoke: ApiEndpoint host is not an allowed Tarn endpoint: " + target.Host,
		}
	}

	// Append query parameters.
	if len(p.QueryParameters) > 0 {
		q := target.Query()
		for k, v := range p.QueryParameters {
			q.Set(k, v)
		}
		target.RawQuery = q.Encode()
	}

	// Build request body for methods that carry a body.
	method := strings.ToUpper(p.Method)
	var bodyReader io.Reader
	if method != http.MethodGet && method != http.MethodHead && len(p.RequestBody) > 0 {
		bodyReader = strings.NewReader(string(p.RequestBody))
	}

	// Apply context timeout.
	reqCtx, cancel := context.WithTimeout(ctx, httpDefaultTimeout)
	defer cancel()

	httpReq, newErr := http.NewRequestWithContext(reqCtx, method, target.String(), bodyReader)
	if newErr != nil {
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrHTTPSocket,
			Cause: "http:invoke: could not build request: " + newErr.Error(),
		}
	}

	// Set caller-supplied headers.
	for k, v := range p.Headers {
		httpReq.Header.Set(k, v)
	}

	// For requests with a JSON body, ensure Content-Type is set.
	if bodyReader != nil && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Multi-account: attach the executing account's SigV4 Credential header for
	// Tarn-hosted surfaces. This mirrors accountAuthHeader() in the worker Lambdas.
	if e.cfg != nil && e.cfg.AccountID != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf(
			"AWS4-HMAC-SHA256 Credential=%s/20000101/us-east-1/tarn/aws4_request, SignedHeaders=host, Signature=0",
			e.cfg.AccountID,
		))
	}

	resp, doErr := http.DefaultClient.Do(httpReq)
	if doErr != nil {
		cause := doErr.Error()
		if reqCtx.Err() != nil {
			cause = "request timed out: " + cause
		}
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrHTTPSocket,
			Cause: "http:invoke: " + cause,
		}
	}
	defer resp.Body.Close()

	// Read the response body, capped at httpBodyCap.
	limitedBody, readErr := io.ReadAll(io.LimitReader(resp.Body, httpBodyCap))
	if readErr != nil {
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrHTTPSocket,
			Cause: "http:invoke: reading response body: " + readErr.Error(),
		}
	}

	// Collect response headers (first value per key, matching AWS behaviour).
	respHeaders := make(map[string]string, len(resp.Header))
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	// Parse ResponseBody: JSON when content-type signals JSON, else plain string.
	var responseBody any
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") || strings.Contains(ct, "+json") {
		if err := json.Unmarshal(limitedBody, &responseBody); err != nil {
			// Fallback to string if the body isn't valid JSON despite the content-type.
			responseBody = string(limitedBody)
		}
	} else {
		responseBody = string(limitedBody)
	}

	taskRes := httpTaskResult{
		StatusCode:   resp.StatusCode,
		Headers:      respHeaders,
		ResponseBody: responseBody,
	}
	outJSON, marshalErr := json.Marshal(taskRes)
	if marshalErr != nil {
		return interpreter.TaskResult{}, &interpreter.StateError{
			Name:  interpreter.ErrRuntime,
			Cause: "http:invoke: cannot marshal result: " + marshalErr.Error(),
		}
	}

	// Non-2xx → States.Http.StatusCodeFailure (retryable, catchable).
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt := string(limitedBody)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200]
		}
		return interpreter.TaskResult{Output: outJSON}, &interpreter.StateError{
			Name:  interpreter.ErrHTTPStatusCode,
			Cause: fmt.Sprintf("%d %s", resp.StatusCode, excerpt),
		}
	}

	return interpreter.TaskResult{Output: outJSON}, nil
}

// isTarnHost returns true when host (host:port or bare host) is a loopback
// address or the configured Tarn endpoint host, which are the only outbound
// targets the HTTP Task executor is allowed to call.
func (e *taskExecutor) isTarnHost(host string) bool {
	// Strip the port if present.
	bare := host
	if i := strings.LastIndex(host, ":"); i >= 0 {
		bare = host[:i]
	}

	// Always allow loopback.
	switch bare {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}

	// Allow the Tarn-configured endpoint host.
	if e.cfg != nil {
		tarnURL, err := url.Parse(e.cfg.Endpoint())
		if err == nil {
			tarnHost := tarnURL.Hostname()
			if tarnHost != "" && bare == tarnHost {
				return true
			}
		}
	}

	return false
}

// resolveLambdaTarget supports two MVP forms: a bare Lambda function ARN as the
// Resource (payload = the effective input), and the optimized integration
// "arn:aws:states:::lambda:invoke" where the effective input carries
// FunctionName and Payload.
func resolveLambdaTarget(req interpreter.TaskRequest) (string, []byte, error) {
	res := req.Resource
	if strings.HasPrefix(res, "arn:aws:states:") && strings.Contains(res, ":lambda:invoke") {
		var p struct {
			FunctionName string          `json:"FunctionName"`
			Payload      json.RawMessage `json:"Payload"`
		}
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return "", nil, &interpreter.StateError{Name: interpreter.ErrTaskFailed, Cause: "invalid lambda:invoke parameters: " + err.Error()}
		}
		name := lambdaNameFromARNOrName(p.FunctionName)
		if name == "" {
			return "", nil, &interpreter.StateError{Name: interpreter.ErrTaskFailed, Cause: "lambda:invoke requires a FunctionName"}
		}
		payload := []byte(p.Payload)
		if len(payload) == 0 {
			payload = []byte("{}")
		}
		return name, payload, nil
	}

	name := lambdaNameFromARNOrName(res)
	if name == "" {
		return "", nil, &interpreter.StateError{Name: interpreter.ErrTaskFailed, Cause: "unsupported Task resource: " + res}
	}
	return name, req.Payload, nil
}

// lambdaNameFromARNOrName extracts a function name from a Lambda function ARN or
// returns a bare name unchanged. Unsupported ARNs yield "".
func lambdaNameFromARNOrName(s string) string {
	if i := strings.LastIndex(s, ":function:"); i >= 0 {
		name := s[i+len(":function:"):]
		if j := strings.IndexByte(name, ':'); j >= 0 { // strip :version or :alias
			name = name[:j]
		}
		return name
	}
	if strings.HasPrefix(s, "arn:") {
		return ""
	}
	return s
}
