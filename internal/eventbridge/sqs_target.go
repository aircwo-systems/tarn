package eventbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

// SQSSender is the SQS behavior EventBridge needs to deliver to queue
// targets and dead-letter queues. *sqs.Service satisfies it.
type SQSSender interface {
	SendMessage(queueName, body string, delaySec int, attrs map[string]*types.MessageAttribute, groupId, dedupId string) (*types.SQSMessage, error)
}

// sqsQueueConfigReader is optionally implemented by an SQSSender so FIFO
// targets on content-based-deduplication queues get a body-hash dedup ID,
// matching what SQS computes for them in AWS.
type sqsQueueConfigReader interface {
	GetQueue(name string) (*types.QueueConfig, error)
}

// SetSQSSender wires SQS delivery into EventBridge. Like SetTaskRunner it is
// a setter so NewService callers stay source-compatible.
func (s *Service) SetSQSSender(sender SQSSender) {
	s.sqsSenderMu.Lock()
	s.sqsSender = sender
	s.sqsSenderMu.Unlock()
}

func (s *Service) getSQSSender() SQSSender {
	s.sqsSenderMu.RLock()
	defer s.sqsSenderMu.RUnlock()
	return s.sqsSender
}

// AWS EventBridge DLQ ERROR_CODE values Tarn can produce.
const (
	dlqErrorInvalidParameter = "INVALID_PARAMETER"
	dlqErrorNoResource       = "NO_RESOURCE"
	dlqErrorSDKClient        = "SDK_CLIENT_ERROR"
	dlqErrorThrottling       = "THROTTLING"
	dlqErrorTimeout          = "TIMEOUT"
)

// deliveryFailure describes an event EventBridge could not hand to a target.
// It is only reported for delivery failures in the AWS sense: a Lambda
// handler that throws, or an ECS task that exits non-zero, was still
// delivered and never reaches the target's dead-letter queue.
type deliveryFailure struct {
	code    string
	message string
}

// classifyDeliveryError maps a delivery error onto an AWS DLQ ERROR_CODE.
func classifyDeliveryError(err error) string {
	if err == nil {
		return dlqErrorSDKClient
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return dlqErrorTimeout
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") || strings.Contains(msg, "nonexistent"):
		return dlqErrorNoResource
	case strings.Contains(msg, "throttl") || strings.Contains(msg, "toomanyrequests") || strings.Contains(msg, "rate exceeded"):
		return dlqErrorThrottling
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out"):
		return dlqErrorTimeout
	}
	return dlqErrorSDKClient
}

// dispatchSQSTarget transforms the event exactly as for a Lambda target and
// enqueues it on the target queue.
func (s *Service) dispatchSQSTarget(target *types.EventBridgeTarget, eventPayload []byte, opts targetDispatchOptions) (bool, []tracesvc.Span, *deliveryFailure) {
	queueName, _ := sqsQueueNameFromARN(target.Arn)
	if queueName == "" {
		queueName = target.Arn
	}
	meta := map[string]string{"targetId": target.ID, "queueArn": target.Arn, "operation": "SendMessage"}
	if opts.correlationID != "" {
		meta["correlationId"] = opts.correlationID
	}

	stamp := func() {
		if opts.invokedAt != nil {
			target.LastInvokedAt = opts.invokedAt
		} else {
			now := time.Now().UTC()
			target.LastInvokedAt = &now
		}
	}
	fail := func(code, detail string, durationMs int64) (bool, []tracesvc.Span, *deliveryFailure) {
		target.LastResult = "ERROR: " + detail
		meta["error"] = detail
		return false, []tracesvc.Span{{Kind: "sqs", Name: queueName, DurationMs: durationMs, Status: "error", Meta: meta}},
			&deliveryFailure{code: code, message: detail}
	}

	if _, err := canonicalSQSQueueARN(target.Arn); err != nil {
		return fail(dlqErrorInvalidParameter, err.Error(), 0)
	}
	payload, payloadErr := buildTargetPayload(eventPayload, target)
	if payloadErr != nil {
		return fail(dlqErrorInvalidParameter, payloadErr.Error(), 0)
	}
	sender := s.getSQSSender()
	if sender == nil {
		return fail(dlqErrorSDKClient, "SQS service unavailable", 0)
	}

	groupID, dedupID := "", ""
	if isFIFOQueueARN(target.Arn) {
		if target.SqsParameters == nil || strings.TrimSpace(target.SqsParameters.MessageGroupId) == "" {
			return fail(dlqErrorInvalidParameter, "SqsParameters.MessageGroupId is required for a FIFO queue target", 0)
		}
		groupID = target.SqsParameters.MessageGroupId
		dedupID = s.fifoDeduplicationID(sender, queueName, payload, opts, target, eventPayload)
		meta["messageGroupId"] = groupID
	}

	start := time.Now()
	msg, err := sender.SendMessage(queueName, string(payload), 0, correlationAttributes(opts.correlationID), groupID, dedupID)
	duration := time.Since(start).Milliseconds()
	stamp()
	if err != nil {
		return fail(classifyDeliveryError(err), err.Error(), duration)
	}
	target.LastResult = "OK"
	if msg != nil {
		meta["messageId"] = msg.MessageId
	}
	return true, []tracesvc.Span{{Kind: "sqs", Name: queueName, DurationMs: duration, Status: "ok", Meta: meta}}, nil
}

