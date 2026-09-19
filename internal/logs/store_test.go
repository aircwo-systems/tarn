package logs

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
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

	result, total, hasMore := s.GetLogEvents("/test", nil)
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if hasMore {
		t.Fatal("expected no additional pages")
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

	result, total, _ := s.GetLogEvents("/test", nil)
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

	result, total, _ := s.GetLogEvents("/test", &LogFilter{Level: LevelERROR})
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

	result, total, _ := s.GetLogEvents("/test", &LogFilter{Pattern: "timeout"})
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

	result, _, _ := s.GetLogEvents("/test", &LogFilter{Pattern: "timeout"})
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
	result, total, _ := s.GetLogEvents("/test", &LogFilter{StartTime: &startTime, EndTime: &endTime})
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

	result, _, _ := s.GetLogEvents("/test", &LogFilter{StreamName: "stream-a"})
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

	result, total, hasMore := s.GetLogEvents("/test", &LogFilter{Offset: 3, Limit: 4})
	if total != 10 {
		t.Fatalf("expected total 10, got %d", total)
	}
	if !hasMore {
		t.Fatal("expected additional pages after limited result")
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
		return
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

	events, total, _ := svc.GetLogEvents("/aws/lambda/myFunc", nil)
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
		got := DetectLevel(tt.msg)
		if got != tt.level {
			t.Errorf("DetectLevel(%q) = %s, want %s", tt.msg, got, tt.level)
		}
	}
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.LogsMaxEventsPerGroup = 1000
	return cfg
}

func TestPaginationCursorDescendingPagesByWindow(t *testing.T) {
	s := NewStore(500)
	s.CreateGroup("/test")

	base := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 300; i++ {
		s.PutLogEvents("/test", "stream1", []LogEvent{
			{
				Timestamp: base.Add(time.Duration(i) * time.Second),
				Message:   fmt.Sprintf("event-%03d", i),
				Level:     LevelINFO,
			},
		})
	}

	firstPage, total, hasMore := s.GetLogEvents("/test", &LogFilter{
		Limit: 200,
		Order: "desc",
	})
	if total != 300 {
		t.Fatalf("expected total 300, got %d", total)
	}
	if len(firstPage) != 200 {
		t.Fatalf("expected first page of 200, got %d", len(firstPage))
	}
	if !hasMore {
		t.Fatal("expected more pages after first descending page")
	}
	if firstPage[0].Message != "event-299" || firstPage[199].Message != "event-100" {
		t.Fatalf("unexpected descending first page bounds: first=%s last=%s", firstPage[0].Message, firstPage[199].Message)
	}

	cursor := firstPage[len(firstPage)-1].Timestamp
	secondPage, _, hasMore := s.GetLogEvents("/test", &LogFilter{
		Limit:  200,
		Order:  "desc",
		Cursor: &cursor,
	})
	if len(secondPage) != 100 {
		t.Fatalf("expected second page of 100, got %d", len(secondPage))
	}
	if hasMore {
		t.Fatal("expected no further pages after second descending page")
	}
	if secondPage[0].Message != "event-099" || secondPage[len(secondPage)-1].Message != "event-000" {
		t.Fatalf("unexpected descending second page bounds: first=%s last=%s", secondPage[0].Message, secondPage[len(secondPage)-1].Message)
	}
}

func TestGetAllLogEventsPaginationDescending(t *testing.T) {
	s := NewStore(500)
	s.CreateGroup("/a")
	s.CreateGroup("/b")

	base := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		s.PutLogEvents("/a", "stream-a", []LogEvent{{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Message:   fmt.Sprintf("a-%d", i),
			Level:     LevelINFO,
		}})
		s.PutLogEvents("/b", "stream-b", []LogEvent{{
			Timestamp: base.Add(time.Duration(i+10) * time.Second),
			Message:   fmt.Sprintf("b-%d", i),
			Level:     LevelINFO,
		}})
	}

	result, total, hasMore := s.GetAllLogEvents(&LogFilter{Limit: 5, Order: "desc"})
	if total != 12 {
		t.Fatalf("expected total 12, got %d", total)
	}
	if len(result) != 5 {
		t.Fatalf("expected 5 events, got %d", len(result))
	}
	if !hasMore {
		t.Fatal("expected more pages for all-log view")
	}
	if result[0].Message != "b-5" || result[4].Message != "b-1" {
		t.Fatalf("unexpected descending all-log page bounds: first=%s last=%s", result[0].Message, result[4].Message)
	}
}

