package logs

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	archivalLambda = "/aws/lambda/hce-lambda-certificate-archival-handler"
	archivalECS    = "/ecs/hce-data-archival-service"
	archivalCorrID = "45fd0000-0000-4000-8000-000000000001"
)

var archivalBase = time.Date(2026, 9, 29, 18, 11, 53, 0, time.UTC)

func certID(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-00000000000%x", n) }

// archivalStore builds the reference scenario from the log summary PRD: an
// ECS task publishes three certificate messages, a Lambda archives them, and
// one certificate is blocked with an error.
func archivalStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(1000)
	s.CreateGroup(archivalECS)
	s.CreateGroup(archivalLambda)
	s.CreateGroup("/tarn/system")
	s.CreateGroup("/tarn/api")

	at := func(ms int) time.Time { return archivalBase.Add(time.Duration(ms) * time.Millisecond) }

	s.PutLogEvents(archivalECS, "ecs/app/task1", []LogEvent{
		{Timestamp: at(0), Level: LevelINFO, Source: SourceOutput, Message: "Datadog tracing enabled: false"},
		{Timestamp: at(10), Level: LevelINFO, Source: SourceOutput, Message: `{"level":"info","message":"published certificate messages","count":3,"correlationId":"` + archivalCorrID + `"}`},
	})

	var lambda []LogEvent
	lambda = append(lambda, LogEvent{Timestamp: at(9000), Level: LevelINFO, Source: SourceRuntime, Message: "START RequestId: abc Version: $LATEST"})
	for i, n := range []int{0xa, 0xb, 0xc} {
		base := 10000 + i*100
		lambda = append(lambda, LogEvent{
			Timestamp: at(base), Level: LevelINFO, Source: SourceRuntime,
			Message: fmt.Sprintf(`{"level":"info","message":"certificate archival started","certificateId":"%s","correlationId":"%s"}`, certID(n), archivalCorrID),
		})
		certOutcome, citizenOutcome := `{"kind":"deleted","statusCode":204}`, `{"kind":"deleted"}`
		if n == 0xc {
			lambda = append(lambda, LogEvent{
				Timestamp: at(base + 10), Level: LevelERROR, Source: SourceRuntime,
				Message: fmt.Sprintf(`{"level":"error","message":"HRT_PPC citizen holds certificates of another type","certificateId":"%s","otherTypes":["LIS_HC2"]}`, certID(n)),
			})
			certOutcome, citizenOutcome = `{"kind":"blocked","reason":"crossTypeCertificatesHeld"}`, `{"kind":"blocked"}`
		}
		lambda = append(lambda, LogEvent{
			Timestamp: at(base + 20), Level: LevelINFO, Source: SourceRuntime,
			Message: fmt.Sprintf(`{"level":"info","message":"certificate archival finished","certificateId":"%s","citizenId":"cit-%d","correlationId":"%s","outcomes":{"certificate":%s,"citizen":{"citizenId":"cit-%d","outcome":%s},"notes":{"kind":"deleted","statusCode":204}}}`,
				certID(n), n, archivalCorrID, certOutcome, n, citizenOutcome),
		})
	}
	lambda = append(lambda,
		LogEvent{Timestamp: at(10500), Level: LevelINFO, Source: SourceRuntime, Message: `{"level":"info","message":"certificate archival batch completed","correlationId":"` + archivalCorrID + `"}`},
		LogEvent{Timestamp: at(10600), Level: LevelINFO, Source: SourceRuntime, Message: "REPORT RequestId: abc\tDuration: 12 ms"},
	)
	s.PutLogEvents(archivalLambda, "2026/09/29/[$LATEST]abc", lambda)

	s.PutLogEvents("/tarn/system", "system", []LogEvent{
		{Timestamp: at(5), Level: LevelINFO, Source: SourceSystem, Message: "rule fired"},
	})
	// Every AWS call the pipeline makes lands here, each line unique.
	s.PutLogEvents("/tarn/api", "requests", []LogEvent{
		{Timestamp: at(20), Level: LevelINFO, Source: SourceAPI, Message: "POST / 200 1.2345ms"},
		{Timestamp: at(30), Level: LevelINFO, Source: SourceAPI, Message: "POST / 200 2.3456ms"},
		{Timestamp: at(10700), Level: LevelERROR, Source: SourceAPI, Message: "POST / 500 3.4567ms"},
	})
	return s
}

