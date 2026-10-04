package sqs

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestMoveToDLQIfExceededPreservesRetryCount(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	svc := NewService(cfg)
	if err := svc.Init(); err != nil {
		t.Fatalf("init service: %v", err)
	}

	dlq, err := svc.CreateQueue("orders-dlq", nil, nil)
	if err != nil {
		t.Fatalf("create dlq: %v", err)
	}

	redrivePolicy, err := json.Marshal(map[string]any{
		"deadLetterTargetArn": dlq.QueueArn,
		"maxReceiveCount":     3,
	})
	if err != nil {
		t.Fatalf("marshal redrive policy: %v", err)
	}

	if _, err := svc.CreateQueue("orders", map[string]string{"RedrivePolicy": string(redrivePolicy)}, nil); err != nil {
		t.Fatalf("create source queue: %v", err)
	}
	if _, err := svc.SendMessage("orders", `{"orderId":"B2","fail":true}`, 0, nil, "", ""); err != nil {
		t.Fatalf("send message: %v", err)
	}

	var msg *types.SQSMessage
	for i := 0; i < 3; i++ {
		msgs, err := svc.ReceiveMessage("orders", 1, 0, 0)
		if err != nil {
			t.Fatalf("receive #%d: %v", i+1, err)
		}
		if len(msgs) != 1 {
			t.Fatalf("receive #%d len = %d, want 1", i+1, len(msgs))
		}
		msg = msgs[0]
	}

	moved, dlqName, err := svc.MoveToDLQIfExceeded("orders", msg)
	if err != nil {
		t.Fatalf("move to dlq: %v", err)
	}
	if !moved {
		t.Fatalf("expected message to move to dlq")
	}
	if dlqName != "orders-dlq" {
		t.Fatalf("dlq name = %q, want %q", dlqName, "orders-dlq")
	}

	dlqMsgs, err := svc.PeekMessages("orders-dlq", 10)
	if err != nil {
		t.Fatalf("peek dlq messages: %v", err)
	}
	if len(dlqMsgs) != 1 {
		t.Fatalf("dlq messages len = %d, want 1", len(dlqMsgs))
	}
	if dlqMsgs[0].MessageAttributes[dlqRetryCountAttribute] == nil {
		t.Fatalf("expected dlq retry count attribute, got %+v", dlqMsgs[0].MessageAttributes)
	}
	if dlqMsgs[0].MessageAttributes[dlqRetryCountAttribute].StringValue != "3" {
		t.Fatalf("retry count attr = %q, want %q", dlqMsgs[0].MessageAttributes[dlqRetryCountAttribute].StringValue, "3")
	}
}

func TestDeleteMessageByID(t *testing.T) {
	svc := NewService(&config.Config{DataDir: t.TempDir()})
	defer svc.Close()
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreateQueue("work", nil, nil); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	msg1, err := svc.SendMessage("work", "hello", 0, nil, "", "")
	if err != nil {
		t.Fatalf("send 1: %v", err)
	}
	_, err = svc.SendMessage("work", "world", 0, nil, "", "")
	if err != nil {
		t.Fatalf("send 2: %v", err)
	}

	if err := svc.DeleteMessageByID("work", msg1.MessageId); err != nil {
		t.Fatalf("delete message by id: %v", err)
	}

	peeked, err := svc.PeekMessages("work", 10)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if len(peeked) != 1 {
		t.Fatalf("expected 1 remaining message, got %d", len(peeked))
	}
	if peeked[0].Body != "world" {
		t.Fatalf("remaining message body = %q, want %q", peeked[0].Body, "world")
	}
}

func TestRedriveMessages(t *testing.T) {
	svc := NewService(&config.Config{DataDir: t.TempDir()})
	defer svc.Close()
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreateQueue("main", nil, nil); err != nil {
		t.Fatalf("create main queue: %v", err)
	}
	if _, err := svc.CreateQueue("dlq", nil, nil); err != nil {
		t.Fatalf("create dlq: %v", err)
	}

	attrs := map[string]*types.MessageAttribute{
		"TarnRetryCount": {DataType: "Number", StringValue: "3"},
		"OriginalAction": {DataType: "String", StringValue: "process"},
	}

	for i := 0; i < 3; i++ {
		if _, err := svc.SendMessage("dlq", fmt.Sprintf("dead-item-%d", i), 0, attrs, "", ""); err != nil {
			t.Fatalf("send dlq %d: %v", i, err)
		}
	}

	moved, err := svc.RedriveMessages("dlq", "main", 0)
	if err != nil {
		t.Fatalf("redrive: %v", err)
	}
	if moved != 3 {
		t.Fatalf("moved = %d, want 3", moved)
	}

	// dlq should now have 0 messages
	dlqPeek, _ := svc.PeekMessages("dlq", 10)
	if len(dlqPeek) != 0 {
		t.Fatalf("dlq still has %d messages", len(dlqPeek))
	}

	// main queue should have 3 messages with TarnRetryCount stripped
	mainPeek, _ := svc.PeekMessages("main", 10)
	if len(mainPeek) != 3 {
		t.Fatalf("main has %d messages, want 3", len(mainPeek))
	}
	for _, m := range mainPeek {
		if m.MessageAttributes["TarnRetryCount"] != nil {
			t.Errorf("expected TarnRetryCount to be stripped, got %+v", m.MessageAttributes)
		}
		if m.MessageAttributes["OriginalAction"] == nil || m.MessageAttributes["OriginalAction"].StringValue != "process" {
			t.Errorf("expected OriginalAction attribute preserved, got %+v", m.MessageAttributes)
		}
	}
}
