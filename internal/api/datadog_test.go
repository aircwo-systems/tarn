package api

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/logs"
	"github.com/aircwo-systems/tarn/internal/trace"
	"github.com/vmihailenco/msgpack/v5"
)

func TestDatadogSDKPublishJoinsNativeSpans(t *testing.T) {
	s, store := datadogHarness(t)
	accountHeaders := map[string]string{"Authorization": "AWS4-HMAC-SHA256 Credential=" + otherAccount + "/date/region/service/aws4_request"}
	formRequest := func(values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		res := datadogRequest(t, s, "POST", "/", "application/x-www-form-urlencoded", []byte(values.Encode()), accountHeaders)
		if res.Code != 200 {
			t.Fatalf("SNS %s = %d %s", values.Get("Action"), res.Code, res.Body.String())
		}
		return res
	}
	res := formRequest(url.Values{"Action": {"CreateTopic"}, "Version": {"2010-03-31"}, "Name": {"orders"}})
	var created struct {
		Result struct {
			ARN string `xml:"TopicArn"`
		} `xml:"CreateTopicResult"`
	}
	if err := xml.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	carrier := `{"x-datadog-trace-id":"18446744073709551615","x-datadog-parent-id":"18446744073709551613","x-datadog-tags":"_dd.p.tid=abcdef1234567890"}`
	formRequest(url.Values{
		"Action": {"Publish"}, "Version": {"2010-03-31"}, "TopicArn": {created.Result.ARN}, "Message": {"order"},
		"MessageAttributes.entry.1.Name": {"_datadog"}, "MessageAttributes.entry.1.Value.DataType": {"Binary"}, "MessageAttributes.entry.1.Value.BinaryValue": {base64.StdEncoding.EncodeToString([]byte(carrier))},
	})
	body, _ := json.Marshal([][]datadogSpan{datadogFixture(otherAccount)})
	if res := datadogRequest(t, s, "PUT", "/v0.4/traces", "application/json", body, nil); res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	got := store.Recent(10)
	if len(got) != 1 || len(got[0].Spans) != 3 || got[0].AccountID != otherAccount {
		t.Fatalf("joined = %+v", got)
	}
	for _, span := range got[0].Spans {
		if span.Kind == "topic" && span.ParentID != "18446744073709551613" {
			t.Fatalf("SNS parent = %+v", span)
		}
	}
	// Same low bits in another account must not extend the first request.
	defaultFixture := datadogFixture(s.cfg.AccountID)
	body, _ = json.Marshal([][]datadogSpan{defaultFixture})
	if res := datadogRequest(t, s, "PUT", "/v0.4/traces", "application/json", body, nil); res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	if store.Count() != 2 {
		t.Fatalf("cross-account merge: %d", store.Count())
	}
	if tr := store.FindByCorrelation(s.cfg.AccountID, "18446744073709551615"); tr == nil || len(tr.Spans) != 2 {
		t.Fatalf("default account = %+v", tr)
	}
}

func datadogHarness(t *testing.T) (*Server, *trace.Store) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	store := trace.OpenStore(cfg.DataDir)
	registry := NewHandlerRegistry(func(id string) (*AccountBundle, error) {
		return newTestBundleWithTraces(t, cfg.ForAccount(id), store), nil
	})
	registry.ConfigureAccounts(cfg.DataDir, cfg.AccountID, nil)
	t.Cleanup(func() { registry.StopAll(); store.Close() })
	s := NewServer(cfg, registry, logs.NewService(cfg), nil)
	s.SetTraceStore(store)
	return s, store
}

func datadogRequest(t *testing.T, s *Server, method, path, contentType string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(w, r)
	return w
}

