package eventsource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	released     []string
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

func (m *mockSQS) ChangeMessageVisibility(queueName, receiptHandle string, timeout int) error {
	return nil
}

func (m *mockSQS) ReleaseMessage(queueName, receiptHandle string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.released = append(m.released, receiptHandle)
	return nil
}

func (m *mockSQS) MoveToDLQIfExceeded(srcQueue string, msg *types.SQSMessage) (bool, string, error) {
	return false, "", nil
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

func TestMatchesFilterPatternWithEscapedBodyJSON(t *testing.T) {
	msg := &types.SQSMessage{
		Body: `{\"id\":\"req-1\",\"type\":\"type2\"}`,
	}

	pattern := `{"body":{"type":["type2"]}}`
	if !matchesFilterPattern(msg, pattern) {
		t.Fatalf("expected escaped JSON body to match pattern %s", pattern)
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
	if err := store.Save(mapping); err != nil {
		t.Fatalf("save mapping: %v", err)
	}

	sqsMock := &mockSQS{
		messages: []*types.SQSMessage{
			{MessageId: "m1", ReceiptHandle: "rh1", Body: "hello", MD5OfBody: "md5"},
		},
	}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store, nil, nil)
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
	if err := store.Save(mapping); err != nil {
		t.Fatalf("save mapping: %v", err)
	}

	sqsMock := &mockSQS{
		messages: []*types.SQSMessage{
			{MessageId: "m1", ReceiptHandle: "rh1", Body: "hello", MD5OfBody: "md5"},
		},
	}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store, nil, nil)
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

func TestPollerPausesOnMissingQueue(t *testing.T) {
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
	if err := store.Save(mapping); err != nil {
		t.Fatalf("save mapping: %v", err)
	}

	sqsMock := &mockSQS{receiveErr: errors.New("queue orders not found")}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store, nil, nil)
	p.poll()

	// Mapping must stay enabled — poller records the error but keeps running.
	if !mapping.Enabled {
		t.Fatal("expected mapping to remain enabled after missing-queue error")
	}
	if mapping.State != "Enabled" {
		t.Fatalf("state = %q, want %q", mapping.State, "Enabled")
	}
	if mapping.LastProcessingResult == "" {
		t.Fatal("expected LastProcessingResult to be populated")
	}

	// poller must still be stoppable after a not-found error.
	p.stop()
	p.stop()
}

func TestPollerDisablesAfterConsecutiveNotFound(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	store.Init()

	mapping := &types.EventSourceMapping{
		UUID:                           "poll-disable-missing-queue",
		QueueName:                      "orders",
		FunctionName:                   "order-logger",
		BatchSize:                      10,
		MaximumBatchingWindowInSeconds: 1,
		Enabled:                        true,
		State:                          "Enabled",
	}
	if err := store.Save(mapping); err != nil {
		t.Fatalf("save mapping: %v", err)
	}

	sqsMock := &mockSQS{receiveErr: errors.New("queue orders not found")}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store, nil, nil)
	for i := 0; i < maxConsecutiveNotFoundRetries; i++ {
		p.poll()
	}

	if mapping.Enabled {
		t.Fatal("expected mapping to be auto-disabled after consecutive not-found errors")
	}
	if mapping.State != "Disabled" {
		t.Fatalf("state = %q, want %q", mapping.State, "Disabled")
	}
	if !strings.Contains(mapping.LastProcessingResult, "DISABLED: resource not found") {
		t.Fatalf("expected disabled not-found result, got %q", mapping.LastProcessingResult)
	}
}

func TestPollerReleasesFilteredOutMessages(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	store.Init()

	mapping := &types.EventSourceMapping{
		UUID:                           "poll-filter-release",
		EventSourceArn:                 "arn:aws:sqs:us-east-1:000000000000:events",
		QueueName:                      "events",
		FunctionName:                   "type2-processor",
		BatchSize:                      10,
		MaximumBatchingWindowInSeconds: 1,
		Enabled:                        true,
		State:                          "Enabled",
		FilterCriteria: &types.FilterCriteria{
			Filters: []types.FilterCriteriaFilter{
				{Pattern: `{"body":{"type":["type2"]}}`},
			},
		},
	}
	if err := store.Save(mapping); err != nil {
		t.Fatalf("save mapping: %v", err)
	}

	sqsMock := &mockSQS{
		messages: []*types.SQSMessage{
			{
				MessageId:     "m1",
				ReceiptHandle: "rh1",
				Body:          `{"id":"req-1","type":"type1"}`,
				MD5OfBody:     "md5",
			},
		},
	}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store, nil, nil)
	p.poll()

	lambdaMock.mu.Lock()
	invocations := len(lambdaMock.invocations)
	lambdaMock.mu.Unlock()
	if invocations != 0 {
		t.Fatalf("expected 0 lambda invocations for filtered-out message, got %d", invocations)
	}

	sqsMock.mu.Lock()
	released := len(sqsMock.released)
	sqsMock.mu.Unlock()
	if released != 1 {
		t.Fatalf("expected 1 released message, got %d", released)
	}
}

