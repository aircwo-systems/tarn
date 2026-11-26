package eventsource

import (
	"encoding/json"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/eventbridge"
	"github.com/aircwo-systems/tarn/internal/sqs"
	"github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// TestEventBridgeToSQSToLambdaChain proves the fan-out pattern end to end:
// PutEvents → event-pattern rule → SQS queue target → event source mapping
// → consumer Lambda, with the EventBridge envelope in Records[0].body and the
// delivery and consumer traces sharing one correlation ID.
func TestEventBridgeToSQSToLambdaChain(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PersistenceEnabled = false
	traces := trace.NewStore()
	defer func() { _ = traces.Close() }()

	queueSvc := sqs.NewService(cfg)
	queueSvc.SetTraceStore(traces)
	queue, err := queueSvc.CreateQueue("user-signins", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	bus := eventbridge.NewService(cfg, eventbridge.NewStore(cfg), nil)
	if err := bus.Init(); err != nil {
		t.Fatal(err)
	}
	bus.SetSQSSender(queueSvc)
	bus.SetTraceStore(traces)
	if _, err := bus.PutRule("user-signed-in", "", `{"source":["app.cognito"],"detail-type":["UserSignedIn"]}`, "ENABLED", "", ""); err != nil {
		t.Fatal(err)
	}
	if failed, err := bus.PutTargets("user-signed-in", "", []types.EventBridgeTarget{{ID: "signins", Arn: queue.QueueArn}}); err != nil || len(failed) != 0 {
		t.Fatalf("PutTargets: %+v %v", failed, err)
	}

	results, failed, err := bus.PutEvents([]types.PutEventsEntry{{
		Source:     "app.cognito",
		DetailType: "UserSignedIn",
		Detail:     `{"sub":"1234","email":"user@example.com"}`,
	}})
	if err != nil || failed != 0 {
		t.Fatalf("PutEvents: %d %v", failed, err)
	}
	bus.DrainDispatch()

	lambda := &mockLambda{}
	mapping := &types.EventSourceMapping{UUID: "signins", QueueName: queue.QueueName, EventSourceArn: queue.QueueArn, FunctionName: "consumer", BatchSize: 10}
	p := newPoller(mapping, queueSvc, nil, lambda, NewStore(cfg), traces, nil)
	p.poll()

	if len(lambda.invocations) != 1 || lambda.functions[0] != "consumer" {
		t.Fatalf("invocations = %v", lambda.functions)
	}
	var event struct {
		Records []struct {
			Body              string `json:"body"`
			EventSource       string `json:"eventSource"`
			EventSourceARN    string `json:"eventSourceARN"`
			MessageAttributes map[string]struct {
				StringValue string `json:"stringValue"`
			} `json:"messageAttributes"`
		}
	}
	if err := json.Unmarshal(lambda.invocations[0], &event); err != nil {
		t.Fatal(err)
	}
	if len(event.Records) != 1 || event.Records[0].EventSource != "aws:sqs" || event.Records[0].EventSourceARN != queue.QueueArn {
		t.Fatalf("records = %+v", event.Records)
	}
	var envelope struct {
		ID         string            `json:"id"`
		Source     string            `json:"source"`
		DetailType string            `json:"detail-type"`
		Detail     map[string]string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(event.Records[0].Body), &envelope); err != nil {
		t.Fatalf("body is not an EventBridge envelope: %v (%s)", err, event.Records[0].Body)
	}
	if envelope.ID != results[0].EventId || envelope.Source != "app.cognito" || envelope.DetailType != "UserSignedIn" || envelope.Detail["sub"] != "1234" {
		t.Fatalf("envelope = %+v", envelope)
	}

	correlationID := event.Records[0].MessageAttributes["correlationId"].StringValue
	if correlationID == "" {
		t.Fatalf("consumer did not receive the correlationId attribute: %+v", event.Records[0].MessageAttributes)
	}
	var delivery, consumer bool
	for _, tr := range traces.Recent(10) {
		if tr.CorrelationID != correlationID {
			continue
		}
		for _, span := range tr.Spans {
			delivery = delivery || span.Kind == "sqs"
			consumer = consumer || (span.Kind == "lambda" && span.Name == "consumer")
		}
	}
	if !delivery || !consumer {
		t.Fatalf("expected EventBridge delivery and ESM consumer traces to share correlation %s (delivery=%v consumer=%v)", correlationID, delivery, consumer)
	}
}
