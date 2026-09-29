package mcp

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SummarizeLogsInput selects events and says how to group them.
type SummarizeLogsInput struct {
	GroupBy string   `json:"groupBy" jsonschema:"JSON field to group structured log lines by. Dotted paths are allowed, for example outcomes.certificate.kind. Use correlationId for one run end to end, an entity ID such as orderId for one outcome per entity, or message to count each kind of log line."`
	Fields  []string `json:"fields,omitempty" jsonschema:"Fields to keep in each sample event, dotted paths allowed. Defaults to message, the groupBy field and level. [\"*\"] keeps everything."`
	Flatten string   `json:"flatten,omitempty" jsonschema:"Key name, typically kind. Any object with this key is replaced by its value, so {\"kind\":\"deleted\",\"statusCode\":204} becomes \"deleted\". Use it for outcome-style logs."`

	Function string   `json:"function,omitempty" jsonschema:"Lambda function name. Limits the summary to its log group. Omit both function and logGroup to search every group, which is what joins a pipeline's services together."`
	LogGroup string   `json:"logGroup,omitempty" jsonschema:"Full log group name, for example /ecs/my-task. Combined with function and groups."`
	Groups   []string `json:"groups,omitempty" jsonschema:"Log group names to search, for scoping to exactly a pipeline's services. Combined with function and logGroup."`

	Pattern string `json:"pattern,omitempty" jsonschema:"Substring every considered line must contain."`
	Level   string `json:"level,omitempty" jsonschema:"Only consider these levels: INFO, WARN, ERROR, or a comma-separated set. With WARN included, warnings are listed alongside errors."`
	Stream  string `json:"stream,omitempty" jsonschema:"Exact log stream name."`
	Since   string `json:"since,omitempty" jsonschema:"RFC3339 lower bound on event time. Set it to just before the flow was triggered to summarise only that run."`
	Until   string `json:"until,omitempty" jsonschema:"RFC3339 upper bound on event time."`

	MaxGroups         int `json:"maxGroups,omitempty" jsonschema:"Maximum groups returned. Defaults to 50, capped at 500. Error groups come first, so they are never the ones cut."`
	MaxErrorsPerGroup int `json:"maxErrorsPerGroup,omitempty" jsonschema:"Maximum errors listed per group, oldest first. Defaults to 5, capped at 50."`

	IncludeRuntime bool `json:"includeRuntime,omitempty" jsonschema:"Include container runtime chatter (START/END/REPORT markers, init logs, extension banners) and Tarn's own /tarn/api request log. Off by default."`

	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// SummaryWindow is the time span the summary covers.
type SummaryWindow struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// SummaryTotals accounts for every event considered.
type SummaryTotals struct {
	EventsScanned   int  `json:"eventsScanned"`
	EventsGrouped   int  `json:"eventsGrouped"`
	Ungrouped       int  `json:"ungrouped" jsonschema:"Lines that are not JSON or lack the groupBy field. A sample is in ungroupedSample."`
	RuntimeFiltered int  `json:"runtimeFiltered,omitempty" jsonschema:"Runtime chatter and /tarn/api request lines withheld. Set includeRuntime to see them."`
	Groups          int  `json:"groups" jsonschema:"Groups found, before maxGroups was applied."`
	GroupsReturned  int  `json:"groupsReturned"`
	ErrorGroups     int  `json:"errorGroups" jsonschema:"Groups with at least one ERROR event."`
	TruncatedScan   bool `json:"truncatedScan,omitempty" jsonschema:"Whether only the newest 50 000 matching events were considered."`
}

// SummaryGroup is every event sharing one groupBy value.
type SummaryGroup struct {
	Key           string           `json:"key"`
	Count         int              `json:"count"`
	Levels        map[string]int   `json:"levels"`
	LogGroups     []string         `json:"logGroups"`
	FirstAt       string           `json:"firstAt"`
	LastAt        string           `json:"lastAt"`
	Errors        []map[string]any `json:"errors" jsonschema:"ERROR events in full, oldest first."`
	ErrorsDropped int              `json:"errorsDropped"`
	First         map[string]any   `json:"first"`
	Last          map[string]any   `json:"last" jsonschema:"The newest event, usually the outcome line."`
}

// SummarizeLogsOutput is the grouped summary.
type SummarizeLogsOutput struct {
	GroupBy         string           `json:"groupBy"`
	Window          SummaryWindow    `json:"window"`
	Totals          SummaryTotals    `json:"totals"`
	Groups          []SummaryGroup   `json:"groups" jsonschema:"Error groups first, then the rest newest first."`
	UngroupedSample []map[string]any `json:"ungroupedSample"`
}

const summarizeLogsDescription = `Summarise structured logs on the local Tarn instance, grouped by a JSON field.

Use this first after triggering a flow, to answer "what happened to each X?"
in one call. It returns one entry per value of groupBy with a count per level,
every error in full, the first and last event, and only the fields asked for.
Its size follows the number of groups, not the number of lines.

Choosing groupBy:
- correlationId: one run end to end. Groups span every log group, so an ECS
  task and the Lambda it fed appear together.
- an entity ID (orderId, certificateId): one outcome per entity. The "last"
  event is usually the outcome line.
- message: counts of each kind of log line. Good for "did anything unusual
  happen?"

For outcome-style logs such as {"kind":"deleted","statusCode":204}, pass
flatten "kind" to shrink them to "deleted".

Lines that are not JSON, or lack the field, are counted in totals.ungrouped
and sampled, never silently dropped. Searches every log group unless function,
logGroup or groups is given. Runtime chatter and Tarn's own /tarn/api request
log are withheld and counted unless includeRuntime is set.

Then use tarn_get_logs with a pattern to read one group's raw lines, and
tarn_get_traces for hop-by-hop timing.`

func addSummarizeLogsTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_summarize_logs",
		Description: summarizeLogsDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Summarise logs by field",
			ReadOnlyHint: true,
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in SummarizeLogsInput) (
		*mcp.CallToolResult, SummarizeLogsOutput, error,
	) {
		if strings.TrimSpace(in.GroupBy) == "" {
			return nil, SummarizeLogsOutput{}, errors.New("groupBy is required, for example correlationId, an entity ID field, or message")
		}

		q := url.Values{}
		q.Set("groupBy", strings.TrimSpace(in.GroupBy))

		var groups []string
		if fn := strings.TrimSpace(in.Function); fn != "" {
			groups = append(groups, logGroupFor(fn))
		}
		if g := strings.TrimSpace(in.LogGroup); g != "" && g != "*" && g != "all" && g != "__all__" {
			groups = append(groups, g)
		}
		for _, g := range in.Groups {
			if g = strings.TrimSpace(g); g != "" {
				groups = append(groups, g)
			}
		}
		if len(groups) > 0 {
			q.Set("groups", strings.Join(groups, ","))
		}

		if len(in.Fields) > 0 {
			q.Set("fields", strings.Join(in.Fields, ","))
		}
		setIf := func(key, v string) {
			if v = strings.TrimSpace(v); v != "" {
				q.Set(key, v)
			}
		}
		setIf("flatten", in.Flatten)
		setIf("pattern", in.Pattern)
		setIf("level", strings.ToUpper(in.Level))
		setIf("stream", in.Stream)
		setIf("since", in.Since)
		setIf("until", in.Until)
		if in.MaxGroups > 0 {
			q.Set("maxGroups", strconv.Itoa(in.MaxGroups))
		}
		if in.MaxErrorsPerGroup > 0 {
			q.Set("maxErrorsPerGroup", strconv.Itoa(in.MaxErrorsPerGroup))
		}
		if in.IncludeRuntime {
			q.Set("includeRuntime", "true")
		}

		var out SummarizeLogsOutput
		if err := c.get(ctx, "/_tarn/admin/logs/summary", in.Account, q, &out); err != nil {
			return nil, SummarizeLogsOutput{}, err
		}
		if out.Groups == nil {
			out.Groups = []SummaryGroup{}
		}
		if out.UngroupedSample == nil {
			out.UngroupedSample = []map[string]any{}
		}
		for i := range out.Groups {
			if out.Groups[i].Errors == nil {
				out.Groups[i].Errors = []map[string]any{}
			}
			if out.Groups[i].LogGroups == nil {
				out.Groups[i].LogGroups = []string{}
			}
		}
		return nil, out, nil
	}

	mcp.AddTool(s, tool, handler)
}
