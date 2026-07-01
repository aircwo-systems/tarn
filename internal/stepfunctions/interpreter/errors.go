// Package interpreter is the pure functional core of Tarn's Step Functions
// support: given a parsed ASL machine, an input value, and an injected task
// executor and clock, it computes an output and an event history with no I/O of
// its own. All side effects live in the service shell.
package interpreter

// StateError is an in-workflow failure. Name is an ASL error name that Retry and
// Catch match on (e.g. "States.TaskFailed"); Cause is a human-readable detail.
type StateError struct {
	Name  string
	Cause string
}

func (e *StateError) Error() string {
	if e.Cause == "" {
		return e.Name
	}
	return e.Name + ": " + e.Cause
}

// Predefined ASL error names. States.ALL is the wildcard matched by Retry/Catch.
const (
	ErrAll                    = "States.ALL"
	ErrTaskFailed             = "States.TaskFailed"
	ErrTimeout                = "States.Timeout"
	ErrRuntime                = "States.Runtime"
	ErrBranchFailed           = "States.BranchFailed"
	ErrNoChoiceMatched        = "States.NoChoiceMatched"
	ErrParameterPathFailure   = "States.ParameterPathFailure"
	ErrResultPathMatchFailure = "States.ResultPathMatchFailure"
	ErrIntrinsicFailure       = "States.IntrinsicFailure"

	// HTTP Task error names (arn:aws:states:::http:invoke).
	// ErrHTTPStatusCode is returned when the HTTP endpoint responds with a
	// non-2xx status code; the status + body excerpt appears in Cause.
	ErrHTTPStatusCode = "States.Http.StatusCodeFailure"
	// ErrHTTPSocket is returned on transport-level failures (connection refused,
	// timeout, DNS failure, etc.) that prevent any HTTP response from arriving.
	ErrHTTPSocket = "States.Http.SocketException"
)
