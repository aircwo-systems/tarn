package logs

import (
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

// LogEvent is a single log entry.
type LogEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	Message    string    `json:"message"`
	Level      LogLevel  `json:"level"`
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
	Limit      int
	Offset     int
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

// GetLogEvents returns filtered events from a group, ordered oldest to newest.
func (s *Store) GetLogEvents(groupName string, filter *LogFilter) ([]LogEvent, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g, exists := s.groups[groupName]
	if !exists {
		return nil, 0
	}

	if g.count == 0 {
		return nil, 0
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

	total := len(matched)

	// Apply offset and limit
	if filter != nil && filter.Offset > 0 {
		if filter.Offset >= len(matched) {
			return nil, total
		}
		matched = matched[filter.Offset:]
	}
	if filter != nil && filter.Limit > 0 && filter.Limit < len(matched) {
		matched = matched[:filter.Limit]
	}

	return matched, total
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

func matchesFilter(evt LogEvent, filter *LogFilter) bool {
	if filter == nil {
		return true
	}
	if filter.Level != "" && evt.Level != filter.Level {
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
	if filter.Pattern != "" && !strings.Contains(strings.ToLower(evt.Message), strings.ToLower(filter.Pattern)) {
		return false
	}
	return true
}
