// Package asl parses and validates Amazon States Language (ASL) definitions into
// a typed, already-validated model. Parsing is intentionally separate from
// interpretation: the interpreter consumes a *Machine it can trust and never
// re-checks structure.
package asl

import (
	"encoding/json"
	"fmt"
)

// StateType enumerates the supported ASL state types.
type StateType string

// Supported state types.
const (
	TypePass     StateType = "Pass"
	TypeTask     StateType = "Task"
	TypeChoice   StateType = "Choice"
	TypeWait     StateType = "Wait"
	TypeSucceed  StateType = "Succeed"
	TypeFail     StateType = "Fail"
	TypeParallel StateType = "Parallel"
	TypeMap      StateType = "Map"
)

// Machine is a parsed ASL state machine (top level or a nested branch/iterator).
type Machine struct {
	StartAt        string
	States         map[string]State
	TimeoutSeconds int
	Comment        string
}

// State is one ASL state. The single discriminator method keeps the interface
// minimal; the interpreter type-switches on the concrete types below.
type State interface {
	StateType() StateType
}

// Path represents an optional ASL path field (InputPath, OutputPath, ResultPath).
// It distinguishes three cases that ASL treats differently: absent (use the
// default), explicit JSON null (discard), and a concrete reference path.
type Path struct {
	Set   bool   // key was present in the JSON
	Null  bool   // key was present and explicitly null
	Value string // reference path, when Set && !Null
}

// UnmarshalJSON records presence and null-ness so the interpreter can honour the
// difference between an absent path and an explicit null.
func (p *Path) UnmarshalJSON(b []byte) error {
	p.Set = true
	if string(b) == "null" {
		p.Null = true
		return nil
	}
	return json.Unmarshal(b, &p.Value)
}

// Or returns the configured path, def when the field was absent, or "" when the
// field was explicitly null (meaning "discard" in ASL I/O processing).
func (p Path) Or(def string) string {
	switch {
	case !p.Set:
		return def
	case p.Null:
		return ""
	default:
		return p.Value
	}
}

// Retrier models one Retry entry on a Task/Parallel/Map state.
type Retrier struct {
	ErrorEquals     []string `json:"ErrorEquals"`
	IntervalSeconds int      `json:"IntervalSeconds"`
	MaxAttempts     *int     `json:"MaxAttempts"`
	BackoffRate     float64  `json:"BackoffRate"`
}

// Catcher models one Catch entry on a Task/Parallel/Map state.
type Catcher struct {
	ErrorEquals []string `json:"ErrorEquals"`
	ResultPath  Path     `json:"ResultPath"`
	Next        string   `json:"Next"`
}

// PassState injects a fixed Result or transformed input and moves on.
type PassState struct {
	Next       string          `json:"Next"`
	End        bool            `json:"End"`
	InputPath  Path            `json:"InputPath"`
	OutputPath Path            `json:"OutputPath"`
	Parameters json.RawMessage `json:"Parameters"`
	Result     json.RawMessage `json:"Result"`
	ResultPath Path            `json:"ResultPath"`
	Comment    string          `json:"Comment"`
}

// StateType implements State.
func (PassState) StateType() StateType { return TypePass }

// TaskState invokes a resource (Lambda in MVP) and applies result processing,
// retry and catch handling.
type TaskState struct {
	Resource       string          `json:"Resource"`
	Next           string          `json:"Next"`
	End            bool            `json:"End"`
	InputPath      Path            `json:"InputPath"`
	OutputPath     Path            `json:"OutputPath"`
	Parameters     json.RawMessage `json:"Parameters"`
	ResultSelector json.RawMessage `json:"ResultSelector"`
	ResultPath     Path            `json:"ResultPath"`
	Retry          []Retrier       `json:"Retry"`
	Catch          []Catcher       `json:"Catch"`
	TimeoutSeconds int             `json:"TimeoutSeconds"`
	Comment        string          `json:"Comment"`
}

// StateType implements State.
func (TaskState) StateType() StateType { return TypeTask }