func summarize(t *testing.T, s *Store, filter *LogFilter, opts SummaryOptions) *LogSummary {
	t.Helper()
	out, err := s.Summarize(filter, opts)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	return out
}

func TestSummaryGroupsByEntity(t *testing.T) {
	s := archivalStore(t)
	out := summarize(t, s, nil, SummaryOptions{
		GroupBy: "certificateId",
		Flatten: "kind",
		Fields:  []string{"message,outcomes,otherTypes"},
	})

	if out.Totals.Groups != 3 || len(out.Groups) != 3 {
		t.Fatalf("groups = %d (returned %d), want 3", out.Totals.Groups, len(out.Groups))
	}
	if out.Totals.ErrorGroups != 1 {
		t.Errorf("errorGroups = %d, want 1", out.Totals.ErrorGroups)
	}

	blocked := out.Groups[0]
	if blocked.Key != certID(0xc) {
		t.Fatalf("first group = %q, want the error group %q", blocked.Key, certID(0xc))
	}
	if blocked.Count != 3 || blocked.Levels["INFO"] != 2 || blocked.Levels["ERROR"] != 1 {
		t.Errorf("count/levels = %d %v, want 3 {INFO:2 ERROR:1}", blocked.Count, blocked.Levels)
	}
	if len(blocked.Errors) != 1 || blocked.Errors[0]["message"] != "HRT_PPC citizen holds certificates of another type" {
		t.Fatalf("errors = %v", blocked.Errors)
	}
	if other, _ := blocked.Errors[0]["otherTypes"].([]any); len(other) != 1 || other[0] != "LIS_HC2" {
		t.Errorf("error otherTypes = %v, want [LIS_HC2]", blocked.Errors[0]["otherTypes"])
	}
	outcomes, _ := blocked.Last["outcomes"].(map[string]any)
	if outcomes["certificate"] != "blocked" || outcomes["notes"] != "deleted" {
		t.Errorf("last.outcomes = %v, want certificate blocked, notes deleted", outcomes)
	}
	if citizen, _ := outcomes["citizen"].(map[string]any); citizen["outcome"] != "blocked" {
		t.Errorf("last.outcomes.citizen = %v, want outcome flattened to blocked", outcomes["citizen"])
	}
	if blocked.Last["message"] != "certificate archival finished" {
		t.Errorf("last.message = %v", blocked.Last["message"])
	}
	if _, ok := blocked.Last["timestamp"]; !ok {
		t.Error("sample events must always carry timestamp")
	}
	if _, ok := blocked.Last["citizenId"]; ok {
		t.Error("citizenId was not requested in fields and should be dropped")
	}
	if blocked.FirstAt != "2026-09-29T18:12:03.2Z" || blocked.LastAt != "2026-09-29T18:12:03.22Z" {
		t.Errorf("firstAt/lastAt = %s/%s", blocked.FirstAt, blocked.LastAt)
	}
	if len(blocked.LogGroups) != 1 || blocked.LogGroups[0] != archivalLambda {
		t.Errorf("logGroups = %v", blocked.LogGroups)
	}

	// The rest sort newest first.
	if out.Groups[1].Key != certID(0xb) || out.Groups[2].Key != certID(0xa) {
		t.Errorf("order = %s, %s; want b then a", out.Groups[1].Key, out.Groups[2].Key)
	}

	// ECS plaintext and JSON lines without certificateId stay visible.
	if out.Totals.Ungrouped != 4 {
		t.Errorf("ungrouped = %d, want 4 (2 ECS lines, batch completed, system)", out.Totals.Ungrouped)
	}
	if len(out.UngroupedSample) != 4 {
		t.Errorf("ungroupedSample = %d, want 4", len(out.UngroupedSample))
	}
	if out.UngroupedSample[0]["message"] != "Datadog tracing enabled: false" || out.UngroupedSample[0]["logGroup"] != archivalECS {
		t.Errorf("ungroupedSample[0] = %v", out.UngroupedSample[0])
	}
	if out.Totals.RuntimeFiltered != 5 {
		t.Errorf("runtimeFiltered = %d, want 5 (START, REPORT, 3 /tarn/api)", out.Totals.RuntimeFiltered)
	}
	if out.Totals.EventsScanned != out.Totals.EventsGrouped+out.Totals.Ungrouped {
		t.Errorf("totals do not add up: %+v", out.Totals)
	}

	raw, _ := json.Marshal(out)
	if len(raw) >= 3*1024 {
		t.Errorf("summary is %d bytes, want under 3 KB:\n%s", len(raw), raw)
	}
}

