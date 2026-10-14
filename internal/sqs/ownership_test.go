package sqs

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestSendAndReceiveMessagesAreDetached(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("owned.fifo", map[string]string{"FifoQueue": "true"}, nil); err != nil {
		t.Fatal(err)
	}
	attrs := map[string]*types.MessageAttribute{
		"text":  {DataType: "String", StringValue: "original"},
		"blob":  {DataType: "Binary", BinaryValue: []byte{1, 2}},
		"empty": {DataType: "Binary", BinaryValue: []byte{}},
		"nil":   nil,
	}
	want := cloneMessageAttributes(attrs)
	sent, err := s.SendMessage("owned.fifo", "body", 0, attrs, "group", "dedup")
	if err != nil {
		t.Fatal(err)
	}
	attrs["blob"].BinaryValue[0] = 9
	attrs["text"].StringValue = "caller mutation"
	delete(attrs, "nil")
	sent.Body = "returned mutation"
	sent.MessageAttributes["blob"].BinaryValue[0] = 8
	duplicate, err := s.SendMessage("owned.fifo", "ignored", 0, nil, "group", "dedup")
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.MessageId != sent.MessageId || duplicate.Body != "body" || !reflect.DeepEqual(duplicate.MessageAttributes, want) {
		t.Fatalf("stored send changed: %+v", duplicate)
	}
	duplicate.MessageAttributes["text"].StringValue = "duplicate mutation"
	s.dirty.Store(false)
	if _, err := s.SendMessage("owned.fifo", "ignored", 0, nil, "group", "dedup"); err != nil {
		t.Fatal(err)
	}
	if s.dirty.Load() {
		t.Fatal("duplicate send marked unchanged state dirty")
	}
	first, err := s.ReceiveMessage("owned.fifo", 1, 0)
	if err != nil || len(first) != 1 {
		t.Fatalf("first receive: %v %v", first, err)
	}
	receipt := first[0].ReceiptHandle
	first[0].MessageAttributes["blob"].BinaryValue[0] = 7
	second, err := s.ReceiveMessage("owned.fifo", 1, 30)
	if err != nil || len(second) != 1 {
		t.Fatalf("second receive: %v %v", second, err)
	}
	if first[0].ReceiptHandle != receipt || first[0].ApproximateReceiveCount != 1 || second[0].ReceiptHandle == receipt || second[0].ApproximateReceiveCount != 2 {
		t.Fatal("receive results changed after a new delivery")
	}
	if !reflect.DeepEqual(second[0].MessageAttributes, want) {
		t.Fatalf("receive mutated stored attributes: %+v", second[0].MessageAttributes)
	}
	if err := s.DeleteMessage("owned.fifo", receipt); err == nil {
		t.Fatal("obsolete receipt deleted current delivery")
	}
	if err := s.DeleteMessage("owned.fifo", second[0].ReceiptHandle); err != nil {
		t.Fatal(err)
	}
}

func TestMessageSnapshotsDuringConcurrentDeliveries(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q", nil, nil); err != nil {
		t.Fatal(err)
	}
	sent, err := s.SendMessage("q", "body", 0, map[string]*types.MessageAttribute{"blob": {DataType: "Binary", BinaryValue: []byte{1}}}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := s.ReceiveMessage("q", 1, 0)
	if err != nil || len(batch) != 1 {
		t.Fatalf("receive: %v %v", batch, err)
	}
	want, _ := json.Marshal(batch[0])
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 1000 {
			got, err := json.Marshal(batch[0])
			if err != nil || string(got) != string(want) {
				t.Error("earlier response changed during redelivery")
				return
			}
			sent.Body = "caller owned"
			sent.MessageAttributes["blob"].BinaryValue[0] = 9
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range 1000 {
			msgs, err := s.ReceiveMessage("q", 1, 0)
			if err != nil || len(msgs) != 1 || msgs[0].Body != "body" || msgs[0].MessageAttributes["blob"].BinaryValue[0] != 1 {
				t.Error("caller mutation reached store")
				return
			}
		}
	}()
	close(start)
	wg.Wait()
}

func TestMessageAttributeNilAndEmptySnapshots(t *testing.T) {
	for _, attrs := range []map[string]*types.MessageAttribute{nil, {}} {
		s := newTestStore()
		if _, err := s.CreateQueue("q", nil, nil); err != nil {
			t.Fatal(err)
		}
		msg, err := s.SendMessage("q", "body", 0, attrs, "", "")
		if err != nil || !reflect.DeepEqual(msg.MessageAttributes, attrs) {
			t.Fatalf("attribute map shape changed: %v %v", msg, err)
		}
	}
}

func BenchmarkReceiveMessageSnapshots(b *testing.B) {
	for _, count := range []int{1, 10} {
		for _, attributes := range []bool{false, true} {
			b.Run(fmt.Sprintf("batch=%d/attributes=%t", count, attributes), func(b *testing.B) {
				s := newTestStore()
				if _, err := s.CreateQueue("q", nil, nil); err != nil {
					b.Fatal(err)
				}
				var attrs map[string]*types.MessageAttribute
				if attributes {
					attrs = map[string]*types.MessageAttribute{"text": {DataType: "String", StringValue: "original"}, "blob": {DataType: "Binary", BinaryValue: make([]byte, 256)}}
				}
				for range count {
					if _, err := s.SendMessage("q", "body", 0, attrs, "", ""); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := s.ReceiveMessage("q", count, 0); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
