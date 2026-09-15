package sqs

import (
	"errors"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
)

func TestDisruptorBlocksSendAtFullRate(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}
	if _, err := svc.CreateQueue("orders", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	svc.SetDisruptorRule(Rule{QueueName: "orders", Enabled: true, FailureRate: 100, Code: DisruptCodeServiceUnavailable})

	_, err := svc.SendMessage("orders", "hello", 0, nil, "", "")
	if err == nil {
		t.Fatalf("expected disrupted send to fail")
	}
	var derr *DisruptError
	if !errors.As(err, &derr) {
		t.Fatalf("expected *DisruptError, got %T", err)
	}
	if derr.Code != DisruptCodeServiceUnavailable || derr.StatusCode != 503 {
		t.Fatalf("unexpected disrupt error: %+v", derr)
	}

	// Nothing should have been persisted.
	msgs, err := svc.PeekMessages("orders", 10)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("disrupted send persisted %d messages", len(msgs))
	}
}

func TestDisruptorDisabledLetsSendThrough(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}
	if _, err := svc.CreateQueue("orders", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	svc.SetDisruptorRule(Rule{QueueName: "orders", Enabled: false, FailureRate: 100, Code: DisruptCodeInternalError})
	if _, err := svc.SendMessage("orders", "hello", 0, nil, "", ""); err != nil {
		t.Fatalf("disabled disruptor should not fail sends: %v", err)
	}

	svc.SetDisruptorRule(Rule{QueueName: "orders", Enabled: true, FailureRate: 0, Code: DisruptCodeInternalError})
	if _, err := svc.SendMessage("orders", "hello", 0, nil, "", ""); err != nil {
		t.Fatalf("zero-rate disruptor should not fail sends: %v", err)
	}

	if ok := svc.ClearDisruptorRule("orders"); !ok {
		t.Fatalf("expected rule to exist")
	}
	if _, err := svc.SendMessage("orders", "hello", 0, nil, "", ""); err != nil {
		t.Fatalf("cleared disruptor should not fail sends: %v", err)
	}
	if ok := svc.ClearDisruptorRule("orders"); ok {
		t.Fatalf("second clear should report missing rule")
	}
}

func TestDisruptorIsPerQueue(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}
	for _, q := range []string{"a", "b"} {
		if _, err := svc.CreateQueue(q, nil, nil); err != nil {
			t.Fatalf("create queue %s: %v", q, err)
		}
	}

	svc.SetDisruptorRule(Rule{QueueName: "a", Enabled: true, FailureRate: 100, Code: DisruptCodeOverLimit})

	if _, err := svc.SendMessage("a", "x", 0, nil, "", ""); err == nil {
		t.Fatalf("expected queue a to fail")
	}
	if _, err := svc.SendMessage("b", "x", 0, nil, "", ""); err != nil {
		t.Fatalf("queue b should be unaffected: %v", err)
	}
}
