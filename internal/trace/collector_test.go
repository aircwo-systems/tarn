package trace

import (
	"testing"
	"time"
)

func TestFinishDoesNotBlockAndKeepsLateSpans(t *testing.T) {
	c := NewCollector()
	c.flushWindow = 50 * time.Millisecond
	inv := c.Begin()
	c.RecordAnon("secrets", "during", 1, "ok", nil)

	got := make(chan []Span, 1)
	start := time.Now()
	inv.Finish(func(spans []Span) { got <- spans })
	if waited := time.Since(start); waited > 10*time.Millisecond {
		t.Fatalf("Finish blocked the caller for %s", waited)
	}
	// A db-proxy span that arrives after the Lambda returned still counts.
	c.RecordAnon("postgres", "late", 1, "ok", nil)

	spans := <-got
	if len(spans) != 2 || spans[0].Name != "during" || spans[1].Name != "late" {
		t.Fatalf("spans = %+v, want during and late", spans)
	}
}

func TestConcurrentInvocationsOfOneFunctionKeepTheirSpans(t *testing.T) {
	c := NewCollector()
	c.flushWindow = time.Millisecond
	first := c.Begin()
	c.RecordAnon("secrets", "shared", 1, "ok", nil)
	second := c.Begin() // same function invoked again before the first finishes

	firstSpans := make(chan []Span, 1)
	first.Finish(func(spans []Span) { firstSpans <- spans })
	if spans := <-firstSpans; len(spans) != 1 {
		t.Fatalf("first invocation lost its spans when a second began: %+v", spans)
	}
	secondSpans := make(chan []Span, 1)
	second.Finish(func(spans []Span) { secondSpans <- spans })
	if spans := <-secondSpans; len(spans) != 0 {
		t.Fatalf("second invocation got spans from before it began: %+v", spans)
	}
}

func TestNilCollectorFinishesSynchronously(t *testing.T) {
	var c *Collector
	called := false
	c.Begin().Finish(func(spans []Span) { called = spans == nil })
	if !called {
		t.Fatal("nil collector must call record immediately with no spans")
	}
}
