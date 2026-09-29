package logs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultSummaryMaxGroups   = 50
	maxSummaryMaxGroups       = 500
	defaultSummaryMaxErrors   = 5
	maxSummaryMaxErrors       = 50
	summaryUngroupedSamples   = 5
	summaryRawKeyLimit        = 200
	defaultSummaryScanLimit   = 50000
	summaryGroupByMessagePath = "message"
	apiRequestLogGroup        = "/tarn/api"
)

// SummaryOptions controls how Summarize groups and projects events. Event
// selection (groups, pattern, stream, time window) lives in LogFilter.
type SummaryOptions struct {
	// GroupBy is a dotted JSON path into structured messages. "message"
	// groups by the line's own message, falling back to the raw line.
	GroupBy string
	// Fields are dotted paths kept in each sample event. Empty keeps
	// message, the GroupBy field and level. "*" keeps everything.
	Fields []string
	// Flatten replaces any object carrying this key with that key's value.
	Flatten string
	// IncludeRuntime keeps container runtime chatter (see IsRuntimeNoise) and
	// Tarn's own API request log, which is otherwise only read when named in
	// the filter's groups.
	IncludeRuntime bool
	// MaxGroups caps the groups returned. Defaults to 50, capped at 500.
	MaxGroups int
	// MaxErrorsPerGroup caps listed errors per group. Defaults to 5, capped at 50.
	MaxErrorsPerGroup int
	// ScanLimit caps how many events are considered, newest kept.
	// Defaults to 50 000.
	ScanLimit int
}

// LogSummary is a grouped view of log events, sized to the number of groups
// rather than the number of lines.
type LogSummary struct {
	GroupBy         string           `json:"groupBy"`
	Window          SummaryWindow    `json:"window"`
	Totals          SummaryTotals    `json:"totals"`
	Groups          []SummaryGroup   `json:"groups"`
	UngroupedSample []map[string]any `json:"ungroupedSample"`
}

// SummaryWindow is the time span summarised: the requested bounds where given,
// otherwise the span of the events considered.
type SummaryWindow struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// SummaryTotals accounts for every event considered, so nothing is withheld
// without being counted.
type SummaryTotals struct {
	EventsScanned   int  `json:"eventsScanned"`
	EventsGrouped   int  `json:"eventsGrouped"`
	Ungrouped       int  `json:"ungrouped"`
	RuntimeFiltered int  `json:"runtimeFiltered,omitempty"` // runtime chatter and /tarn/api lines withheld
	Groups          int  `json:"groups"`
	GroupsReturned  int  `json:"groupsReturned"`
	ErrorGroups     int  `json:"errorGroups"`
	TruncatedScan   bool `json:"truncatedScan,omitempty"`
}

// SummaryGroup is every considered event sharing one GroupBy value.
type SummaryGroup struct {
	Key           string           `json:"key"`
	Count         int              `json:"count"`
	Levels        map[string]int   `json:"levels"`
	LogGroups     []string         `json:"logGroups"`
	FirstAt       string           `json:"firstAt"`
	LastAt        string           `json:"lastAt"`
	Errors        []map[string]any `json:"errors"`
	ErrorsDropped int              `json:"errorsDropped"`
	First         map[string]any   `json:"first"`
	Last          map[string]any   `json:"last"`
}

// GroupedLogEvent is a log event paired with the log group it came from.
type GroupedLogEvent struct {
	Group string
	Event LogEvent
}

// ParseSummaryPath splits a dotted path, rejecting empty segments.
func ParseSummaryPath(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("path is empty")
	}
	parts := strings.Split(path, ".")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("path %q has an empty segment", path)
		}
	}
	return parts, nil
}

// summaryEvent is a candidate event with its message parsed at most once.
type summaryEvent struct {
	group string
	event LogEvent
	doc   map[string]any // nil when the message is not a JSON object
}

type summaryGroupState struct {
	key       string
	count     int
	levels    map[string]int
	logGroups []string
	first     *summaryEvent
	last      *summaryEvent
	errors    []*summaryEvent
	errCount  int
	listed    int
}

// Summarize groups events matching filter by a JSON field. Order, limit,
// offset and cursor are ignored. Errors are validation errors.
//
// A structured line's own "level" field wins over the stored level, for
// counts, error listing and filter.Level alike. The Node runtime stores every
// console.log line as INFO, so {"level":"error",...} would otherwise be
// counted as info and the error hidden from the summary.
func (s *Service) Summarize(filter *LogFilter, opts SummaryOptions) (*LogSummary, error) {
	return s.store.Summarize(filter, opts)
}