func TestSummaryGroupsByMessage(t *testing.T) {
	out := summarize(t, archivalStore(t), nil, SummaryOptions{GroupBy: "message"})

	counts := map[string]int{}
	for _, g := range out.Groups {
		counts[g.Key] = g.Count
	}
	want := map[string]int{
		"certificate archival finished":                      3,
		"certificate archival started":                       3,
		"HRT_PPC citizen holds certificates of another type": 1,
		"certificate archival batch completed":               1,
		"published certificate messages":                     1,
		"Datadog tracing enabled: false":                     1,
		"rule fired":                                         1,
	}
	for k, v := range want {
		if counts[k] != v {
			t.Errorf("count[%q] = %d, want %d", k, counts[k], v)
		}
	}
	if out.Totals.Ungrouped != 0 {
		t.Errorf("groupBy=message should group plain lines too, ungrouped = %d", out.Totals.Ungrouped)
	}
	if len(out.Groups) != len(want) {
		t.Errorf("groups = %d, want %d: /tarn/api request lines must not appear by default", len(out.Groups), len(want))
	}
	if out.Groups[0].Key != "HRT_PPC citizen holds certificates of another type" {
		t.Errorf("first group = %q, want the error line", out.Groups[0].Key)
	}
}

func TestSummaryJoinsServicesByCorrelationID(t *testing.T) {
	out := summarize(t, archivalStore(t), nil, SummaryOptions{GroupBy: "correlationId"})

	if len(out.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(out.Groups))
	}
	g := out.Groups[0]
	if len(g.LogGroups) != 2 || g.LogGroups[0] != archivalLambda || g.LogGroups[1] != archivalECS {
		t.Errorf("logGroups = %v, want both services", g.LogGroups)
	}
	if g.First["logGroup"] != archivalECS {
		t.Errorf("first = %v, want the ECS line tagged with its group", g.First)
	}
	if g.First["correlationId"] != archivalCorrID || g.First["level"] != "info" {
		t.Errorf("default fields should keep message, groupBy field and level: %v", g.First)
	}
}

func TestSummaryDottedPaths(t *testing.T) {
	s := archivalStore(t)
	out := summarize(t, s, nil, SummaryOptions{GroupBy: "outcomes.certificate.kind"})
	keys := map[string]int{}
	for _, g := range out.Groups {
		keys[g.Key] = g.Count
	}
	if keys["deleted"] != 2 || keys["blocked"] != 1 || len(keys) != 2 {
		t.Errorf("groups = %v, want deleted:2 blocked:1", keys)
	}

	out = summarize(t, s, nil, SummaryOptions{
		GroupBy: "certificateId",
		Fields:  []string{"outcomes.citizen.outcome.kind"},
	})
	last := out.Groups[0].Last
	outcomes, _ := last["outcomes"].(map[string]any)
	citizen, _ := outcomes["citizen"].(map[string]any)
	outcome, _ := citizen["outcome"].(map[string]any)
	if outcome["kind"] != "blocked" {
		t.Fatalf("last = %v, want outcomes.citizen.outcome.kind rebuilt", last)
	}
	if len(outcomes) != 1 || len(citizen) != 1 || len(last) != 2 {
		t.Errorf("projection kept more than the requested path: %v", last)
	}
}

