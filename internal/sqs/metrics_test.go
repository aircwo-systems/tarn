package sqs

import (
	"fmt"
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestQueueMessageCountsPreserveOverlappingStates(t *testing.T) {
	s := newTestStore()
	if _, err := s.CreateQueue("q", nil, nil); err != nil {
		t.Fatal(err)
	}
	now := nowMs()
	// Every combination of deletion, staleness, visibility, delay and expiry.
	for state := range 32 {
		m := &types.SQSMessage{VisibleAt: now, DelayUntil: now, ExpiresAt: now + 3600000}
		m.Deleted = state&1 != 0
		m.Stale = state&2 != 0
		if state&4 != 0 {
			m.VisibleAt = now + 3600000
		}
		if state&8 != 0 {
			m.DelayUntil = now + 3600000
		}
		if state&16 != 0 {
			m.ExpiresAt = now - 3600000
		}
		s.queues["q"].messages = append(s.queues["q"].messages, m)
	}
	names := []string{
		"ApproximateNumberOfMessages",
		"ApproximateNumberOfMessagesNotVisible",
		"ApproximateNumberOfMessagesDelayed",
		"ApproximateNumberOfMessagesStale",
	}
	// Eight entries are live. Two are visible, and four satisfy each other
	// count. Stale/visible and in-flight/delayed intentionally overlap.
	expected := map[string]string{names[0]: "2", names[1]: "4", names[2]: "4", names[3]: "4"}
	requests := [][]string{nil, {"All"}}
	for subset := range 16 {
		requested := []string{}
		for i, name := range names {
			if subset&(1<<i) != 0 {
				requested = append(requested, name)
			}
		}
		requests = append(requests, requested)
	}
	for i, requested := range requests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			got, err := s.GetQueueAttributes("q", requested)
			if err != nil {
				t.Fatal(err)
			}
			if i >= 2 && len(got) != len(requested) {
				t.Fatalf("returned %v for requested keys %v", got, requested)
			}
			keys := requested
			if i < 2 {
				keys = names
			}
			for _, name := range keys {
				if got[name] != expected[name] {
					t.Errorf("%s = %q, want %q", name, got[name], expected[name])
				}
			}
		})
	}
}

func BenchmarkGetQueueMessageCounts(b *testing.B) {
	for _, size := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			s := newTestStore()
			if _, err := s.CreateQueue("q", nil, nil); err != nil {
				b.Fatal(err)
			}
			now := nowMs()
			q := s.queues["q"]
			q.messages = make([]*types.SQSMessage, size)
			for i := range size {
				id := fmt.Sprint(i)
				m := &types.SQSMessage{
					MessageId: id, ReceiptHandle: id, Body: "body",
					VisibleAt: now, DelayUntil: now, ExpiresAt: now + 86400000,
				}
				switch i % 5 {
				case 1:
					m.VisibleAt = now + 3600000
				case 2:
					m.DelayUntil = now + 3600000
				case 3:
					m.Stale = true
				case 4:
					m.Deleted = true
				}
				q.messages[i] = m
			}
			requested := []string{
				"ApproximateNumberOfMessages", "ApproximateNumberOfMessagesNotVisible",
				"ApproximateNumberOfMessagesDelayed", "ApproximateNumberOfMessagesStale",
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := s.GetQueueAttributes("q", requested); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
