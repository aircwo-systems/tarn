package sqs

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
)

func TestReaperWakesFIFOAfterExpiryStaleParkingAndDLQ(t *testing.T) {
	for _, reason := range []string{"expiry", "stale", "dlq"} {
		t.Run(reason, func(t *testing.T) {
			svc, source, target := newDLQTestService(t, true, true, 1)
			if reason != "dlq" {
				if err := svc.SetQueueAttributes(source, map[string]string{"RedrivePolicy": "{}"}); err != nil {
					t.Fatal(err)
				}
			}
			first := receiveDLQTestMessage(t, svc, source, 60)
			if _, err := svc.SendMessage(source, "second", 0, nil, "group", "second"); err != nil {
				t.Fatal(err)
			}
			msgs, wake, _, err := svc.store.receive(source, 10, 30)
			if err != nil || len(msgs) != 0 || wake == nil {
				t.Fatalf("FIFO not blocked: %v %v", msgs, err)
			}
			q := svc.store.queues[source]
			q.mu.Lock()
			for _, msg := range q.messages {
				if msg.MessageId == first.MessageId {
					if reason == "expiry" {
						msg.ExpiresAt = nowMs() - 1
					} else {
						msg.VisibleAt = nowMs()
						if reason == "stale" {
							msg.ApproximateReceiveCount = defaultStaleReceiveCount
						}
					}
				}
			}
			q.mu.Unlock()
			svc.store.Reap()
			select {
			case <-wake:
			default:
				t.Fatal("reaper did not notify FIFO waiters")
			}
			msgs, err = svc.ReceiveMessage(source, 10, 30, 0)
			if err != nil || len(msgs) != 1 || msgs[0].Body != "second" {
				t.Fatalf("following message not available: %v %v", msgs, err)
			}
			if reason == "dlq" {
				assertDLQMessages(t, svc, target, 1)
			}
		})
	}
}

func TestUnchangedReaperDoesNotWakeFIFO(t *testing.T) {
	svc, source, _ := newDLQTestService(t, true, true, 1)
	receiveDLQTestMessage(t, svc, source, 60)
	_, wake, _, err := svc.store.receive(source, 1, 30)
	if err != nil || wake == nil {
		t.Fatal(err)
	}
	svc.store.dirty.Store(false)
	svc.store.Reap()
	select {
	case <-wake:
		t.Fatal("unchanged queue woke its long poll")
	default:
	}
	if svc.store.dirty.Load() {
		t.Fatal("unchanged reap marked store dirty")
	}
}

func TestFIFOLongPollUsesBlockerRetentionDeadline(t *testing.T) {
	svc, source, _ := newDLQTestService(t, true, true, 1)
	first := receiveDLQTestMessage(t, svc, source, 60)
	if _, err := svc.SendMessage(source, "second", 0, nil, "group", "second"); err != nil {
		t.Fatal(err)
	}
	q := svc.store.queues[source]
	q.mu.Lock()
	expiry := nowMs() + 80
	q.messages[0].ExpiresAt = expiry
	q.mu.Unlock()
	_, _, next, err := svc.store.receive(source, 1, 30)
	if err != nil || next != expiry {
		t.Fatalf("next wake %d, want expiry %d: %v", next, expiry, err)
	}
	msgs, err := svc.ReceiveMessage(source, 1, 30, 2)
	if err != nil || len(msgs) != 1 || msgs[0].MessageId == first.MessageId || msgs[0].Body != "second" {
		t.Fatalf("retention did not unblock without reaper: %v %v", msgs, err)
	}
}

func TestStopJoinsActiveReaperAndIsIdempotent(t *testing.T) {
	svc, source, _ := newDLQTestService(t, false, true, 1)
	receiveDLQTestMessage(t, svc, source, 0)
	q := svc.store.queues[source]
	q.mu.Lock()
	locked := true
	defer func() {
		if locked {
			q.mu.Unlock()
		}
	}()
	svc.Start()
	svc.Start() // Only one reaper may own the completion channel.
	deadline := time.Now().Add(3 * time.Second)
	active := false
	for time.Now().Before(deadline) {
		if !svc.store.mu.TryLock() {
			active = true
			break
		}
		svc.store.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	if !active {
		t.Fatal("reaper never started its locked queue pass")
	}
	stopped := make(chan struct{})
	go func() { svc.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned while a reap was still active")
	case <-time.After(20 * time.Millisecond):
	}
	q.mu.Unlock()
	locked = false
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not join completed reaper")
	}
	select {
	case <-svc.reaperDone:
	default:
		t.Fatal("completion not signaled")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); svc.Stop(); svc.Start() }()
	}
	wg.Wait()
}

func TestCloseStopsReaperAndFlushesPendingState(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Close)
	svc.Start()
	if _, err := svc.CreateQueue("pending", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendMessage("pending", "final message", 0, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	svc.Close()
	svc.Close()
	svc.Stop()
	select {
	case <-svc.reaperDone:
	default:
		t.Fatal("Close did not stop reaper")
	}
	reloaded := NewStore(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reloaded.Close)
	msgs, err := reloaded.PeekMessages("pending", 10)
	if err != nil || len(msgs) != 1 || msgs[0].Body != "final message" {
		t.Fatalf("final state missing: %v %v", msgs, err)
	}
	if _, err := os.Stat(cfg.QueuesStatePath()); err != nil {
		t.Fatal(err)
	}
}

func TestStopBeforeStartAndConcurrentLifecycle(t *testing.T) {
	for _, stopFirst := range []bool{true, false} {
		t.Run(fmt.Sprint("stopFirst=", stopFirst), func(t *testing.T) {
			svc := NewService(config.Default())
			t.Cleanup(svc.Close)
			if stopFirst {
				svc.Stop()
			}
			var wg sync.WaitGroup
			for range 16 {
				wg.Add(1)
				go func() { defer wg.Done(); svc.Start(); svc.Stop() }()
			}
			wg.Wait()
			svc.Close()
		})
	}
}
