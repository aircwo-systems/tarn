package eventsource

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// slowLambda records how many invokes run at once and which FIFO groups each
// invoke holds.
type slowLambda struct {
	delay time.Duration

	mu       sync.Mutex
	running  int
	peak     int
	records  int
	inFlight map[string]bool // FIFO group -> held by a running invoke
	overlap  []string        // groups seen in two invokes at once
}

func (l *slowLambda) Invoke(_ context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	var event struct {
		Records []struct {
			Attributes map[string]string `json:"attributes"`
		} `json:"Records"`
	}
	_ = json.Unmarshal(input.Payload, &event)
	groups := map[string]bool{}
	for _, r := range event.Records {
		if g := r.Attributes["MessageGroupId"]; g != "" {
			groups[g] = true
		}
	}

	l.mu.Lock()
	l.running++
	l.peak = max(l.peak, l.running)
	l.records += len(event.Records)
	for g := range groups {
		if l.inFlight[g] {
			l.overlap = append(l.overlap, g)
		}
		l.inFlight[g] = true
	}
	l.mu.Unlock()

	time.Sleep(l.delay)

	l.mu.Lock()
	l.running--
	for g := range groups {
		delete(l.inFlight, g)
	}
	l.mu.Unlock()
	return &types.InvokeOutput{StatusCode: 200, Payload: []byte(`{}`)}, nil
}

func (l *slowLambda) snapshot() (peak, records int, overlap []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peak, l.records, append([]string(nil), l.overlap...)
}

func startSQS(t *testing.T) *sqssvc.Service {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	sqs := sqssvc.NewService(cfg)
	if err := sqs.Init(); err != nil {
		t.Fatalf("sqs init: %v", err)
	}
	sqs.Start()
	t.Cleanup(sqs.Stop)
	return sqs
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func TestPollerDrainsBacklogConcurrently(t *testing.T) {
	sqs := startSQS(t)
	if _, err := sqs.CreateQueue("backlog", nil, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := sqs.SendMessage("backlog", fmt.Sprintf(`{"i":%d}`, i), 0, nil, "", ""); err != nil {
			t.Fatal(err)
		}
	}

	lambda := &slowLambda{delay: 50 * time.Millisecond, inFlight: map[string]bool{}}
	mapping := &types.EventSourceMapping{UUID: "backlog", QueueName: "backlog", FunctionName: "fn", BatchSize: 10, Enabled: true, State: "Enabled",
		ScalingConfig: &types.ScalingConfig{MaximumConcurrency: 4}}
	p := newPoller(mapping, sqs, nil, lambda, NewStore(sqsTestConfig(t)), nil, nil)
	p.start()
	defer p.stop()

	// 20 batches of 50ms: one loop at one batch per tick needs 20s; four
	// loops polling back to back need about 250ms after the first tick.
	if !waitFor(t, 3*time.Second, func() bool { _, n, _ := lambda.snapshot(); return n == 200 }) {
		_, n, _ := lambda.snapshot()
		t.Fatalf("processed %d/200 messages in 3s; backlog is not drained at invoke speed", n)
	}
	peak, _, _ := lambda.snapshot()
	if peak < 2 {
		t.Fatalf("peak concurrent invokes = %d, want the backlog spread over several loops", peak)
	}
	if peak > 4 {
		t.Fatalf("peak concurrent invokes = %d, exceeds ScalingConfig.MaximumConcurrency 4", peak)
	}
}

func TestPollerLoopsCappedAtLambdaConcurrency(t *testing.T) {
	p := newPoller(&types.EventSourceMapping{QueueName: "q", ScalingConfig: &types.ScalingConfig{MaximumConcurrency: 50}}, nil, nil, nil, nil, nil, nil)
	p.lambdaMaxConcurrency = 10
	if got := p.loops(); got != 10 {
		t.Fatalf("loops = %d, want the Lambda concurrency cap 10", got)
	}
	stream := newPoller(&types.EventSourceMapping{SourceType: "dynamodb-stream", ScalingConfig: &types.ScalingConfig{MaximumConcurrency: 50}}, nil, nil, nil, nil, nil, nil)
	if got := stream.loops(); got != 1 {
		t.Fatalf("stream loops = %d, want 1 to keep sequence order", got)
	}
}

func TestPollerConcurrentLoopsNeverShareAFIFOGroup(t *testing.T) {
	sqs := startSQS(t)
	if _, err := sqs.CreateQueue("orders.fifo", map[string]string{"FifoQueue": "true", "ContentBasedDeduplication": "true"}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		group := fmt.Sprintf("g%d", i%3)
		if _, err := sqs.SendMessage("orders.fifo", fmt.Sprintf(`{"i":%d}`, i), 0, nil, group, ""); err != nil {
			t.Fatal(err)
		}
	}

	lambda := &slowLambda{delay: 20 * time.Millisecond, inFlight: map[string]bool{}}
	mapping := &types.EventSourceMapping{UUID: "fifo", QueueName: "orders.fifo", FunctionName: "fn", BatchSize: 2, Enabled: true, State: "Enabled",
		ScalingConfig: &types.ScalingConfig{MaximumConcurrency: 5}}
	p := newPoller(mapping, sqs, nil, lambda, NewStore(sqsTestConfig(t)), nil, nil)
	p.start()
	defer p.stop()

	if !waitFor(t, 10*time.Second, func() bool { _, n, _ := lambda.snapshot(); return n == 60 }) {
		_, n, _ := lambda.snapshot()
		t.Fatalf("processed %d/60 FIFO messages", n)
	}
	if _, _, overlap := lambda.snapshot(); len(overlap) > 0 {
		t.Fatalf("message groups %v were delivered to two invokes at once", overlap)
	}
}

func TestPollDoesNotRepeatOnFilterMissesOrFailures(t *testing.T) {
	cfg := sqsTestConfig(t)
	misses := make([]*types.SQSMessage, 10)
	for i := range misses {
		misses[i] = &types.SQSMessage{MessageId: fmt.Sprint(i), ReceiptHandle: fmt.Sprint(i), Body: `{"type":"other"}`}
	}
	filtered := &types.EventSourceMapping{UUID: "f", QueueName: "q", FunctionName: "fn", BatchSize: 10,
		FilterCriteria: &types.FilterCriteria{Filters: []types.FilterCriteriaFilter{{Pattern: `{"body":{"type":["wanted"]}}`}}}}
	p := newPoller(filtered, &mockSQS{messages: misses}, nil, &mockLambda{}, NewStore(cfg), nil, nil)
	if p.poll() {
		t.Fatal("a full batch of released filter misses must not trigger an immediate re-poll")
	}

	full := make([]*types.SQSMessage, 10)
	for i := range full {
		full[i] = &types.SQSMessage{MessageId: fmt.Sprint(i), ReceiptHandle: fmt.Sprint(i), Body: "{}"}
	}
	failing := &types.EventSourceMapping{UUID: "e", QueueName: "q", FunctionName: "fn", BatchSize: 10}
	p = newPoller(failing, &mockSQS{messages: full}, nil, &mockLambda{invokeErr: fmt.Errorf("boom")}, NewStore(cfg), nil, nil)
	if p.poll() {
		t.Fatal("a failed batch must back off to the tick, not re-poll at full speed")
	}
}

func sqsTestConfig(t *testing.T) *config.Config {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	return cfg
}