// Summarize groups events matching filter by a JSON field. See Service.Summarize.
func (s *Store) Summarize(filter *LogFilter, opts SummaryOptions) (*LogSummary, error) {
	groupPath, err := ParseSummaryPath(opts.GroupBy)
	if err != nil {
		return nil, fmt.Errorf("invalid groupBy: %w", err)
	}
	projection, err := parseSummaryFields(opts.Fields, opts.GroupBy)
	if err != nil {
		return nil, err
	}
	if filter != nil && filter.StartTime != nil && filter.EndTime != nil && filter.StartTime.After(*filter.EndTime) {
		return nil, errors.New("since must not be after until")
	}

	maxGroups := clampSummary(opts.MaxGroups, defaultSummaryMaxGroups, maxSummaryMaxGroups)
	maxErrors := clampSummary(opts.MaxErrorsPerGroup, defaultSummaryMaxErrors, maxSummaryMaxErrors)
	scanLimit := opts.ScanLimit
	if scanLimit <= 0 {
		scanLimit = defaultSummaryScanLimit
	}
	// Every AWS call a pipeline makes is logged to /tarn/api with a unique
	// duration, which would swamp both groupBy=message and the ungrouped
	// sample. Read it only when asked for by name.
	skipAPILog := !opts.IncludeRuntime && (filter == nil || !slices.Contains(filter.Groups, apiRequestLogGroup))
	listWarn := filter != nil && filter.Level != "" && levelMatches(LevelWARN, filter.Level)
	flatten := strings.TrimSpace(opts.Flatten)
	byMessage := strings.TrimSpace(opts.GroupBy) == summaryGroupByMessagePath

	// Level is matched below against each line's effective level.
	var wantLevel LogLevel
	if filter != nil && filter.Level != "" {
		f := *filter
		wantLevel, f.Level = f.Level, ""
		filter = &f
	}

	candidates, truncated := s.summaryCandidates(filter, scanLimit)

	out := &LogSummary{
		GroupBy:         strings.TrimSpace(opts.GroupBy),
		Groups:          make([]SummaryGroup, 0),
		UngroupedSample: make([]map[string]any, 0),
	}
	out.Totals.TruncatedScan = truncated

	states := map[string]*summaryGroupState{}
	var ungrouped []*summaryEvent
	var firstAt, lastAt time.Time

	for i := range candidates {
		c := candidates[i]
		if (skipAPILog && c.Group == apiRequestLogGroup) ||
			(!opts.IncludeRuntime && IsRuntimeNoise(string(c.Event.Source), c.Event.Message)) {
			out.Totals.RuntimeFiltered++
			continue
		}
		ev := &summaryEvent{group: c.Group, event: c.Event, doc: parseJSONObject(c.Event.Message)}
		if lvl, ok := structuredLevel(ev.doc); ok {
			ev.event.Level = lvl
		}
		if wantLevel != "" && !levelMatches(ev.event.Level, wantLevel) {
			continue
		}
		out.Totals.EventsScanned++
		if firstAt.IsZero() || ev.event.Timestamp.Before(firstAt) {
			firstAt = ev.event.Timestamp
		}
		if ev.event.Timestamp.After(lastAt) {
			lastAt = ev.event.Timestamp
		}

		key, ok := summaryKey(ev, groupPath, byMessage)
		if !ok {
			out.Totals.Ungrouped++
			// Keep the newest: without a since bound the oldest lines are
			// usually from earlier runs or startup.
			ungrouped = append(ungrouped, ev)
			if len(ungrouped) > summaryUngroupedSamples {
				ungrouped = ungrouped[1:]
			}
			continue
		}
		out.Totals.EventsGrouped++

		st, exists := states[key]
		if !exists {
			st = &summaryGroupState{key: key, levels: map[string]int{}, first: ev}
			states[key] = st
		}
		st.count++
		st.levels[string(ev.event.Level)]++
		if !slices.Contains(st.logGroups, ev.group) {
			st.logGroups = append(st.logGroups, ev.group)
		}
		st.last = ev
		if ev.event.Level == LevelERROR {
			st.errCount++
		}
		if ev.event.Level == LevelERROR || (listWarn && ev.event.Level == LevelWARN) {
			st.listed++
			if len(st.errors) < maxErrors {
				st.errors = append(st.errors, ev)
			}
		}
	}

	ordered := make([]*summaryGroupState, 0, len(states))
	for _, st := range states {
		ordered = append(ordered, st)
		if st.errCount > 0 {
			out.Totals.ErrorGroups++
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.errCount != b.errCount {
			return a.errCount > b.errCount
		}
		if !a.last.event.Timestamp.Equal(b.last.event.Timestamp) {
			return a.last.event.Timestamp.After(b.last.event.Timestamp)
		}
		return a.key < b.key
	})
	out.Totals.Groups = len(ordered)
	if len(ordered) > maxGroups {
		ordered = ordered[:maxGroups]
	}
	out.Totals.GroupsReturned = len(ordered)

	for _, st := range ordered {
		sort.Strings(st.logGroups)
		multi := len(st.logGroups) > 1
		g := SummaryGroup{
			Key:           st.key,
			Count:         st.count,
			Levels:        st.levels,
			LogGroups:     st.logGroups,
			FirstAt:       formatSummaryTime(st.first.event.Timestamp),
			LastAt:        formatSummaryTime(st.last.event.Timestamp),
			Errors:        make([]map[string]any, 0, len(st.errors)),
			ErrorsDropped: st.listed - len(st.errors),
			First:         projectSummaryEvent(st.first, projection, flatten, multi),
			Last:          projectSummaryEvent(st.last, projection, flatten, multi),
		}
		for _, ev := range st.errors {
			g.Errors = append(g.Errors, projectSummaryEvent(ev, projection, flatten, multi))
		}
		out.Groups = append(out.Groups, g)
	}
	for _, ev := range ungrouped {
		out.UngroupedSample = append(out.UngroupedSample, projectSummaryEvent(ev, projection, flatten, true))
	}

	if filter != nil && filter.StartTime != nil {
		out.Window.From = formatSummaryTime(*filter.StartTime)
	} else if !firstAt.IsZero() {
		out.Window.From = formatSummaryTime(firstAt)
	}
	if filter != nil && filter.EndTime != nil {
		out.Window.To = formatSummaryTime(*filter.EndTime)
	} else if !lastAt.IsZero() {
		out.Window.To = formatSummaryTime(lastAt)
	}

	return out, nil
}

