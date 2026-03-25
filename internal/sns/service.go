package sns

import (
	"context"
	"encoding/json"
	"fmt"
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
			if _, sendErr := s.sqs.SendMessage(queueName, body, 0, nil, "", ""); sendErr != nil {
				status = "error"
			}

		case "lambda":
			if s.lambda == nil {
				status = "error"
				break
			}
			fnName := lambdaNameFromEndpoint(sub.Endpoint)
			payload, payloadErr := buildLambdaEnvelope(topic.TopicArn, sub.SubscriptionArn, messageID, input)
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
			ID:         messageID[:8],
			StartedAt:  topicStart,
			DurationMs: time.Since(topicStart).Milliseconds(),
			Status:     overallStatus,
			Method:     "POST",
			Path:       "/",
			Spans:      spans,
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

func buildLambdaEnvelope(topicArn, subscriptionArn, messageID string, input PublishInput) ([]byte, error) {
	event := map[string]any{
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
					"MessageAttributes": mapMessageAttributes(input.MessageAttributes),
				},
			},
		},
	}
	return json.Marshal(event)
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

	// Minimal support for exact-match filter values.
	var raw map[string]any
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
		for key, allowed := range raw {
			want, ok := stringCandidates(allowed)
			if !ok {
				continue
			}
			actual := fmt.Sprintf("%v", body[key])
			if !containsString(want, actual) {
				return false
			}
		}
		return true
	}

	for key, allowed := range raw {
		want, ok := stringCandidates(allowed)
		if !ok {
			continue
		}
		attr, exists := attrs[key]
		if !exists {
			return false
		}
		actual := attr.StringValue
		if actual == "" {
			actual = attr.BinaryValue
		}
		if !containsString(want, actual) {
			return false
		}
	}

	return true
}

func stringCandidates(value any) ([]string, bool) {
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out, true
	default:
		return []string{fmt.Sprintf("%v", v)}, true
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
