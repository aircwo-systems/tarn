package api

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/internal/account"
	logssvc "github.com/aircwo-systems/tarn/internal/logs"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/vmihailenco/msgpack/v5"
)

const maxDatadogBody = 8 << 20

// newDatadogMux serves the Datadog agent endpoints that tracers and log
// forwarders call. It is the single list of those routes: requests it matches
// bypass AWS dispatch and do not count as account activity.
func (s *Server) newDatadogMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /info", s.datadogInfo)
	mux.HandleFunc("PUT /v0.4/traces", s.datadogTraces)
	mux.HandleFunc("POST /api/v2/logs", s.datadogLogs)
	mux.HandleFunc("POST /v1/input", s.datadogLogs)
	// Tracers poll remote configuration; 404 is the agent's "not enabled" reply.
	mux.HandleFunc("POST /v0.7/config", http.NotFound)
	return mux
}

// Datadog v0.4 carries unsigned 64-bit IDs, not floating-point JSON numbers.
type datadogSpan struct {
	TraceID  uint64             `json:"trace_id" msgpack:"trace_id"`
	SpanID   uint64             `json:"span_id" msgpack:"span_id"`
	ParentID uint64             `json:"parent_id" msgpack:"parent_id"`
	Name     string             `json:"name" msgpack:"name"`
	Service  string             `json:"service" msgpack:"service"`
	Resource string             `json:"resource" msgpack:"resource"`
	Type     string             `json:"type" msgpack:"type"`
	Start    int64              `json:"start" msgpack:"start"`
	Duration int64              `json:"duration" msgpack:"duration"`
	Error    int32              `json:"error" msgpack:"error"`
	Meta     map[string]string  `json:"meta" msgpack:"meta"`
	Metrics  map[string]float64 `json:"metrics" msgpack:"metrics"`
}

func (s *Server) datadogInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": "tarn", "endpoints": []string{"/v0.4/traces"},
		"client_drop_p0s": false,
	})
}

func datadogBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDatadogBody)
	var reader io.Reader = r.Body
	if encoding := strings.ToLower(r.Header.Get("Content-Encoding")); encoding == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "invalid gzip body", http.StatusBadRequest)
			return nil, false
		}
		defer gz.Close()
		reader = gz
	} else if encoding != "" && encoding != "identity" {
		http.Error(w, "unsupported content encoding", http.StatusUnsupportedMediaType)
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxDatadogBody+1))
	var tooLarge *http.MaxBytesError
	if len(body) > maxDatadogBody || errors.As(err, &tooLarge) {
		http.Error(w, "Datadog payload exceeds 8 MiB", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// An explicit account header or signed key wins; uncredentialled tracers can
// select their account with DD_TAGS=tarn.account_id:<12-digit account>.
func (s *Server) datadogAccount(r *http.Request, tagged string) (string, error) {
	explicit := cmp.Or(strings.TrimSpace(r.Header.Get("X-Tarn-Account-Id")), account.FromRequest(r, ""))
	if explicit != "" && tagged != "" && explicit != tagged {
		return "", fmt.Errorf("account header and tarn.account_id tag disagree")
	}
	id := cmp.Or(explicit, tagged, s.cfg.AccountID)
	if !validAccountID(id) {
		return "", ErrInvalidAccountID
	}
	return id, nil
}

// datadogAccounts resolves every account in a batch before anything is
// stored, so a rejected account cannot leave a partial import behind.
func (s *Server) datadogAccounts(w http.ResponseWriter, accounts map[string]struct{}) (map[string]*HandlerSet, bool) {
	resolved := make(map[string]*HandlerSet, len(accounts))
	for id := range accounts {
		if s.registry.IsArchived(id) {
			writeAccountArchived(w, id)
			return nil, false
		}
		hs := s.registry.get(id)
		if hs == nil || hs.Logs == nil {
			http.Error(w, "account telemetry unavailable", http.StatusServiceUnavailable)
			return nil, false
		}
		resolved[id] = hs
	}
	return resolved, true
}

func (s *Server) datadogTraces(w http.ResponseWriter, r *http.Request) {
	if s.traceStore == nil {
		http.Error(w, "trace store unavailable", http.StatusServiceUnavailable)
		return
	}
	body, ok := datadogBody(w, r)
	if !ok {
		return
	}
	var chunks [][]datadogSpan
	var err error
	switch strings.Split(r.Header.Get("Content-Type"), ";")[0] {
	case "application/json":
		err = json.Unmarshal(body, &chunks)
	case "application/msgpack", "application/x-msgpack":
		err = msgpack.Unmarshal(body, &chunks)
	default:
		http.Error(w, "expected application/msgpack or application/json", http.StatusUnsupportedMediaType)
		return
	}
	if err != nil || chunks == nil {
		http.Error(w, "invalid Datadog v0.4 traces", http.StatusBadRequest)
		return
	}
	accounts := make(map[string]struct{})
	imports := make([]*tracesvc.Trace, 0, len(chunks))
	count := 0
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		if count += len(chunk); count > 10000 {
			http.Error(w, "too many spans", http.StatusRequestEntityTooLarge)
			return
		}
		trace, err := s.datadogChunk(r, chunk)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		accounts[trace.AccountID] = struct{}{}
		imports = append(imports, trace)
	}
	if _, ok := s.datadogAccounts(w, accounts); !ok {
		return
	}
	if err := s.traceStore.Merge(imports...); err != nil {
		http.Error(w, "failed to store trace", http.StatusInternalServerError)
		return
	}
	s.touchAll(accounts)
	writeJSON(w, http.StatusOK, map[string]any{"rate_by_service": map[string]float64{}})
}

// datadogChunk converts one tracer chunk: spans of a single trace from one
// process. Tags such as the account and the 128-bit high bits may appear only
// on the chunk's local root span.
func (s *Server) datadogChunk(r *http.Request, chunk []datadogSpan) (*tracesvc.Trace, error) {
	traceID := chunk[0].TraceID
	var tagged, high string
	seen := make(map[uint64]bool, len(chunk))
	spans := make([]tracesvc.Span, 0, len(chunk))
	for _, sp := range chunk {
		if sp.TraceID == 0 || sp.TraceID != traceID || sp.SpanID == 0 || seen[sp.SpanID] || sp.Start <= 0 || sp.Duration < 0 || sp.Duration > math.MaxInt64-sp.Start {
			return nil, fmt.Errorf("invalid Datadog span IDs or timing")
		}
		seen[sp.SpanID] = true
		if id := sp.Meta["tarn.account_id"]; id != "" {
			if tagged != "" && tagged != id {
				return nil, fmt.Errorf("trace chunk contains multiple accounts")
			}
			tagged = id
		}
		if value := strings.ToLower(sp.Meta["_dd.p.tid"]); value != "" {
			if _, err := strconv.ParseUint(value, 16, 64); len(value) != 16 || err != nil || high != "" && high != value {
				return nil, fmt.Errorf("invalid 128-bit trace ID")
			}
			high = value
		}
		spans = append(spans, convertDatadogSpan(sp))
	}
	id, err := s.datadogAccount(r, tagged)
	if err != nil {
		return nil, err
	}
	return &tracesvc.Trace{
		ID:            fmt.Sprintf("dd:%s:%s%016x", id, cmp.Or(high, "0000000000000000"), traceID),
		AccountID:     id,
		CorrelationID: strconv.FormatUint(traceID, 10),
		Spans:         spans,
	}, nil
}

func convertDatadogSpan(sp datadogSpan) tracesvc.Span {
	meta := make(map[string]string, len(sp.Meta)+4)
	for key, value := range sp.Meta {
		// _dd.* tags are tracer bookkeeping (sampling, propagation), not span data.
		if !strings.HasPrefix(key, "_dd.") {
			meta[key] = value
		}
	}
	for key, value := range sp.Metrics {
		if !strings.HasPrefix(key, "_") {
			meta["metric."+key] = strconv.FormatFloat(value, 'g', -1, 64)
		}
	}
	meta["operation"] = sp.Name
	meta["resource"] = sp.Resource
	meta["source"] = "datadog"
	meta["logGroup"] = "/services/" + sp.Service
	kind := sp.Type
	if kind == "" || kind == "web" {
		kind = "service"
	} else if kind == "sql" && meta["db.system"] != "" {
		kind = meta["db.system"]
	}
	code, _ := strconv.Atoi(meta["http.status_code"])
	status := tracesvc.StatusForHTTP(code)
	if sp.Error != 0 {
		status = "error"
	}
	start := time.Unix(0, sp.Start).UTC()
	span := tracesvc.Span{
		ID: strconv.FormatUint(sp.SpanID, 10), Kind: kind, Name: datadogSpanName(sp), Service: sp.Service,
		StartedAt: &start, DurationNs: sp.Duration, DurationMs: sp.Duration / int64(time.Millisecond), Status: status, Meta: meta,
	}
	if sp.ParentID != 0 {
		span.ParentID = strconv.FormatUint(sp.ParentID, 10)
	}
	return span
}

// datadogSpanName says what the span did; the service is kept separately.
// Request spans often carry only the method as their resource.
func datadogSpanName(sp datadogSpan) string {
	name := cmp.Or(sp.Resource, sp.Name, sp.Service)
	if method := sp.Meta["http.method"]; method != "" && name == method {
		route := sp.Meta["http.route"]
		if route == "" {
			if u, err := url.Parse(sp.Meta["http.url"]); err == nil {
				route = u.Path
			}
		}
		if route != "" {
			name += " " + route
		}
	}
	return name
}

type datadogLogIDs struct {
	TraceID json.RawMessage `json:"trace_id"`
	SpanID  json.RawMessage `json:"span_id"`
}

type datadogLog struct {
	Message   json.RawMessage `json:"message"`
	Service   string          `json:"service"`
	Hostname  string          `json:"hostname"`
	Source    string          `json:"ddsource"`
	Tags      string          `json:"ddtags"`
	Status    string          `json:"status"`
	Level     string          `json:"level"`
	Timestamp json.RawMessage `json:"timestamp"`
	TraceID   json.RawMessage `json:"dd.trace_id"`
	SpanID    json.RawMessage `json:"dd.span_id"`
	DD        datadogLogIDs   `json:"dd"`
}

// datadogID normalises a log's trace or span ID to the decimal low 64 bits
// that imported traces are correlated by. 128-bit trace IDs arrive as 32 hex
// digits from newer tracers.
func datadogID(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	value := strings.Trim(string(raw), `"`)
	var id uint64
	var err error
	if len(value) == 32 {
		if _, err = strconv.ParseUint(value[:16], 16, 64); err == nil {
			id, err = strconv.ParseUint(value[16:], 16, 64)
		}
	} else {
		id, err = strconv.ParseUint(value, 10, 64)
	}
	if err != nil || id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

func parseDatadogTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 {
		return time.Now().UTC(), nil
	}
	value := strings.Trim(string(raw), `"`)
	if millis, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.UnixMilli(millis).UTC(), nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid log timestamp")
}

func datadogLevel(status string) logssvc.LogLevel {
	switch level := logssvc.LogLevel(strings.ToUpper(status)); level {
	case logssvc.LevelDEBUG, logssvc.LevelINFO, logssvc.LevelWARN, logssvc.LevelERROR:
		return level
	case "WARNING":
		return logssvc.LevelWARN
	case "CRITICAL", "FATAL", "EMERGENCY":
		return logssvc.LevelERROR
	default:
		return logssvc.LevelINFO
	}
}

func (s *Server) datadogLogs(w http.ResponseWriter, r *http.Request) {
	body, ok := datadogBody(w, r)
	if !ok {
		return
	}
	var records []json.RawMessage
	if err := json.Unmarshal(body, &records); err != nil || records == nil || len(records) > 10000 {
		http.Error(w, "expected a JSON array of Datadog logs", http.StatusBadRequest)
		return
	}
	type streamKey struct{ account, group, stream string }
	batches := make(map[streamKey][]logssvc.LogEvent)
	var order []streamKey
	accounts := make(map[string]struct{})
	for _, raw := range records {
		var record datadogLog
		if err := json.Unmarshal(raw, &record); err != nil || record.Service == "" || strings.ContainsAny(record.Service, "/\\\r\n") {
			http.Error(w, "each log requires a service name without path separators", http.StatusBadRequest)
			return
		}
		var tagged string
		for _, tag := range strings.Split(record.Tags, ",") {
			if value, ok := strings.CutPrefix(strings.TrimSpace(tag), "tarn.account_id:"); ok {
				tagged = value
			}
		}
		id, err := s.datadogAccount(r, tagged)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		stamp, err := parseDatadogTimestamp(record.Timestamp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Agents may wrap the original Winston JSON record in message.
		var inner datadogLog
		var message string
		if json.Unmarshal(record.Message, &message) == nil {
			_ = json.Unmarshal([]byte(message), &inner)
		}
		key := streamKey{id, "/services/" + record.Service, cmp.Or(record.Hostname, record.Source, "datadog")}
		if _, ok := batches[key]; !ok {
			order = append(order, key)
		}
		accounts[id] = struct{}{}
		batches[key] = append(batches[key], logssvc.LogEvent{
			Timestamp: stamp, Message: string(raw), Level: datadogLevel(cmp.Or(record.Status, record.Level)), Source: logssvc.SourceOutput,
			TraceID: cmp.Or(datadogID(record.TraceID), datadogID(record.DD.TraceID), datadogID(inner.DD.TraceID)),
			SpanID:  cmp.Or(datadogID(record.SpanID), datadogID(record.DD.SpanID), datadogID(inner.DD.SpanID)),
		})
	}
	resolved, ok := s.datadogAccounts(w, accounts)
	if !ok {
		return
	}
	for _, key := range order {
		logs := resolved[key.account].Logs
		logs.CreateLogGroup(key.group)
		logs.PutLogEvents(key.group, key.stream, batches[key])
	}
	s.touchAll(accounts)
	w.WriteHeader(http.StatusAccepted)
}

// Imported telemetry marks its target accounts active; the request itself is
// not an AWS call, so withLogging does not touch the caller's account.
func (s *Server) touchAll(accounts map[string]struct{}) {
	for id := range accounts {
		s.registry.Touch(id)
	}
}
