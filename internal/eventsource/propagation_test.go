package eventsource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/sns"
	"github.com/aircwo-systems/tarn/internal/sqs"
	"github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

func messageCarrier(high string) string {
	return fmt.Sprintf(`{"x-datadog-trace-id":"18446744073709551615","x-datadog-parent-id":"18446744073709551614","x-datadog-tags":"_dd.p.tid=%s"}`, high)
}

func TestSNSQueueLambdaJoinsImportedRequest(t *testing.T) {
	for _, raw := range []bool{true, false} {
		t.Run(fmt.Sprint("raw=", raw), func(t *testing.T) {
			cfg := config.Default()
			cfg.PersistenceEnabled = false
			cfg.AccountID = "111111111111"
			traces := trace.NewStore()
			defer traces.Close()
			queueSvc := sqs.NewService(cfg)
			queueSvc.SetTraceStore(traces)
			queue, err := queueSvc.CreateQueue("orders", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			topicSvc := sns.NewService(cfg, queueSvc, nil)
			topicSvc.SetTraceStore(traces)
			topic, err := topicSvc.CreateTopic("orders", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := topicSvc.Subscribe(topic.TopicArn, "sqs", queue.QueueArn, map[string]string{"RawMessageDelivery": fmt.Sprint(raw)}); err != nil {
				t.Fatal(err)
			}
			carrier := messageCarrier("abcdef1234567890")
			published, err := topicSvc.Publish(context.Background(), sns.PublishInput{TopicArn: topic.TopicArn, Message: "order", MessageAttributes: map[string]types.SNSMessageAttribute{"_datadog": {DataType: "Binary", BinaryValue: base64.StdEncoding.EncodeToString([]byte(carrier))}}})
			if err != nil {
				t.Fatal(err)
			}
			lambda := &mockLambda{}
			mapping := &types.EventSourceMapping{UUID: "traced-orders", QueueName: queue.QueueName, EventSourceArn: queue.QueueArn, FunctionName: "consumer", BatchSize: 1}
			p := newPoller(mapping, queueSvc, nil, lambda, NewStore(cfg), traces, nil)
			p.poll()
			if len(lambda.invocations) != 1 {
				t.Fatalf("invocations = %d", len(lambda.invocations))
			}
			var event struct {
				Records []struct {
					MessageAttributes map[string]struct{ BinaryValue string }
				}
			}
			if err := json.Unmarshal(lambda.invocations[0], &event); err != nil {
				t.Fatal(err)
			}
			gotCarrier, err := base64.StdEncoding.DecodeString(event.Records[0].MessageAttributes["_datadog"].BinaryValue)
			if err != nil || string(gotCarrier) != carrier {
				t.Fatalf("consumer carrier = %s, %v", gotCarrier, err)
			}
			// The producer's agent chunk arrives after the publish and invoke.
			start := time.Now().Add(-time.Second)
			id := "dd:" + cfg.AccountID + ":abcdef1234567890ffffffffffffffff"
			if err := traces.Merge(&trace.Trace{ID: id, AccountID: cfg.AccountID, CorrelationID: "18446744073709551615", Spans: []trace.Span{{ID: "18446744073709551614", Kind: "service", Name: "POST /orders", StartedAt: &start, DurationNs: int64(time.Millisecond), Meta: map[string]string{"http.route": "/orders", "http.status_code": "200"}}}}); err != nil {
				t.Fatal(err)
			}
			got := traces.Recent(10)
			if len(got) != 1 || len(got[0].Spans) != 5 || got[0].ID != id || got[0].Path != "/orders" {
				t.Fatalf("joined trace = %+v", got)
			}
			byID := map[string]trace.Span{}
			var consumer trace.Span
			for _, span := range got[0].Spans {
				byID[span.ID] = span
				if span.Kind == "lambda" {
					consumer = span
				}
			}
			receive := byID[consumer.ParentID]
			send := byID[receive.ParentID]
			publish := byID[send.ParentID]
			if receive.Meta["operation"] != "ReceiveMessage" || send.Kind != "queue" || publish.ID != "tarn:sns:"+published.MessageID || publish.ParentID != "18446744073709551614" {
				t.Fatalf("broken parent chain: lambda=%+v receive=%+v send=%+v publish=%+v", consumer, receive, send, publish)
			}
		})
	}
}

func TestMixedBatchKeepsRequestsAndRetriesSeparate(t *testing.T) {
	cfg := config.Default()
	cfg.PersistenceEnabled = false
	cfg.AccountID = "111111111111"
	traces := trace.NewStore()
	defer traces.Close()
	msgs := []*types.SQSMessage{
		{MessageId: "a", MessageAttributes: map[string]*types.MessageAttribute{"_datadog": {DataType: "String", StringValue: messageCarrier("1111111111111111")}}},
		{MessageId: "b", MessageAttributes: map[string]*types.MessageAttribute{"_datadog": {DataType: "String", StringValue: messageCarrier("2222222222222222")}}},
		{MessageId: "c", MessageAttributes: map[string]*types.MessageAttribute{"_datadog": {DataType: "String", StringValue: "invalid"}}},
	}
	p := newPoller(&types.EventSourceMapping{QueueName: "orders"}, nil, nil, nil, NewStore(cfg), traces, nil)
	for attempt := 1; attempt <= 2; attempt++ {
		for _, msg := range msgs {
			msg.ApproximateReceiveCount = attempt
		}
		p.recordTrace(time.Now(), "consumer", msgs, 1, 2, len(msgs), 1, nil)
	}
	got := traces.Recent(10)
	if len(got) != 4 {
		t.Fatalf("traces = %d, want two distributed requests and two legacy attempts", len(got))
	}
	for _, tr := range got {
		if !strings.HasPrefix(tr.ID, "dd:") {
			continue
		}
		if len(tr.Spans) != 4 || tr.Status != 500 {
			t.Fatalf("retry spans = %+v", tr)
		}
		want := "a"
		if strings.Contains(tr.ID, "2222222222222222") {
			want = "b"
		}
		attempts := map[string]bool{}
		for _, span := range tr.Spans {
			if span.Kind == "lambda" && span.Meta["batchMessageIds"] != want {
				t.Fatalf("unrelated message attached: %+v", span)
			}
			if span.Kind == "queue" {
				attempts[span.Meta["receiveCount"]] = true
			}
		}
		if !attempts["1"] || !attempts["2"] {
			t.Fatalf("attempts = %v", attempts)
		}
	}
}

func TestDirectSQSContextsJoinConsumer(t *testing.T) {
	for name, carrier := range map[string]string{
		"64 bit Datadog": `{"x-datadog-trace-id":"12","x-datadog-parent-id":"13"}`,
		"W3C":            `{"traceparent":"00-abcdef1234567890000000000000000c-000000000000000d-01"}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			cfg.PersistenceEnabled = false
			traces := trace.NewStore()
			defer traces.Close()
			queueSvc := sqs.NewService(cfg)
			queueSvc.SetTraceStore(traces)
			queue, err := queueSvc.CreateQueue("orders", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			sent, err := queueSvc.SendMessage(queue.QueueName, "order", 0, map[string]*types.MessageAttribute{"_datadog": {DataType: "String", StringValue: carrier}}, "", "")
			if err != nil {
				t.Fatal(err)
			}
			p := newPoller(&types.EventSourceMapping{UUID: "direct-sqs", QueueName: queue.QueueName, EventSourceArn: queue.QueueArn, FunctionName: "consumer", BatchSize: 1}, queueSvc, nil, &mockLambda{}, NewStore(cfg), traces, nil)
			p.poll()
			got := traces.Recent(10)
			ctx := trace.ContextFromSQSMessage(sent)
			if len(got) != 1 || got[0].ID != ctx.ID(cfg.AccountID) || len(got[0].Spans) != 3 {
				t.Fatalf("direct SQS trace = %+v", got)
			}
			for _, span := range got[0].Spans {
				if span.ID == trace.QueueSpanID(sent.MessageId) && span.ParentID != "13" {
					t.Fatalf("send parent = %+v", span)
				}
			}
		})
	}
}
