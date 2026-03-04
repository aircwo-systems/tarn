package eventsource

import (
	"testing"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/pkg/types"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	s := NewStore(cfg)
	if err := s.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	return s
}

func TestStoreSaveAndGet(t *testing.T) {
	s := testStore(t)

	m := &types.EventSourceMapping{
		UUID:           "uuid-1",
		EventSourceArn: "arn:aws:sqs:us-east-1:000000000000:my-queue",
		FunctionName:   "my-func",
		QueueName:      "my-queue",
		BatchSize:      5,
		Enabled:        true,
		State:          "Enabled",
	}

	if err := s.Save(m); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Get("uuid-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.FunctionName != "my-func" {
		t.Fatalf("functionName = %q, want %q", got.FunctionName, "my-func")
	}
	if got.BatchSize != 5 {
		t.Fatalf("batchSize = %d, want 5", got.BatchSize)
	}
}

func TestStoreGetNotFound(t *testing.T) {
	s := testStore(t)

	_, err := s.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent UUID")
	}
}

func TestStoreList(t *testing.T) {
	s := testStore(t)

	if err := s.Save(&types.EventSourceMapping{UUID: "a", FunctionName: "fn-a"}); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := s.Save(&types.EventSourceMapping{UUID: "b", FunctionName: "fn-b"}); err != nil {
		t.Fatalf("save b: %v", err)
	}

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
}

func TestStoreDelete(t *testing.T) {
	s := testStore(t)

	if err := s.Save(&types.EventSourceMapping{UUID: "del-1"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := s.Delete("del-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get("del-1"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestStoreDeleteNotFound(t *testing.T) {
	s := testStore(t)

	if err := s.Delete("nonexistent"); err == nil {
		t.Fatal("expected error deleting nonexistent")
	}
}

func TestStorePersistenceRoundTrip(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true

	s1 := NewStore(cfg)
	if err := s1.Init(); err != nil {
		t.Fatalf("init s1: %v", err)
	}

	if err := s1.Save(&types.EventSourceMapping{
		UUID:         "persist-1",
		FunctionName: "fn-persist",
		QueueName:    "q-persist",
		BatchSize:    7,
		State:        "Enabled",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Create a new store instance to verify it loads from disk
	s2 := NewStore(cfg)
	if err := s2.Init(); err != nil {
		t.Fatalf("init s2: %v", err)
	}

	got, err := s2.Get("persist-1")
	if err != nil {
		t.Fatalf("get after reload: %v", err)
	}
	if got.FunctionName != "fn-persist" {
		t.Fatalf("functionName = %q, want %q", got.FunctionName, "fn-persist")
	}
	if got.BatchSize != 7 {
		t.Fatalf("batchSize = %d, want 7", got.BatchSize)
	}
}
