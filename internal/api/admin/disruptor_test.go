package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
)

func TestSetDisruptorRulesSingleAndBulk(t *testing.T) {
	h := newTestHandler(t)
	for _, q := range []string{"a", "b"} {
		if _, err := h.sqs.CreateQueue(q, nil, nil); err != nil {
			t.Fatalf("create queue %s: %v", q, err)
		}
	}

	// Single queue.
	req := httptest.NewRequest(http.MethodPut, "/_tarn/admin/sqs/disruptor",
		strings.NewReader(`{"queue":"a","enabled":true,"failureRate":50,"code":"ServiceUnavailable"}`))
	rec := httptest.NewRecorder()
	h.SetDisruptorRules(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("single set status = %d, body: %s", rec.Code, rec.Body.String())
	}
	rule, ok := h.sqs.GetDisruptorRule("a")
	if !ok || !rule.Enabled || rule.FailureRate != 50 || rule.Code != sqssvc.DisruptCodeServiceUnavailable {
		t.Fatalf("unexpected stored rule: %+v ok=%v", rule, ok)
	}

	// Bulk: multiple queues at once.
	req = httptest.NewRequest(http.MethodPut, "/_tarn/admin/sqs/disruptor",
		strings.NewReader(`{"queues":["a","b"],"enabled":true,"failureRate":100,"code":"InternalError"}`))
	rec = httptest.NewRecorder()
	h.SetDisruptorRules(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk set status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Rules []sqssvc.Rule `json:"rules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode bulk response: %v", err)
	}
	if len(payload.Rules) != 2 {
		t.Fatalf("bulk rules len = %d, want 2", len(payload.Rules))
	}

	// List reflects both.
	req = httptest.NewRequest(http.MethodGet, "/_tarn/admin/sqs/disruptor", nil)
	rec = httptest.NewRecorder()
	h.ListDisruptorRules(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	payload.Rules = nil
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(payload.Rules) != 2 {
		t.Fatalf("list rules len = %d, want 2", len(payload.Rules))
	}
}

func TestSetDisruptorRulesValidation(t *testing.T) {
	h := newTestHandler(t)
	if _, err := h.sqs.CreateQueue("a", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"missing target", `{"enabled":true,"failureRate":50}`, http.StatusBadRequest},
		{"bad rate", `{"queue":"a","enabled":true,"failureRate":101}`, http.StatusBadRequest},
		{"bad code", `{"queue":"a","enabled":true,"failureRate":50,"code":"Nope"}`, http.StatusBadRequest},
		{"unknown queue", `{"queue":"ghost","enabled":true,"failureRate":50}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/_tarn/admin/sqs/disruptor", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.SetDisruptorRules(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestClearDisruptorRules(t *testing.T) {
	h := newTestHandler(t)
	for _, q := range []string{"a", "b"} {
		if _, err := h.sqs.CreateQueue(q, nil, nil); err != nil {
			t.Fatalf("create queue %s: %v", q, err)
		}
		h.sqs.SetDisruptorRule(sqssvc.Rule{QueueName: q, Enabled: true, FailureRate: 100})
	}

	// Clear one via query param.
	req := httptest.NewRequest(http.MethodDelete, "/_tarn/admin/sqs/disruptor?queue=a", nil)
	rec := httptest.NewRecorder()
	h.ClearDisruptorRules(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear one status = %d", rec.Code)
	}
	if _, ok := h.sqs.GetDisruptorRule("a"); ok {
		t.Fatalf("expected rule for a to be cleared")
	}
	if _, ok := h.sqs.GetDisruptorRule("b"); !ok {
		t.Fatalf("expected rule for b to remain")
	}

	// Clear all.
	req = httptest.NewRequest(http.MethodDelete, "/_tarn/admin/sqs/disruptor?all=true", nil)
	rec = httptest.NewRecorder()
	h.ClearDisruptorRules(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear all status = %d", rec.Code)
	}
	if got := h.sqs.ListDisruptorRules(); len(got) != 0 {
		t.Fatalf("expected no rules after clear-all, got %+v", got)
	}
}

func TestOverviewSurfacesDisruptorState(t *testing.T) {
	h := newTestHandler(t)
	if _, err := h.sqs.CreateQueue("a", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if _, err := h.sqs.CreateQueue("b", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	h.sqs.SetDisruptorRule(sqssvc.Rule{QueueName: "a", Enabled: true, FailureRate: 75, Code: sqssvc.DisruptCodeThrottling})

	req := httptest.NewRequest(http.MethodGet, "/_tarn/admin/overview", nil)
	rec := httptest.NewRecorder()
	h.Overview(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d", rec.Code)
	}
	var payload struct {
		Queues []queueSummary `json:"queues"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	byName := make(map[string]queueSummary, len(payload.Queues))
	for _, q := range payload.Queues {
		byName[q.Name] = q
	}
	armed, ok := byName["a"]
	if !ok {
		t.Fatalf("queue a missing from overview")
	}
	if !armed.DisruptEnabled || armed.DisruptFailureRate != 75 || armed.DisruptCode != sqssvc.DisruptCodeThrottling {
		t.Fatalf("unexpected disruptor state for a: %+v", armed)
	}
	if plain, ok := byName["b"]; !ok || plain.DisruptEnabled {
		t.Fatalf("queue b should show disruptor off, got %+v", plain)
	}
}
