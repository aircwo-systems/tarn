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

// ClearLogGroup removes all events from a group but keeps the group.
func (s *Service) ClearLogGroup(name string) error {
	return s.store.ClearGroup(name)
}

// GetAllLogEvents returns filtered events across all log groups.
func (s *Service) GetAllLogEvents(filter *LogFilter) ([]LogEvent, int) {
	return s.store.GetAllLogEvents(filter)
}

// PruneOlderThan removes events older than the given cutoff from all groups.
func (s *Service) PruneOlderThan(cutoff time.Time) int {
	return s.store.PruneOlderThan(cutoff)
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
			Source:    SourceSystem,
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
			Source:    SourceAPI,
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
		msg, level, source := classifyLambdaLogEvent(msg)

		events = append(events, LogEvent{
			Timestamp: ts,
			Message:   msg,
			Level:     level,
			Source:    source,
		})
	}

	if len(events) > 0 {
		s.store.PutLogEvents(groupName, streamName, events)
	}
}

func classifyLambdaLogEvent(msg string) (string, LogLevel, LogSource) {
	if outputMsg, outputLevel, ok := parseLambdaOutputRecord(msg); ok {
		return outputMsg, outputLevel, SourceOutput
	}
	// Spring Boot / Log4j / Logback format: leading whitespace then LEVEL token
	// e.g. "  INFO 1 --- [main] com.example : message" or "ERROR [main] ..."
	if level, ok := extractLeadingLevel(msg); ok {
		return msg, level, SourceOutput
	}
	return msg, detectLevel(msg), SourceRuntime
}

// extractLeadingLevel checks if the message starts with (optional whitespace +) a log level
// token (INFO, DEBUG, WARN, ERROR, TRACE), as produced by Spring Boot / Log4j / Logback.
func extractLeadingLevel(msg string) (LogLevel, bool) {
	trimmed := strings.TrimLeft(msg, " \t")
	for _, level := range []LogLevel{LevelERROR, LevelWARN, LevelDEBUG, LevelINFO} {
		token := string(level)
		if strings.HasPrefix(trimmed, token) && len(trimmed) > len(token) {
			next := trimmed[len(token)]
			if next == ' ' || next == '\t' || next == ':' {
				return level, true
			}
		}
	}
	return "", false
}

func parseLambdaOutputRecord(msg string) (string, LogLevel, bool) {
	parts := strings.SplitN(msg, "\t", 3)
	if len(parts) != 3 {
		return "", "", false
	}

	requestID := strings.TrimSpace(parts[0])
	levelToken := strings.TrimSpace(strings.ToUpper(parts[1]))
	message := strings.TrimSpace(parts[2])

	if !looksLikeLambdaRequestID(requestID) || !isLambdaOutputLevel(levelToken) || message == "" {
		return "", "", false
	}

	return message, LogLevel(levelToken), true
}

func looksLikeLambdaRequestID(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t:") {
		return false
	}

	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}

	return true
}

func isLambdaOutputLevel(value string) bool {
	switch LogLevel(value) {
	case LevelDEBUG, LevelINFO, LevelWARN, LevelERROR:
		return true
	default:
		return false
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
// Handles the following formats:
//   - "2024-01-15T10:30:00.000Z"          (Node.js / Python Lambda, RFC3339 with Z)
//   - "2024-01-15T10:30:00.000+00:00"     (Spring Boot 3, RFC3339 with offset)
//   - "2024-01-15 10:30:00.000  INFO ..."  (Spring Boot 2, space separator)
//   - "2024-01-15T10:30:00.000Z\tReqId\t" (tab-separated Node.js extended)
func parseTimestamp(line string) (time.Time, string) {
	if len(line) < 19 {
		return time.Now().UTC(), line
	}

	// ISO 8601 with T separator: "2024-01-15T..."
	if line[4] == '-' && line[7] == '-' && line[10] == 'T' {
		// Scan forward from the time part to find where the timestamp ends.
		// Valid endings: 'Z' (UTC), '+' or '-' (offset), ' ' or '\t' (no timezone).
		// Spring Boot 3 uses "+00:00"; Node.js uses "Z"; some runtimes use space.
		suffix := line[10:]
		for i, ch := range suffix {
			if ch == 'Z' {
				end := 10 + i + 1
				if ts, err := time.Parse(time.RFC3339Nano, line[:end]); err == nil {
					msg := strings.TrimSpace(line[end:])
					if msg == "" {
						msg = line
					}
					return ts, msg
				}
			} else if (ch == '+' || ch == '-') && i > 0 {
				// Could be start of timezone offset like "+00:00"; try consuming 6 more chars
				if i+7 <= len(suffix) {
					end := 10 + i + 6
					if ts, err := time.Parse(time.RFC3339Nano, line[:end]); err == nil {
						msg := strings.TrimSpace(line[end:])
						if msg == "" {
							msg = line
						}
						return ts, msg
					}
				}
			} else if (ch == ' ' || ch == '\t') && i >= 8 {
				// No timezone — try parsing as-is
				end := 10 + i
				if ts, err := time.Parse("2006-01-02T15:04:05.999999999", line[:end]); err == nil {
					msg := strings.TrimSpace(line[end:])
					if msg == "" {
						msg = line
					}
					return ts, msg
				}
				break
			}
		}
	}

	// Space-separated date: "2024-01-15 10:30:00.000  INFO ..." (Spring Boot 2 / Log4j)
	if len(line) >= 23 && line[4] == '-' && line[7] == '-' && line[10] == ' ' && line[13] == ':' && line[16] == ':' {
		// Try parsing "2024-01-15 10:30:00.000" as a local timestamp (no timezone)
		for _, layout := range []string{"2006-01-02 15:04:05.000", "2006-01-02 15:04:05"} {
			end := len(layout)
			if end <= len(line) {
				if ts, err := time.ParseInLocation(layout, line[:end], time.UTC); err == nil {
					msg := strings.TrimSpace(line[end:])
					if msg == "" {
						msg = line
					}
					return ts, msg
				}
			}
		}
	}

	// Tab-separated timestamp (Node.js extended: "2024-01-15T10:30:00.000Z\tRequestId\t...")
	if idx := strings.IndexByte(line, '\t'); idx > 10 && idx < 35 {
		if ts, err := time.Parse(time.RFC3339Nano, line[:idx]); err == nil {
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