// ChoiceState branches to the first matching rule, or to Default.
type ChoiceState struct {
	InputPath  Path         `json:"InputPath"`
	OutputPath Path         `json:"OutputPath"`
	Choices    []ChoiceRule `json:"Choices"`
	Default    string       `json:"Default"`
	Comment    string       `json:"Comment"`
}

// StateType implements State.
func (ChoiceState) StateType() StateType { return TypeChoice }

// ChoiceRule is a single (possibly nested) Choice condition. Comparison operators
// are pointers so an absent operator is distinguishable from a zero value. Next
// is only meaningful on the top-level rules of a Choice state.
type ChoiceRule struct {
	Variable string `json:"Variable"`
	Next     string `json:"Next"`

	StringEquals            *string `json:"StringEquals"`
	StringLessThan          *string `json:"StringLessThan"`
	StringGreaterThan       *string `json:"StringGreaterThan"`
	StringLessThanEquals    *string `json:"StringLessThanEquals"`
	StringGreaterThanEquals *string `json:"StringGreaterThanEquals"`

	StringEqualsPath            *string `json:"StringEqualsPath"`
	StringLessThanPath          *string `json:"StringLessThanPath"`
	StringGreaterThanPath       *string `json:"StringGreaterThanPath"`
	StringLessThanEqualsPath    *string `json:"StringLessThanEqualsPath"`
	StringGreaterThanEqualsPath *string `json:"StringGreaterThanEqualsPath"`

	NumericEquals            *float64 `json:"NumericEquals"`
	NumericLessThan          *float64 `json:"NumericLessThan"`
	NumericGreaterThan       *float64 `json:"NumericGreaterThan"`
	NumericLessThanEquals    *float64 `json:"NumericLessThanEquals"`
	NumericGreaterThanEquals *float64 `json:"NumericGreaterThanEquals"`

	NumericEqualsPath            *string `json:"NumericEqualsPath"`
	NumericLessThanPath          *string `json:"NumericLessThanPath"`
	NumericGreaterThanPath       *string `json:"NumericGreaterThanPath"`
	NumericLessThanEqualsPath    *string `json:"NumericLessThanEqualsPath"`
	NumericGreaterThanEqualsPath *string `json:"NumericGreaterThanEqualsPath"`

	BooleanEquals     *bool   `json:"BooleanEquals"`
	BooleanEqualsPath *string `json:"BooleanEqualsPath"`

	TimestampEquals            *string `json:"TimestampEquals"`
	TimestampLessThan          *string `json:"TimestampLessThan"`
	TimestampGreaterThan       *string `json:"TimestampGreaterThan"`
	TimestampLessThanEquals    *string `json:"TimestampLessThanEquals"`
	TimestampGreaterThanEquals *string `json:"TimestampGreaterThanEquals"`

	IsPresent   *bool `json:"IsPresent"`
	IsNull      *bool `json:"IsNull"`
	IsNumeric   *bool `json:"IsNumeric"`
	IsString    *bool `json:"IsString"`
	IsBoolean   *bool `json:"IsBoolean"`
	IsTimestamp *bool `json:"IsTimestamp"`

	And []ChoiceRule `json:"And"`
	Or  []ChoiceRule `json:"Or"`
	Not *ChoiceRule  `json:"Not"`
}

// WaitState pauses for a duration or until a timestamp.
type WaitState struct {
	Next          string `json:"Next"`
	End           bool   `json:"End"`
	InputPath     Path   `json:"InputPath"`
	OutputPath    Path   `json:"OutputPath"`
	Seconds       *int   `json:"Seconds"`
	Timestamp     string `json:"Timestamp"`
	SecondsPath   string `json:"SecondsPath"`
	TimestampPath string `json:"TimestampPath"`
	Comment       string `json:"Comment"`
}

// StateType implements State.
func (WaitState) StateType() StateType { return TypeWait }

// SucceedState ends the (sub)execution successfully.
type SucceedState struct {
	InputPath  Path   `json:"InputPath"`
	OutputPath Path   `json:"OutputPath"`
	Comment    string `json:"Comment"`
}

// StateType implements State.
func (SucceedState) StateType() StateType { return TypeSucceed }

