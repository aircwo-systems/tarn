package eventsource

import (
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
	"testing"
)

func TestPollerPartialFailuresRequireOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "ReportBatchItemFailures"}[enabled], func(t *testing.T) {
			cfg := config.Default()
			cfg.DataDir = t.TempDir()
			mapping := &types.EventSourceMapping{UUID: "audit", QueueName: "events", FunctionName: "handler", BatchSize: 2}
			if enabled {
				mapping.FunctionResponseTypes = []string{"ReportBatchItemFailures"}
			}
			q := &mockSQS{messages: []*types.SQSMessage{{MessageId: "failed", ReceiptHandle: "failed-handle"}, {MessageId: "ok", ReceiptHandle: "ok-handle"}}}
			p := newPoller(mapping, q, nil, &partialFailureLambda{messageID: "failed"}, NewStore(cfg), nil, nil)
			p.poll()
			want := 2
			if enabled {
				want = 1
			}
			if len(q.deleted) != want {
				t.Fatalf("deleted=%v want %d", q.deleted, want)
			}
			if enabled && q.deleted[0] != "ok-handle" {
				t.Fatalf("deleted failed message: %v", q.deleted)
			}
		})
	}
}

func TestMappingTagsSurvivePollerSaveAndRestart(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	store := NewStore(cfg)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, store, nil, nil, nil)
	m, err := svc.CreateMapping("arn:aws:sqs:us-east-1:000000000000:events", "handler", "handler", 2, 0, false, nil, nil, MappingOptions{FunctionResponseTypes: []string{"ReportBatchItemFailures"}, Tags: map[string]string{"project": "events"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateTags(m.EventSourceMappingArn, map[string]string{"project": "updated"}, nil); err != nil {
		t.Fatal(err)
	}
	// A poller holds an older snapshot of tags when it writes its status.
	m.LastProcessingResult = "OK"
	if err := store.Save(m); err != nil {
		t.Fatal(err)
	}
	store.Close()
	restored := NewStore(cfg)
	if err := restored.Init(); err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := restored.Get(m.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tags["project"] != "updated" || len(got.FunctionResponseTypes) != 1 || got.EventSourceMappingArn != m.EventSourceMappingArn {
		t.Fatalf("restored=%+v", got)
	}
	got.Tags["project"] = "caller mutation"
	again, _ := restored.Get(m.UUID)
	if again.Tags["project"] != "updated" {
		t.Fatal("tag map aliased with store")
	}
}