func TestGetAllLogEventsWithGroupsFilter(t *testing.T) {
	s := NewStore(500)
	s.CreateGroup("/a")
	s.CreateGroup("/b")
	s.CreateGroup("/c")

	base := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		s.PutLogEvents("/a", "stream-a", []LogEvent{{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Message:   fmt.Sprintf("a-%d", i),
			Level:     LevelINFO,
		}})
		s.PutLogEvents("/b", "stream-b", []LogEvent{{
			Timestamp: base.Add(time.Duration(i+10) * time.Second),
			Message:   fmt.Sprintf("b-%d", i),
			Level:     LevelINFO,
		}})
		s.PutLogEvents("/c", "stream-c", []LogEvent{{
			Timestamp: base.Add(time.Duration(i+20) * time.Second),
			Message:   fmt.Sprintf("c-%d", i),
			Level:     LevelINFO,
		}})
	}

	// Filter to only groups /a and /c
	result, total, _ := s.GetAllLogEvents(&LogFilter{Groups: []string{"/a", "/c"}, Order: "asc"})
	if total != 6 {
		t.Fatalf("expected total 6, got %d", total)
	}
	if len(result) != 6 {
		t.Fatalf("expected 6 events, got %d", len(result))
	}
	for _, ev := range result {
		if !strings.HasPrefix(ev.StreamName, "/a/") && !strings.HasPrefix(ev.StreamName, "/c/") {
			t.Fatalf("unexpected stream in filtered groups: %s", ev.StreamName)
		}
	}
}

func TestLevelMatchesCommaSet(t *testing.T) {
	if !levelMatches(LevelWARN, "ERROR,WARN") {
		t.Fatal("expected WARN to match ERROR,WARN")
	}
	if levelMatches(LevelINFO, "ERROR, WARN") {
		t.Fatal("expected INFO not to match ERROR, WARN")
	}
	if !levelMatches(LevelERROR, "ERROR") {
		t.Fatal("expected single level match")
	}
}

func TestContainsFold(t *testing.T) {
	cases := []struct {
		s      string
		sub    string
		expect bool
	}{
		{"Hello Bob World", "bob", true},
		{"Hello BOB World", "bob", true},
		{"Hello bob World", "BOB", true},
		{"Hello bOb World", "BoB", true},
		{"Hello World", "bob", false},
		{"bob", "bob", true},
		{"bob", "", true},
		{"", "bob", false},
		{"", "", true},
		{"a", "ab", false},
		{"prefix bob suffix", "bob", true},
		{"prefix BOB", "BOB", true},
		{"BOB suffix", "BOB", true},
		{"unicode Élément", "él", true},
	}

	for _, c := range cases {
		got := ContainsFold(c.s, c.sub)
		if got != c.expect {
			t.Errorf("ContainsFold(%q, %q) = %v; want %v", c.s, c.sub, got, c.expect)
		}
	}
}

