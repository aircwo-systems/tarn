package sqs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
)

func TestDisruptedSendMessageXMLReturnsConfiguredError(t *testing.T) {
	h := newTestHandler(t)
	if _, err := h.svc.CreateQueue("orders", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	h.svc.SetDisruptorRule(sqssvc.Rule{QueueName: "orders", Enabled: true, FailureRate: 100, Code: sqssvc.DisruptCodeServiceUnavailable})

	form := "Action=SendMessage&QueueUrl=http://localhost:4566/000000000000/orders&MessageBody=hello"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ServiceUnavailable") {
		t.Fatalf("expected ServiceUnavailable code in body: %s", rec.Body.String())
	}

	h.svc.ClearDisruptorRule("orders")
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.Dispatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("after clear status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestDisruptedSendMessageJSONReturnsConfiguredError(t *testing.T) {
	h := newTestHandler(t)
	if _, err := h.svc.CreateQueue("orders", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	h.svc.SetDisruptorRule(sqssvc.Rule{QueueName: "orders", Enabled: true, FailureRate: 100, Code: sqssvc.DisruptCodeInternalError})

	body := `{"QueueUrl":"http://localhost:4566/000000000000/orders","MessageBody":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.SendMessage")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body: %s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Amzn-Errortype") != sqssvc.DisruptCodeInternalError {
		t.Fatalf("X-Amzn-Errortype = %q, want InternalError", rec.Header().Get("X-Amzn-Errortype"))
	}
}

func TestDisruptedSendMessageBatchFailsPerEntry(t *testing.T) {
	h := newTestHandler(t)
	if _, err := h.svc.CreateQueue("orders", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	h.svc.SetDisruptorRule(sqssvc.Rule{QueueName: "orders", Enabled: true, FailureRate: 100, Code: sqssvc.DisruptCodeServiceUnavailable})

	form := "Action=SendMessageBatch&QueueUrl=http://localhost:4566/000000000000/orders" +
		"&SendMessageBatchRequestEntry.1.Id=a&SendMessageBatchRequestEntry.1.MessageBody=one" +
		"&SendMessageBatchRequestEntry.2.Id=b&SendMessageBatchRequestEntry.2.MessageBody=two"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("batch status = %d, want 200 with per-entry failures", rec.Code)
	}
	resp := rec.Body.String()
	if !strings.Contains(resp, "ServiceUnavailable") {
		t.Fatalf("expected per-entry ServiceUnavailable failures: %s", resp)
	}
	if strings.Contains(resp, "SendMessageBatchResultEntry") {
		t.Fatalf("expected no successful entries at 100%% failure rate: %s", resp)
	}
}

// Terraform's aws_sqs_queue delete waiter matches the legacy query error code
// (AWS.SimpleQueueService.NonExistentQueue), which AWS sends alongside the
// JSON shape in X-Amzn-Query-Error.
func TestJSONMissingQueueSendsQueryCompatibleErrorCode(t *testing.T) {
	h := newTestHandler(t)

	body := `{"QueueUrl":"http://localhost:4566/000000000000/gone"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.GetQueueAttributes")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Amzn-Errortype"); got != "QueueDoesNotExist" {
		t.Fatalf("X-Amzn-Errortype = %q, want QueueDoesNotExist", got)
	}
	if got := rec.Header().Get("X-Amzn-Query-Error"); got != "AWS.SimpleQueueService.NonExistentQueue;Sender" {
		t.Fatalf("X-Amzn-Query-Error = %q, want AWS.SimpleQueueService.NonExistentQueue;Sender", got)
	}
}
