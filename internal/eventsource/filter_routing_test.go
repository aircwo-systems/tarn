package eventsource

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

type recordingLambda struct {
	mu        sync.Mutex
	functions []string
	payloads  [][]byte
}

func (r *recordingLambda) Invoke(_ context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	r.mu.Lock()
	r.functions = append(r.functions, input.FunctionName)
	r.payloads = append(r.payloads, input.Payload)
	r.mu.Unlock()
	return &types.InvokeOutput{StatusCode: 200, Payload: []byte(`{}`)}, nil
}

func (r *recordingLambda) getPayloads() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([][]byte, len(r.payloads))
	copy(cp, r.payloads)
	return cp
}

func (r *recordingLambda) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, fn := range r.functions {
		if fn == name {
			n++
		}
	}
	return n
}

// Regression test: with two filtered ESM pollers on one queue, a type2 message
// should reach the type2 function (not spin forever / route to DLQ).
func TestFilteredPollersRouteType2Message(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	sqs := sqssvc.NewService(cfg)
	if err := sqs.Init(); err != nil {
		t.Fatalf("sqs init: %v", err)
	}
	sqs.Start()
	defer sqs.Stop()

	_, err := sqs.CreateQueue("events-dlq", nil, nil)
	if err != nil {
		t.Fatalf("create dlq: %v", err)
	}

	_, err = sqs.CreateQueue("events", map[string]string{
		"RedrivePolicy": `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:000000000000:events-dlq","maxReceiveCount":3}`,
	}, nil)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	lambda := &recordingLambda{}

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("esm store init: %v", err)
	}
	svc := NewService(cfg, store, lambda, sqs, nil)
	if err := svc.Init(); err != nil {
		t.Fatalf("esm svc init: %v", err)
	}

	if _, err := svc.CreateMapping(
		"arn:aws:sqs:us-east-1:000000000000:events",
		"arn:aws:lambda:us-east-1:000000000000:function:type1-fn",
		"type1-fn",
		1,
		0,
		true,
		&types.FilterCriteria{Filters: []types.FilterCriteriaFilter{{Pattern: `{"body":{"type":["type1"]}}`}}},
	); err != nil {
		t.Fatalf("create mapping type1: %v", err)
	}

	if _, err := svc.CreateMapping(
		"arn:aws:sqs:us-east-1:000000000000:events",
		"arn:aws:lambda:us-east-1:000000000000:function:type2-fn",
		"type2-fn",
		1,
		0,
		true,
		&types.FilterCriteria{Filters: []types.FilterCriteriaFilter{{Pattern: `{"body":{"type":["type2"]}}`}}},
	); err != nil {
		t.Fatalf("create mapping type2: %v", err)
	}

	if _, err := sqs.SendMessage("events", `{"id":"req-2001","type":"type2","body":"hello"}`, 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if lambda.count("type2-fn") > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	svc.Stop()

	if got := lambda.count("type2-fn"); got == 0 {
		t.Fatalf("expected type2-fn to be invoked at least once, got %d", got)
	}
	if got := lambda.count("type1-fn"); got != 0 {
		t.Fatalf("expected type1-fn invocations = 0, got %d", got)
	}

	dlqMsgs, err := sqs.PeekMessages("events-dlq", 10)
	if err != nil {
		t.Fatalf("peek dlq: %v", err)
	}
	if len(dlqMsgs) != 0 {
		t.Fatalf("expected no DLQ messages for type2 routing, got %d", len(dlqMsgs))
	}
}

func TestPollerDeliversMessageAttributesToLambda(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	sqs := sqssvc.NewService(cfg)
	if err := sqs.Init(); err != nil {
		t.Fatalf("sqs init: %v", err)
	}
	sqs.Start()
	defer sqs.Stop()

	_, err := sqs.CreateQueue("attr-queue", nil, nil)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	l := &recordingLambda{}
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("store init: %v", err)
	}
	svc := NewService(cfg, store, l, sqs, nil)
	if err := svc.Init(); err != nil {
		t.Fatalf("eventsource init: %v", err)
	}
	svc.Start()
	defer svc.Stop()

	if _, err := svc.CreateMapping(
		"arn:aws:sqs:us-east-1:000000000000:attr-queue",
		"arn:aws:lambda:us-east-1:000000000000:function:attr-fn",
		"attr-fn",
		1,
		0,
		true,
		nil,
	); err != nil {
		t.Fatalf("create mapping: %v", err)
	}

	attrs := map[string]*types.MessageAttribute{
		"TraceId": {
			DataType:    "String",
			StringValue: "trace-xyz-123",
		},
		"BinPayload": {
			DataType:    "Binary",
			BinaryValue: []byte("my-binary-bytes"),
		},
	}

	if _, err := sqs.SendMessage("attr-queue", `{"test":"payload"}`, 0, attrs, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if l.count("attr-fn") > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if got := l.count("attr-fn"); got == 0 {
		t.Fatalf("expected attr-fn to be invoked at least once, got %d", got)
	}

	payloads := l.getPayloads()
	if len(payloads) == 0 {
		t.Fatalf("no payloads received")
	}

	var event struct {
		Records []struct {
			MessageAttributes map[string]struct {
				DataType    string `json:"dataType"`
				StringValue string `json:"stringValue"`
				BinaryValue string `json:"binaryValue"`
			} `json:"messageAttributes"`
		} `json:"Records"`
	}

	if err := json.Unmarshal(payloads[0], &event); err != nil {
		t.Fatalf("failed to unmarshal lambda payload: %v", err)
	}

	if len(event.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(event.Records))
	}

	recAttrs := event.Records[0].MessageAttributes
	if len(recAttrs) != 2 {
		t.Fatalf("expected 2 messageAttributes, got %d: %+v", len(recAttrs), recAttrs)
	}

	traceAttr, ok := recAttrs["TraceId"]
	if !ok {
		t.Fatalf("expected TraceId in messageAttributes, got %+v", recAttrs)
	}
	if traceAttr.StringValue != "trace-xyz-123" {
		t.Fatalf("expected StringValue=trace-xyz-123, got %q", traceAttr.StringValue)
	}

	binAttr, ok := recAttrs["BinPayload"]
	if !ok {
		t.Fatalf("expected BinPayload in messageAttributes, got %+v", recAttrs)
	}
	wantBase64 := "bXktYmluYXJ5LWJ5dGVz"
	if binAttr.BinaryValue != wantBase64 {
		t.Fatalf("expected BinaryValue=%q, got %q", wantBase64, binAttr.BinaryValue)
	}
}