func TestScanLogs(t *testing.T) {
	s := NewStore(500)
	s.CreateGroup("/aws/lambda/checkout")
	s.CreateGroup("/aws/lambda/auth")
	s.CreateGroup("/tarn/api")

	base := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

	// In checkout: 4 events, 3 contain "correlation-id"
	s.PutLogEvents("/aws/lambda/checkout", "stream-1", []LogEvent{
		{Timestamp: base.Add(1 * time.Second), Message: "User initialized order #101 correlation-id=corr-101", Level: LevelINFO},
		{Timestamp: base.Add(2 * time.Second), Message: "Processing payment CORRELATION-ID=CORR-101", Level: LevelINFO},
		{Timestamp: base.Add(3 * time.Second), Message: "Payment succeeded", Level: LevelINFO},
		{Timestamp: base.Add(4 * time.Second), Message: "Order confirmed correlation-id=corr-101", Level: LevelINFO},
	})

	// In auth: 2 events, 1 contains "correlation-id"
	s.PutLogEvents("/aws/lambda/auth", "stream-1", []LogEvent{
		{Timestamp: base.Add(10 * time.Second), Message: "Login attempt correlation-id=corr-auth-99", Level: LevelINFO},
		{Timestamp: base.Add(11 * time.Second), Message: "Token generated for user: alice", Level: LevelINFO},
	})

	// In api: 2 events, 0 contain "correlation-id"
	s.PutLogEvents("/tarn/api", "requests", []LogEvent{
		{Timestamp: base.Add(20 * time.Second), Message: "GET /checkout 200 45ms", Level: LevelINFO},
		{Timestamp: base.Add(21 * time.Second), Message: "POST /auth 200 12ms", Level: LevelINFO},
	})

	scan := s.ScanLogs(&LogScanFilter{
		Pattern: "correlation-id",
	})

	if scan.Pattern != "correlation-id" {
		t.Fatalf("expected pattern correlation-id, got %s", scan.Pattern)
	}
	if scan.TotalMatches != 4 {
		t.Fatalf("expected 4 total matches, got %d", scan.TotalMatches)
	}
	if scan.TotalScanned != 8 {
		t.Fatalf("expected 8 total scanned, got %d", scan.TotalScanned)
	}
	if len(scan.Groups) != 2 {
		t.Fatalf("expected 2 groups with matches, got %d", len(scan.Groups))
	}

	// Should be sorted by match count descending (/aws/lambda/checkout with 3 first)
	if scan.Groups[0].GroupName != "/aws/lambda/checkout" || scan.Groups[0].MatchCount != 3 {
		t.Fatalf("expected checkout first with 3 matches, got %+v", scan.Groups[0])
	}
	if scan.Groups[1].GroupName != "/aws/lambda/auth" || scan.Groups[1].MatchCount != 1 {
		t.Fatalf("expected auth second with 1 match, got %+v", scan.Groups[1])
	}

	// Verify sample events
	if len(scan.Groups[0].SampleEvents) != 3 {
		t.Fatalf("expected 3 sample events for checkout, got %d", len(scan.Groups[0].SampleEvents))
	}
	// Last match should be the most recent one (t = base + 4s)
	expectedLastMatch := base.Add(4 * time.Second)
	if scan.Groups[0].LastMatch == nil || !scan.Groups[0].LastMatch.Equal(expectedLastMatch) {
		t.Fatalf("expected lastMatch %v, got %v", expectedLastMatch, scan.Groups[0].LastMatch)
	}
}

func TestScanLogsFilteredByLevelAndGroup(t *testing.T) {
	s := NewStore(500)
	s.CreateGroup("/a")
	s.CreateGroup("/b")

	base := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	s.PutLogEvents("/a", "stream-1", []LogEvent{
		{Timestamp: base.Add(1 * time.Second), Message: "Error handler correlation-id=corr-err-1", Level: LevelERROR},
		{Timestamp: base.Add(2 * time.Second), Message: "Info handler correlation-id=corr-info-1", Level: LevelINFO},
	})
	s.PutLogEvents("/b", "stream-1", []LogEvent{
		{Timestamp: base.Add(3 * time.Second), Message: "Error handler correlation-id=corr-err-2", Level: LevelERROR},
	})

	// Filter by level ERROR
	res := s.ScanLogs(&LogScanFilter{
		Pattern: "correlation-id",
		Level:   LevelERROR,
	})
	if res.TotalMatches != 2 {
		t.Fatalf("expected 2 ERROR matches, got %d", res.TotalMatches)
	}

	// Filter by group /a only
	resGroup := s.ScanLogs(&LogScanFilter{
		Pattern: "correlation-id",
		Groups:  []string{"/a"},
	})
	if resGroup.TotalMatches != 2 {
		t.Fatalf("expected 2 matches in group /a, got %d", resGroup.TotalMatches)
	}
	if len(resGroup.Groups) != 1 || resGroup.Groups[0].GroupName != "/a" {
		t.Fatalf("expected only group /a in results, got %+v", resGroup.Groups)
	}
}

