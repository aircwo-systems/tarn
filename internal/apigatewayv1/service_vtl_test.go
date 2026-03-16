package apigatewayv1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/openstack-project/openstack/pkg/types"
)

func TestEvaluateVTL_MultilineSetPayloadToJson(t *testing.T) {
	input := &InvokeInput{
		Headers: http.Header{
			"Service-Name":   []string{"orders-service"},
			"Correlation-Id": []string{"corr-123"},
			"Channel":        []string{"web"},
		},
		Query: url.Values{},
		Body:  []byte(`{"ignored":true}`),
	}
	pathParams := map[string]string{"aggregateId": "agg-42"}

	tmpl := `#set($aggregateId = $input.params().path.get('aggregateId'))
#set($serviceName = $input.params().header.get('service-name'))
#set($correlationId = $input.params().header.get('correlation-id'))
#set($channel = $input.params().header.get('channel'))
#set($payload = {
  "event": {
    "service-name": "$serviceName"
  },
  "aggregate": {
    "aggregateId": "$aggregateId"
  },
  "source": {
    "correlationId": "$correlationId",
    "channel": "$channel"
  }
})
Action=SendMessage&MessageGroupId=$util.urlEncode($aggregateId)&MessageBody=$util.urlEncode($util.toJson($payload))`

	result := evaluateVTL(tmpl, input, pathParams)
	params, err := url.ParseQuery(result)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	if got := params.Get("Action"); got != "SendMessage" {
		t.Fatalf("Action = %q, want %q", got, "SendMessage")
	}
	if got := params.Get("MessageGroupId"); got != "agg-42" {
		t.Fatalf("MessageGroupId = %q, want %q", got, "agg-42")
	}

	messageBody := params.Get("MessageBody")
	if messageBody == "$payload" || messageBody == `"$payload"` {
		t.Fatalf("MessageBody resolved to unresolved payload marker: %q", messageBody)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(messageBody), &payload); err != nil {
		t.Fatalf("MessageBody is not valid JSON: %v; body=%q", err, messageBody)
	}

	event, _ := payload["event"].(map[string]any)
	aggregate, _ := payload["aggregate"].(map[string]any)
	source, _ := payload["source"].(map[string]any)

	if got, _ := event["service-name"].(string); got != "orders-service" {
		t.Fatalf("event.service-name = %q, want %q", got, "orders-service")
	}
	if got, _ := aggregate["aggregateId"].(string); got != "agg-42" {
		t.Fatalf("aggregate.aggregateId = %q, want %q", got, "agg-42")
	}
	if got, _ := source["correlationId"].(string); got != "corr-123" {
		t.Fatalf("source.correlationId = %q, want %q", got, "corr-123")
	}
	if got, _ := source["channel"].(string); got != "web" {
		t.Fatalf("source.channel = %q, want %q", got, "web")
	}
}

func TestEvaluateVTL_NestedFieldAccessFromObjectVariable(t *testing.T) {
	input := &InvokeInput{Headers: http.Header{}, Query: url.Values{}, Body: []byte(`{}`)}
	tmpl := `#set($payload = {"aggregate":{"aggregateId":"agg-99"}})
MessageBody=$payload.aggregate.aggregateId`

	result := evaluateVTL(tmpl, input, nil)
	if result != "MessageBody=agg-99" {
		t.Fatalf("result = %q, want %q", result, "MessageBody=agg-99")
	}
}

func TestNormalizeQueryParams_HandlesIndentedKeys(t *testing.T) {
	raw := "Action=SendMessage&\n  MessageGroupId=agg-42&\n  MessageBody=%7B%22ok%22%3Atrue%7D"
	params, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	params = normalizeQueryParams(params)

	if got := getQueryParam(params, "MessageGroupId"); got != "agg-42" {
		t.Fatalf("MessageGroupId = %q, want %q", got, "agg-42")
	}
	if got := getQueryParam(params, "MessageBody"); got != `{"ok":true}` {
		t.Fatalf("MessageBody = %q, want %q", got, `{"ok":true}`)
	}
}

func TestEvaluateVTL_ChainedGetExpressionsResolveFinalArgument(t *testing.T) {
	input := &InvokeInput{
		Headers: http.Header{"X-Service": []string{"billing"}},
		Query:   url.Values{"dedup": []string{"dedup-99"}},
		Body:    []byte(`{}`),
	}
	pathParams := map[string]string{"aggregateId": "agg-99"}

	tmpl := `Action=SendMessage&MessageGroupId=$util.urlEncode($input.params().path.get('aggregateId'))&MessageDeduplicationId=$util.urlEncode($input.params().querystring.get('dedup'))&MessageBody=$util.urlEncode($input.params().header.get('x-service'))`

	result := evaluateVTL(tmpl, input, pathParams)
	params, err := url.ParseQuery(result)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	if got := params.Get("MessageGroupId"); got != "agg-99" {
		t.Fatalf("MessageGroupId = %q, want %q", got, "agg-99")
	}
	if got := params.Get("MessageDeduplicationId"); got != "dedup-99" {
		t.Fatalf("MessageDeduplicationId = %q, want %q", got, "dedup-99")
	}
	if got := params.Get("MessageBody"); got != "billing" {
		t.Fatalf("MessageBody = %q, want %q", got, "billing")
	}
}

