package logs

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// LogLevel represents log severity.
type LogLevel string

const (
	LevelDEBUG LogLevel = "DEBUG"
	LevelINFO  LogLevel = "INFO"
	LevelWARN  LogLevel = "WARN"
	LevelERROR LogLevel = "ERROR"
)

// LogSource describes where a log line originated.
type LogSource string

const (
	SourceSystem  LogSource = "system"
	SourceAPI     LogSource = "api"
	SourceRuntime LogSource = "runtime"
	SourceOutput  LogSource = "output"
)

// LogEvent is a single log entry.
type LogEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	Message    string    `json:"message"`
	Level      LogLevel  `json:"level"`
	Source     LogSource `json:"source,omitempty"`
	StreamName string    `json:"streamName"`
}

// LogStream represents a log stream within a group.
type LogStream struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	LastEvent time.Time `json:"lastEvent"`
}

// LogGroupSummary is the external representation of a log group.
type LogGroupSummary struct {
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	EventCount  int       `json:"eventCount"`
	StreamCount int       `json:"streamCount"`
	LastEvent   time.Time `json:"lastEvent,omitempty"`
}

// LogFilter specifies criteria for querying log events.
type LogFilter struct {
	StartTime  *time.Time
	EndTime    *time.Time
	Level      LogLevel
	Pattern    string
	StreamName string
	Order      string
	Limit      int
	Offset     int        // Deprecated: use Cursor for pagination
	Cursor     *time.Time // Asc: after this timestamp. Desc: before this timestamp.
	Groups     []string   // Optional: filter to specific log group names
}

// LogScanFilter specifies criteria for scanning log events across groups.
type LogScanFilter struct {
	Pattern    string     `json:"pattern"`
	Level      LogLevel   `json:"level"`
	StartTime  *time.Time `json:"startTime,omitempty"`
	EndTime    *time.Time `json:"endTime,omitempty"`
	Groups     []string   `json:"groups,omitempty"`
	MaxSamples int        `json:"maxSamples,omitempty"`
}

// LogGroupScanResult represents match stats for a single log group.
type LogGroupScanResult struct {
	GroupName    string     `json:"groupName"`
	MatchCount   int        `json:"matchCount"`
	TotalEvents  int        `json:"totalEvents"`
	LastMatch    *time.Time `json:"lastMatch,omitempty"`
	SampleEvents []LogEvent `json:"sampleEvents,omitempty"`
}

// LogScanResult represents the aggregated result of scanning logs across groups.
type LogScanResult struct {
	Pattern      string               `json:"pattern"`
	TotalMatches int                  `json:"totalMatches"`
	TotalScanned int                  `json:"totalScanned"`
	Groups       []LogGroupScanResult `json:"groups"`
	DurationMs   float64              `json:"durationMs"`
}

// logGroup holds events in a ring buffer.
type logGroup struct {
	name      string
	createdAt time.Time
	streams   map[string]*LogStream
	events    []LogEvent
	head      int // next write position
	count     int // number of stored events
	maxEvents int
}

// Store holds all log groups in memory.
type Store struct {
	mu        sync.RWMutex
	groups    map[string]*logGroup
	maxEvents int
}

// NewStore creates a store with the given ring buffer size per group.
func NewStore(maxEventsPerGroup int) *Store {
	if maxEventsPerGroup <= 0 {
		maxEventsPerGroup = 10000
	}
	return &Store{
		groups:    make(map[string]*logGroup),
		maxEvents: maxEventsPerGroup,
	}
}

// CreateGroup creates a log group if it does not already exist.
func (s *Store) CreateGroup(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.groups[name]; exists {
		return
	}
	s.groups[name] = &logGroup{
		name:      name,
		createdAt: time.Now().UTC(),
		streams:   make(map[string]*LogStream),
		events:    make([]LogEvent, s.maxEvents),
		maxEvents: s.maxEvents,
	}
}

// DeleteGroup removes a log group and all its events.
func (s *Store) DeleteGroup(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.groups, name)
	return nil
}

