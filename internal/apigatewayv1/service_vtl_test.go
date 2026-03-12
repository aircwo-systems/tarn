package apigatewayv1

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestEvaluateVTL_MultilineSetPayloadToJson(t *testing.T) {
	input := &InvokeInput{
		Headers: http.Header{
			"service-name":   []string{"orders-service"},
			"correlation-id": []string{"corr-123"},
			"channel":        []string{"web"},
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
