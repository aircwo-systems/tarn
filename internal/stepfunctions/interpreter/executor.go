package interpreter

import (
	"context"
	"encoding/json"
)

// TaskRequest carries the resource identifier and the post-Parameters payload
// for a single Task-state invocation.
type TaskRequest struct {
	Resource string          // ASL "Resource" ARN
	Payload  json.RawMessage // effective input after Parameters processing
}

// TaskResult is the outcome of one Task-state invocation. Output holds the raw
// JSON result; the remaining fields locate the observability data for the call
// (the Lambda log group/stream and request id) so the interpreter can record
// them on the history events for UI deep-linking. They are optional and may be
// empty for executors that don't produce logs.
type TaskResult struct {
	Output    json.RawMessage
	LogGroup  string
	LogStream string
	RequestID string
}

// TaskExecutor executes one Task state's resource and returns its result. The
// interpreter depends on this interface; the concrete Lambda-backed
// implementation lives in the service shell.
type TaskExecutor interface {
	RunTask(ctx context.Context, req TaskRequest) (TaskResult, error)
}