// ClearGroup removes all events and streams from a group but keeps the group itself.
func (s *Store) ClearGroup(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	g, exists := s.groups[name]
	if !exists {
		return fmt.Errorf("log group not found: %s", name)
	}

	g.events = make([]LogEvent, g.maxEvents)
	g.head = 0
	g.count = 0
	g.streams = make(map[string]*LogStream)
	return nil
}

// PutLogEvents appends events to a group's ring buffer.
func (s *Store) PutLogEvents(groupName, streamName string, events []LogEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	g, exists := s.groups[groupName]
	if !exists {
		return
	}

	now := time.Now().UTC()

	// Ensure stream exists
	stream, ok := g.streams[streamName]
	if !ok {
		stream = &LogStream{
			Name:      streamName,
			CreatedAt: now,
		}
		g.streams[streamName] = stream
	}

	for _, evt := range events {
		evt.StreamName = streamName
		g.events[g.head] = evt
		g.head = (g.head + 1) % g.maxEvents
		if g.count < g.maxEvents {
			g.count++
		}
		if evt.Timestamp.After(stream.LastEvent) {
			stream.LastEvent = evt.Timestamp
		}
	}
}

// GetLogEvents returns filtered events from a group in the requested order.
func (s *Store) GetLogEvents(groupName string, filter *LogFilter) ([]LogEvent, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g, exists := s.groups[groupName]
	if !exists {
		return nil, 0, false
	}

	if g.count == 0 {
		return nil, 0, false
	}

	// Collect matching events from ring buffer (oldest first)
	var matched []LogEvent
	start := (g.head - g.count + g.maxEvents) % g.maxEvents

	for i := 0; i < g.count; i++ {
		idx := (start + i) % g.maxEvents
		evt := g.events[idx]

		if !matchesFilter(evt, filter) {
			continue
		}
		matched = append(matched, evt)
	}

	return paginateEvents(matched, filter)
}

// ScanLogs performs a fast full-text scan across log groups and aggregates match counts.
func (s *Store) ScanLogs(filter *LogScanFilter) *LogScanResult {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()

	pattern := ""
	var level LogLevel
	var startTime, endTime *time.Time
	maxSamples := 3
	var allowedGroups map[string]struct{}

	if filter != nil {
		pattern = strings.TrimSpace(filter.Pattern)
		level = filter.Level
		startTime = filter.StartTime
		endTime = filter.EndTime
		if filter.MaxSamples > 0 {
			maxSamples = filter.MaxSamples
			if maxSamples > 20 {
				maxSamples = 20
			}
		}
		if len(filter.Groups) > 0 {
			allowedGroups = make(map[string]struct{}, len(filter.Groups))
			for _, g := range filter.Groups {
				allowedGroups[g] = struct{}{}
			}
		}
	}

	result := &LogScanResult{
		Pattern: pattern,
		Groups:  make([]LogGroupScanResult, 0),
	}

	for _, g := range s.groups {
		if allowedGroups != nil {
			if _, ok := allowedGroups[g.name]; !ok {
				continue
			}
		}
		result.TotalScanned += g.count
		if g.count == 0 {
			continue
		}

		groupRes := LogGroupScanResult{
			GroupName:    g.name,
			TotalEvents:  g.count,
			SampleEvents: make([]LogEvent, 0, maxSamples),
		}

		ringStart := (g.head - g.count + g.maxEvents) % g.maxEvents
		// Scan newest to oldest for latest match timestamp and fresh samples
		for i := g.count - 1; i >= 0; i-- {
			idx := (ringStart + i) % g.maxEvents
			evt := g.events[idx]

			if level != "" && !levelMatches(evt.Level, level) {
				continue
			}
			if startTime != nil && evt.Timestamp.Before(*startTime) {
				continue
			}
			if endTime != nil && evt.Timestamp.After(*endTime) {
				continue
			}
			if pattern != "" && !ContainsFold(evt.Message, pattern) {
				continue
			}

			groupRes.MatchCount++
			if groupRes.LastMatch == nil || evt.Timestamp.After(*groupRes.LastMatch) {
				ts := evt.Timestamp
				groupRes.LastMatch = &ts
			}
			if len(groupRes.SampleEvents) < maxSamples {
				tagged := evt
				tagged.StreamName = g.name + "/" + evt.StreamName
				groupRes.SampleEvents = append(groupRes.SampleEvents, tagged)
			}
		}

		if groupRes.MatchCount > 0 {
			result.TotalMatches += groupRes.MatchCount
			result.Groups = append(result.Groups, groupRes)
		}
	}

	// Sort groups by MatchCount descending, ties broken by GroupName
	sort.Slice(result.Groups, func(i, j int) bool {
		if result.Groups[i].MatchCount != result.Groups[j].MatchCount {
			return result.Groups[i].MatchCount > result.Groups[j].MatchCount
		}
		return result.Groups[i].GroupName < result.Groups[j].GroupName
	})

	result.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return result
}

