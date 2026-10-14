package sqs

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func newDLQTestService(t *testing.T, fifo, targetExists bool, maxReceive int) (*Service, string, string) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	svc := NewService(cfg)
	t.Cleanup(svc.Close)
	source, target := "source", "dlq"
	var attrs map[string]string
	if fifo {
		source += ".fifo"
		target += ".fifo"
		attrs = map[string]string{"FifoQueue": "true"}
	}
	if targetExists {
		if _, err := svc.CreateQueue(target, attrs, nil); err != nil {
			t.Fatal(err)
		}
	}
	sourceAttrs := map[string]string{"RedrivePolicy": fmt.Sprintf(`{"deadLetterTargetArn":"arn:aws:sqs:%s:%s:%s","maxReceiveCount":%d}`, cfg.Region, cfg.AccountID, target, maxReceive)}
	if fifo {
		sourceAttrs["FifoQueue"] = "true"
	}
	if _, err := svc.CreateQueue(source, sourceAttrs, nil); err != nil {
		t.Fatal(err)
	}
	return svc, source, target
}

func receiveDLQTestMessage(t *testing.T, svc *Service, source string, visibility int) *types.SQSMessage {
	t.Helper()
	if _, err := svc.SendMessage(source, "body", 0, map[string]*types.MessageAttribute{"blob": {DataType: "Binary", BinaryValue: []byte{1, 2}}}, "group", "dedup"); err != nil {
		t.Fatal(err)
	}
	msgs, err := svc.ReceiveMessage(source, 1, visibility, 0)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("receive: %v %v", msgs, err)
	}
	return msgs[0]
}

func assertDLQMessages(t *testing.T, svc *Service, name string, count int) []*types.SQSMessage {
	t.Helper()
	msgs, err := svc.PeekMessages(name, 100)
	if err != nil || len(msgs) != count {
		t.Fatalf("queue %s has %d messages, want %d: %v", name, len(msgs), count, err)
	}
	return msgs
}

func TestMissingDLQRetainsSourceForLaterRetry(t *testing.T) {
	for _, reaper := range []bool{false, true} {
		t.Run(fmt.Sprint("reaper=", reaper), func(t *testing.T) {
			svc, source, target := newDLQTestService(t, false, false, 1)
			msg := receiveDLQTestMessage(t, svc, source, 0)
			if reaper {
				svc.store.Reap()
			} else if moved, _, err := svc.MoveToDLQIfExceeded(source, msg); moved || err == nil {
				t.Fatalf("missing target: moved=%t err=%v", moved, err)
			}
			retained := assertDLQMessages(t, svc, source, 1)[0]
			if retained.ReceiptHandle != msg.ReceiptHandle || retained.ApproximateReceiveCount != 1 {
				t.Fatalf("failed transfer changed source: %+v", retained)
			}
			if _, err := svc.CreateQueue(target, nil, nil); err != nil {
				t.Fatal(err)
			}
			if reaper {
				svc.store.Reap()
			} else if moved, _, err := svc.MoveToDLQIfExceeded(source, msg); !moved || err != nil {
				t.Fatalf("retry: %t %v", moved, err)
			}
			assertDLQMessages(t, svc, source, 0)
			assertDLQMessages(t, svc, target, 1)
		})
	}
}

func TestDLQTransferRevalidatesReceiptAndStoredEligibility(t *testing.T) {
	svc, source, target := newDLQTestService(t, false, true, 2)
	first := receiveDLQTestMessage(t, svc, source, 0)
	first.ApproximateReceiveCount = 99
	if moved, _, err := svc.MoveToDLQIfExceeded(source, first); moved || err != nil {
		t.Fatalf("caller forged eligibility: %t %v", moved, err)
	}
	second, err := svc.ReceiveMessage(source, 1, 30, 0)
	if err != nil || len(second) != 1 {
		t.Fatalf("redelivery: %v %v", second, err)
	}
	if moved, _, err := svc.MoveToDLQIfExceeded(source, first); moved || err != nil {
		t.Fatalf("obsolete receipt transferred current delivery: %t %v", moved, err)
	}
	assertDLQMessages(t, svc, target, 0)
	second[0].Body = "caller cannot replace stored body"
	second[0].ApproximateReceiveCount = 0
	if moved, name, err := svc.MoveToDLQIfExceeded(source, second[0]); !moved || name != target || err != nil {
		t.Fatalf("valid receipt: %t %s %v", moved, name, err)
	}
	if moved, _, err := svc.MoveToDLQIfExceeded(source, second[0]); moved || err != nil {
		t.Fatalf("repeated transfer: %t %v", moved, err)
	}
	got := assertDLQMessages(t, svc, target, 1)[0]
	if got.Body != "body" || got.MessageAttributes[dlqRetryCountAttribute].StringValue != "2" {
		t.Fatalf("transfer used caller payload: %+v", got)
	}
}

