package trace

import (
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
)

const propagationFixture = `{"x-datadog-trace-id":"18446744073709551615","x-datadog-parent-id":"18446744073709551614","x-datadog-tags":"_dd.p.tid=abcdef1234567890,_dd.p.dm=-0"}`

func TestMessagingPropagation(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(propagationFixture))
	for name, msg := range map[string]*types.SQSMessage{
		"sqs string":   {MessageAttributes: map[string]*types.MessageAttribute{"_datadog": {DataType: "String", StringValue: propagationFixture}}},
		"sns binary":   {MessageAttributes: map[string]*types.MessageAttribute{"_datadog": {DataType: "Binary", BinaryValue: []byte(propagationFixture)}}},
		"sns envelope": {Body: `{"Type":"Notification","MessageAttributes":{"_datadog":{"Type":"Binary","Value":"` + encoded + `"}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := ContextFromSQSMessage(msg)
			if ctx == nil || ctx.TraceID != "abcdef1234567890ffffffffffffffff" || ctx.ParentID != "18446744073709551614" {
				t.Fatalf("context = %+v", ctx)
			}
		})
	}
}

func TestPropagationValidation(t *testing.T) {
	for _, carrier := range []string{
		`{`, `null`, `{}`, `{"x-datadog-trace-id":12,"x-datadog-parent-id":"3"}`,
		`{"x-datadog-trace-id":"0","x-datadog-parent-id":"3"}`,
		`{"x-datadog-trace-id":"18446744073709551616","x-datadog-parent-id":"3"}`,
		`{"x-datadog-trace-id":"12","x-datadog-parent-id":"3","x-datadog-tags":"_dd.p.tid=garbage"}`,
	} {
		msg := &types.SQSMessage{MessageAttributes: map[string]*types.MessageAttribute{"_datadog": {StringValue: carrier}}}
		if got := ContextFromSQSMessage(msg); got != nil {
			t.Fatalf("invalid %s produced %+v", carrier, got)
		}
	}
	headers := http.Header{"Traceparent": {"00-abcdef1234567890ffffffffffffffff-fffffffffffffffe-01"}}
	if got := ContextFromHeaders(headers); got == nil || got.TraceID != "abcdef1234567890ffffffffffffffff" || got.ParentID != "18446744073709551614" {
		t.Fatalf("W3C context = %+v", got)
	}
}

func TestRecordJoinedBeforeAndAfterImport(t *testing.T) {
	for _, nativeFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "native first", false: "import first"}[nativeFirst], func(t *testing.T) {
			store := NewStore()
			defer store.Close()
			ctx := ContextFromSQSMessage(&types.SQSMessage{MessageAttributes: map[string]*types.MessageAttribute{"_datadog": {StringValue: propagationFixture}}})
			start := time.Now().Add(-time.Second)
			imported := &Trace{ID: "dd:111111111111:abcdef1234567890ffffffffffffffff", AccountID: "111111111111", CorrelationID: "18446744073709551615", Spans: []Span{{ID: "18446744073709551614", Kind: "service", Name: "POST /orders", StartedAt: &start, DurationNs: int64(time.Second), Meta: map[string]string{"http.route": "/orders", "http.method": "POST", "http.status_code": "200"}}}}
			record := func() {
				store.RecordJoined(ctx, "111111111111", &Trace{ID: "send", StartedAt: start.Add(time.Millisecond), Spans: []Span{{ID: "tarn:queue:message", Kind: "queue", Name: "orders", DurationMs: 1}}})
			}
			if nativeFirst {
				record()
			}
			if err := store.Merge(imported); err != nil {
				t.Fatal(err)
			}
			if !nativeFirst {
				record()
			}
			// Retrying an intake chunk must retain the native span.
			if err := store.Merge(imported); err != nil {
				t.Fatal(err)
			}
			got := store.Recent(10)
			if len(got) != 1 || len(got[0].Spans) != 2 || got[0].Path != "/orders" || got[0].Spans[1].ParentID != "18446744073709551614" {
				t.Fatalf("joined traces = %+v", got)
			}
			store.RecordJoined(ctx, "111111111111", &Trace{StartedAt: start.Add(time.Second), Spans: []Span{{Kind: "lambda", Name: "consumer", Status: "error", DurationMs: 1}}})
			if got := store.Recent(1)[0]; got.Status != 500 || got.Path != "/orders" {
				t.Fatalf("consumer error hidden by successful HTTP response: %+v", got)
			}
		})
	}
}