func TestSummaryFlattenLeavesSharedDocsIntact(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("g")
	s.PutLogEvents("g", "s", []LogEvent{{
		Timestamp: archivalBase, Level: LevelERROR,
		Message: `{"id":"x","items":[{"kind":"a","n":1},{"other":{"kind":"b"}},"plain"],"keep":{"n":2}}`,
	}})

	out := summarize(t, s, nil, SummaryOptions{GroupBy: "id", Fields: []string{"*"}, Flatten: "kind"})
	g := out.Groups[0]
	items, _ := g.Last["items"].([]any)
	if len(items) != 3 || items[0] != "a" || items[2] != "plain" {
		t.Fatalf("items = %v, want flattened inside the array", items)
	}
	if other, _ := items[1].(map[string]any); other["other"] != "b" {
		t.Errorf("items[1] = %v, want nested kind flattened", items[1])
	}
	if keep, _ := g.Last["keep"].(map[string]any); keep["n"] == nil {
		t.Errorf("objects without kind must be unchanged: %v", g.Last["keep"])
	}
	// first, last and the error are one event; each must be flattened alike.
	if first, _ := g.First["items"].([]any); first[0] != "a" {
		t.Errorf("first.items = %v", first)
	}
	if errItems, _ := g.Errors[0]["items"].([]any); errItems[0] != "a" {
		t.Errorf("errors[0].items = %v", errItems)
	}
}

func TestSummaryBoundsAndErrorCap(t *testing.T) {
	s := NewStore(1000)
	s.CreateGroup("g")
	var events []LogEvent
	for i := range 20 {
		events = append(events, LogEvent{
			Timestamp: archivalBase.Add(time.Duration(i) * time.Second), Level: LevelINFO,
			Message: fmt.Sprintf(`{"id":"e%02d"}`, i),
		})
	}
	for i := range 8 {
		events = append(events, LogEvent{
			Timestamp: archivalBase.Add(time.Minute + time.Duration(i)*time.Second), Level: LevelERROR,
			Message: fmt.Sprintf(`{"id":"e00","attempt":%d}`, i),
		})
	}
	events = append(events, LogEvent{Timestamp: archivalBase.Add(2 * time.Minute), Level: LevelWARN, Message: `{"id":"e00","warn":true}`})
	s.PutLogEvents("g", "s", events)

	out := summarize(t, s, nil, SummaryOptions{GroupBy: "id", MaxGroups: 5, MaxErrorsPerGroup: 3, Fields: []string{"attempt"}})
	if len(out.Groups) != 5 || out.Totals.Groups != 20 || out.Totals.GroupsReturned != 5 {
		t.Fatalf("returned %d, totals %+v", len(out.Groups), out.Totals)
	}
	g := out.Groups[0]
	if g.Key != "e00" || len(g.Errors) != 3 || g.ErrorsDropped != 5 {
		t.Fatalf("error group = %s errors=%d dropped=%d", g.Key, len(g.Errors), g.ErrorsDropped)
	}
	if g.Errors[0]["attempt"] != json.Number("0") || g.Errors[2]["attempt"] != json.Number("2") {
		t.Errorf("errors must be oldest first: %v", g.Errors)
	}
	if g.Levels["WARN"] != 1 {
		t.Errorf("WARN should be counted: %v", g.Levels)
	}

	// WARN is listed only when asked for.
	out = summarize(t, s, &LogFilter{Level: "ERROR,WARN"}, SummaryOptions{GroupBy: "id", MaxErrorsPerGroup: 50})
	if out.Groups[0].ErrorsDropped != 0 || len(out.Groups[0].Errors) != 9 {
		t.Errorf("with level WARN: errors=%d dropped=%d, want 9 listed", len(out.Groups[0].Errors), out.Groups[0].ErrorsDropped)
	}

	// Caps are clamped, not rejected.
	out = summarize(t, s, nil, SummaryOptions{GroupBy: "id", MaxGroups: 100000, MaxErrorsPerGroup: 100000})
	if out.Totals.GroupsReturned != 20 || len(out.Groups[0].Errors) != 8 {
		t.Errorf("clamped caps returned %d groups, %d errors", out.Totals.GroupsReturned, len(out.Groups[0].Errors))
	}

	out = summarize(t, s, nil, SummaryOptions{GroupBy: "id", ScanLimit: 4})
	if !out.Totals.TruncatedScan || out.Totals.EventsScanned != 4 {
		t.Errorf("scan cap: %+v", out.Totals)
	}
}

