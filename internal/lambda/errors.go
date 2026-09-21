package lambda

import "errors"

// Sentinel errors let the API layer map service failures onto the AWS error
// codes and HTTP statuses SDKs expect. Wrap them with %w to keep context.
var (
	// ErrFunctionNotFound is returned when the named function does not exist.
	ErrFunctionNotFound = errors.New("function not found")
	// ErrFunctionNotReady is returned when a function exists but is not Active.
	ErrFunctionNotReady = errors.New("function not ready")
	// ErrTooManyRequests is returned when the concurrency cap is exhausted.
	ErrTooManyRequests = errors.New("too many requests")
)
