package trace

import (
	"sync"
	"time"
)

// SubSpan is a service call recorded during a lambda invocation.
type SubSpan struct {
	Kind       string
	Name       string
	DurationMs int64
	Status     string
	Meta       map[string]string
}

// Collector tracks sub-spans produced during in-flight lambda invocations.
// Reporters such as the db-proxy cannot say which invocation called them, so
// every sub-span is attributed to all invocations in flight at the time.
type Collector struct {
	mu          sync.Mutex
	inflight    map[*Invocation]struct{}
	flushWindow time.Duration
}

// Invocation collects the sub-spans of one in-flight invocation.
type Invocation struct {
	c     *Collector
	spans []SubSpan
}

// telemetryFlushWindow is how long an invocation keeps collecting after the
// Lambda returns, so async telemetry reporters (e.g. the db-proxy) can deliver
// in-flight spans.
//
// The Lambda's HTTP response reaches Tarn before the db-proxy finishes its
// reportSpan POST — both happen after the Lambda closes its DB connection, but
// the HTTP response path is shorter. Without this window, successful DB calls
// are invisible in the trace.
const telemetryFlushWindow = 60 * time.Millisecond

// NewCollector creates a new Collector.
func NewCollector() *Collector {
	return &Collector{inflight: make(map[*Invocation]struct{}), flushWindow: telemetryFlushWindow}
}

// Begin starts collecting sub-spans for one invocation. Call before invoking
// the lambda and pass the result's spans to a trace via Finish. A nil
// Collector returns a nil Invocation, which Finish treats as having no spans.
func (c *Collector) Begin() *Invocation {
	if c == nil {
		return nil
	}
	inv := &Invocation{c: c}
	c.mu.Lock()
	c.inflight[inv] = struct{}{}
	c.mu.Unlock()
	return inv
}

// Finish hands the invocation's sub-spans to record once the telemetry flush
// window has passed. It does not block: record runs on its own goroutine, so
// callers must not use it to build their response.
func (inv *Invocation) Finish(record func(subSpans []Span)) {
	if inv == nil {
		record(nil)
		return
	}
	time.AfterFunc(inv.c.flushWindow, func() {
		inv.c.mu.Lock()
		delete(inv.c.inflight, inv)
		spans := inv.spans
		inv.c.mu.Unlock()
		record(SubSpansToSpans(spans))
	})
}

// Wait blocks for the telemetry flush window and returns the invocation's
// sub-spans. Prefer Finish wherever the caller's latency matters.
func (inv *Invocation) Wait() []Span {
	done := make(chan []Span, 1)
	inv.Finish(func(subSpans []Span) { done <- subSpans })
	return <-done
}

// RecordAnon appends a sub-span to all currently in-flight invocations.
// Used by services (e.g. secrets) that cannot identify which lambda called them.
func (c *Collector) RecordAnon(kind, name string, durationMs int64, status string, meta map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	span := SubSpan{Kind: kind, Name: name, DurationMs: durationMs, Status: status, Meta: meta}
	for inv := range c.inflight {
		inv.spans = append(inv.spans, span)
	}
}

// SubSpansToSpans converts SubSpans into Spans for inclusion in a Trace.
func SubSpansToSpans(subs []SubSpan) []Span {
	out := make([]Span, len(subs))
	for i, s := range subs {
		out[i] = Span{Kind: s.Kind, Name: s.Name, DurationMs: s.DurationMs, Status: s.Status, Meta: s.Meta}
	}
	return out
}