func TestSummaryFiltersCompose(t *testing.T) {
	s := archivalStore(t)
	since := archivalBase.Add(10100 * time.Millisecond)
	until := archivalBase.Add(10150 * time.Millisecond)

	out := summarize(t, s, &LogFilter{StartTime: &since, EndTime: &until}, SummaryOptions{GroupBy: "certificateId"})
	if len(out.Groups) != 1 || out.Groups[0].Key != certID(0xb) {
		t.Errorf("window: groups = %+v", out.Groups)
	}
	if out.Window.From != "2026-09-29T18:12:03.1Z" || out.Window.To != "2026-09-29T18:12:03.15Z" {
		t.Errorf("window = %+v", out.Window)
	}

	out = summarize(t, s, &LogFilter{Level: LevelERROR}, SummaryOptions{GroupBy: "certificateId"})
	if out.Totals.EventsScanned != 1 || out.Groups[0].Key != certID(0xc) {
		t.Errorf("level: %+v", out.Totals)
	}

	out = summarize(t, s, &LogFilter{Pattern: "archival finished", Groups: []string{archivalLambda}}, SummaryOptions{GroupBy: "certificateId"})
	if out.Totals.EventsScanned != 3 || out.Totals.Groups != 3 {
		t.Errorf("pattern+groups: %+v", out.Totals)
	}

	out = summarize(t, s, &LogFilter{StreamName: "ecs/app/task1"}, SummaryOptions{GroupBy: "correlationId"})
	if out.Totals.EventsScanned != 2 {
		t.Errorf("stream: %+v", out.Totals)
	}

	out = summarize(t, s, &LogFilter{Groups: []string{"/tarn/api"}}, SummaryOptions{GroupBy: "message"})
	if out.Totals.EventsScanned != 3 || out.Totals.ErrorGroups != 1 {
		t.Errorf("/tarn/api named in groups should be read: %+v", out.Totals)
	}

	out = summarize(t, s, &LogFilter{Groups: []string{archivalLambda}}, SummaryOptions{GroupBy: "message", IncludeRuntime: true})
	if out.Totals.RuntimeFiltered != 0 || out.Totals.EventsScanned != 10 {
		t.Errorf("includeRuntime: %+v", out.Totals)
	}
}

func TestSummaryValidation(t *testing.T) {
	s := archivalStore(t)
	late, early := archivalBase.Add(time.Hour), archivalBase
	cases := []struct {
		name   string
		filter *LogFilter
		opts   SummaryOptions
	}{
		{"missing groupBy", nil, SummaryOptions{}},
		{"empty segment", nil, SummaryOptions{GroupBy: "outcomes..kind"}},
		{"trailing dot", nil, SummaryOptions{GroupBy: "outcomes."}},
		{"bad field", nil, SummaryOptions{GroupBy: "id", Fields: []string{"a..b"}}},
		{"reversed window", &LogFilter{StartTime: &late, EndTime: &early}, SummaryOptions{GroupBy: "id"}},
	}
	for _, c := range cases {
		if _, err := s.Summarize(c.filter, c.opts); err == nil {
			t.Errorf("%s: want an error", c.name)
		}
	}

	out := summarize(t, s, &LogFilter{Pattern: "no such line anywhere"}, SummaryOptions{GroupBy: "id"})
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"groups":[]`) || !strings.Contains(string(raw), `"ungroupedSample":[]`) {
		t.Errorf("empty result must serialise empty lists, got %s", raw)
	}
}

func TestSummaryUngroupedSampleKeepsNewest(t *testing.T) {
	s := NewStore(100)
	s.CreateGroup("g")
	var events []LogEvent
	for i := range 8 {
		events = append(events, LogEvent{Timestamp: archivalBase.Add(time.Duration(i) * time.Second), Level: LevelINFO, Message: fmt.Sprintf("line %d", i)})
	}
	s.PutLogEvents("g", "s", events)

	out := summarize(t, s, nil, SummaryOptions{GroupBy: "id"})
	if out.Totals.Ungrouped != 8 || len(out.UngroupedSample) != 5 {
		t.Fatalf("ungrouped = %d, sample = %d", out.Totals.Ungrouped, len(out.UngroupedSample))
	}
	if out.UngroupedSample[0]["message"] != "line 3" || out.UngroupedSample[4]["message"] != "line 7" {
		t.Errorf("sample = %v, want the newest five, oldest first", out.UngroupedSample)
	}
}