// summaryCandidates returns events matching filter across groups, oldest
// first. When more than limit match, the newest limit are kept.
func (s *Store) summaryCandidates(filter *LogFilter, limit int) ([]GroupedLogEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if filter != nil && filter.Pattern != "" && filter.patternMatcher == nil {
		filter.patternMatcher = CompilePatternMatcher(filter.Pattern)
	}

	var allowed map[string]struct{}
	if filter != nil && len(filter.Groups) > 0 {
		allowed = make(map[string]struct{}, len(filter.Groups))
		for _, name := range filter.Groups {
			allowed[name] = struct{}{}
		}
	}

	var all []GroupedLogEvent
	for _, g := range s.groups {
		if allowed != nil {
			if _, ok := allowed[g.name]; !ok {
				continue
			}
		}
		start := (g.head - g.count + g.maxEvents) % g.maxEvents
		for i := 0; i < g.count; i++ {
			evt := g.events[(start+i)%g.maxEvents]
			if !matchesFilter(evt, filter) {
				continue
			}
			all = append(all, GroupedLogEvent{Group: g.name, Event: evt})
		}
	}

	// Stable, with the group name as tie-break, because lines without a
	// timestamp prefix are stamped at ingest and often share one.
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Event.Timestamp.Equal(all[j].Event.Timestamp) {
			return all[i].Event.Timestamp.Before(all[j].Event.Timestamp)
		}
		return all[i].Group < all[j].Group
	})

	if len(all) > limit {
		return all[len(all)-limit:], true
	}
	return all, false
}

// summaryKey resolves the group key for an event.
func summaryKey(ev *summaryEvent, path []string, byMessage bool) (string, bool) {
	if byMessage {
		if ev.doc != nil {
			if msg, ok := ev.doc["message"].(string); ok && msg != "" {
				return truncateRunes(msg, summaryRawKeyLimit), true
			}
		}
		return truncateRunes(ev.event.Message, summaryRawKeyLimit), true
	}
	if ev.doc == nil {
		return "", false
	}
	v, ok := lookupPath(ev.doc, path)
	if !ok {
		return "", false
	}
	switch t := v.(type) {
	case string:
		if t == "" {
			return "", false
		}
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	}
	return "", false
}

// summaryProjection is the parsed form of SummaryOptions.Fields.
type summaryProjection struct {
	all   bool
	paths [][]string
}

