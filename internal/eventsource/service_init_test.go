package eventsource

import (
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestServiceInitDedupesMappings(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true

	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	older := &types.EventSourceMapping{
		UUID:           "dup-old",
		EventSourceArn: "arn:aws:sqs:us-east-1:000000000000:orders",
		FunctionName:   "orders-handler",
		QueueName:      "orders",
		BatchSize:      1,
		Enabled:        true,
		State:          "Enabled",
		LastModified:   time.Now().UTC().Add(-time.Minute),
	}
	newer := &types.EventSourceMapping{
		UUID:           "dup-new",
		EventSourceArn: "arn:aws:sqs:us-east-1:000000000000:orders",
		FunctionName:   "arn:aws:lambda:us-east-1:000000000000:function:orders-handler",
		QueueName:      "orders",
		BatchSize:      3,
		Enabled:        true,
		State:          "Enabled",
		LastModified:   time.Now().UTC(),
	}

	if err := store.Save(older); err != nil {
		t.Fatalf("save older: %v", err)
	}
	if err := store.Save(newer); err != nil {
		t.Fatalf("save newer: %v", err)
	}

	svc := NewService(cfg, store, nil, nil)
	if err := svc.Init(); err != nil {
		t.Fatalf("service init: %v", err)
	}

	mappings := svc.ListMappings("", "")
	if len(mappings) != 1 {
		t.Fatalf("mapping count after dedupe = %d, want 1", len(mappings))
	}
	if mappings[0].UUID != "dup-new" {
		t.Fatalf("winner uuid = %q, want %q", mappings[0].UUID, "dup-new")
	}
	if mappings[0].FunctionName != "orders-handler" {
		t.Fatalf("normalized function name = %q, want %q", mappings[0].FunctionName, "orders-handler")
	}
}