func BenchmarkScanLogs(b *testing.B) {
	s := NewStore(10000)
	s.CreateGroup("/group-1")
	s.CreateGroup("/group-2")
	s.CreateGroup("/group-3")

	base := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	for g := 1; g <= 3; g++ {
		grp := fmt.Sprintf("/group-%d", g)
		events := make([]LogEvent, 10000)
		for i := 0; i < 10000; i++ {
			msg := "normal log event processing data"
			if i%20 == 0 {
				msg = fmt.Sprintf("action correlation-id=corr-%d on item %d", i, i)
			}
			events[i] = LogEvent{
				Timestamp: base.Add(time.Duration(i) * time.Millisecond),
				Message:   msg,
				Level:     LevelINFO,
			}
		}
		s.PutLogEvents(grp, "stream-1", events)
	}

	filter := &LogScanFilter{
		Pattern: "correlation-id",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := s.ScanLogs(filter)
		if res.TotalMatches != 1500 {
			b.Fatalf("expected 1500 matches, got %d", res.TotalMatches)
		}
	}
}

func TestFlexibleWhitespacePatternMatching(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test/json")

	now := time.Now().UTC()
	s.PutLogEvents("/test/json", "stream-1", []LogEvent{
		{Timestamp: now, Message: `{"level":"warn","message":"something failed","count":42}`, Level: LevelWARN},
		{Timestamp: now.Add(time.Second), Message: `2026-04-01 12:00:00 [INFO] key = value processed`, Level: LevelINFO},
	})

	cases := []struct {
		pattern string
		match   bool
	}{
		{`"level": "warn"`, true},
		{`"level":"warn"`, true},
		{`"count":  42`, true},
		{`"count":42`, true},
		{`key=value`, true},
		{`key = value`, true},
		{`"level": "error"`, false},
	}

	for _, c := range cases {
		events, total, _ := s.GetLogEvents("/test/json", &LogFilter{Pattern: c.pattern})
		matched := total > 0 && len(events) > 0
		if matched != c.match {
			t.Errorf("GetLogEvents with pattern %q matched=%v, want %v", c.pattern, matched, c.match)
		}

		scan := s.ScanLogs(&LogScanFilter{Pattern: c.pattern})
		scanMatched := scan.TotalMatches > 0
		if scanMatched != c.match {
			t.Errorf("ScanLogs with pattern %q matched=%v, want %v", c.pattern, scanMatched, c.match)
		}
	}
}

func TestMultiTermPatternMatching(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("/test/multi")

	now := time.Now().UTC()
	s.PutLogEvents("/test/multi", "stream-1", []LogEvent{
		{Timestamp: now, Message: `{"level":"warn","message":"database timeout","status":500,"service":"orders-api"}`, Level: LevelWARN},
		{Timestamp: now.Add(time.Second), Message: `{"level":"info","message":"database connected","status":200,"service":"orders-api"}`, Level: LevelINFO},
		{Timestamp: now.Add(2 * time.Second), Message: `2026-04-01 12:00:00 [ERROR] orders-api failed with Internal Server Error`, Level: LevelERROR},
	})

	cases := []struct {
		pattern string
		count   int
	}{
		{`"level": "warn" "status": 500`, 1},
		{`"status": 500 orders-api`, 1},
		{`orders-api "status": 200`, 1},
		{`orders-api 500`, 1},
		{`orders-api failed`, 1},
		{`orders-api "Internal Server Error"`, 1},
		{`orders-api nonexistent`, 0},
	}

	for _, c := range cases {
		events, total, _ := s.GetLogEvents("/test/multi", &LogFilter{Pattern: c.pattern})
		if total != c.count || len(events) != c.count {
			t.Errorf("GetLogEvents with pattern %q got total=%d, want %d", c.pattern, total, c.count)
		}

		scan := s.ScanLogs(&LogScanFilter{Pattern: c.pattern})
		if scan.TotalMatches != c.count {
			t.Errorf("ScanLogs with pattern %q got TotalMatches=%d, want %d", c.pattern, scan.TotalMatches, c.count)
		}
	}
}
