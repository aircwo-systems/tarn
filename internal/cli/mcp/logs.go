package mcp

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LogsInput selects which log events to return.
type LogsInput struct {
	Function string `json:"function,omitempty" jsonschema:"Lambda function name. Resolves to its log group. Either function or logGroup is required."`
	LogGroup string `json:"logGroup,omitempty" jsonschema:"Full log group name, for example /aws/lambda/my-func. Either function or logGroup is required."`

	Pattern string `json:"pattern,omitempty" jsonschema:"Substring the message must contain. Use this to narrow to a specific error or marker."`
	Level   string `json:"level,omitempty" jsonschema:"Filter to one level: INFO, WARN, or ERROR."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum events to return, newest first. Defaults to 50, capped at 200."`

	IncludeRuntime bool `json:"includeRuntime,omitempty" jsonschema:"Include container runtime chatter: START/END/REPORT markers, init logs, and extension banners. Off by default because it is roughly four fifths of the volume and rarely explains a failure."`

	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// LogEvent is one line from a log group.
type LogEvent struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level,omitempty"`
	Source    string `json:"source,omitempty" jsonschema:"output for what the function printed, runtime for container chatter."`
	Message   string `json:"message"`
}

// LogsOutput carries the selected events plus enough context to widen the
// search when they are not the ones wanted.
type LogsOutput struct {
	LogGroup string     `json:"logGroup"`
	Events   []LogEvent `json:"events" jsonschema:"Oldest first, so the sequence reads in the order it happened."`

	Returned        int  `json:"returned"`
	TotalInGroup    int  `json:"totalInGroup" jsonschema:"Events in the group before filtering."`
	RuntimeFiltered int  `json:"runtimeFiltered,omitempty" jsonschema:"Runtime chatter withheld. Set includeRuntime to see it."`
	Truncated       bool `json:"truncated,omitempty" jsonschema:"Whether older events were cut to satisfy the limit."`
}

const logsDescription = `Read logs a Lambda function wrote on the local Tarn instance.

Returns the newest events, then orders them oldest first so the sequence reads
in the order it happened. This is the tool for failures the invoke response
cannot explain: a handler that caught its own error, returned wrong data, or
printed something you need to see.

Container runtime chatter is withheld by default. Each invocation emits START,
END, REPORT, init markers, and extension banners that together are about four
fifths of the volume and almost never explain a fault. Set includeRuntime to
see them, for example when diagnosing cold starts or timeouts.

Narrow with "pattern" for a substring, or "level" for INFO, WARN, or ERROR.
Searching for a thrown error is usually fastest as level ERROR.`

func addLogsTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_get_logs",
		Description: logsDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Read Lambda logs",
			ReadOnlyHint: true,
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in LogsInput) (
		*mcp.CallToolResult, LogsOutput, error,
	) {
		group := strings.TrimSpace(in.LogGroup)
		if group == "" {
			if strings.TrimSpace(in.Function) == "" {
				return nil, LogsOutput{}, errors.New("either function or logGroup is required")
			}
			group = logGroupFor(in.Function)
		}

		limit := clampInt(in.Limit, 50, 200)

		q := url.Values{}
		// Ask for the tail. The API defaults to the oldest events, which for a
		// debugging caller is container boot noise and nothing else.
		q.Set("order", "desc")
		if in.Pattern != "" {
			q.Set("pattern", in.Pattern)
		}
		if in.Level != "" {
			q.Set("level", strings.ToUpper(in.Level))
		}
		// Over-fetch so runtime filtering still leaves a full page.
		fetch := limit
		if !in.IncludeRuntime {
			fetch = clampInt(limit*6, 300, 1000)
		}
		q.Set("limit", strconv.Itoa(fetch))

		var raw struct {
			Events []struct {
				Timestamp time.Time `json:"timestamp"`
				Message   string    `json:"message"`
				Level     string    `json:"level"`
				Source    string    `json:"source"`
			} `json:"events"`
			Total int `json:"total"`
		}
		if err := c.get(ctx, "/_tarn/admin/logs/events/"+url.PathEscape(group), in.Account, q, &raw); err != nil {
			return nil, LogsOutput{}, err
		}

		out := LogsOutput{LogGroup: group, TotalInGroup: raw.Total}

		type timedEvent struct {
			at    time.Time
			event LogEvent
		}

		kept := make([]timedEvent, 0, len(raw.Events))
		for _, e := range raw.Events {
			if !in.IncludeRuntime && isRuntimeNoise(e.Source, e.Message) {
				out.RuntimeFiltered++
				continue
			}
			kept = append(kept, timedEvent{at: e.Timestamp, event: LogEvent{
				Timestamp: e.Timestamp.UTC().Format(time.RFC3339Nano),
				Level:     e.Level,
				Source:    e.Source,
				Message:   e.Message,
			}})
		}

		// Sort rather than trusting the response order. Runtime chatter is
		// ingested in batches, so its timestamps interleave with the function's
		// own output rather than following it.
		sort.SliceStable(kept, func(i, j int) bool { return kept[i].at.After(kept[j].at) })
		if len(kept) > limit {
			kept = kept[:limit]
			out.Truncated = true
		}
		// Present oldest first so the sequence reads in the order it happened.
		sort.SliceStable(kept, func(i, j int) bool { return kept[i].at.Before(kept[j].at) })

		events := make([]LogEvent, 0, len(kept))
		for _, k := range kept {
			events = append(events, k.event)
		}

		out.Events = events
		out.Returned = len(events)
		return nil, out, nil
	}

	mcp.AddTool(s, tool, handler)
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

// isRuntimeNoise reports whether an event is container chatter rather than
// something the function itself produced.
//
// Only recognized markers are filtered. Treating every runtime-sourced line as
// noise would be fail-closed, and the consumer here is a model that cannot ask
// what was withheld — an unrecognized line is far better shown than silently
// dropped.
func isRuntimeNoise(source, message string) bool {
	if source == "output" {
		return false
	}
	for _, marker := range runtimeNoiseMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
