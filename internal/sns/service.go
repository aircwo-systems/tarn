package sns

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/aircwo-systems/tarn/internal/config"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// SQSInterface defines the SQS behavior required by SNS fanout.
type SQSInterface interface {
	SendMessage(queueName, body string, delaySec int, attrs map[string]*types.MessageAttribute, groupID, dedupID string) (*types.SQSMessage, error)
}

// LambdaInterface defines the Lambda behavior required by SNS fanout.
type LambdaInterface interface {
	Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error)
}

// PublishInput captures publish request fields.
type PublishInput struct {
	TopicArn          string
	TargetArn         string
	Message           string
	Subject           string
	MessageStructure  string
	MessageAttributes map[string]types.SNSMessageAttribute
}

// PublishOutput captures publish response data.
type PublishOutput struct {
	MessageID string
}

// Service implements SNS business logic.
type Service struct {
	cfg        *config.Config
	store      *Store
	sqs        SQSInterface
	lambda     LambdaInterface
	traceStore *tracesvc.Store
}

// NewService creates a new SNS service.
func NewService(cfg *config.Config, sqsSvc SQSInterface, lambdaSvc LambdaInterface) *Service {
	return &Service{
		cfg:    cfg,
		store:  NewStore(cfg),
		sqs:    sqsSvc,
		lambda: lambdaSvc,
	}
}

// SetTraceStore attaches a trace store for publish traces.
func (s *Service) SetTraceStore(ts *tracesvc.Store) { s.traceStore = ts }

// Init loads persisted SNS state if configured.
func (s *Service) Init() error { return s.store.Init() }

func (s *Service) CreateTopic(name string, attrs map[string]string, tags map[string]string) (*types.SNSTopic, error) {
	return s.store.CreateTopic(name, attrs, tags)
}

func (s *Service) DeleteTopic(topicArn string) error { return s.store.DeleteTopic(topicArn) }

func (s *Service) GetTopic(topicArn string) (*types.SNSTopic, error) {
	return s.store.GetTopic(topicArn)
}

func (s *Service) ListTopics() []*types.SNSTopic { return s.store.ListTopics() }

func (s *Service) GetTopicAttributes(topicArn string) (map[string]string, error) {
	return s.store.GetTopicAttributes(topicArn)
}

func (s *Service) SetTopicAttribute(topicArn, name, value string) error {
	return s.store.SetTopicAttribute(topicArn, name, value)
}

func (s *Service) TagTopic(topicArn string, tags map[string]string) error {
	return s.store.TagTopic(topicArn, tags)
}

func (s *Service) UntagTopic(topicArn string, tagKeys []string) error {
	return s.store.UntagTopic(topicArn, tagKeys)
}

func (s *Service) ListTopicTags(topicArn string) (map[string]string, error) {
	return s.store.ListTopicTags(topicArn)
}

func (s *Service) Subscribe(topicArn, protocol, endpoint string, attrs map[string]string) (*types.SNSSubscription, error) {
	return s.store.Subscribe(topicArn, protocol, endpoint, attrs)
}

func (s *Service) Unsubscribe(subscriptionArn string) error {
	return s.store.Unsubscribe(subscriptionArn)
}

func (s *Service) GetSubscription(subscriptionArn string) (*types.SNSSubscription, error) {
	return s.store.GetSubscription(subscriptionArn)
}

func (s *Service) GetSubscriptionAttributes(subscriptionArn string) (map[string]string, error) {
	return s.store.GetSubscriptionAttributes(subscriptionArn)
}

func (s *Service) SetSubscriptionAttribute(subscriptionArn, name, value string) error {
	return s.store.SetSubscriptionAttribute(subscriptionArn, name, value)
}

func (s *Service) ListSubscriptions() []*types.SNSSubscription { return s.store.ListSubscriptions() }

func (s *Service) ListSubscriptionsByTopic(topicArn string) ([]*types.SNSSubscription, error) {
	return s.store.ListSubscriptionsByTopic(topicArn)
}

