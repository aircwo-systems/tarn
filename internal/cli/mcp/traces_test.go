package mcp

import (
	"strings"
	"testing"
)

// TestTracesForwardsFilters covers that tarn_get_traces sends the filter
// arguments a model supplies straight through as query parameters, since the
// filtering itself lives server-side.
func TestTracesForwardsFilters(t *testing.T) {
	inst := newInstance(t).json("GET /_tarn/admin/traces", map[string]any{
		"traces": []map[string]any{
			{
				"id": "trace-1", "correlationId": "corr-1",
				"startedAt":  "2026-09-04T21:33:20.502763Z",
				"durationMs": 42, "status": 200,
				"method": "POST", "path": "/",
				"spans": []map[string]any{
					{"kind": "queue", "name": "orders", "durationMs": 5, "status": "ok"},
					{"kind": "lambda", "name": "order-processor", "durationMs": 30, "status": "ok",
						"meta": map[string]string{"requestId": "abc"}},
				},
			},
		},
	})

	var out TracesOutput
	call(t, inst.session(), "tarn_get_traces", map[string]any{
		"correlationId": "corr-1", "resource": "orders", "kind": "queue", "limit": 5,
	}, &out)

	q := inst.lastRequest().Query
	if !strings.Contains(q, "correlationId=corr-1") {
		t.Errorf("query = %q, want correlationId forwarded", q)
	}
	if !strings.Contains(q, "resource=orders") {
		t.Errorf("query = %q, want resource forwarded", q)
	}
	if !strings.Contains(q, "kind=queue") {
		t.Errorf("query = %q, want kind forwarded", q)
	}
	if !strings.Contains(q, "limit=5") {
		t.Errorf("query = %q, want limit forwarded", q)
	}

	if len(out.Traces) != 1 {
		t.Fatalf("traces len = %d, want 1", len(out.Traces))
	}
	trace := out.Traces[0]
	if trace.ID != "trace-1" || trace.CorrelationID != "corr-1" {
		t.Errorf("unexpected trace identity: %+v", trace)
	}
	if trace.DurationMs != 42 || trace.Status != 200 || trace.Method != "POST" || trace.Path != "/" {
		t.Errorf("unexpected trace fields: %+v", trace)
	}
	if !strings.HasPrefix(trace.StartedAt, "2026-09-04T21:33:20") {
		t.Errorf("startedAt = %q, want RFC3339 formatted", trace.StartedAt)
	}
	if len(trace.Spans) != 2 {
		t.Fatalf("spans len = %d, want 2", len(trace.Spans))
	}
	if trace.Spans[0].Kind != "queue" || trace.Spans[0].Name != "orders" {
		t.Errorf("unexpected first span: %+v", trace.Spans[0])
	}
	if trace.Spans[1].Kind != "lambda" || trace.Spans[1].Meta["requestId"] != "abc" {
		t.Errorf("unexpected second span: %+v", trace.Spans[1])
	}
}

// TestTracesDefaultsAndCapsLimit covers the same bound tarn_get_logs applies:
// a small default page for exploration, and a hard cap so a wide query cannot
// pull the whole store into one response.
func TestTracesDefaultsAndCapsLimit(t *testing.T) {
	inst := newInstance(t).json("GET /_tarn/admin/traces", map[string]any{"traces": []map[string]any{}})

	var out TracesOutput
	call(t, inst.session(), "tarn_get_traces", nil, &out)
	if !strings.Contains(inst.lastRequest().Query, "limit=20") {
		t.Errorf("query = %q, want default limit=20", inst.lastRequest().Query)
	}

	call(t, inst.session(), "tarn_get_traces", map[string]any{"limit": 10000}, &out)
	if !strings.Contains(inst.lastRequest().Query, "limit=100") {
		t.Errorf("query = %q, want limit capped at 100", inst.lastRequest().Query)
	}
}
