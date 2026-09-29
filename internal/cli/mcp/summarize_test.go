package mcp

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSummarizeLogsForwardsQuery(t *testing.T) {
	inst := newInstance(t).json("GET /_tarn/admin/logs/summary", map[string]any{
		"groupBy": "certificateId",
		"window":  map[string]any{"from": "2026-09-29T18:11:53Z", "to": "2026-09-29T18:12:04Z"},
		"totals":  map[string]any{"eventsScanned": 4, "eventsGrouped": 3, "ungrouped": 1, "groups": 1, "groupsReturned": 1, "errorGroups": 1},
		"groups": []map[string]any{{
			"key": "c", "count": 3, "levels": map[string]int{"INFO": 2, "ERROR": 1},
			"logGroups": []string{"/aws/lambda/archival"},
			"firstAt":   "2026-09-29T18:12:03.98Z", "lastAt": "2026-09-29T18:12:03.99Z",
			"errors":        []map[string]any{{"timestamp": "2026-09-29T18:12:03.985Z", "message": "blocked"}},
			"errorsDropped": 0,
			"first":         map[string]any{"timestamp": "2026-09-29T18:12:03.98Z", "message": "started"},
			"last":          map[string]any{"timestamp": "2026-09-29T18:12:03.99Z", "message": "finished", "outcomes": map[string]any{"certificate": "blocked"}},
		}},
		"ungroupedSample": []map[string]any{{"timestamp": "2026-09-29T18:11:53Z", "message": "Datadog tracing enabled: false"}},
	})

	var out SummarizeLogsOutput
	call(t, inst.session(), "tarn_summarize_logs", map[string]any{
		"groupBy": "certificateId",
		"fields":  []string{"message", "outcomes"},
		"flatten": "kind",
		"level":   "error",
		"since":   "2026-09-29T18:11:00Z",
	}, &out)

	if len(out.Groups) != 1 || out.Groups[0].Key != "c" || len(out.Groups[0].Errors) != 1 {
		t.Fatalf("output = %+v", out)
	}
	if out.Totals.Ungrouped != 1 || len(out.UngroupedSample) != 1 {
		t.Errorf("ungrouped lines must be passed through: %+v", out)
	}

	q, _ := url.ParseQuery(inst.lastRequest().Query)
	want := map[string]string{
		"groupBy": "certificateId", "fields": "message,outcomes", "flatten": "kind",
		"level": "ERROR", "since": "2026-09-29T18:11:00Z",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), v)
		}
	}
	// With neither function nor logGroup, every group is searched.
	if q.Has("groups") {
		t.Errorf("groups = %q, want unset so all groups are searched", q.Get("groups"))
	}
}

func TestSummarizeLogsScopesToFunction(t *testing.T) {
	inst := newInstance(t).json("GET /_tarn/admin/logs/summary", map[string]any{"groupBy": "id"})

	var out SummarizeLogsOutput
	call(t, inst.session(), "tarn_summarize_logs", map[string]any{
		"groupBy": "id", "function": "worker", "logGroup": "/ecs/feeder", "groups": []string{"/tarn/system"},
	}, &out)

	q, _ := url.ParseQuery(inst.lastRequest().Query)
	if q.Get("groups") != "/aws/lambda/worker,/ecs/feeder,/tarn/system" {
		t.Errorf("groups = %q", q.Get("groups"))
	}
	if out.Groups == nil || out.UngroupedSample == nil {
		t.Errorf("empty lists must decode as [], got %+v", out)
	}
}

func TestSummarizeLogsSurfacesValidationErrors(t *testing.T) {
	inst := newInstance(t).handle("GET /_tarn/admin/logs/summary", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid groupBy: path \"a..b\" has an empty segment"}`))
	})

	res, err := inst.session().CallTool(t.Context(), &mcp.CallToolParams{
		Name: "tarn_summarize_logs", Arguments: map[string]any{"groupBy": "a..b"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var text string
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*mcp.TextContent); ok {
			text = tc.Text
		}
	}
	if !res.IsError || !strings.Contains(text, "empty segment") {
		t.Errorf("want the server's reason in the error, got %q", text)
	}
}