func TestPollerPausesOnMissingFunction(t *testing.T) {
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
	if err := store.Save(mapping); err != nil {
		t.Fatalf("save mapping: %v", err)
	}

	sqsMock := &mockSQS{
		messages: []*types.SQSMessage{
			{MessageId: "m1", ReceiptHandle: "rh1", Body: "hello", MD5OfBody: "md5"},
		},
	}
	lambdaMock := &mockLambda{invokeErr: errors.New("function order-logger not found")}

	p := newPoller(mapping, sqsMock, lambdaMock, store, nil, nil)
	p.poll()

	// Mapping must stay enabled so the poller restarts on next boot.
	if !mapping.Enabled {
		t.Fatal("expected mapping to remain enabled after missing-function error")
	}
	if mapping.State != "Enabled" {
		t.Fatalf("state = %q, want %q", mapping.State, "Enabled")
	}
	if mapping.LastProcessingResult == "" {
		t.Fatal("expected LastProcessingResult to be populated")
	}
}

// TestPollerRetriesAfterMissingQueue verifies that a not-found error on the queue
// does not permanently kill the poller — it should recover when the queue appears.
func TestPollerRetriesAfterMissingQueue(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	store.Init()

	mapping := &types.EventSourceMapping{
		UUID:                           "poll-retry",
		QueueName:                      "orders",
		FunctionName:                   "order-processor",
		BatchSize:                      10,
		MaximumBatchingWindowInSeconds: 1,
		Enabled:                        true,
		State:                          "Enabled",
	}
	if err := store.Save(mapping); err != nil {
		t.Fatalf("save mapping: %v", err)
	}

	sqsMock := &mockSQS{receiveErr: errors.New("queue orders not found")}
	lambdaMock := &mockLambda{}

	p := newPoller(mapping, sqsMock, lambdaMock, store, nil, nil)
	p.start()

	// Wait for at least one failing poll.
	time.Sleep(1500 * time.Millisecond)

	// Now "create" the queue by clearing the error and adding a message.
	sqsMock.mu.Lock()
	sqsMock.receiveErr = nil
	sqsMock.messages = []*types.SQSMessage{
		{MessageId: "m1", ReceiptHandle: "rh1", Body: "hello", MD5OfBody: "md5"},
	}
	sqsMock.mu.Unlock()

	// Give the poller time to retry and process.
	time.Sleep(1500 * time.Millisecond)
	p.stop()

	lambdaMock.mu.Lock()
	invCount := len(lambdaMock.invocations)
	lambdaMock.mu.Unlock()

	if invCount < 1 {
		t.Fatalf("expected at least 1 lambda invocation after queue appeared, got %d", invCount)
	}
}

// TestServiceRestartStartsPollers simulates a full server restart where the
// persisted state.json has mappings with Enabled=false (corrupted by old code).
// The new service must re-enable them and process messages.
func TestServiceRestartStartsPollers(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true

	// --- First "boot": create a mapping and simulate a poller error
	// that left Enabled=false on disk (old bug behavior).
	store1 := NewStore(cfg)
	if err := store1.Init(); err != nil {
		t.Fatalf("store1 Init: %v", err)
	}

	mapping := &types.EventSourceMapping{
		UUID:                           "restart-test-uuid",
		EventSourceArn:                 "arn:aws:sqs:us-east-1:000000000000:orders",
		QueueName:                      "orders",
		FunctionName:                   "order-processor",
		BatchSize:                      10,
		MaximumBatchingWindowInSeconds: 1,
		Enabled:                        false, // simulates old bug: saved as disabled
		State:                          "Disabled",
		LastProcessingResult:           "ERROR: function order-processor not found",
	}
	if err := store1.Save(mapping); err != nil {
		t.Fatalf("save mapping: %v", err)
	}

	// --- Second "boot": new service instance reads the same state.json.
	store2 := NewStore(cfg)
	if err := store2.Init(); err != nil {
		t.Fatalf("store2 Init: %v", err)
	}

	sqsMock := &mockSQS{
		messages: []*types.SQSMessage{
			{MessageId: "m1", ReceiptHandle: "rh1", Body: "hello", MD5OfBody: "md5"},
		},
	}
	lambdaMock := &mockLambda{}

	svc := NewService(cfg, store2, lambdaMock, sqsMock)
	svc.Start()

	// Give pollers time to process.
	time.Sleep(1500 * time.Millisecond)
	svc.Stop()

	// Mapping must have been re-enabled.
	reloaded, err := store2.Get("restart-test-uuid")
	if err != nil {
		t.Fatalf("get mapping after restart: %v", err)
	}
	if !reloaded.Enabled {
		t.Fatal("expected mapping to be re-enabled after restart")
	}
	if reloaded.State != "Enabled" {
		t.Fatalf("state = %q, want %q", reloaded.State, "Enabled")
	}

	// Lambda must have been invoked.
	lambdaMock.mu.Lock()
	invCount := len(lambdaMock.invocations)
	lambdaMock.mu.Unlock()
	if invCount < 1 {
		t.Fatalf("expected at least 1 lambda invocation after restart, got %d", invCount)
	}
}
