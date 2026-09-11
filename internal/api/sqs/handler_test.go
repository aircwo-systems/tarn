package sqs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	svc := sqssvc.NewService(cfg)
	return NewHandler(svc)
}

func TestXmlEscape_PreservesQuotesInElementText(t *testing.T) {
	input := `{"aggregateId":"agg-42","note":"it's fine"}`
	got := xmlEscape(input)
	if got != input {
		t.Fatalf("xmlEscape should not escape quotes in text nodes, got %q", got)
	}
}

func TestXmlEscape_EscapesReservedXMLChars(t *testing.T) {
	input := `<a>&</a>`
	got := xmlEscape(input)
	want := `&lt;a&gt;&amp;&lt;/a&gt;`
	if got != want {
		t.Fatalf("xmlEscape(%q) = %q, want %q", input, got, want)
	}
}

func TestDispatchUnknownXMLActionReturnsEmptyOK(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=UnknownThing"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<UnknownThingResponse") {
		t.Fatalf("expected empty OK response shape, got: %s", rec.Body.String())
	}
}

func TestDispatchUnknownJSONActionReturnsEmptyOK(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.UnknownThing")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Fatalf("expected empty json object, got: %s", rec.Body.String())
	}
}

func TestJSONSendMessageBatch_PreservesMessageAttributes(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.svc.CreateQueue("batch-test-queue", nil, nil); err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}

	batchPayload := `{
		"QueueUrl": "http://localhost:4566/000000000000/batch-test-queue",
		"Entries": [
			{
				"Id": "msg1",
				"MessageBody": "hello batch",
				"MessageAttributes": {
					"Author": {
						"DataType": "String",
						"StringValue": "Alice"
					}
				}
			}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(batchPayload))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.SendMessageBatch")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	msgs, err := h.svc.ReceiveMessage("batch-test-queue", 1, 30, 0)
	if err != nil {
		t.Fatalf("receive message error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].MessageAttributes == nil {
		t.Fatalf("expected MessageAttributes to be preserved, got nil")
	}
	attr, ok := msgs[0].MessageAttributes["Author"]
	if !ok {
		t.Fatalf("expected 'Author' attribute, got %+v", msgs[0].MessageAttributes)
	}
	if attr.StringValue != "Alice" {
		t.Fatalf("expected Author='Alice', got %q", attr.StringValue)
	}
}

func TestJSONReceiveMessage_ReturnsMessageAttributes(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.svc.CreateQueue("recv-test-queue", nil, nil); err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}

	batchPayload := `{
		"QueueUrl": "http://localhost:4566/000000000000/recv-test-queue",
		"Entries": [
			{
				"Id": "msg1",
				"MessageBody": "hello recv",
				"MessageAttributes": {
					"Author": {
						"DataType": "String",
						"StringValue": "Alice"
					}
				}
			}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(batchPayload))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.SendMessageBatch")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	recvPayload := `{
		"QueueUrl": "http://localhost:4566/000000000000/recv-test-queue"
	}`
	recvReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(recvPayload))
	recvReq.Header.Set("Content-Type", "application/x-amz-json-1.0")
	recvReq.Header.Set("X-Amz-Target", "AmazonSQS.ReceiveMessage")
	recvRec := httptest.NewRecorder()
	h.Dispatch(recvRec, recvReq)

	if recvRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recvRec.Code, recvRec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Body              string `json:"Body"`
			MessageAttributes map[string]struct {
				DataType    string `json:"DataType"`
				StringValue string `json:"StringValue"`
			} `json:"MessageAttributes"`
		} `json:"Messages"`
	}
	if err := json.Unmarshal(recvRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(resp.Messages))
	}
	attr, ok := resp.Messages[0].MessageAttributes["Author"]
	if !ok {
		t.Fatalf("expected Author in MessageAttributes, got %+v", resp.Messages[0].MessageAttributes)
	}
	if attr.StringValue != "Alice" {
		t.Fatalf("expected Author=Alice, got %q", attr.StringValue)
	}
}

func TestQuerySendMessage_PreservesBinaryAttributes(t *testing.T) {
	h := newTestHandler(t)

	if _, err := h.svc.CreateQueue("bin-test-queue", nil, nil); err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}

	form := "Action=SendMessage" +
		"&QueueUrl=http://localhost:4566/000000000000/bin-test-queue" +
		"&MessageBody=hello" +
		"&MessageAttribute.1.Name=BinaryField" +
		"&MessageAttribute.1.Value.DataType=Binary" +
		"&MessageAttribute.1.Value.BinaryValue=ZGF0YQ==" // "data"

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	msgs, err := h.svc.ReceiveMessage("bin-test-queue", 1, 30, 0)
	if err != nil {
		t.Fatalf("receive error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	attr, ok := msgs[0].MessageAttributes["BinaryField"]
	if !ok {
		t.Fatalf("expected BinaryField attribute, got %+v", msgs[0].MessageAttributes)
	}
	if string(attr.BinaryValue) != "data" {
		t.Fatalf("expected BinaryValue data, got %q", string(attr.BinaryValue))
	}
}