// Publish publishes a message to a topic and fans out to subscriptions.
func (s *Service) Publish(ctx context.Context, input PublishInput) (*PublishOutput, error) {
	topicArn := strings.TrimSpace(input.TopicArn)
	if topicArn == "" {
		topicArn = strings.TrimSpace(input.TargetArn)
	}
	if topicArn == "" {
		return nil, fmt.Errorf("TopicArn or TargetArn is required")
	}
	if strings.TrimSpace(input.Message) == "" {
		return nil, fmt.Errorf("message is required")
	}

	topic, err := s.store.GetTopic(topicArn)
	if err != nil {
		return nil, err
	}

	subs, err := s.store.ListSubscriptionsByTopic(topicArn)
	if err != nil {
		return nil, err
	}

	messageID := uuid.NewString()
	correlationID := correlationIDFromSNSAttributes(input.MessageAttributes)
	if correlationID == "" {
		correlationID = messageID
	}
	messageAttrs := withCorrelationSNSAttributes(input.MessageAttributes, correlationID)
	spans := make([]tracesvc.Span, 0, len(subs)+1)
	topicStart := time.Now()

	for _, sub := range subs {
		if sub.PendingConfirmation {
			continue
		}
		if !matchesSubscriptionFilter(sub, input.MessageAttributes, input.Message) {
			continue
		}

		targetStart := time.Now()
		status := "ok"
		switch strings.ToLower(sub.Protocol) {
		case "sqs":
			if s.sqs == nil {
				status = "error"
				break
			}
			queueName, parseErr := queueNameFromSubscriptionEndpoint(sub.Endpoint)
			if parseErr != nil {
				status = "error"
				break
			}

			body := input.Message
			if !sub.RawMessageDelivery {
				envelope, envelopeErr := buildSQSEnvelope(topic.TopicArn, sub.SubscriptionArn, messageID, input)
				if envelopeErr != nil {
					status = "error"
					break
				}
				body = envelope
			}
				if _, sendErr := s.sqs.SendMessage(queueName, body, 0, snsToSQSMessageAttributes(messageAttrs), "", ""); sendErr != nil {
					status = "error"
				}

		case "lambda":
			if s.lambda == nil {
				status = "error"
				break
			}
			fnName := lambdaNameFromEndpoint(sub.Endpoint)
			payload, payloadErr := buildLambdaEnvelope(topic.TopicArn, sub.SubscriptionArn, messageID, input, correlationID)
			if payloadErr != nil {
				status = "error"
				break
			}
			if _, invokeErr := s.lambda.Invoke(ctx, &types.InvokeInput{
				FunctionName:   fnName,
				Payload:        payload,
				InvocationType: "Event",
			}); invokeErr != nil {
				status = "error"
			}
		default:
			// Unsupported protocols are ignored to keep publish behavior resilient.
		}

		kind := "topic"
		name := topic.Name
		if strings.EqualFold(sub.Protocol, "sqs") {
			kind = "queue"
			if qn, err := queueNameFromSubscriptionEndpoint(sub.Endpoint); err == nil {
				name = qn
			} else {
				name = sub.Endpoint
			}
		} else if strings.EqualFold(sub.Protocol, "lambda") {
			kind = "lambda"
			name = lambdaNameFromEndpoint(sub.Endpoint)
		}
		spans = append(spans, tracesvc.Span{
			Kind:       kind,
			Name:       name,
			DurationMs: time.Since(targetStart).Milliseconds(),
			Status:     status,
		})
	}

	spans = append([]tracesvc.Span{{
		Kind:       "topic",
		Name:       topic.Name,
		DurationMs: time.Since(topicStart).Milliseconds(),
		Status:     "ok",
	}}, spans...)

	if s.traceStore != nil {
		overallStatus := 200
		for _, span := range spans {
			if span.Status == "error" {
				overallStatus = 500
				break
			}
		}
		s.traceStore.Add(&tracesvc.Trace{
			ID:            messageID[:8],
			CorrelationID: correlationID,
			StartedAt:     topicStart,
			DurationMs:    time.Since(topicStart).Milliseconds(),
			Status:        overallStatus,
			Method:        "POST",
			Path:          "/",
			Spans:         spans,
		})
	}

	return &PublishOutput{MessageID: messageID}, nil
}

func queueNameFromSubscriptionEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("empty endpoint")
	}

	if strings.HasPrefix(endpoint, "arn:aws:sqs:") {
		parts := strings.Split(endpoint, ":")
		if len(parts) < 6 || parts[len(parts)-1] == "" {
			return "", fmt.Errorf("invalid queue endpoint ARN %q", endpoint)
		}
		return parts[len(parts)-1], nil
	}

	if strings.Contains(endpoint, "/") {
		parts := strings.Split(endpoint, "/")
		name := parts[len(parts)-1]
		if name == "" {
			return "", fmt.Errorf("invalid queue endpoint %q", endpoint)
		}
		return name, nil
	}

	return endpoint, nil
}

func lambdaNameFromEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	const marker = ":function:"
	if !strings.Contains(endpoint, marker) {
		return endpoint
	}
	idx := strings.Index(endpoint, marker)
	name := endpoint[idx+len(marker):]
	if colon := strings.IndexByte(name, ':'); colon >= 0 {
		name = name[:colon]
	}
	if name == "" {
		return endpoint
	}
	return name
}

func buildSQSEnvelope(topicArn, subscriptionArn, messageID string, input PublishInput) (string, error) {
	payload := map[string]any{
		"Type":              "Notification",
		"MessageId":         messageID,
		"TopicArn":          topicArn,
		"Subject":           input.Subject,
		"Message":           input.Message,
		"Timestamp":         time.Now().UTC().Format(time.RFC3339Nano),
		"UnsubscribeURL":    "",
		"SubscriptionArn":   subscriptionArn,
		"MessageAttributes": mapMessageAttributes(input.MessageAttributes),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func buildLambdaEnvelope(topicArn, subscriptionArn, messageID string, input PublishInput, correlationID string) ([]byte, error) {
	attrs := withCorrelationSNSAttributes(input.MessageAttributes, correlationID)
	event := map[string]any{
		"correlationId": correlationID,
		"Records": []map[string]any{
			{
				"EventSource":          "aws:sns",
				"EventVersion":         "1.0",
				"EventSubscriptionArn": subscriptionArn,
				"Sns": map[string]any{
					"Type":              "Notification",
					"MessageId":         messageID,
					"TopicArn":          topicArn,
					"Subject":           input.Subject,
					"Message":           input.Message,
					"Timestamp":         time.Now().UTC().Format(time.RFC3339Nano),
					"MessageAttributes": mapMessageAttributes(attrs),
				},
			},
		},
	}
	return json.Marshal(event)
}

func withCorrelationSNSAttributes(
	attrs map[string]types.SNSMessageAttribute,
	correlationID string,
) map[string]types.SNSMessageAttribute {
	out := make(map[string]types.SNSMessageAttribute, len(attrs)+1)
	for key, value := range attrs {
		out[key] = value
	}
	if correlationID != "" {
		if _, exists := out["correlationId"]; !exists {
			out["correlationId"] = types.SNSMessageAttribute{
				DataType:    "String",
				StringValue: correlationID,
			}
		}
	}
	return out
}

func correlationIDFromSNSAttributes(attrs map[string]types.SNSMessageAttribute) string {
	for _, key := range []string{"correlationId", "CorrelationId", "x-correlation-id"} {
		if attr, ok := attrs[key]; ok && strings.TrimSpace(attr.StringValue) != "" {
			return strings.TrimSpace(attr.StringValue)
		}
	}
	return ""
}

func snsToSQSMessageAttributes(
	attrs map[string]types.SNSMessageAttribute,
) map[string]*types.MessageAttribute {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]*types.MessageAttribute, len(attrs))
	for key, value := range attrs {
		out[key] = &types.MessageAttribute{
			DataType:    value.DataType,
			StringValue: value.StringValue,
			BinaryValue: []byte(value.BinaryValue),
		}
	}
	return out
}