func TestDLQBelowThresholdDoesNotRequireDestination(t *testing.T) {
	svc, source, _ := newDLQTestService(t, false, false, 2)
	msg := receiveDLQTestMessage(t, svc, source, 0)
	if moved, name, err := svc.MoveToDLQIfExceeded(source, msg); moved || name != "" || err != nil {
		t.Fatalf("below-threshold message required a DLQ: %t %s %v", moved, name, err)
	}
	svc.store.Reap()
	assertDLQMessages(t, svc, source, 1)
}

func TestPollerDLQTransferRetainsFIFOSendDeduplication(t *testing.T) {
	svc, source, target := newDLQTestService(t, true, true, 1)
	existing, err := svc.SendMessage(target, "already delivered", 0, nil, "group", "dedup")
	if err != nil {
		t.Fatal(err)
	}
	msg := receiveDLQTestMessage(t, svc, source, 30)
	if moved, _, err := svc.MoveToDLQIfExceeded(source, msg); !moved || err != nil {
		t.Fatalf("transfer: %t %v", moved, err)
	}
	got := assertDLQMessages(t, svc, target, 1)[0]
	if got.MessageId != existing.MessageId || got.Body != "already delivered" {
		t.Fatalf("FIFO destination duplicate changed: %+v", got)
	}
	assertDLQMessages(t, svc, source, 0)
}