// FailState ends the (sub)execution with an error.
type FailState struct {
	Error   string `json:"Error"`
	Cause   string `json:"Cause"`
	Comment string `json:"Comment"`
}

// StateType implements State.
func (FailState) StateType() StateType { return TypeFail }

// ParallelState runs every branch concurrently and collects their outputs.
type ParallelState struct {
	Branches       []*Machine      `json:"Branches"`
	Next           string          `json:"Next"`
	End            bool            `json:"End"`
	InputPath      Path            `json:"InputPath"`
	OutputPath     Path            `json:"OutputPath"`
	Parameters     json.RawMessage `json:"Parameters"`
	ResultSelector json.RawMessage `json:"ResultSelector"`
	ResultPath     Path            `json:"ResultPath"`
	Retry          []Retrier       `json:"Retry"`
	Catch          []Catcher       `json:"Catch"`
	Comment        string          `json:"Comment"`
}

// StateType implements State.
func (ParallelState) StateType() StateType { return TypeParallel }

// MapState runs an item processor once per element of an input array. Only the
// inline processor form is supported in MVP (no distributed/S3 item source).
type MapState struct {
	ItemsPath      string          `json:"ItemsPath"`
	ItemProcessor  *Machine        `json:"ItemProcessor"`
	Iterator       *Machine        `json:"Iterator"` // legacy alias for ItemProcessor
	MaxConcurrency int             `json:"MaxConcurrency"`
	Next           string          `json:"Next"`
	End            bool            `json:"End"`
	InputPath      Path            `json:"InputPath"`
	OutputPath     Path            `json:"OutputPath"`
	Parameters     json.RawMessage `json:"Parameters"`
	ItemSelector   json.RawMessage `json:"ItemSelector"`
	ResultSelector json.RawMessage `json:"ResultSelector"`
	ResultPath     Path            `json:"ResultPath"`
	Retry          []Retrier       `json:"Retry"`
	Catch          []Catcher       `json:"Catch"`
	Comment        string          `json:"Comment"`
}

// StateType implements State.
func (MapState) StateType() StateType { return TypeMap }

// Processor returns the Map state's item processor, accepting either the modern
// ItemProcessor or the legacy Iterator field.
func (m MapState) Processor() *Machine {
	if m.ItemProcessor != nil {
		return m.ItemProcessor
	}
	return m.Iterator
}

// UnmarshalJSON decodes a machine, dispatching each state to its concrete type
// based on the "Type" field. Nested Parallel branches and Map processors are
// themselves *Machine and recurse through this method.
func (m *Machine) UnmarshalJSON(b []byte) error {
	var raw struct {
		StartAt        string                     `json:"StartAt"`
		States         map[string]json.RawMessage `json:"States"`
		TimeoutSeconds int                        `json:"TimeoutSeconds"`
		Comment        string                     `json:"Comment"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	m.StartAt = raw.StartAt
	m.TimeoutSeconds = raw.TimeoutSeconds
	m.Comment = raw.Comment
	m.States = make(map[string]State, len(raw.States))
	for name, rawState := range raw.States {
		st, err := unmarshalState(rawState)
		if err != nil {
			return fmt.Errorf("state %q: %w", name, err)
		}
		m.States[name] = st
	}
	return nil
}

func unmarshalState(b json.RawMessage) (State, error) {
	var head struct {
		Type StateType `json:"Type"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case TypePass:
		return decodeState[PassState](b)
	case TypeTask:
		return decodeState[TaskState](b)
	case TypeChoice:
		return decodeState[ChoiceState](b)
	case TypeWait:
		return decodeState[WaitState](b)
	case TypeSucceed:
		return decodeState[SucceedState](b)
	case TypeFail:
		return decodeState[FailState](b)
	case TypeParallel:
		return decodeState[ParallelState](b)
	case TypeMap:
		return decodeState[MapState](b)
	case "":
		return nil, fmt.Errorf("missing Type")
	default:
		return nil, fmt.Errorf("unknown state Type %q", head.Type)
	}
}

// decodeState unmarshals into a concrete state value and returns it as a State.
func decodeState[T State](b json.RawMessage) (State, error) {
	var s T
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return s, nil
}
