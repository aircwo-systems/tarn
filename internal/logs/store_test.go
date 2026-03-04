package logs

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/openstack-project/openstack/internal/config"
)

func TestCreateGroup(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/aws/lambda/myFunc")

	groups := s.ListGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "/aws/lambda/myFunc" {
		t.Fatalf("expected group name /aws/lambda/myFunc, got %s", groups[0].Name)
	}
}

func TestCreateGroupIdempotent(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/aws/lambda/myFunc")
	s.CreateGroup("/aws/lambda/myFunc")

	groups := s.ListGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group after duplicate create, got %d", len(groups))
	}
}

func TestDeleteGroup(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/aws/lambda/myFunc")
	_ = s.DeleteGroup("/aws/lambda/myFunc")

	groups := s.ListGroups()
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups after delete, got %d", len(groups))
	}
}

func TestPutAndGetLogEvents(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	events := []LogEvent{
		{Timestamp: now, Message: "first", Level: LevelINFO},
		{Timestamp: now.Add(time.Second), Message: "second", Level: LevelWARN},
		{Timestamp: now.Add(2 * time.Second), Message: "third", Level: LevelERROR},
	}
	s.PutLogEvents("/test", "stream1", events)

	result, total := s.GetLogEvents("/test", nil)
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 events, got %d", len(result))
	}
	if result[0].Message != "first" {
		t.Errorf("expected first event 'first', got '%s'", result[0].Message)
	}
	if result[2].Message != "third" {
		t.Errorf("expected third event 'third', got '%s'", result[2].Message)
	}
}

func TestRingBufferOverflow(t *testing.T) {
	maxEvents := 5
	s := NewStore(maxEvents)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	for i := 0; i < 8; i++ {
		s.PutLogEvents("/test", "stream1", []LogEvent{
			{Timestamp: now.Add(time.Duration(i) * time.Second), Message: fmt.Sprintf("event-%d", i), Level: LevelINFO},
		})
	}

	result, total := s.GetLogEvents("/test", nil)
	if total != maxEvents {
		t.Fatalf("expected total %d (buffer full), got %d", maxEvents, total)
	}
	// Should have events 3,4,5,6,7 (oldest 0,1,2 evicted)
	if result[0].Message != "event-3" {
		t.Errorf("expected oldest surviving event 'event-3', got '%s'", result[0].Message)
	}
	if result[4].Message != "event-7" {
		t.Errorf("expected newest event 'event-7', got '%s'", result[4].Message)
	}
}

func TestFilterByLevel(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	s.PutLogEvents("/test", "stream1", []LogEvent{
		{Timestamp: now, Message: "info msg", Level: LevelINFO},
		{Timestamp: now.Add(time.Second), Message: "error msg", Level: LevelERROR},
		{Timestamp: now.Add(2 * time.Second), Message: "warn msg", Level: LevelWARN},
		{Timestamp: now.Add(3 * time.Second), Message: "another error", Level: LevelERROR},
	})

	result, total := s.GetLogEvents("/test", &LogFilter{Level: LevelERROR})
	if total != 2 {
		t.Fatalf("expected 2 ERROR events, got %d", total)
	}
	for _, evt := range result {
		if evt.Level != LevelERROR {
			t.Errorf("expected ERROR level, got %s", evt.Level)
		}
	}
}

func TestFilterByPattern(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	s.PutLogEvents("/test", "stream1", []LogEvent{
		{Timestamp: now, Message: "request started", Level: LevelINFO},
		{Timestamp: now.Add(time.Second), Message: "processing timeout", Level: LevelERROR},
		{Timestamp: now.Add(2 * time.Second), Message: "request completed", Level: LevelINFO},
	})

	result, total := s.GetLogEvents("/test", &LogFilter{Pattern: "timeout"})
	if total != 1 {
		t.Fatalf("expected 1 event matching 'timeout', got %d", total)
	}
	if !strings.Contains(result[0].Message, "timeout") {
		t.Errorf("expected message to contain 'timeout', got '%s'", result[0].Message)
	}
}

func TestFilterByPatternCaseInsensitive(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	s.PutLogEvents("/test", "stream1", []LogEvent{
		{Timestamp: now, Message: "TIMEOUT occurred", Level: LevelERROR},
		{Timestamp: now.Add(time.Second), Message: "all good", Level: LevelINFO},
	})

	result, _ := s.GetLogEvents("/test", &LogFilter{Pattern: "timeout"})
	if len(result) != 1 {
		t.Fatalf("expected case-insensitive match, got %d results", len(result))
	}
}

func TestFilterByTimeRange(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	s.PutLogEvents("/test", "stream1", []LogEvent{
		{Timestamp: base, Message: "before", Level: LevelINFO},
		{Timestamp: base.Add(time.Hour), Message: "during", Level: LevelINFO},
		{Timestamp: base.Add(2 * time.Hour), Message: "after", Level: LevelINFO},
	})

	startTime := base.Add(30 * time.Minute)
	endTime := base.Add(90 * time.Minute)
	result, total := s.GetLogEvents("/test", &LogFilter{StartTime: &startTime, EndTime: &endTime})
	if total != 1 {
		t.Fatalf("expected 1 event in time range, got %d", total)
	}
	if result[0].Message != "during" {
		t.Errorf("expected 'during', got '%s'", result[0].Message)
	}
}

