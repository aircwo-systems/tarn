package sqs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/persist"
)

func newRetryTestStore(t *testing.T) (*Store, *config.Config) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	s := NewStore(cfg)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if _, err := s.CreateQueue("q", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendMessage("q", "pending", 0, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	return s, cfg
}

func waitForPersistedMessages(t *testing.T, path string, count int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		var snapshot struct {
			Queues []queueSnapshot `json:"queues"`
		}
		if err == nil && json.Unmarshal(data, &snapshot) == nil && len(snapshot.Queues) == 1 && len(snapshot.Queues[0].Messages) == count {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("automatic persistence did not save %d messages", count)
}

func TestFailedFlushRetainsDirtyAndRetriesWithoutMutation(t *testing.T) {
	s, cfg := newRetryTestStore(t)
	// Exclude the background writer while installing a deterministic filesystem
	// failure. Explicit Flush and automatic retry still use the same callback.
	s.flusher.Close()
	if err := os.RemoveAll(cfg.QueuesDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.QueuesDir(), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	s.flusher = persist.StartFlusher(&s.dirty, s.flushToDisk)
	s.Flush()
	if !s.dirty.Load() {
		t.Fatal("failed persistence cleared its retry signal")
	}
	if err := os.Remove(cfg.QueuesDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.QueuesDir(), 0755); err != nil {
		t.Fatal(err)
	}
	// No further queue mutation or manual flush should be necessary.
	waitForPersistedMessages(t, cfg.QueuesStatePath(), 1)
	reloaded := NewStore(cfg)
	if err := reloaded.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reloaded.Close)
	msgs, err := reloaded.PeekMessages("q", 1)
	if err != nil || len(msgs) != 1 || msgs[0].Body != "pending" {
		t.Fatalf("retry lost content: %v %v", msgs, err)
	}
}

func TestFailedRenameCleansTemporaryFileAndCloseRetries(t *testing.T) {
	s, cfg := newRetryTestStore(t)
	s.flusher.Close()
	if err := os.Remove(cfg.QueuesStatePath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfg.QueuesStatePath(), 0755); err != nil {
		t.Fatal(err)
	}
	s.flusher = persist.StartFlusher(&s.dirty, s.flushToDisk)
	s.Flush()
	if !s.dirty.Load() {
		t.Fatal("rename failure was not retained")
	}
	files, err := filepath.Glob(filepath.Join(cfg.QueuesDir(), "state-*.json.tmp"))
	if err != nil || len(files) != 0 {
		t.Fatalf("failed rename leaked temporary files: %v %v", files, err)
	}
	if err := os.Remove(cfg.QueuesStatePath()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	waitForPersistedMessages(t, cfg.QueuesStatePath(), 1)
}

func TestFailedFinalWriteRemainsDirtyAndCloseIsBounded(t *testing.T) {
	s, cfg := newRetryTestStore(t)
	s.flusher.Close()
	if err := os.RemoveAll(cfg.QueuesDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.QueuesDir(), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	s.flusher = persist.StartFlusher(&s.dirty, s.flushToDisk)
	s.Flush()
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close retried forever on a failed filesystem")
	}
	if !s.dirty.Load() {
		t.Fatal("failed final flush was treated as durable")
	}
	if err := os.Remove(cfg.QueuesDir()); err != nil {
		t.Fatal(err)
	}
	s.Flush() // Explicit recovery remains available after the worker is closed.
	waitForPersistedMessages(t, cfg.QueuesStatePath(), 1)
}

func TestMutationDuringFlushSchedulesAnotherSnapshot(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = true
	s := NewStore(cfg)
	written := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	s.flusher = persist.StartFlusher(&s.dirty, func() {
		s.flushToDisk()
		once.Do(func() { close(written); <-release })
	})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); s.Close() })
	if _, err := s.CreateQueue("q", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendMessage("q", "first", 0, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.Flush(); close(done) }()
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("first snapshot did not finish")
	}
	if _, err := s.SendMessage("q", "second", 0, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	if !s.dirty.Load() {
		t.Fatal("concurrent mutation was not retained")
	}
	unblock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("flush did not return")
	}
	waitForPersistedMessages(t, cfg.QueuesStatePath(), 2)
}
