package eventsource

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

// mockSQS implements SQSInterface for testing.
type mockSQS struct {
	mu           sync.Mutex
	messages     []*types.SQSMessage
	deleted      []string
	receiveErr   error
	receiveCalls int
}

func (m *mockSQS) ReceiveMessage(queueName string, maxCount, visTimeout, waitTimeSec int) ([]*types.SQSMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.receiveCalls++
	if m.receiveErr != nil {
		return nil, m.receiveErr
	}

	count := maxCount
	if count > len(m.messages) {
		count = len(m.messages)
	}
	result := m.messages[:count]
	m.messages = m.messages[count:]
	return result, nil
}

func (m *mockSQS) DeleteMessage(queueName, receiptHandle string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, receiptHandle)
	return nil
}

// mockLambda implements LambdaInterface for testing.
type mockLambda struct {
	mu          sync.Mutex
	functions   []string
	invocations [][]byte
	invokeErr   error
}

func (m *mockLambda) Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.invokeErr != nil {
		return nil, m.invokeErr
	}
	m.functions = append(m.functions, input.FunctionName)
	m.invocations = append(m.invocations, input.Payload)
	return &types.InvokeOutput{StatusCode: 200}, nil
}

func TestBuildSQSEventPayload(t *testing.T) {
	msgs := []*types.SQSMessage{
		{
			MessageId:     "msg-1",
			ReceiptHandle: "rh-1",
			Body:          `{"order":"123"}`,
			MD5OfBody:     "abc123",
		},
	}

	payload := buildSQSEventPayload(msgs, "arn:aws:sqs:us-east-1:000000000000:orders", "orders")

	var result map[string][]sqsEventRecord
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	records := result["Records"]
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	if records[0].MessageId != "msg-1" {
		t.Fatalf("messageId = %q, want %q", records[0].MessageId, "msg-1")
	}
	if records[0].EventSource != "aws:sqs" {
		t.Fatalf("eventSource = %q, want %q", records[0].EventSource, "aws:sqs")
	}
	if records[0].Body != `{"order":"123"}` {
		t.Fatalf("body = %q, want %q", records[0].Body, `{"order":"123"}`)
	}
}

func TestPollerStartStop(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	store.Init()

	mapping := &types.EventSourceMapping{
		UUID:                           "poll-1",
		QueueName:                      "test-queue",
		FunctionName:                   "test-func",
		BatchSize:                      10,
		MaximumBatchingWindowInSeconds: 1,
		Enabled:                        true,
		State:                          "Enabled",
	}
	store.Save(mapping)

	sqsMock := &mockSQS{
		messages: []*types.SQSMessage{
			{MessageId: "m1", ReceiptHandle: "rh1", Body: "hello", MD5OfBody: "md5"},
		},
	}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store)
	p.start()

	// Give the poller time to tick at least once
	time.Sleep(1500 * time.Millisecond)
	p.stop()

	lambdaMock.mu.Lock()
	invCount := len(lambdaMock.invocations)
	lambdaMock.mu.Unlock()

	if invCount < 1 {
		t.Fatalf("expected at least 1 invocation, got %d", invCount)
	}

	sqsMock.mu.Lock()
	delCount := len(sqsMock.deleted)
	sqsMock.mu.Unlock()

	if delCount < 1 {
		t.Fatalf("expected at least 1 deletion, got %d", delCount)
	}
}

func TestPollerNormalizesFunctionARN(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	store.Init()

	mapping := &types.EventSourceMapping{
		UUID:                           "poll-arn",
		EventSourceArn:                 "arn:aws:sqs:us-east-1:000000000000:test-queue",
		QueueName:                      "test-queue",
		FunctionName:                   "arn:aws:lambda:us-east-1:000000000000:function:test-func",
		BatchSize:                      10,
		MaximumBatchingWindowInSeconds: 1,
		Enabled:                        true,
		State:                          "Enabled",
	}
	store.Save(mapping)

	sqsMock := &mockSQS{
		messages: []*types.SQSMessage{
			{MessageId: "m1", ReceiptHandle: "rh1", Body: "hello", MD5OfBody: "md5"},
		},
	}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store)
	p.start()

	time.Sleep(1500 * time.Millisecond)
	p.stop()

	lambdaMock.mu.Lock()
	defer lambdaMock.mu.Unlock()
	if len(lambdaMock.functions) < 1 {
		t.Fatalf("expected at least 1 invocation, got %d", len(lambdaMock.functions))
	}
	if lambdaMock.functions[0] != "test-func" {
		t.Fatalf("function name = %q, want %q", lambdaMock.functions[0], "test-func")
	}
}

func TestPollerDisablesOnMissingQueue(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	store.Init()

	mapping := &types.EventSourceMapping{
		UUID:                           "poll-missing-queue",
		QueueName:                      "orders",
		FunctionName:                   "order-logger",
		BatchSize:                      10,
		MaximumBatchingWindowInSeconds: 1,
		Enabled:                        true,
		State:                          "Enabled",
	}
	store.Save(mapping)

	sqsMock := &mockSQS{receiveErr: errors.New("queue orders not found")}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store)
	p.poll()

	if mapping.Enabled {
		t.Fatal("expected mapping to be disabled")
	}
	if mapping.State != "Disabled" {
		t.Fatalf("state = %q, want %q", mapping.State, "Disabled")
	}
	if mapping.LastProcessingResult == "" {
		t.Fatal("expected LastProcessingResult to be populated")
	}

	// stop should be idempotent even if poll already stopped it.
	p.stop()
	p.stop()
}

func TestPollerDisablesOnMissingFunction(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	store.Init()

	mapping := &types.EventSourceMapping{
		UUID:                           "poll-missing-function",
		EventSourceArn:                 "arn:aws:sqs:us-east-1:000000000000:orders",
		QueueName:                      "orders",
		FunctionName:                   "arn:aws:lambda:us-east-1:000000000000:function:order-logger",
		BatchSize:                      10,
		MaximumBatchingWindowInSeconds: 1,
		Enabled:                        true,
		State:                          "Enabled",
	}
	store.Save(mapping)

	sqsMock := &mockSQS{
		messages: []*types.SQSMessage{
			{MessageId: "m1", ReceiptHandle: "rh1", Body: "hello", MD5OfBody: "md5"},
		},
	}
	lambdaMock := &mockLambda{invokeErr: errors.New("function order-logger not found")}

	p := newPoller(mapping, sqsMock, lambdaMock, store)
	p.poll()

	if mapping.Enabled {
		t.Fatal("expected mapping to be disabled")
	}
	if mapping.State != "Disabled" {
		t.Fatalf("state = %q, want %q", mapping.State, "Disabled")
	}
	if mapping.LastProcessingResult == "" {
		t.Fatal("expected LastProcessingResult to be populated")
	}
}
