package asl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseError describes a problem with an ASL definition. Path locates the
// offending element (e.g. "States.Process.Next") when known.
type ParseError struct {
	Path string
	Msg  string
}

func (e *ParseError) Error() string {
	if e.Path == "" {
		return e.Msg
	}
	return e.Path + ": " + e.Msg
}

// Parse decodes and structurally validates an ASL definition. It returns a
// *ParseError on any problem so callers can surface AWS's InvalidDefinition.
func Parse(definition string) (*Machine, error) {
	if strings.TrimSpace(definition) == "" {
		return nil, &ParseError{Msg: "definition is empty"}
	}
	var m Machine
	if err := json.Unmarshal([]byte(definition), &m); err != nil {
		return nil, &ParseError{Msg: "invalid JSON: " + err.Error()}
	}
	if err := validateMachine(&m, ""); err != nil {
		return nil, err
	}
	return &m, nil
}

func validateMachine(m *Machine, prefix string) error {
	if m.StartAt == "" {
		return &ParseError{Path: join(prefix, "StartAt"), Msg: "StartAt is required"}
	}
	if len(m.States) == 0 {
		return &ParseError{Path: join(prefix, "States"), Msg: "States must not be empty"}
	}
	if _, ok := m.States[m.StartAt]; !ok {
		return &ParseError{Path: join(prefix, "StartAt"), Msg: fmt.Sprintf("StartAt %q is not a defined state", m.StartAt)}
	}
	for name, st := range m.States {
		if err := validateState(m, st, join(prefix, "States."+name)); err != nil {
			return err
		}
	}
	return nil
}

func validateState(m *Machine, st State, path string) error {
	switch s := st.(type) {
	case PassState:
		return validateTransition(m, path, s.Next, s.End)
	case TaskState:
		if strings.TrimSpace(s.Resource) == "" {
			return &ParseError{Path: path, Msg: "Task state requires a Resource"}
		}
		if err := validateTransition(m, path, s.Next, s.End); err != nil {
			return err
		}
		return validateCatchers(m, path, s.Catch)
	case WaitState:
		if err := validateWait(s, path); err != nil {
			return err
		}
		return validateTransition(m, path, s.Next, s.End)
	case ChoiceState:
		return validateChoice(m, s, path)
	case SucceedState:
		return nil
	case FailState:
		return nil
	case ParallelState:
		if len(s.Branches) == 0 {
			return &ParseError{Path: path, Msg: "Parallel state requires at least one branch"}
		}
		for i, b := range s.Branches {
			if err := validateMachine(b, fmt.Sprintf("%s.Branches[%d]", path, i)); err != nil {
				return err
			}
		}
		if err := validateTransition(m, path, s.Next, s.End); err != nil {
			return err
		}
		return validateCatchers(m, path, s.Catch)
	case MapState:
		proc := s.Processor()
		if proc == nil {
			return &ParseError{Path: path, Msg: "Map state requires an ItemProcessor"}
		}
		if err := validateMachine(proc, path+".ItemProcessor"); err != nil {
			return err
		}
		if err := validateTransition(m, path, s.Next, s.End); err != nil {
			return err
		}
		return validateCatchers(m, path, s.Catch)
	default:
		return &ParseError{Path: path, Msg: "unsupported state type"}
	}
}

// validateTransition enforces ASL's Next/End rules for non-Choice states.
func validateTransition(m *Machine, path, next string, end bool) error {
	if end {
		if next != "" {
			return &ParseError{Path: path, Msg: "state must not set both Next and End"}
		}
		return nil
	}
	if next == "" {
		return &ParseError{Path: path, Msg: "non-terminal state must set Next or End"}
	}
	if _, ok := m.States[next]; !ok {
		return &ParseError{Path: path, Msg: fmt.Sprintf("Next %q is not a defined state", next)}
	}
	return nil
}

func validateChoice(m *Machine, s ChoiceState, path string) error {
	if len(s.Choices) == 0 {
		return &ParseError{Path: path, Msg: "Choice state requires at least one rule"}
	}
	for i, rule := range s.Choices {
		rp := fmt.Sprintf("%s.Choices[%d]", path, i)
		if rule.Next == "" {
			return &ParseError{Path: rp, Msg: "Choice rule requires Next"}
		}
		if _, ok := m.States[rule.Next]; !ok {
			return &ParseError{Path: rp, Msg: fmt.Sprintf("Next %q is not a defined state", rule.Next)}
		}
		if !ruleHasCondition(rule) {
			return &ParseError{Path: rp, Msg: "Choice rule has no condition"}
		}
	}
	if s.Default != "" {
		if _, ok := m.States[s.Default]; !ok {
			return &ParseError{Path: path, Msg: fmt.Sprintf("Default %q is not a defined state", s.Default)}
		}
	}
	return nil
}

func validateCatchers(m *Machine, path string, catchers []Catcher) error {
	for i, c := range catchers {
		cp := fmt.Sprintf("%s.Catch[%d]", path, i)
		if c.Next == "" {
			return &ParseError{Path: cp, Msg: "Catch requires Next"}
		}
		if _, ok := m.States[c.Next]; !ok {
			return &ParseError{Path: cp, Msg: fmt.Sprintf("Next %q is not a defined state", c.Next)}
		}
	}
	return nil
}

func validateWait(s WaitState, path string) error {
	set := 0
	if s.Seconds != nil {
		set++
	}
	if s.Timestamp != "" {
		set++
	}
	if s.SecondsPath != "" {
		set++
	}
	if s.TimestampPath != "" {
		set++
	}
	if set != 1 {
		return &ParseError{Path: path, Msg: "Wait state requires exactly one of Seconds, Timestamp, SecondsPath, TimestampPath"}
	}
	return nil
}

// ruleHasCondition checks a Choice rule carries at least a combinator or a
// Variable to compare. Deeper per-operator validation is deferred to evaluation.
func ruleHasCondition(r ChoiceRule) bool {
	if len(r.And) > 0 || len(r.Or) > 0 || r.Not != nil {
		return true
	}
	return strings.TrimSpace(r.Variable) != ""
}

func join(prefix, field string) string {
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}