func mapMessageAttributes(attrs map[string]types.SNSMessageAttribute) map[string]map[string]string {
	if len(attrs) == 0 {
		return map[string]map[string]string{}
	}
	out := make(map[string]map[string]string, len(attrs))
	for name, attr := range attrs {
		entry := map[string]string{"Type": attr.DataType}
		if attr.StringValue != "" {
			entry["Value"] = attr.StringValue
		} else if attr.BinaryValue != "" {
			entry["Value"] = attr.BinaryValue
		}
		out[name] = entry
	}
	return out
}

func matchesSubscriptionFilter(sub *types.SNSSubscription, attrs map[string]types.SNSMessageAttribute, messageBody string) bool {
	if strings.TrimSpace(sub.FilterPolicy) == "" {
		return true
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(sub.FilterPolicy), &raw); err != nil {
		return true
	}

	scope := strings.TrimSpace(sub.FilterPolicyScope)
	if scope == "" {
		scope = "MessageAttributes"
	}

	if strings.EqualFold(scope, "MessageBody") {
		var body map[string]any
		if err := json.Unmarshal([]byte(messageBody), &body); err != nil {
			return false
		}
		for key, condJSON := range raw {
			var conditions []any
			if err := json.Unmarshal(condJSON, &conditions); err != nil {
				continue
			}
			bodyVal, bodyExists := body[key]
			bodyStr := ""
			if bodyExists {
				bodyStr = fmt.Sprintf("%v", bodyVal)
			}
			if !matchesFilterConditions(conditions, bodyExists, bodyStr) {
				return false
			}
		}
		return true
	}

	for key, condJSON := range raw {
		var conditions []any
		if err := json.Unmarshal(condJSON, &conditions); err != nil {
			continue
		}
		attr, attrExists := attrs[key]
		attrVal := ""
		if attrExists {
			attrVal = attr.StringValue
			if attrVal == "" {
				attrVal = attr.BinaryValue
			}
		}
		if !matchesFilterConditions(conditions, attrExists, attrVal) {
			return false
		}
	}
	return true
}

// matchesFilterConditions returns true if any condition in the array matches.
// Conditions within a single attribute key are OR-evaluated; all keys are AND-evaluated by the caller.
func matchesFilterConditions(conditions []any, exists bool, value string) bool {
	for _, cond := range conditions {
		switch c := cond.(type) {
		case string:
			if exists && c == value {
				return true
			}
		case map[string]any:
			if evalFilterOperator(c, exists, value) {
				return true
			}
		}
	}
	return false
}

func evalFilterOperator(op map[string]any, attrExists bool, attrValue string) bool {
	if existsVal, ok := op["exists"]; ok {
		wantExists, _ := existsVal.(bool)
		return attrExists == wantExists
	}
	if !attrExists {
		return false
	}
	if prefixVal, ok := op["prefix"]; ok {
		prefix, _ := prefixVal.(string)
		return strings.HasPrefix(attrValue, prefix)
	}
	if anythingBut, ok := op["anything-but"]; ok {
		switch v := anythingBut.(type) {
		case []any:
			for _, item := range v {
				if fmt.Sprintf("%v", item) == attrValue {
					return false
				}
			}
			return true
		default:
			return fmt.Sprintf("%v", v) != attrValue
		}
	}
	if numericConds, ok := op["numeric"]; ok {
		return evalNumericFilter(numericConds, attrValue)
	}
	return false
}

func evalNumericFilter(conds any, attrValue string) bool {
	conditions, ok := conds.([]any)
	if !ok || len(conditions) < 2 {
		return false
	}
	attrFloat, err := strconv.ParseFloat(attrValue, 64)
	if err != nil {
		return false
	}
	for i := 0; i+1 < len(conditions); i += 2 {
		op, _ := conditions[i].(string)
		limit, _ := conditions[i+1].(float64)
		switch op {
		case "=":
			if attrFloat != limit {
				return false
			}
		case "!=":
			if attrFloat == limit {
				return false
			}
		case "<":
			if !(attrFloat < limit) {
				return false
			}
		case "<=":
			if !(attrFloat <= limit) {
				return false
			}
		case ">":
			if !(attrFloat > limit) {
				return false
			}
		case ">=":
			if !(attrFloat >= limit) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