func datadogFixture(accountID string) []datadogSpan {
	start := time.Now().Add(-time.Second).UnixNano()
	return []datadogSpan{
		{TraceID: math.MaxUint64, SpanID: math.MaxUint64 - 1, Service: "candidate-frontend", Name: "web.request", Resource: "GET /candidate", Type: "web", Start: start, Duration: int64(100 * time.Millisecond), Meta: map[string]string{"tarn.account_id": accountID, "http.method": "GET", "http.route": "/candidate", "http.status_code": "200", "_dd.p.tid": "abcdef1234567890"}},
		{TraceID: math.MaxUint64, SpanID: math.MaxUint64 - 2, ParentID: math.MaxUint64 - 1, Service: "candidate-gateway", Name: "http.request", Resource: "GET /api", Type: "http", Start: start + int64(20*time.Millisecond), Duration: int64(50 * time.Millisecond), Meta: map[string]string{"http.status_code": "200"}},
	}
}

func TestDatadogTraceCodecsAndRetry(t *testing.T) {
	for _, codec := range []string{"json", "msgpack", "gzip"} {
		t.Run(codec, func(t *testing.T) {
			s, store := datadogHarness(t)
			fixture := [][]datadogSpan{datadogFixture(otherAccount)}
			body, err := json.Marshal(fixture)
			contentType := "application/json"
			headers := map[string]string{}
			if codec != "json" {
				body, err = msgpack.Marshal(fixture)
				contentType = "application/msgpack"
			}
			if err != nil {
				t.Fatal(err)
			}
			if codec == "gzip" {
				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				writer.Write(body)
				writer.Close()
				body = compressed.Bytes()
				headers["Content-Encoding"] = "gzip"
			}
			for range 2 {
				res := datadogRequest(t, s, "PUT", "/v0.4/traces", contentType, body, headers)
				if res.Code != 200 || !strings.Contains(res.Body.String(), "rate_by_service") {
					t.Fatalf("response = %d %s", res.Code, res.Body.String())
				}
			}
			traces := store.Recent(10)
			if len(traces) != 1 {
				t.Fatalf("traces = %d", len(traces))
			}
			tr := traces[0]
			if tr.ID != "dd:"+otherAccount+":abcdef1234567890ffffffffffffffff" || tr.AccountID != otherAccount || tr.CorrelationID != "18446744073709551615" {
				t.Fatalf("identity = %+v", tr)
			}
			if len(tr.Spans) != 2 || tr.Spans[1].ParentID != "18446744073709551614" || tr.Spans[1].StartedAt == nil || tr.DurationMs != 100 || tr.Path != "/candidate" {
				t.Fatalf("trace = %+v", tr)
			}
			defaultLogs := s.registry.get(s.cfg.AccountID).Logs
			_, total, _ := defaultLogs.GetLogEvents("/tarn/api", nil)
			if total != 0 {
				t.Fatalf("Datadog ingestion wrote %d AWS API logs", total)
			}
		})
	}
}

