package sqs

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestRejectedQueueAttributesLeaveStateUnchanged(t *testing.T) {
	for _, attrs := range []map[string]string{
		{"FifoThroughputLimit": "perMessageGroupId"},
		{"DeduplicationScope": "messageGroup", "FifoThroughputLimit": "invalid"},
	} {
		t.Run(fmt.Sprint(attrs), func(t *testing.T) {
			s := newTestStore()
			before, err := s.CreateQueue("q.fifo", map[string]string{"FifoQueue": "true"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			s.dirty.Store(false)
			if err := s.SetQueueAttributes("q.fifo", attrs); err == nil {
				t.Fatal("expected invalid attributes to fail")
			}
			after, err := s.GetQueue("q.fifo")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected update changed config: before=%+v after=%+v", before, after)
			}
			if s.dirty.Load() {
				t.Fatal("rejected update marked the store dirty")
			}
		})
	}
}

func TestQueueConfigsAreSnapshots(t *testing.T) {
	s := newTestStore()
	tags := map[string]string{"env": "dev"}
	created, err := s.CreateQueue("q", nil, tags)
	if err != nil {
		t.Fatal(err)
	}
	tags["env"] = "caller mutation"
	if created.Tags["env"] != "dev" {
		t.Fatal("created config shares caller-owned tags")
	}

	readers := map[string]func() (*types.QueueConfig, error){
		"created":  func() (*types.QueueConfig, error) { return created, nil },
		"existing": func() (*types.QueueConfig, error) { return s.CreateQueue("q", nil, nil) },
		"get":      func() (*types.QueueConfig, error) { return s.GetQueue("q") },
		"list":     func() (*types.QueueConfig, error) { return s.ListQueues("q")[0], nil },
	}
	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			q, err := read()
			if err != nil {
				t.Fatal(err)
			}
			q.VisibilityTimeout = 99
			q.Tags["env"] = "snapshot mutation"
			stored, err := s.GetQueue("q")
			if err != nil {
				t.Fatal(err)
			}
			if stored.VisibilityTimeout != 30 || stored.Tags["env"] != "dev" {
				t.Fatalf("snapshot changed stored config: %+v", stored)
			}
		})
	}

	snapshot, err := s.GetQueue("q")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TagQueue("q", map[string]string{"env": "updated"}); err != nil {
		t.Fatal(err)
	}
	if err := s.IncrementProcessedCount("q", 1); err != nil {
		t.Fatal(err)
	}
	if snapshot.Tags["env"] != "dev" || snapshot.ProcessedCount != 0 {
		t.Fatal("stored updates changed an earlier snapshot")
	}
}

func TestQueueConfigSnapshotsDuringUpdates(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q", nil, map[string]string{"env": "dev"}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	readers := []func() (*types.QueueConfig, error){
		func() (*types.QueueConfig, error) { return s.GetQueue("q") },
		func() (*types.QueueConfig, error) { return s.CreateQueue("q", nil, nil) },
		func() (*types.QueueConfig, error) { return s.ListQueues("")[0], nil },
	}
	for _, read := range readers {
		wg.Go(func() {
			<-start
			for range 100 {
				q, err := read()
				if err != nil {
					t.Error(err)
					return
				}
				if q.VisibilityTimeout != 30 && q.VisibilityTimeout != 45 {
					t.Errorf("invalid snapshot visibility: %d", q.VisibilityTimeout)
				}
				if q.Tags["env"] != "dev" && q.Tags["env"] != "updated" {
					t.Errorf("invalid snapshot tags: %v", q.Tags)
				}
				if q.ProcessedCount < 0 || q.ProcessedCount > 100 {
					t.Errorf("invalid snapshot processed count: %d", q.ProcessedCount)
				}
			}
		})
	}
	wg.Go(func() {
		<-start
		for range 100 {
			if err := s.SetQueueAttributes("q", map[string]string{"VisibilityTimeout": "45"}); err != nil {
				t.Error(err)
				return
			}
			if err := s.TagQueue("q", map[string]string{"env": "updated"}); err != nil {
				t.Error(err)
				return
			}
			if err := s.IncrementProcessedCount("q", 1); err != nil {
				t.Error(err)
				return
			}
		}
	})
	close(start)
	wg.Wait()
}

func TestPeekMessageAttributesAreIsolated(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q", nil, nil); err != nil {
		t.Fatal(err)
	}
	attrs := map[string]*types.MessageAttribute{
		"blob": {DataType: "Binary", BinaryValue: []byte{1, 2}},
		"text": {DataType: "String", StringValue: "original"},
		"nil":  nil,
	}
	if _, err := s.SendMessage("q", "body", 0, attrs, "", ""); err != nil {
		t.Fatal(err)
	}
	peek, err := s.PeekMessages("q", 1)
	if err != nil || len(peek) != 1 {
		t.Fatalf("peek: %v %v", peek, err)
	}
	peek[0].MessageAttributes["blob"].BinaryValue[0] = 9
	peek[0].MessageAttributes["text"].StringValue = "mutated"
	delete(peek[0].MessageAttributes, "nil")

	msgs, err := s.ReceiveMessage("q", 1, 30)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("receive: %v %v", msgs, err)
	}
	if !reflect.DeepEqual(msgs[0].MessageAttributes, attrs) {
		t.Fatalf("peek changed stored attributes: %v", msgs[0].MessageAttributes)
	}
}
