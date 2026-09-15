package sqs

import (
	"fmt"
	"math/rand/v2"
	"sync"
)

// Disruptor error codes supported for SQS send-failure injection. Codes map
// to AWS-shaped error responses so SDK retry behavior stays realistic.
const (
	DisruptCodeInternalError      = "InternalError"
	DisruptCodeServiceUnavailable = "ServiceUnavailable"
	DisruptCodeOverLimit          = "OverLimit"
	DisruptCodeThrottling         = "Throttling"
)

// disruptStatus maps a disruptor error code to its HTTP status.
func disruptStatus(code string) int {
	switch code {
	case DisruptCodeServiceUnavailable:
		return 503
	case DisruptCodeInternalError:
		return 500
	case DisruptCodeThrottling:
		return 400
	case DisruptCodeOverLimit:
		return 400
	default:
		return 500
	}
}

// DisruptError is returned by SendMessage when a disruptor rule fires. It
// carries the AWS-shaped code and HTTP status so both the query/XML and JSON
// handlers can render realistic failures (including per-entry batch failures).
type DisruptError struct {
	QueueName  string
	Code       string
	Message    string
	StatusCode int
}

func (e *DisruptError) Error() string { return e.Message }

// Rule controls send-failure injection for a single queue. FailureRate is a
// percentage 0-100: each SendMessage to the queue fails independently with
// that probability while Enabled is true.
type Rule struct {
	QueueName   string `json:"queue"`
	Enabled     bool   `json:"enabled"`
	FailureRate int    `json:"failureRate"`
	Code        string `json:"code"`
	Message     string `json:"message,omitempty"`
}

// normalized returns a copy with clamped rate and defaulted code.
func (r Rule) normalized() Rule {
	if r.FailureRate < 0 {
		r.FailureRate = 0
	}
	if r.FailureRate > 100 {
		r.FailureRate = 100
	}
	switch r.Code {
	case DisruptCodeInternalError, DisruptCodeServiceUnavailable, DisruptCodeOverLimit, DisruptCodeThrottling:
	default:
		r.Code = DisruptCodeServiceUnavailable
	}
	if r.Message == "" {
		r.Message = fmt.Sprintf("Disruptor: injected %s failure for queue %q", r.Code, r.QueueName)
	}
	return r
}

// Disruptor holds per-queue send-failure rules. It is in-memory only by
// design: chaos toggles must not survive restarts or leak into persisted
// queue state. One Disruptor lives inside each per-account SQS Service, so
// rules are automatically account-scoped.
type Disruptor struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

// newDisruptor creates an empty Disruptor.
func newDisruptor() *Disruptor {
	return &Disruptor{rules: make(map[string]Rule)}
}

// Set stores (or replaces) the rule for a queue. A rule with Enabled=false or
// FailureRate<=0 disables injection but is retained so the UI can show the
// last configuration.
func (d *Disruptor) Set(rule Rule) Rule {
	rule = rule.normalized()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rules[rule.QueueName] = rule
	return rule
}

// Clear removes the rule for a queue. Returns true when a rule existed.
func (d *Disruptor) Clear(queueName string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.rules[queueName]; !ok {
		return false
	}
	delete(d.rules, queueName)
	return true
}

// ClearAll removes every rule.
func (d *Disruptor) ClearAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rules = make(map[string]Rule)
}

// Get returns the rule for a queue, if any.
func (d *Disruptor) Get(queueName string) (Rule, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	r, ok := d.rules[queueName]
	return r, ok
}

// List returns all rules sorted by queue name.
func (d *Disruptor) List() []Rule {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Rule, 0, len(d.rules))
	for _, r := range d.rules {
		out = append(out, r)
	}
	// Insertion order is nondeterministic; keep output stable for the UI.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].QueueName < out[j-1].QueueName; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// active reports whether injection is armed for the queue (without rolling).
func (d *Disruptor) active(queueName string) (Rule, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	r, ok := d.rules[queueName]
	if !ok || !r.Enabled || r.FailureRate <= 0 {
		return Rule{}, false
	}
	return r, true
}

// shouldFail rolls the dice for an armed rule. It returns the DisruptError to
// fail with, or nil to let the send proceed.
func (d *Disruptor) shouldFail(queueName string) *DisruptError {
	rule, ok := d.active(queueName)
	if !ok {
		return nil
	}
	if rule.FailureRate < 100 {
		if rand.IntN(100) >= rule.FailureRate {
			return nil
		}
	}
	return &DisruptError{
		QueueName:  queueName,
		Code:       rule.Code,
		Message:    rule.Message,
		StatusCode: disruptStatus(rule.Code),
	}
}