func TestConcurrentReaperAndPollerDLQTransfersAreUnique(t *testing.T) {
	svc, source, target := newDLQTestService(t, false, true, 1)
	const count = 24
	var candidates []*types.SQSMessage
	for i := range count {
		if _, err := svc.SendMessage(source, fmt.Sprint(i), 0, nil, "group", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
		msgs, err := svc.ReceiveMessage(source, 1, 60, 0)
		if err != nil || len(msgs) != 1 {
			t.Fatalf("receive: %v %v", msgs, err)
		}
		candidates = append(candidates, msgs[0])
	}
	q := svc.store.queues[source]
	q.mu.Lock()
	for _, msg := range q.messages {
		msg.VisibleAt = nowMs()
	}
	q.mu.Unlock()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, msg := range candidates {
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, _, err := svc.MoveToDLQIfExceeded(source, msg); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range 3 {
			svc.store.Reap()
		}
	}()
	close(start)
	wg.Wait()
	assertDLQMessages(t, svc, source, 0)
	seen := make(map[string]bool)
	for _, msg := range assertDLQMessages(t, svc, target, count) {
		if seen[msg.Body] || msg.MessageAttributes[dlqRetryCountAttribute].StringValue != "1" {
			t.Fatalf("duplicate or bad metadata: %+v", msg)
		}
		seen[msg.Body] = true
	}
}

func TestDLQPathsPreserveTimingDisruptionAndTracing(t *testing.T) {
	for _, reaper := range []bool{false, true} {
		t.Run(fmt.Sprint("reaper=", reaper), func(t *testing.T) {
			svc, source, target := newDLQTestService(t, true, true, 1)
			traces := tracesvc.NewStore()
			t.Cleanup(func() { _ = traces.Close() })
			svc.SetTraceStore(traces)
			carrier := `{"x-datadog-trace-id":"1234","x-datadog-parent-id":"5678"}`
			attrs := map[string]*types.MessageAttribute{"_datadog": {DataType: "String", StringValue: carrier}, "blob": {DataType: "Binary", BinaryValue: []byte{1}}}
			if _, err := svc.SendMessage(source, "body", 0, attrs, "group", "dedup"); err != nil {
				t.Fatal(err)
			}
			msgs, err := svc.ReceiveMessage(source, 1, 60, 0)
			if err != nil || len(msgs) != 1 {
				t.Fatalf("receive: %v %v", msgs, err)
			}
			original := msgs[0]
			if err := svc.SetQueueAttributes(target, map[string]string{"DelaySeconds": "5", "MessageRetentionPeriod": "120"}); err != nil {
				t.Fatal(err)
			}
			svc.SetDisruptorRule(Rule{QueueName: target, Enabled: true, FailureRate: 100})
			if reaper {
				svc.store.Reap()
				assertDLQMessages(t, svc, target, 0)
				if err := svc.ChangeMessageVisibility(source, original.ReceiptHandle, 0); err != nil {
					t.Fatal(err)
				}
				svc.store.Reap()
			} else {
				moved, _, err := svc.MoveToDLQIfExceeded(source, original)
				var disrupted *DisruptError
				if moved || !errors.As(err, &disrupted) {
					t.Fatalf("poller skipped disruption: %t %v", moved, err)
				}
				assertDLQMessages(t, svc, source, 1)
				assertDLQMessages(t, svc, target, 0)
				svc.ClearDisruptorRule(target)
				if moved, _, err := svc.MoveToDLQIfExceeded(source, original); !moved || err != nil {
					t.Fatalf("retry: %t %v", moved, err)
				}
			}
			got := assertDLQMessages(t, svc, target, 1)[0]
			if got.MessageId == original.MessageId || got.MessageGroupId != "group" || got.MessageDeduplicationId != "dedup" || got.MessageAttributes[dlqRetryCountAttribute].StringValue != "1" || got.MessageAttributes["blob"].BinaryValue[0] != 1 {
				t.Fatalf("metadata lost: %+v", got)
			}
			if got.ExpiresAt-got.SentTimestamp != 120000 {
				t.Fatal("destination retention not refreshed")
			}
			if reaper && got.DelayUntil != got.SentTimestamp {
				t.Fatal("reaper unexpectedly applied destination send delay")
			}
			if !reaper && got.DelayUntil-got.SentTimestamp != 5000 {
				t.Fatal("poller lost destination send delay")
			}
			joined := traces.Recent(10)
			wantSpans := 2
			if reaper {
				wantSpans = 1
			}
			if len(joined) != 1 || len(joined[0].Spans) != wantSpans {
				t.Fatalf("wrong trace policy: %+v", joined)
			}
			assertDLQMessages(t, svc, source, 0)
			if _, err := svc.RedriveMessages(target, source, 0); err != nil {
				t.Fatal(err)
			}
			redriven := assertDLQMessages(t, svc, source, 1)[0]
			if redriven.MessageAttributes[dlqRetryCountAttribute] != nil || redriven.MessageAttributes["blob"].BinaryValue[0] != 1 || redriven.MessageId == got.MessageId {
				t.Fatalf("redrive changed metadata semantics: %+v", redriven)
			}
		})
	}
}

func TestDLQTransferFailureRetainsSource(t *testing.T) {
	svc, source, target := newDLQTestService(t, false, true, 1)
	msg := receiveDLQTestMessage(t, svc, source, 0)
	if err := svc.DeleteQueue(target); err != nil {
		t.Fatal(err)
	}
	target += ".fifo"
	if _, err := svc.CreateQueue(target, map[string]string{"FifoQueue": "true"}, nil); err != nil {
		t.Fatal(err)
	}
	q, err := svc.GetQueue(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetQueueAttributes(source, map[string]string{"RedrivePolicy": fmt.Sprintf(`{"deadLetterTargetArn":%q,"maxReceiveCount":1}`, q.QueueArn)}); err != nil {
		t.Fatal(err)
	}
	// Standard messages can lack a group, so this destination cannot accept one.
	stored := svc.store.queues[source]
	stored.mu.Lock()
	stored.messages[0].MessageGroupId = ""
	stored.mu.Unlock()
	if moved, _, err := svc.MoveToDLQIfExceeded(source, msg); moved || err == nil {
		t.Fatalf("invalid delivery: %t %v", moved, err)
	}
	svc.store.Reap()
	assertDLQMessages(t, svc, source, 1)
	assertDLQMessages(t, svc, target, 0)
	q, err = svc.GetQueue(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetQueueAttributes(source, map[string]string{"RedrivePolicy": fmt.Sprintf(`{"deadLetterTargetArn":%q,"maxReceiveCount":1}`, q.QueueArn)}); err != nil {
		t.Fatal(err)
	}
	if moved, _, err := svc.MoveToDLQIfExceeded(source, msg); moved || err == nil {
		t.Fatalf("self transfer: %t %v", moved, err)
	}
}

func TestOppositeDLQTransfersAndRedriveDoNotDeadlock(t *testing.T) {
	svc, source, target := newDLQTestService(t, false, true, 1)
	q, err := svc.GetQueue(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetQueueAttributes(target, map[string]string{"RedrivePolicy": fmt.Sprintf(`{"deadLetterTargetArn":%q,"maxReceiveCount":1}`, q.QueueArn)}); err != nil {
		t.Fatal(err)
	}
	a := receiveDLQTestMessage(t, svc, source, 0)
	b := receiveDLQTestMessage(t, svc, target, 0)
	start := make(chan struct{})
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		if _, _, err := svc.MoveToDLQIfExceeded(source, a); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		if _, _, err := svc.MoveToDLQIfExceeded(target, b); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		if _, err := svc.RedriveMessages(source, target, 1); err != nil {
			t.Error(err)
		}
	}()
	go func() { wg.Wait(); close(done) }()
	close(start)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("opposite transfers deadlocked")
	}
	left, err := svc.PeekMessages(source, 100)
	if err != nil {
		t.Fatal(err)
	}
	right, err := svc.PeekMessages(target, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(left)+len(right) != 2 {
		t.Fatal("opposite transfers lost or duplicated a message")
	}
}