func parseSummaryFields(fields []string, groupBy string) (summaryProjection, error) {
	var raw []string
	for _, f := range fields {
		for _, part := range strings.Split(f, ",") {
			if part = strings.TrimSpace(part); part != "" {
				raw = append(raw, part)
			}
		}
	}
	if len(raw) == 0 {
		raw = []string{"message", strings.TrimSpace(groupBy), "level"}
	}

	var p summaryProjection
	seen := map[string]bool{}
	for _, f := range raw {
		if f == "*" {
			return summaryProjection{all: true}, nil
		}
		if seen[f] {
			continue
		}
		seen[f] = true
		path, err := ParseSummaryPath(f)
		if err != nil {
			return p, fmt.Errorf("invalid fields: %w", err)
		}
		p.paths = append(p.paths, path)
	}
	return p, nil
}

// projectSummaryEvent builds a sample event: the projected fields, flattened,
// plus timestamp and, when withGroup is set, the log group it came from.
func projectSummaryEvent(ev *summaryEvent, p summaryProjection, flatten string, withGroup bool) map[string]any {
	out := map[string]any{}
	switch {
	case p.all && ev.doc != nil:
		maps.Copy(out, ev.doc)
	case p.all:
		out["message"] = ev.event.Message
		out["level"] = string(ev.event.Level)
	default:
		for _, path := range p.paths {
			v, ok := lookupPath(ev.doc, path)
			if !ok && len(path) == 1 {
				// Plain-text lines have no JSON, but their message and
				// level are still the most useful thing to show.
				switch path[0] {
				case "message":
					v, ok = ev.event.Message, true
				case "level":
					v, ok = string(ev.event.Level), ev.event.Level != ""
				}
			}
			if ok {
				setPath(out, path, v)
			}
		}
	}

	if flatten != "" {
		for k, v := range out {
			out[k] = flattenValue(v, flatten)
		}
	}
	out["timestamp"] = formatSummaryTime(ev.event.Timestamp)
	if withGroup {
		out["logGroup"] = ev.group
	}
	return out
}

// flattenValue returns v with every object carrying key replaced by that
// key's value. It builds new containers, so shared parsed docs are untouched.
func flattenValue(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		if inner, ok := t[key]; ok {
			return flattenValue(inner, key)
		}
		cp := make(map[string]any, len(t))
		for k, val := range t {
			cp[k] = flattenValue(val, key)
		}
		return cp
	case []any:
		cp := make([]any, len(t))
		for i, val := range t {
			cp[i] = flattenValue(val, key)
		}
		return cp
	}
	return v
}

func lookupPath(doc map[string]any, path []string) (any, bool) {
	if doc == nil {
		return nil, false
	}
	var cur any = doc
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[seg]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// setPath writes v at path in out, creating parent objects as needed.
func setPath(out map[string]any, path []string, v any) {
	cur := out
	for _, seg := range path[:len(path)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
	cur[path[len(path)-1]] = v
}

// parseJSONObject parses message as a JSON object, returning nil for anything
// else. Numbers are kept as json.Number so IDs survive intact.
func parseJSONObject(message string) map[string]any {
	trimmed := strings.TrimSpace(message)
	if !strings.HasPrefix(trimmed, "{") {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil
	}
	return doc
}

// structuredLevel reads a JSON line's own level: a name such as "error" or
// "warning", or a pino/bunyan number (50 error, 40 warn, 30 info).
func structuredLevel(doc map[string]any) (LogLevel, bool) {
	if doc == nil {
		return "", false
	}
	switch v := doc["level"].(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "error", "err", "fatal", "critical", "crit", "panic", "alert", "emergency":
			return LevelERROR, true
		case "warn", "warning":
			return LevelWARN, true
		case "info", "notice", "information":
			return LevelINFO, true
		case "debug", "trace", "verbose":
			return LevelDEBUG, true
		}
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return "", false
		}
		switch {
		case n >= 50:
			return LevelERROR, true
		case n >= 40:
			return LevelWARN, true
		case n >= 30:
			return LevelINFO, true
		default:
			return LevelDEBUG, true
		}
	}
	return "", false
}

func formatSummaryTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func clampSummary(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// runtimeNoiseMarkers identify container lifecycle chatter. These are emitted
// by the Lambda runtime interface emulator and by Tarn's own invocation
// boundary markers, not by the function under test.
var runtimeNoiseMarkers = []string{
	"(rapid)",
	"[secrets-proxy]",
	"START RequestId:",
	"END RequestId:",
	"REPORT RequestId:",
}

// IsRuntimeNoise reports whether an event is container chatter rather than
// something the function itself produced.
//
// Only recognized markers are filtered. Treating every runtime-sourced line as
// noise would be fail-closed, and the consumer is often a model that cannot
// ask what was withheld: an unrecognized line is far better shown than
// silently dropped.
func IsRuntimeNoise(source, message string) bool {
	if source == string(SourceOutput) {
		return false
	}
	for _, marker := range runtimeNoiseMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
