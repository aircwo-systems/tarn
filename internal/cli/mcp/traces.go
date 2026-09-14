package mcp

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TracesInput selects which recorded request traces to return.
type TracesInput struct {
	CorrelationID string `json:"correlationId,omitempty" jsonschema:"Exact match. Async hops (an SQS poller batch, an EventBridge target, a Step Functions task) join their trace to the request that triggered them by this ID, which SQS consumers read from a correlationId message attribute. Pass the ID from one trace to pull every hop it touched."`
	Resource      string `json:"resource,omitempty" jsonschema:"Substring match against any span's resource name (a function, queue, topic, table, or ECS service). Use this to find traces that touched a specific resource without knowing its correlation ID."`
	Kind          string `json:"kind,omitempty" jsonschema:"Exact match against a span kind, for example lambda, queue, gateway, or ecs. Narrows to traces with at least one span of this kind."`
	Limit         int    `json:"limit,omitempty" jsonschema:"Maximum traces to return, newest first. Defaults to 20, capped at 100."`

	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Accepted for consistency with the other tools, but traces are instance-wide, not account-scoped: a single request path can cross accounts (for example an EventBridge rule in one account firing an ECS task recorded under another), so this does not filter the result."`
}

// TraceInfo is one recorded request: a correlation ID plus every hop it took.
type TraceInfo struct {
	ID            string     `json:"id"`
	CorrelationID string     `json:"correlationId,omitempty"`
	StartedAt     string     `json:"startedAt" jsonschema:"RFC3339 timestamp."`
	DurationMs    int64      `json:"durationMs"`
	Status        int        `json:"status"`
	Method        string     `json:"method,omitempty"`
	Path          string     `json:"path,omitempty"`
	Spans         []SpanInfo `json:"spans"`
}

// SpanInfo is one hop within a trace.
type SpanInfo struct {
	Kind       string            `json:"kind" jsonschema:"gateway, lambda, queue, topic, ecs, stepfunctions, and similar."`
	Name       string            `json:"name" jsonschema:"The resource this hop ran against."`
	DurationMs int64             `json:"durationMs"`
	Status     string            `json:"status" jsonschema:"ok, error, or client_error."`
	Meta       map[string]string `json:"meta,omitempty"`
}

// TracesOutput carries the matching traces, newest first.
type TracesOutput struct {
	Traces []TraceInfo `json:"traces"`
}

const tracesDescription = `Read recorded request traces from the local Tarn instance.

A trace is one record per request path through Tarn: an API Gateway call into
a Lambda, an SQS queue's poller delivering a batch to a Lambda, an EventBridge
rule firing its targets (including an ECS task), or a Step Functions
execution's tasks. Each trace has a list of spans, one per hop, with the kind
of resource, its name, how long it took, and whether it succeeded.

Asynchronous pipelines have no caller left waiting for a result, so a trace is
often the only record that hops happened at all and in what order. When a
queue, topic, schedule, or state machine triggers work, use this tool to see
the whole path end to end: which consumer picked up a message, which task an
EventBridge rule started, how long each hop took, and where a hop failed.

Hops started by different requests share a trace when they carry the same
correlation ID: SQS consumers read it from a correlationId message attribute,
so publishing with that attribute and pulling the trace by correlationId is
the way to verify a full queue -> Lambda -> downstream pipeline.

Use tarn_get_logs instead when a span shows a failure and the trace's status
alone does not explain it: the span says a Lambda invocation errored, the logs
say why. Use this tool first to find which hop failed and its resource name,
then read that resource's logs.

Traces are instance-wide, not scoped to an account.`

// addTracesTool wires tarn_get_traces to an instance client.
func addTracesTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_get_traces",
		Description: tracesDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Read request traces",
			ReadOnlyHint: true,
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in TracesInput) (
		*mcp.CallToolResult, TracesOutput, error,
	) {
		limit := clampInt(in.Limit, 20, 100)

		q := url.Values{}
		if strings.TrimSpace(in.CorrelationID) != "" {
			q.Set("correlationId", in.CorrelationID)
		}
		if strings.TrimSpace(in.Resource) != "" {
			q.Set("resource", in.Resource)
		}
		if strings.TrimSpace(in.Kind) != "" {
			q.Set("kind", in.Kind)
		}
		q.Set("limit", strconv.Itoa(limit))

		var raw struct {
			Traces []struct {
				ID            string    `json:"id"`
				CorrelationID string    `json:"correlationId"`
				StartedAt     time.Time `json:"startedAt"`
				DurationMs    int64     `json:"durationMs"`
				Status        int       `json:"status"`
				Method        string    `json:"method"`
				Path          string    `json:"path"`
				Spans         []struct {
					Kind       string            `json:"kind"`
					Name       string            `json:"name"`
					DurationMs int64             `json:"durationMs"`
					Status     string            `json:"status"`
					Meta       map[string]string `json:"meta"`
				} `json:"spans"`
			} `json:"traces"`
		}
		if err := c.get(ctx, "/_tarn/admin/traces", in.Account, q, &raw); err != nil {
			return nil, TracesOutput{}, err
		}

		out := TracesOutput{Traces: make([]TraceInfo, 0, len(raw.Traces))}
		for _, t := range raw.Traces {
			info := TraceInfo{
				ID:            t.ID,
				CorrelationID: t.CorrelationID,
				StartedAt:     t.StartedAt.UTC().Format(time.RFC3339Nano),
				DurationMs:    t.DurationMs,
				Status:        t.Status,
				Method:        t.Method,
				Path:          t.Path,
				Spans:         make([]SpanInfo, 0, len(t.Spans)),
			}
			for _, sp := range t.Spans {
				info.Spans = append(info.Spans, SpanInfo{
					Kind:       sp.Kind,
					Name:       sp.Name,
					DurationMs: sp.DurationMs,
					Status:     sp.Status,
					Meta:       sp.Meta,
				})
			}
			out.Traces = append(out.Traces, info)
		}

		return nil, out, nil
	}

	mcp.AddTool(s, tool, handler)
}