// fifoDeduplicationID returns the MessageDeduplicationId for a FIFO target.
// Content-based queues dedupe on the SHA-256 of the body, as SQS does. Other
// queues dedupe on the event ID scoped to the rule and target, so one event
// fanned out by two rules (or two targets) to the same queue stays two
// messages, while a re-delivery of the same event to the same target dedupes.
func (s *Service) fifoDeduplicationID(sender SQSSender, queueName string, payload []byte, opts targetDispatchOptions, target *types.EventBridgeTarget, eventPayload []byte) string {
	if reader, ok := sender.(sqsQueueConfigReader); ok {
		if cfg, err := reader.GetQueue(queueName); err == nil && cfg != nil && cfg.ContentBasedDeduplication {
			sum := sha256.Sum256(payload)
			return hex.EncodeToString(sum[:])
		}
	}
	eventID := eventIDFromPayload(eventPayload)
	if eventID == "" {
		eventID = uuid.NewString()
	}
	rule := opts.ruleArn
	if rule == "" {
		rule = opts.ruleKey
	}
	sum := sha256.Sum256([]byte(rule + "\x00" + target.ID + "\x00" + eventID))
	return hex.EncodeToString(sum[:])
}

// forwardToDeadLetterQueue sends the original event to the target's DLQ with
// the message attributes AWS EventBridge attaches to dead-lettered events.
func (s *Service) forwardToDeadLetterQueue(target *types.EventBridgeTarget, eventPayload []byte, opts targetDispatchOptions, failure *deliveryFailure) []tracesvc.Span {
	if target == nil || failure == nil || target.DeadLetterConfig == nil {
		return nil
	}
	dlqArn := strings.TrimSpace(target.DeadLetterConfig.Arn)
	if dlqArn == "" {
		return nil
	}
	queueName, err := sqsQueueNameFromARN(dlqArn)
	meta := map[string]string{
		"targetId":  target.ID,
		"targetArn": target.Arn,
		"errorCode": failure.code,
		"operation": "DeadLetter",
	}
	if err != nil {
		meta["error"] = err.Error()
		return []tracesvc.Span{{Kind: "dlq", Name: dlqArn, Status: "error", Meta: meta}}
	}
	sender := s.getSQSSender()
	if sender == nil {
		meta["error"] = "SQS service unavailable"
		return []tracesvc.Span{{Kind: "dlq", Name: queueName, Status: "error", Meta: meta}}
	}

	message := failure.message
	if len(message) > 256 {
		message = message[:256]
	}
	attrs := correlationAttributes(opts.correlationID)
	if attrs == nil {
		attrs = map[string]*types.MessageAttribute{}
	}
	for key, value := range map[string]string{
		"ERROR_CODE":                failure.code,
		"ERROR_MESSAGE":             message,
		"EXHAUSTED_RETRY_CONDITION": "MaximumRetryAttempts",
		"RETRY_ATTEMPTS":            "0",
		"RULE_ARN":                  opts.ruleArn,
		"TARGET_ARN":                target.Arn,
	} {
		if value == "" {
			continue
		}
		attrs[key] = &types.MessageAttribute{DataType: "String", StringValue: value}
	}

	groupID, dedupID := "", ""
	if isFIFOQueueARN(dlqArn) {
		groupID = target.ID
		eventID := eventIDFromPayload(eventPayload)
		if eventID == "" {
			eventID = uuid.NewString()
		}
		sum := sha256.Sum256([]byte("dlq\x00" + opts.ruleArn + "\x00" + target.ID + "\x00" + eventID))
		dedupID = hex.EncodeToString(sum[:])
	}

	start := time.Now()
	msg, sendErr := sender.SendMessage(queueName, string(eventPayload), 0, attrs, groupID, dedupID)
	duration := time.Since(start).Milliseconds()
	if sendErr != nil {
		meta["error"] = sendErr.Error()
		return []tracesvc.Span{{Kind: "dlq", Name: queueName, DurationMs: duration, Status: "error", Meta: meta}}
	}
	if msg != nil {
		meta["messageId"] = msg.MessageId
	}
	return []tracesvc.Span{{Kind: "dlq", Name: queueName, DurationMs: duration, Status: "ok", Meta: meta}}
}

