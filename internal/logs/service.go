package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/openstack-project/openstack/internal/config"
)

// Service implements logging business logic.
type Service struct {
	cfg   *config.Config
	store *Store
}

// NewService creates a new logs service and initializes system log groups.
func NewService(cfg *config.Config) *Service {
	s := &Service{
		cfg:   cfg,
		store: NewStore(cfg.LogsMaxEventsPerGroup),
	}
	s.store.CreateGroup("/openstack/api")
	s.store.CreateGroup("/openstack/system")
	return s
}

// CreateLogGroup creates a log group (idempotent).
func (s *Service) CreateLogGroup(name string) {
	s.store.CreateGroup(name)
}

// DeleteLogGroup removes a log group and all its events.
func (s *Service) DeleteLogGroup(name string) error {
	return s.store.DeleteGroup(name)
}

// PutLogEvents appends events to a log group.
func (s *Service) PutLogEvents(groupName, streamName string, events []LogEvent) {
	s.store.PutLogEvents(groupName, streamName, events)
}

// GetLogEvents returns filtered events from a log group.
func (s *Service) GetLogEvents(groupName string, filter *LogFilter) ([]LogEvent, int) {
	return s.store.GetLogEvents(groupName, filter)
}

// ListGroups returns summaries of all log groups.
func (s *Service) ListGroups() []LogGroupSummary {
	return s.store.ListGroups()
}

// GetGroup returns a summary for a single log group.
func (s *Service) GetGroup(name string) (*LogGroupSummary, error) {
	return s.store.GetGroup(name)
}

// ListStreams returns all streams in a log group.
func (s *Service) ListStreams(groupName string) []LogStream {
	return s.store.ListStreams(groupName)
}

// LogSystemEvent writes an event to the /openstack/system log group.
func (s *Service) LogSystemEvent(level LogLevel, message string) {
	s.store.PutLogEvents("/openstack/system", "system", []LogEvent{
		{
			Timestamp: time.Now().UTC(),
			Message:   message,
			Level:     level,
		},
	})
}

// LogAPIRequest writes an event to the /openstack/api log group.
func (s *Service) LogAPIRequest(method, path string, status int, duration time.Duration) {
	level := LevelINFO
	if status >= 500 {
		level = LevelERROR
	} else if status >= 400 {
		level = LevelWARN
	}

	msg := fmt.Sprintf("%s %s %d %s", method, path, status, duration)
	s.store.PutLogEvents("/openstack/api", "requests", []LogEvent{
		{
			Timestamp: time.Now().UTC(),
			Message:   msg,
			Level:     level,
		},
	})
}

// IngestContainerLogs parses raw Docker container output and writes structured
// log events to the /aws/lambda/{functionName} log group.
func (s *Service) IngestContainerLogs(functionName, streamName, rawLogs string) {
	groupName := fmt.Sprintf("/aws/lambda/%s", functionName)
	s.store.CreateGroup(groupName)

	lines := strings.Split(rawLogs, "\n")
	events := make([]LogEvent, 0, len(lines))

	for _, line := range lines {
		line = cleanDockerLogLine(line)
		if line == "" {
			continue
		}

		ts, msg := parseTimestamp(line)
		level := detectLevel(msg)

		events = append(events, LogEvent{
			Timestamp: ts,
			Message:   msg,
			Level:     level,
		})
	}

	if len(events) > 0 {
		s.store.PutLogEvents(groupName, streamName, events)
	}
}

// cleanDockerLogLine removes Docker log stream header bytes.
// Docker multiplexed log output has an 8-byte header per frame.
func cleanDockerLogLine(line string) string {
	// Docker log frames start with a stream type byte (1=stdout, 2=stderr)
	// followed by 3 zero bytes and 4 bytes of payload length.
	// If the line starts with these control bytes, strip the 8-byte header.
	if len(line) >= 8 && (line[0] == 1 || line[0] == 2) && line[1] == 0 && line[2] == 0 && line[3] == 0 {
		line = line[8:]
	}
	return strings.TrimSpace(line)
}

// parseTimestamp tries to extract a timestamp prefix from a log line.
// Returns the parsed time and the remaining message.
func parseTimestamp(line string) (time.Time, string) {
	// Try ISO 8601 format used by Lambda runtimes: "2024-01-15T10:30:00.000Z"
	if len(line) >= 24 && line[4] == '-' && line[7] == '-' && line[10] == 'T' {
		ts, err := time.Parse(time.RFC3339Nano, line[:24])
		if err == nil {
			msg := strings.TrimSpace(line[24:])
			if msg == "" {
				msg = line
			}
			return ts, msg
		}
		// Try with variable-length fractional seconds
		if idx := strings.IndexByte(line, 'Z'); idx > 10 && idx < 35 {
			ts, err = time.Parse(time.RFC3339Nano, line[:idx+1])
			if err == nil {
				msg := strings.TrimSpace(line[idx+1:])
				if msg == "" {
					msg = line
				}
				return ts, msg
			}
		}
	}

	// Try tab-separated timestamp (some runtimes use "2024-01-15T10:30:00.000Z\tRequestId\t...")
	if idx := strings.IndexByte(line, '\t'); idx > 10 && idx < 35 {
		ts, err := time.Parse(time.RFC3339Nano, line[:idx])
		if err == nil {
			return ts, strings.TrimSpace(line[idx+1:])
		}
	}

	return time.Now().UTC(), line
}

// detectLevel infers the log level from the message content.
func detectLevel(msg string) LogLevel {
	upper := strings.ToUpper(msg)

	// Lambda lifecycle events
	if strings.HasPrefix(upper, "START REQUESTID") || strings.HasPrefix(upper, "START REQUEST") {
		return LevelINFO
	}
	if strings.HasPrefix(upper, "END REQUESTID") || strings.HasPrefix(upper, "END REQUEST") {
		return LevelINFO
	}
	if strings.HasPrefix(upper, "REPORT REQUESTID") || strings.HasPrefix(upper, "REPORT REQUEST") {
		return LevelINFO
	}

	// Error patterns
	if strings.Contains(upper, "ERROR") || strings.Contains(upper, "EXCEPTION") ||
		strings.Contains(upper, "FATAL") || strings.Contains(upper, "PANIC") {
		return LevelERROR
	}

	// Warning patterns
	if strings.Contains(upper, "WARN") || strings.Contains(upper, "WARNING") {
		return LevelWARN
	}

	// Debug patterns
	if strings.Contains(upper, "DEBUG") || strings.Contains(upper, "TRACE") {
		return LevelDEBUG
	}

	return LevelINFO
}