func TestFilterByStream(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	s.PutLogEvents("/test", "stream-a", []LogEvent{
		{Timestamp: now, Message: "from a", Level: LevelINFO},
	})
	s.PutLogEvents("/test", "stream-b", []LogEvent{
		{Timestamp: now.Add(time.Second), Message: "from b", Level: LevelINFO},
	})

	result, _ := s.GetLogEvents("/test", &LogFilter{StreamName: "stream-a"})
	if len(result) != 1 {
		t.Fatalf("expected 1 event from stream-a, got %d", len(result))
	}
	if result[0].Message != "from a" {
		t.Errorf("expected 'from a', got '%s'", result[0].Message)
	}
}

func TestPaginationOffsetAndLimit(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		s.PutLogEvents("/test", "stream1", []LogEvent{
			{Timestamp: now.Add(time.Duration(i) * time.Second), Message: fmt.Sprintf("event-%d", i), Level: LevelINFO},
		})
	}

	result, total := s.GetLogEvents("/test", &LogFilter{Offset: 3, Limit: 4})
	if total != 10 {
		t.Fatalf("expected total 10, got %d", total)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 events (offset=3, limit=4), got %d", len(result))
	}
	if result[0].Message != "event-3" {
		t.Errorf("expected 'event-3', got '%s'", result[0].Message)
	}
	if result[3].Message != "event-6" {
		t.Errorf("expected 'event-6', got '%s'", result[3].Message)
	}
}

func TestListStreams(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	s.PutLogEvents("/test", "stream-b", []LogEvent{
		{Timestamp: now, Message: "b", Level: LevelINFO},
	})
	s.PutLogEvents("/test", "stream-a", []LogEvent{
		{Timestamp: now, Message: "a", Level: LevelINFO},
	})

	streams := s.ListStreams("/test")
	if len(streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(streams))
	}
	// Should be sorted alphabetically
	if streams[0].Name != "stream-a" {
		t.Errorf("expected first stream 'stream-a', got '%s'", streams[0].Name)
	}
}

func TestGetGroup(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test")

	now := time.Now().UTC()
	s.PutLogEvents("/test", "stream1", []LogEvent{
		{Timestamp: now, Message: "hello", Level: LevelINFO},
	})

	summary, err := s.GetGroup("/test")
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.EventCount != 1 {
		t.Errorf("expected 1 event, got %d", summary.EventCount)
	}
	if summary.StreamCount != 1 {
		t.Errorf("expected 1 stream, got %d", summary.StreamCount)
	}
}

func TestGetGroupNotFound(t *testing.T) {
	s := NewStore(100)
	summary, err := s.GetGroup("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if summary != nil {
		t.Fatalf("expected nil summary for nonexistent group, got %+v", summary)
	}
}

func TestIngestContainerLogs(t *testing.T) {
	svc := NewService(testConfig())

	rawLogs := `START RequestId: abc-123
2026-01-15T10:30:00.000Z	abc-123	INFO	handling request
2026-01-15T10:30:01.000Z	abc-123	ERROR	something went wrong
END RequestId: abc-123
REPORT RequestId: abc-123	Duration: 50ms`

	svc.IngestContainerLogs("myFunc", "2026/01/15/[abc123]", rawLogs)

	events, total := svc.GetLogEvents("/aws/lambda/myFunc", nil)
	if total != 5 {
		t.Fatalf("expected 5 events, got %d", total)
	}

	// Check that ERROR level was detected
	foundError := false
	outputCount := 0
	runtimeCount := 0
	var outputMessages []string
	for _, evt := range events {
		if evt.Level == LevelERROR {
			foundError = true
		}
		if evt.Source == SourceOutput {
			outputCount++
			outputMessages = append(outputMessages, evt.Message)
		}
		if evt.Source == SourceRuntime {
			runtimeCount++
		}
	}
	if !foundError {
		t.Error("expected at least one ERROR-level event")
	}
	if outputCount != 2 {
		t.Fatalf("expected 2 output events, got %d", outputCount)
	}
	if runtimeCount != 3 {
		t.Fatalf("expected 3 runtime events, got %d", runtimeCount)
	}
	if len(outputMessages) != 2 || outputMessages[0] != "handling request" || outputMessages[1] != "something went wrong" {
		t.Fatalf("expected normalized output messages, got %#v", outputMessages)
	}
}

func TestDetectLevel(t *testing.T) {
	tests := []struct {
		msg   string
		level LogLevel
	}{
		{"START RequestId: abc-123", LevelINFO},
		{"END RequestId: abc-123", LevelINFO},
		{"REPORT RequestId: abc-123 Duration: 50ms", LevelINFO},
		{"ERROR: something failed", LevelERROR},
		{"WARNING: deprecated API", LevelWARN},
		{"DEBUG: trace info", LevelDEBUG},
		{"just a normal log line", LevelINFO},
		{"TypeError: cannot read property", LevelERROR},
		{"Exception in handler", LevelERROR},
	}

	for _, tt := range tests {
		got := detectLevel(tt.msg)
		if got != tt.level {
			t.Errorf("detectLevel(%q) = %s, want %s", tt.msg, got, tt.level)
		}
	}
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.LogsMaxEventsPerGroup = 1000
	return cfg
}
