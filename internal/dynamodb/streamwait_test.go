package dynamodb

import (
	"strconv"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// newStreamWaitStore builds a store with one streamed table and returns its
// stream ARN.
func newStreamWaitStore(t *testing.T) (*Store, string) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false

	store := NewStore(cfg)
	table, err := store.CreateTable(testTableDefinition())
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return store, table.LatestStreamArn
}

type streamBatchResult struct {
	records   []*types.StreamRecord
	next      string
	err       error
	waitedFor time.Duration
}

func streamBatchAsync(store *Store, arn, lastSequence string, limit, waitSec int, cancel <-chan struct{}) <-chan streamBatchResult {
	out := make(chan streamBatchResult, 1)
	go func() {
		start := time.Now()
		records, next, err := store.StreamBatchUntil(arn, lastSequence, limit, waitSec, cancel)
		out <- streamBatchResult{records, next, err, time.Since(start)}
	}()
	return out
}

func TestStreamBatchUntilWakesOnAppend(t *testing.T) {
	store, arn := newStreamWaitStore(t)
	res := streamBatchAsync(store, arn, "", 10, 20, nil)

	// Give the reader time to park on the stream, then write behind its back.
	time.Sleep(50 * time.Millisecond)
	if _, err := store.PutItem("orders", testItem("acct#1", "order#1", "PENDING", 1, "1"), "", nil, nil, "NONE"); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-res:
		if r.err != nil || len(r.records) != 1 {
			t.Fatalf("got %d records, err %v", len(r.records), r.err)
		}
		// The wait is 20s, so anything near that means the append did not wake it.
		if r.waitedFor > 2*time.Second {
			t.Fatalf("waited %s for a record appended at 50ms", r.waitedFor)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream batch did not wake when a record was appended")
	}
}

func TestStreamBatchUntilCancel(t *testing.T) {
	store, arn := newStreamWaitStore(t)
	cancel := make(chan struct{})
	res := streamBatchAsync(store, arn, "", 10, 20, cancel)
	time.Sleep(20 * time.Millisecond)
	close(cancel)

	select {
	case r := <-res:
		if r.err != nil || len(r.records) != 0 {
			t.Fatalf("got %d records, err %v, want an empty result", len(r.records), r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not end the stream batch wait")
	}
}

func TestStreamBatchUntilGivesUpAtItsDeadline(t *testing.T) {
	store, arn := newStreamWaitStore(t)

	r := <-streamBatchAsync(store, arn, "", 10, 1, nil)
	if r.err != nil || len(r.records) != 0 {
		t.Fatalf("got %d records, err %v, want an empty result", len(r.records), r.err)
	}
	if r.waitedFor > 3*time.Second {
		t.Fatalf("waited %s on a 1s deadline", r.waitedFor)
	}
}

func TestStreamBatchUntilWakesWhenTableDeleted(t *testing.T) {
	store, arn := newStreamWaitStore(t)
	res := streamBatchAsync(store, arn, "", 10, 20, nil)
	time.Sleep(20 * time.Millisecond)
	if _, err := store.DeleteTable("orders"); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-res:
		if r.err == nil {
			t.Fatal("want table-not-found after the table was deleted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream batch kept waiting on a deleted table")
	}
}

func TestStreamBatchUntilWakesWhenStreamDisabled(t *testing.T) {
	store, arn := newStreamWaitStore(t)
	res := streamBatchAsync(store, arn, "", 10, 20, nil)
	time.Sleep(20 * time.Millisecond)
	if _, err := store.UpdateTableDefinition("orders", &types.DynamoDBStreamSpecification{StreamEnabled: false}); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-res:
		if r.err == nil {
			t.Fatal("want stream-not-found after the stream was disabled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream batch kept waiting on a disabled stream")
	}
}

func TestStreamBatchWithNoWaitLeavesNoChannel(t *testing.T) {
	store, arn := newStreamWaitStore(t)

	records, _, err := store.StreamBatch(arn, "", 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("batch: %d records, err %v", len(records), err)
	}
	// A one-shot read must not leave a channel behind for a signal to close,
	// which would pin a channel per stream for the life of the store.
	store.mu.RLock()
	waiting := len(store.wakes)
	store.mu.RUnlock()
	if waiting != 0 {
		t.Fatalf("%d streams left waiting after a plain StreamBatch", waiting)
	}
}

func TestStreamSignalWakesEveryReaderOnOneStream(t *testing.T) {
	store, arn := newStreamWaitStore(t)

	// Two readers park on the same stream, as two mappings on one stream do.
	readers := map[string]<-chan streamBatchResult{
		"first":  streamBatchAsync(store, arn, "", 10, 20, nil),
		"second": streamBatchAsync(store, arn, "", 10, 20, nil),
	}
	time.Sleep(50 * time.Millisecond)
	for i := range 5 {
		item := testItem("acct#1", "order#"+strconv.Itoa(i), "PENDING", i, "1")
		if _, err := store.PutItem("orders", item, "", nil, nil, "NONE"); err != nil {
			t.Fatal(err)
		}
	}

	// Closing collapses the burst into a single wake, so every reader is
	// released rather than only the one that happens to be reading. How many
	// records each sees depends on how far the writes got before it re-read,
	// which is why the assertion is "at least one" and not a count: a real
	// consumer loops and drains the rest.
	for name, res := range readers {
		select {
		case r := <-res:
			if r.err != nil {
				t.Fatalf("%s reader: %v", name, r.err)
			}
			if len(r.records) == 0 {
				t.Fatalf("%s reader woke but found no record", name)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s reader did not wake on the write burst", name)
		}
	}

	// Everything written is still there for a reader that comes back.
	records, _, err := store.StreamBatch(arn, "", 10)
	if err != nil || len(records) != 5 {
		t.Fatalf("stream holds %d records, want all 5 (err %v)", len(records), err)
	}
}
