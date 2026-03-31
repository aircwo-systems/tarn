package sqs

import (
	"encoding/json"
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