func TestDatadogLateChunksAndAccountIsolation(t *testing.T) {
	s, store := datadogHarness(t)
	fixture := datadogFixture(otherAccount)
	// The tracer repeats the high trace bits on the first span of each chunk.
	fixture[1].Meta["_dd.p.tid"] = fixture[0].Meta["_dd.p.tid"]
	for _, chunk := range [][]datadogSpan{{fixture[1]}, {fixture[0]}} {
		body, _ := msgpack.Marshal([][]datadogSpan{chunk})
		res := datadogRequest(t, s, "PUT", "/v0.4/traces", "application/msgpack", body, map[string]string{"X-Tarn-Account-Id": otherAccount})
		if res.Code != 200 {
			t.Fatalf("late chunk = %d %s", res.Code, res.Body.String())
		}
	}
	if tr := store.Recent(1)[0]; len(tr.Spans) != 2 || tr.Spans[0].ParentID != "" || tr.DurationMs != 100 {
		t.Fatalf("merged = %+v", tr)
	}
	for _, id := range []string{s.cfg.AccountID, otherAccount} {
		res := datadogRequest(t, s, "GET", "/_tarn/admin/traces", "", nil, map[string]string{"Authorization": "AWS4-HMAC-SHA256 Credential=" + id + "/date/region/service/aws4_request"})
		var payload struct {
			Traces []trace.Trace `json:"traces"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		want := 0
		if id == otherAccount {
			want = 1
		}
		if len(payload.Traces) != want {
			t.Fatalf("account %s sees %d traces, want %d", id, len(payload.Traces), want)
		}
	}
}

func TestDatadogLogsRetainExactTraceContext(t *testing.T) {
	s, _ := datadogHarness(t)
	body, _ := msgpack.Marshal([][]datadogSpan{datadogFixture(otherAccount)})
	datadogRequest(t, s, "PUT", "/v0.4/traces", "application/msgpack", body, nil)
	stamp := time.Now().UnixMilli()
	logsBody := []byte(fmt.Sprintf(`[{"service":"candidate-frontend","hostname":"container-1","ddtags":"tarn.account_id:%s","message":"request failed","status":"error","timestamp":%d,"dd.trace_id":"18446744073709551615","dd.span_id":"18446744073709551614"}]`, otherAccount, stamp))
	res := datadogRequest(t, s, "POST", "/api/v2/logs", "application/json", logsBody, nil)
	if res.Code != 202 {
		t.Fatalf("logs = %d %s", res.Code, res.Body.String())
	}
	events, total, _ := s.registry.get(otherAccount).Logs.GetLogEvents("/services/candidate-frontend", nil)
	if total != 1 || events[0].TraceID != "18446744073709551615" || events[0].SpanID != "18446744073709551614" || events[0].Timestamp.UnixMilli() != stamp || events[0].Level != logs.LevelERROR {
		t.Fatalf("logs = %+v", events)
	}
	for _, id := range []string{otherAccount, s.cfg.AccountID} {
		res = datadogRequest(t, s, "GET", "/_tarn/admin/traces/for-log?traceId=18446744073709551615", "", nil, map[string]string{"Authorization": "AWS4-HMAC-SHA256 Credential=" + id + "/date/region/service/aws4_request"})
		if id == otherAccount && !strings.Contains(res.Body.String(), "candidate-frontend") || id != otherAccount && strings.TrimSpace(res.Body.String()) != "null" {
			t.Fatalf("account %s lookup = %s", id, res.Body.String())
		}
	}
	if group, _ := s.registry.get(s.cfg.AccountID).Logs.GetGroup("/services/candidate-frontend"); group != nil {
		t.Fatal("service log group leaked into default account")
	}
}

func TestDatadogRejectsInvalidAndArchivedPayloads(t *testing.T) {
	s, store := datadogHarness(t)
	for _, change := range []func(*datadogSpan){
		func(sp *datadogSpan) { sp.SpanID = 0 },
		func(sp *datadogSpan) { sp.Duration = -1 },
		func(sp *datadogSpan) { sp.Start = math.MaxInt64 },
		func(sp *datadogSpan) { sp.Meta["tarn.account_id"] = "invalid" },
	} {
		fixture := datadogFixture(otherAccount)
		change(&fixture[0])
		body, _ := json.Marshal([][]datadogSpan{fixture})
		res := datadogRequest(t, s, "PUT", "/v0.4/traces", "application/json", body, nil)
		if res.Code != 400 {
			t.Fatalf("invalid payload = %d %s", res.Code, res.Body.String())
		}
	}
	body, _ := json.Marshal([][]datadogSpan{datadogFixture(otherAccount)})
	res := datadogRequest(t, s, "PUT", "/v0.4/traces", "application/json", body, map[string]string{"X-Tarn-Account-Id": s.cfg.AccountID})
	if res.Code != 400 || store.Count() != 0 {
		t.Fatalf("conflicting account = %d, traces %d", res.Code, store.Count())
	}
	s.registry.get(otherAccount)
	if err := s.registry.Archive(otherAccount); err != nil {
		t.Fatal(err)
	}
	res = datadogRequest(t, s, "PUT", "/v0.4/traces", "application/json", body, nil)
	if res.Code != 403 || store.Count() != 0 {
		t.Fatalf("archived account = %d, traces %d", res.Code, store.Count())
	}
	res = datadogRequest(t, s, "POST", "/api/v2/logs", "application/json", []byte(`[{"service":"frontend","ddtags":"tarn.account_id:`+otherAccount+`","message":"test"}]`), nil)
	if res.Code != 403 {
		t.Fatalf("archived logs = %d", res.Code)
	}
}

func TestDatadogPayloadLimitAndAgentInfo(t *testing.T) {
	s, _ := datadogHarness(t)
	for _, compressed := range []bool{false, true} {
		body := bytes.Repeat([]byte("x"), maxDatadogBody+1)
		headers := map[string]string{}
		if compressed {
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			gz.Write(body)
			gz.Close()
			body = buf.Bytes()
			headers["Content-Encoding"] = "gzip"
		}
		res := datadogRequest(t, s, "PUT", "/v0.4/traces", "application/msgpack", body, headers)
		if res.Code != 413 {
			t.Fatalf("compressed=%v response=%d", compressed, res.Code)
		}
	}
	res := datadogRequest(t, s, "GET", "/info", "", nil, nil)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "/v0.4/traces") {
		t.Fatalf("info = %d %s", res.Code, res.Body.String())
	}
	// The wire ID must never be converted through float64.
	if got := datadogID(json.RawMessage(strconv.FormatUint(math.MaxUint64, 10))); got != "18446744073709551615" {
		t.Fatalf("ID = %s", got)
	}
}

func TestDatadogRemoteConfigReportsDisabled(t *testing.T) {
	s, _ := datadogHarness(t)
	rec := datadogRequest(t, s, "POST", "/v0.7/config", "application/json", []byte(`{"client":{}}`), nil)
	if rec.Code != 404 {
		t.Fatalf("remote config poll = %d, want 404", rec.Code)
	}
}

// Newer tracers inject 128-bit trace IDs into logs as 32 hex digits.
func TestDatadogLogsAccept128BitHexTraceIDs(t *testing.T) {
	s, _ := datadogHarness(t)
	body, _ := msgpack.Marshal([][]datadogSpan{datadogFixture(otherAccount)})
	datadogRequest(t, s, "PUT", "/v0.4/traces", "application/msgpack", body, nil)
	logsBody := []byte(fmt.Sprintf(`[{"service":"employer-frontend","ddtags":"tarn.account_id:%s","message":"GET /employer 200","dd":{"trace_id":"6ac545fb00000000ffffffffffffffff","span_id":"5982176574396183361"}}]`, otherAccount))
	if res := datadogRequest(t, s, "POST", "/api/v2/logs", "application/json", logsBody, nil); res.Code != 202 {
		t.Fatalf("logs = %d %s", res.Code, res.Body.String())
	}
	events, _, _ := s.registry.get(otherAccount).Logs.GetLogEvents("/services/employer-frontend", nil)
	if len(events) != 1 || events[0].TraceID != "18446744073709551615" || events[0].SpanID != "5982176574396183361" {
		t.Fatalf("logs = %+v", events)
	}
	res := datadogRequest(t, s, "GET", "/_tarn/admin/traces/for-log?traceId="+events[0].TraceID, "", nil, map[string]string{"Authorization": "AWS4-HMAC-SHA256 Credential=" + otherAccount + "/date/region/service/aws4_request"})
	if !strings.Contains(res.Body.String(), "candidate-frontend") {
		t.Fatalf("lookup = %s", res.Body.String())
	}
}

func TestDatadogSpanNameSaysWhatTheSpanDid(t *testing.T) {
	for _, tc := range []struct {
		span datadogSpan
		want string
	}{
		{datadogSpan{Service: "fe", Name: "express.middleware", Resource: "session"}, "session"},
		{datadogSpan{Service: "fe", Name: "express.request", Resource: "GET", Meta: map[string]string{"http.method": "GET", "http.route": "/employer/home"}}, "GET /employer/home"},
		{datadogSpan{Service: "fe", Name: "express.request", Resource: "GET", Meta: map[string]string{"http.method": "GET", "http.url": "http://localhost/candidate?x=1"}}, "GET /candidate"},
		{datadogSpan{Service: "fe", Name: "redis.command"}, "redis.command"},
	} {
		if got := datadogSpanName(tc.span); got != tc.want {
			t.Errorf("datadogSpanName(%+v) = %q, want %q", tc.span, got, tc.want)
		}
	}
}