func TestEvaluateVTL_ToJsonCompactsJSONOutput(t *testing.T) {
	input := &InvokeInput{
		Headers: http.Header{},
		Query:   url.Values{},
		Body:    []byte(`{}`),
	}

	tmpl := `#set($payload = {
  "b": "two",
  "a": "one"
})
MessageBody=$util.toJson($payload)`

	result := evaluateVTL(tmpl, input, nil)
	params, err := url.ParseQuery(result)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	body := params.Get("MessageBody")
	if strings.Contains(body, "\n") || strings.Contains(body, ": ") {
		t.Fatalf("MessageBody should be compact JSON, got %q", body)
	}
	if body != `{"a":"one","b":"two"}` {
		t.Fatalf("MessageBody = %q, want %q", body, `{"a":"one","b":"two"}`)
	}
}

func TestInvokeAWSIntegration_UsesTemplateFallbackAndForwardsFIFOFields(t *testing.T) {
	t.Run("template fallback forwards group and dedup", func(t *testing.T) {
		var gotQueue, gotBody, gotGroup, gotDedup string
		svc := &Service{
			sqsSend: func(queueName, body, groupId, dedupId string) (string, string, error) {
				gotQueue, gotBody, gotGroup, gotDedup = queueName, body, groupId, dedupId
				return "m-1", "md5-1", nil
			},
		}

		input := &InvokeInput{
			Headers: http.Header{
				"Content-Type": []string{"application/vnd.openstack+json; charset=utf-8"},
				"X-Dedup-Id":   []string{"dedup-42"},
			},
			Query: url.Values{},
			Body:  []byte(`{"event":"created"}`),
		}
		pathParams := map[string]string{"aggregateId": "agg-42"}
		integ := &types.RestIntegration{
			SQSQueueName: "orders.fifo",
			RequestTemplates: map[string]string{
				"application/json": `#set($aggregateId = $input.params().path.get('aggregateId'))
#set($dedup = $input.params().header.get('x-dedup-id'))
Action=SendMessage&MessageGroupId=$util.urlEncode($aggregateId)&MessageDeduplicationId=$util.urlEncode($dedup)&MessageBody=$util.urlEncode($input.body)`,
			},
		}

		out, err := svc.invokeAWSIntegration(context.Background(), nil, input, integ, pathParams, time.Now())
		if err != nil {
			t.Fatalf("invokeAWSIntegration: %v", err)
		}
		if out.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", out.StatusCode, http.StatusOK)
		}
		if gotQueue != "orders.fifo" {
			t.Fatalf("queueName = %q, want %q", gotQueue, "orders.fifo")
		}
		if gotBody != `{"event":"created"}` {
			t.Fatalf("body = %q, want %q", gotBody, `{"event":"created"}`)
		}
		if gotGroup != "agg-42" {
			t.Fatalf("groupId = %q, want %q", gotGroup, "agg-42")
		}
		if gotDedup != "dedup-42" {
			t.Fatalf("dedupId = %q, want %q", gotDedup, "dedup-42")
		}
	})

	t.Run("missing message body falls back to raw request body", func(t *testing.T) {
		var gotBody string
		svc := &Service{
			sqsSend: func(queueName, body, groupId, dedupId string) (string, string, error) {
				gotBody = body
				return "m-2", "md5-2", nil
			},
		}
		input := &InvokeInput{
			Headers: http.Header{"Content-Type": []string{"application/json"}},
			Query:   url.Values{},
			Body:    []byte(`{"fallback":true}`),
		}
		integ := &types.RestIntegration{
			SQSQueueName: "orders.fifo",
			RequestTemplates: map[string]string{
				"application/json": `Action=SendMessage&MessageGroupId=agg-42`,
			},
		}

		if _, err := svc.invokeAWSIntegration(context.Background(), nil, input, integ, nil, time.Now()); err != nil {
			t.Fatalf("invokeAWSIntegration: %v", err)
		}
		if gotBody != `{"fallback":true}` {
			t.Fatalf("body = %q, want %q", gotBody, `{"fallback":true}`)
		}
	})
}
