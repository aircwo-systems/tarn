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
// It is keyed by function name — appropriate for local dev where concurrent
// invocations of the same function are rare.
type Collector struct {
	mu       sync.Mutex
	inflight map[string][]SubSpan
}

// NewCollector creates a new Collector.
func NewCollector() *Collector {
	return &Collector{inflight: make(map[string][]SubSpan)}
}

// Begin marks a lambda invocation as starting. Call before invoking the lambda.
func (c *Collector) Begin(functionName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight[functionName] = nil
}

// Collect retrieves and clears all sub-spans collected for a function since Begin.
// Call after the lambda invocation completes.
func (c *Collector) Collect(functionName string) []SubSpan {
	c.mu.Lock()
	defer c.mu.Unlock()
	spans, ok := c.inflight[functionName]
	if !ok {
		return nil
	}
	delete(c.inflight, functionName)
	return spans
}

// CollectWithFlush waits briefly before collecting to let async telemetry
// reporters (e.g. the db-proxy) deliver any in-flight spans.
//
// The Lambda's HTTP response reaches Tarn before the db-proxy finishes
// its reportSpan POST — both happen after the Lambda closes its DB connection,
// but the HTTP response path is shorter. Without this pause, successful DB
// calls are invisible in the trace because the inflight entry is cleared
// before RecordAnon has a chance to run.
const telemetryFlushWindow = 60 * time.Millisecond

func (c *Collector) CollectWithFlush(functionName string) []SubSpan {
	time.Sleep(telemetryFlushWindow)
	return c.Collect(functionName)
}

// RecordAnon appends a sub-span to all currently in-flight invocations.
// Used by services (e.g. secrets) that cannot identify which lambda called them.
func (c *Collector) RecordAnon(kind, name string, durationMs int64, status string, meta map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	span := SubSpan{Kind: kind, Name: name, DurationMs: durationMs, Status: status, Meta: meta}
	for fn := range c.inflight {
		c.inflight[fn] = append(c.inflight[fn], span)
	}
}

// SubSpansToSpans converts SubSpans into Spans for inclusion in a Trace.
func SubSpansToSpans(subs []SubSpan) []Span {
	out := make([]Span, len(subs))
	for i, s := range subs {
		out[i] = Span(s)
	}
	return out
}