// correlationAttributes carries Tarn's correlation ID as an SQS message
// attribute (the same one SNS → SQS sets) so the consuming event source
// mapping's trace joins the EventBridge delivery trace.
func correlationAttributes(correlationID string) map[string]*types.MessageAttribute {
	if strings.TrimSpace(correlationID) == "" {
		return nil
	}
	return map[string]*types.MessageAttribute{
		"correlationId": {DataType: "String", StringValue: correlationID},
	}
}

func eventIDFromPayload(eventPayload []byte) string {
	var envelope struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(eventPayload, &envelope); err != nil {
		return ""
	}
	return strings.TrimSpace(envelope.ID)
}

// canonicalSQSQueueARN validates arn:aws:sqs:<region>:<account>:<queueName>.
func canonicalSQSQueueARN(arn string) (string, error) {
	arn = strings.TrimSpace(arn)
	if _, err := sqsQueueNameFromARN(arn); err != nil {
		return "", err
	}
	return arn, nil
}

func sqsQueueNameFromARN(arn string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(arn), ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "sqs" || parts[3] == "" || parts[4] == "" {
		return "", fmt.Errorf("target Arn must be an SQS queue ARN (arn:aws:sqs:<region>:<account>:<queueName>)")
	}
	if !validSQSQueueName(parts[5]) {
		return "", fmt.Errorf("target Arn must reference a valid SQS queue name")
	}
	return parts[5], nil
}

func validSQSQueueName(name string) bool {
	base := strings.TrimSuffix(name, ".fifo")
	if base == "" || len(name) > 80 {
		return false
	}
	for _, r := range base {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func isFIFOQueueARN(arn string) bool {
	return strings.HasSuffix(strings.TrimSpace(arn), ".fifo")
}

// validateSQSTarget enforces the PutTargets rules AWS applies to queue
// targets: FIFO queues need a MessageGroupId, standard queues reject one.
func validateSQSTarget(target *types.EventBridgeTarget) error {
	if isFIFOQueueARN(target.Arn) {
		if target.SqsParameters == nil || strings.TrimSpace(target.SqsParameters.MessageGroupId) == "" {
			return fmt.Errorf("Parameter(s) SqsParameters must be specified for target: %s", target.ID)
		}
		return nil
	}
	if target.SqsParameters != nil && strings.TrimSpace(target.SqsParameters.MessageGroupId) != "" {
		return fmt.Errorf("Parameter MessageGroupId is not valid for target %s: MessageGroupId is only valid for FIFO queues", target.ID)
	}
	return nil
}

// validateTargetDeadLetterConfig checks DeadLetterConfig.Arn names an SQS queue.
func validateTargetDeadLetterConfig(target *types.EventBridgeTarget) error {
	if target.DeadLetterConfig == nil || strings.TrimSpace(target.DeadLetterConfig.Arn) == "" {
		return nil
	}
	if _, err := sqsQueueNameFromARN(target.DeadLetterConfig.Arn); err != nil {
		return fmt.Errorf("DeadLetterConfig Arn must be an SQS queue ARN: %s", target.DeadLetterConfig.Arn)
	}
	return nil
}

// cloneTarget deep-copies every pointer field of a target so stored rules
// never alias request or response values.
func cloneTarget(src types.EventBridgeTarget) types.EventBridgeTarget {
	dst := src
	if src.LastInvokedAt != nil {
		t := *src.LastInvokedAt
		dst.LastInvokedAt = &t
	}
	if src.InputTransformer != nil {
		cloned := *src.InputTransformer
		if src.InputTransformer.InputPathsMap != nil {
			cloned.InputPathsMap = cloneStringMap(src.InputTransformer.InputPathsMap)
		}
		dst.InputTransformer = &cloned
	}
	dst.EcsParameters = cloneECSParameters(src.EcsParameters)
	if src.SqsParameters != nil {
		params := *src.SqsParameters
		dst.SqsParameters = &params
	}
	if src.DeadLetterConfig != nil {
		dlq := *src.DeadLetterConfig
		dst.DeadLetterConfig = &dlq
	}
	if src.RetryPolicy != nil {
		policy := *src.RetryPolicy
		if src.RetryPolicy.MaximumRetryAttempts != nil {
			v := *src.RetryPolicy.MaximumRetryAttempts
			policy.MaximumRetryAttempts = &v
		}
		if src.RetryPolicy.MaximumEventAgeInSeconds != nil {
			v := *src.RetryPolicy.MaximumEventAgeInSeconds
			policy.MaximumEventAgeInSeconds = &v
		}
		dst.RetryPolicy = &policy
	}
	return dst
}
