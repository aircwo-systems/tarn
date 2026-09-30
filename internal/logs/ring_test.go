package logs

import (
	"fmt"
	"testing"
	"time"
)

func putN(s *Store, group string, from, n int, at func(i int) time.Time) {
	events := make([]LogEvent, 0, n)
	for i := from; i < from+n; i++ {
		events = append(events, LogEvent{Timestamp: at(i), Message: fmt.Sprintf("m%d", i), Level: LevelINFO})
	}
	s.PutLogEvents(group, "s", events)
}

func TestLogGroupGrowsOnDemand(t *testing.T) {
	s := NewStore(10000)
	s.CreateGroup("g")
	if c := cap(s.groups["g"].events); c != 0 {
		t.Fatalf("new group reserved %d events up front", c)
	}
	putN(s, "g", 0, 3, func(int) time.Time { return time.Now() })
	if c := cap(s.groups["g"].events); c > initialLogCapacity {
		t.Fatalf("3 events allocated room for %d", c)
	}
}

func TestLogGroupWrapsAfterGrowing(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("g")
	base := time.Now()
	putN(s, "g", 0, 150, func(i int) time.Time { return base.Add(time.Duration(i) * time.Millisecond) })

	events, total, _ := s.GetLogEvents("g", &LogFilter{})
	if total != 100 || len(events) != 100 {
		t.Fatalf("kept %d events (total %d), want the newest 100", len(events), total)
	}
	if events[0].Message != "m50" || events[99].Message != "m149" {
		t.Fatalf("order after wrap: first %s last %s, want m50..m149", events[0].Message, events[99].Message)
	}
	if c := cap(s.groups["g"].events); c != 100 {
		t.Fatalf("buffer capacity %d, want exactly maxEvents once full", c)
	}
}

func TestPruneKeepsBufferWhenNothingExpired(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("g")
	putN(s, "g", 0, 10, func(int) time.Time { return time.Now() })
	before := &s.groups["g"].events[0]

	if n := s.PruneOlderThan(time.Now().Add(-time.Hour)); n != 0 {
		t.Fatalf("pruned %d, want 0", n)
	}
	if &s.groups["g"].events[0] != before {
		t.Fatal("prune rebuilt a buffer it removed nothing from")
	}
}

func TestPruneKeepsNewestInOrderAndShrinks(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("g")
	base := time.Now().Add(-time.Hour)
	// 150 events wrap the ring; the first 140 are old.
	putN(s, "g", 0, 150, func(i int) time.Time {
		if i < 140 {
			return base.Add(time.Duration(i) * time.Millisecond)
		}
		return time.Now().Add(time.Duration(i) * time.Millisecond)
	})

	if n := s.PruneOlderThan(time.Now().Add(-time.Minute)); n != 90 {
		t.Fatalf("pruned %d, want 90 of the 100 held", n)
	}
	events, _, _ := s.GetLogEvents("g", &LogFilter{})
	if len(events) != 10 || events[0].Message != "m140" || events[9].Message != "m149" {
		t.Fatalf("survivors %d, first %v", len(events), events)
	}
	if c := cap(s.groups["g"].events); c > initialLogCapacity {
		t.Fatalf("pruned group still holds room for %d events", c)
	}

	// It keeps working as a growing buffer afterwards.
	putN(s, "g", 150, 5, func(int) time.Time { return time.Now() })
	events, _, _ = s.GetLogEvents("g", &LogFilter{})
	if len(events) != 15 || events[14].Message != "m154" {
		t.Fatalf("after pruning, appends read back as %d events ending %s", len(events), events[len(events)-1].Message)
	}
}

func TestClearGroupReleasesBuffer(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("g")
	putN(s, "g", 0, 50, func(int) time.Time { return time.Now() })
	if err := s.ClearGroup("g"); err != nil {
		t.Fatal(err)
	}
	if s.groups["g"].events != nil {
		t.Fatal("clear kept the buffer")
	}
	putN(s, "g", 0, 1, func(int) time.Time { return time.Now() })
	if events, _, _ := s.GetLogEvents("g", &LogFilter{}); len(events) != 1 {
		t.Fatalf("after clear got %d events", len(events))
	}
}
