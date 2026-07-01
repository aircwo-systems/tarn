package types

import "time"

// Step Functions state-machine kinds and lifecycle statuses.
const (
	StateMachineTypeStandard = "STANDARD"
	StateMachineTypeExpress  = "EXPRESS"

	StateMachineStatusActive   = "ACTIVE"
	StateMachineStatusDeleting = "DELETING"
)

// Execution lifecycle statuses.
const (
	ExecutionStatusRunning   = "RUNNING"
	ExecutionStatusSucceeded = "SUCCEEDED"
	ExecutionStatusFailed    = "FAILED"
	ExecutionStatusAborted   = "ABORTED"
	ExecutionStatusTimedOut  = "TIMED_OUT"
)

// StateMachine models a Step Functions state machine. Definition holds the raw
// Amazon States Language (ASL) JSON exactly as supplied by the caller.
type StateMachine struct {
	Name       string            `json:"Name"`
	Arn        string            `json:"Arn"`
	Definition string            `json:"Definition"`
	RoleArn    string            `json:"RoleArn,omitempty"`
	Type       string            `json:"Type"`
	Status     string            `json:"Status"`
	Tags       map[string]string `json:"Tags,omitempty"`
	CreatedAt  time.Time         `json:"CreatedAt"`
}

// Execution models a single run of a state machine. Output, Error and Cause are
// only populated once the execution reaches a terminal status.
type Execution struct {
	Arn             string         `json:"Arn"`
	Name            string         `json:"Name"`
	StateMachineArn string         `json:"StateMachineArn"`
	Status          string         `json:"Status"`
	Input           string         `json:"Input"`
	Output          string         `json:"Output,omitempty"`
	Error           string         `json:"Error,omitempty"`
	Cause           string         `json:"Cause,omitempty"`
	StartDate       time.Time      `json:"StartDate"`
	StopDate        *time.Time     `json:"StopDate,omitempty"`
	History         []HistoryEvent `json:"History,omitempty"`
	// TraceID links this execution to the X-Ray trace emitted for it (see the
	// Step Functions service's trace emission). Empty when tracing is disabled.
	TraceID string `json:"TraceID,omitempty"`
}

// HistoryEvent is one entry in an execution's ordered event history, surfaced by
// GetExecutionHistory. ID increases monotonically within an execution and
// PreviousID links to the prior event (0 for the first).
type HistoryEvent struct {
	ID         int64          `json:"id"`
	PreviousID int64          `json:"previousEventId"`
	Type       string         `json:"type"`
	Timestamp  time.Time      `json:"timestamp"`
	Details    map[string]any `json:"details,omitempty"`
}