// ListGroups returns summaries of all log groups.
func (s *Store) ListGroups() []LogGroupSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]LogGroupSummary, 0, len(s.groups))
	for _, g := range s.groups {
		summary := LogGroupSummary{
			Name:        g.name,
			CreatedAt:   g.createdAt,
			EventCount:  g.count,
			StreamCount: len(g.streams),
		}
		if g.count > 0 {
			summary.LastEvent = s.lastEventTime(g)
		}
		result = append(result, summary)
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// GetGroup returns a summary for a single log group.
func (s *Store) GetGroup(name string) (*LogGroupSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g, exists := s.groups[name]
	if !exists {
		return nil, nil
	}

	summary := &LogGroupSummary{
		Name:        g.name,
		CreatedAt:   g.createdAt,
		EventCount:  g.count,
		StreamCount: len(g.streams),
	}
	if g.count > 0 {
		summary.LastEvent = s.lastEventTime(g)
	}
	return summary, nil
}

// ListStreams returns all streams in a log group.
func (s *Store) ListStreams(groupName string) []LogStream {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g, exists := s.groups[groupName]
	if !exists {
		return nil
	}

	result := make([]LogStream, 0, len(g.streams))
	for _, stream := range g.streams {
		result = append(result, *stream)
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// lastEventTime returns the timestamp of the most recent event in a group.
// Caller must hold at least a read lock.
func (s *Store) lastEventTime(g *logGroup) time.Time {
	if g.count == 0 {
		return time.Time{}
	}
	// The most recent event is at head-1 (wrapping)
	lastIdx := (g.head - 1 + g.maxEvents) % g.maxEvents
	return g.events[lastIdx].Timestamp
}

// PruneOlderThan removes events older than the given cutoff from all groups.
func (s *Store) PruneOlderThan(cutoff time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	totalPruned := 0
	for _, g := range s.groups {
		if g.count == 0 {
			continue
		}

		// Rebuild the ring buffer keeping only events newer than cutoff
		kept := make([]LogEvent, g.maxEvents)
		keptCount := 0
		start := (g.head - g.count + g.maxEvents) % g.maxEvents

		for i := 0; i < g.count; i++ {
			idx := (start + i) % g.maxEvents
			evt := g.events[idx]
			if !evt.Timestamp.Before(cutoff) {
				kept[keptCount%g.maxEvents] = evt
				keptCount++
			}
		}

		pruned := g.count - keptCount
		totalPruned += pruned
		g.events = kept
		g.count = keptCount
		if keptCount > g.maxEvents {
			g.count = g.maxEvents
		}
		g.head = g.count % g.maxEvents
	}
	return totalPruned
}

// GetAllLogEvents returns filtered events across ALL log groups in the requested order.
func (s *Store) GetAllLogEvents(filter *LogFilter) ([]LogEvent, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []LogEvent

	var allowedGroups map[string]struct{}
	if filter != nil && len(filter.Groups) > 0 {
		allowedGroups = make(map[string]struct{}, len(filter.Groups))
		for _, name := range filter.Groups {
			allowedGroups[name] = struct{}{}
		}
	}

	for _, g := range s.groups {
		if allowedGroups != nil {
			if _, ok := allowedGroups[g.name]; !ok {
				continue
			}
		}
		if g.count == 0 {
			continue
		}
		start := (g.head - g.count + g.maxEvents) % g.maxEvents
		for i := 0; i < g.count; i++ {
			idx := (start + i) % g.maxEvents
			evt := g.events[idx]

			if !matchesFilter(evt, filter) {
				continue
			}
			// Tag group name into StreamName for context (prefix with group)
			tagged := evt
			tagged.StreamName = g.name + "/" + evt.StreamName
			all = append(all, tagged)
		}
	}

	// Sort by timestamp ascending (oldest first)
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.Before(all[j].Timestamp)
	})

	return paginateEvents(all, filter)
}

func paginateEvents(events []LogEvent, filter *LogFilter) ([]LogEvent, int, bool) {
	total := len(events)
	if total == 0 {
		return nil, 0, false
	}

	order := "asc"
	if filter != nil && strings.EqualFold(filter.Order, "desc") {
		order = "desc"
	}

	paged := events
	if filter != nil && filter.Cursor != nil {
		cursor := *filter.Cursor
		filtered := paged[:0]
		for _, evt := range paged {
			if order == "desc" {
				if evt.Timestamp.Before(cursor) {
					filtered = append(filtered, evt)
				}
				continue
			}
			if evt.Timestamp.After(cursor) {
				filtered = append(filtered, evt)
			}
		}
		paged = filtered
	}

	if order == "desc" {
		reversed := make([]LogEvent, 0, len(paged))
		for i := len(paged) - 1; i >= 0; i-- {
			reversed = append(reversed, paged[i])
		}
		paged = reversed
	}

	if filter != nil && filter.Offset > 0 {
		if filter.Offset >= len(paged) {
			return nil, total, false
		}
		paged = paged[filter.Offset:]
	}

	hasMore := false
	if filter != nil && filter.Limit > 0 && filter.Limit < len(paged) {
		hasMore = true
		paged = paged[:filter.Limit]
	}

	return paged, total, hasMore
}

// levelMatches reports whether level is in want, a single level or a
// comma-separated set such as "ERROR,WARN".
func levelMatches(level, want LogLevel) bool {
	for _, w := range strings.Split(string(want), ",") {
		if LogLevel(strings.TrimSpace(w)) == level {
			return true
		}
	}
	return false
}

// ContainsFold reports whether substr is within s using case-insensitive ASCII comparison,
// falling back to unicode lowercase comparison if non-ASCII characters are present.
// It performs zero heap allocations in the common ASCII path.
func ContainsFold(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}

	for i := 0; i < len(substr); i++ {
		if substr[i] >= 0x80 {
			return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
		}
	}

	firstLower := substr[0]
	if firstLower >= 'A' && firstLower <= 'Z' {
		firstLower += 'a' - 'A'
	}
	firstUpper := firstLower
	if firstUpper >= 'a' && firstUpper <= 'z' {
		firstUpper -= 'a' - 'A'
	}

	n := len(substr)
	max := len(s) - n
	for i := 0; i <= max; i++ {
		c := s[i]
		if c == firstLower || c == firstUpper {
			matched := true
			for j := 1; j < n; j++ {
				sc := s[i+j]
				if sc >= 'A' && sc <= 'Z' {
					sc += 'a' - 'A'
				}
				subC := substr[j]
				if subC >= 'A' && subC <= 'Z' {
					subC += 'a' - 'A'
				}
				if sc != subC {
					matched = false
					break
				}
			}
			if matched {
				return true
			}
		}
	}
	return false
}

func matchesFilter(evt LogEvent, filter *LogFilter) bool {
	if filter == nil {
		return true
	}
	if filter.Level != "" && !levelMatches(evt.Level, filter.Level) {
		return false
	}
	if filter.StreamName != "" && evt.StreamName != filter.StreamName {
		return false
	}
	if filter.StartTime != nil && evt.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && evt.Timestamp.After(*filter.EndTime) {
		return false
	}
	if filter.Pattern != "" && !ContainsFold(evt.Message, filter.Pattern) {
		return false
	}
	return true
}
