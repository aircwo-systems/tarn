package eventsource

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	tracesvc "github.com/openstack-project/openstack/internal/trace"
	"github.com/openstack-project/openstack/pkg/types"
)

// lambdaErrorMessage extracts the errorMessage field from a Lambda error payload.
func lambdaErrorMessage(payload []byte) string {
	var e struct {
		ErrorMessage string `json:"errorMessage"`
		ErrorType    string `json:"errorType"`
	}
	if err := json.Unmarshal(payload, &e); err != nil || e.ErrorMessage == "" {
		return string(payload)
	}
	if e.ErrorType != "" {
		return e.ErrorType + ": " + e.ErrorMessage
	}
	return e.ErrorMessage
}

// batchItemFailures parses the Lambda response payload for the SQS ESM
// batchItemFailures protocol. Returns the set of failed messageIds.
// If the function returned a FunctionError, all message IDs are returned as failed.
func parseBatchItemFailures(output *types.InvokeOutput, msgs []*types.SQSMessage) map[string]bool {
	// Function error (throw / unhandled exception) — all messages failed.
	// Check both the FunctionError header and the payload shape: some RIE builds
	// omit X-Amz-Function-Error but still return {"errorType":...} in the body.
	functionFailed := output.FunctionError != ""
	if !functionFailed && len(output.Payload) > 0 {
		var e struct {
			ErrorType string `json:"errorType"`
		}
		if json.Unmarshal(output.Payload, &e) == nil && e.ErrorType != "" {
			functionFailed = true
		}
	}
	if functionFailed {
		ids := make(map[string]bool, len(msgs))
		for _, m := range msgs {
			ids[m.MessageId] = true
		}
		return ids
	}

	// Partial failure via batchItemFailures response
	if len(output.Payload) == 0 {
		return nil
	}
	var resp struct {
		BatchItemFailures []struct {
			ItemIdentifier string `json:"itemIdentifier"`
		} `json:"batchItemFailures"`
	}
	if err := json.Unmarshal(output.Payload, &resp); err != nil || len(resp.BatchItemFailures) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(resp.BatchItemFailures))
	for _, item := range resp.BatchItemFailures {
		ids[item.ItemIdentifier] = true
	}
	return ids
}

// SQSInterface abstracts SQS operations needed by the poller.
type SQSInterface interface {
	ReceiveMessage(queueName string, maxCount, visTimeout, waitTimeSec int) ([]*types.SQSMessage, error)
	DeleteMessage(queueName, receiptHandle string) error
	// ChangeMessageVisibility updates the visibility timeout of an in-flight message.
	// Setting timeout=0 makes the message immediately visible to other consumers.
	ChangeMessageVisibility(queueName, receiptHandle string, timeout int) error
	// MoveToDLQIfExceeded checks whether msg has exceeded the queue's maxReceiveCount
	// and, if so, delivers it to the configured DLQ and deletes it from srcQueue.
	// Returns (true, dlqName, nil) when moved, (false, "", nil) when below the threshold.
	MoveToDLQIfExceeded(srcQueue string, msg *types.SQSMessage) (bool, string, error)
}

// LambdaInterface abstracts Lambda operations needed by the poller.
type LambdaInterface interface {
	Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error)
}

type poller struct {
	mapping    *types.EventSourceMapping
	sqs        SQSInterface
	lambda     LambdaInterface
	store      *Store
	traceStore *tracesvc.Store
	collector  *tracesvc.Collector
	done       chan struct{}
	stopOnce   sync.Once
}

func newPoller(mapping *types.EventSourceMapping, sqsSvc SQSInterface, lambdaSvc LambdaInterface, store *Store, traceStore *tracesvc.Store, collector *tracesvc.Collector) *poller {
	return &poller{
		mapping:    mapping,
		sqs:        sqsSvc,
		lambda:     lambdaSvc,
		store:      store,
		traceStore: traceStore,
		collector:  collector,
		done:       make(chan struct{}),
	}
}

func (p *poller) start() {
	log.Printf("[eventsource] %s: starting poller queue=%s function=%s", p.mapping.UUID, p.mapping.QueueName, p.mapping.FunctionName)
	go p.run()
}

func (p *poller) stop() {
	p.stopOnce.Do(func() {
		close(p.done)
	})
}

func (p *poller) run() {
	interval := p.mapping.MaximumBatchingWindowInSeconds
	if interval < 1 {
		interval = 1
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.poll()
		}
	}
}

func (p *poller) poll() {
	pollStart := time.Now()

	msgs, err := p.sqs.ReceiveMessage(p.mapping.QueueName, p.mapping.BatchSize, 30, 1)
	if err != nil {
		if isNotFoundError(err) {
			p.disableWithError(err)
			return
		}
		log.Printf("[eventsource] %s: receive error: %v", p.mapping.UUID, err)
		p.updateResult(fmt.Sprintf("ERROR: %v", err))
		return
	}
	if len(msgs) == 0 {
		return
	}

	// Apply filter criteria: partition messages into matching and non-matching.
	// Non-matching messages have their visibility reset to 0 so other pollers
	// (with different filter policies on the same queue) can process them.
	matching := msgs
	if p.mapping.FilterCriteria != nil && len(p.mapping.FilterCriteria.Filters) > 0 {
		matching = make([]*types.SQSMessage, 0, len(msgs))
		for _, msg := range msgs {
			if matchesAnyFilter(msg, p.mapping.FilterCriteria) {
				matching = append(matching, msg)
			} else {
				// Release the message immediately for other consumers.
				if err := p.sqs.ChangeMessageVisibility(p.mapping.QueueName, msg.ReceiptHandle, 0); err != nil {
					log.Printf("[eventsource] %s: reset visibility for filtered-out message %s: %v", p.mapping.UUID, msg.MessageId, err)
				}
			}
		}
	}
	if len(matching) == 0 {
		return
	}
	msgs = matching

	payload := buildSQSEventPayload(msgs, p.mapping.EventSourceArn, p.mapping.QueueName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	functionName := normalizeLambdaFunctionName(p.mapping.FunctionName)
	if functionName != p.mapping.FunctionName {
		// Heal persisted mappings that were saved with FunctionName as an ARN.
		p.mapping.FunctionName = functionName
		_ = p.store.Save(p.mapping)
	}

	if p.collector != nil {
		p.collector.Begin(functionName)
	}
	lambdaStart := time.Now()
	sqsDurationMs := lambdaStart.Sub(pollStart).Milliseconds()
	output, err := p.lambda.Invoke(ctx, &types.InvokeInput{
		FunctionName:   functionName,
		Payload:        payload,
		InvocationType: "RequestResponse",
	})
	lambdaDurationMs := time.Since(lambdaStart).Milliseconds()

	var subSpans []tracesvc.Span
	if p.collector != nil {
		subSpans = tracesvc.SubSpansToSpans(p.collector.Collect(functionName))
	}

	if err != nil {
		if isNotFoundError(err) {
			p.disableWithError(err)
			return
		}
		log.Printf("[eventsource] %s: invoke error: %v", p.mapping.UUID, err)
		p.updateResult(fmt.Sprintf("ERROR: %v", err))
		p.recordTrace(pollStart, functionName, sqsDurationMs, lambdaDurationMs, len(msgs), 0, subSpans)
		return
	}

	log.Printf("[eventsource] %s: invoke response: statusCode=%d functionError=%q payload=%s",
		p.mapping.UUID, output.StatusCode, output.FunctionError, truncate(output.Payload, 120))

	// Determine which messages failed. Failed messages are NOT deleted so their
	// visibility timeout expires, they become visible again for retry.
	// Messages that exceed the queue's maxReceiveCount are moved to the DLQ directly
	// by the poller — this avoids a race condition between the poller and the reaper.
	failed := parseBatchItemFailures(output, msgs)

	failCount := 0
	for _, msg := range msgs {
		if !failed[msg.MessageId] {
			if err := p.sqs.DeleteMessage(p.mapping.QueueName, msg.ReceiptHandle); err != nil {
				log.Printf("[eventsource] %s: delete message error: %v", p.mapping.UUID, err)
			}
			continue
		}

		// Failed message: move to DLQ if maxReceiveCount exceeded, otherwise leave for retry.
		moved, dlqName, err := p.sqs.MoveToDLQIfExceeded(p.mapping.QueueName, msg)
		if err != nil {
			log.Printf("[eventsource] %s: DLQ check error for message %s: %v", p.mapping.UUID, msg.MessageId, err)
		} else if moved {
			log.Printf("[eventsource] %s: message %s moved to DLQ after %d attempt(s)", p.mapping.UUID, msg.MessageId, msg.ApproximateReceiveCount)
			p.recordDLQTrace(pollStart, p.mapping.QueueName, dlqName)
		}
		failCount++
	}

	if failCount > 0 {
		errMsg := lambdaErrorMessage(output.Payload)
		log.Printf("[eventsource] %s: %d/%d message(s) failed (%s)", p.mapping.UUID, failCount, len(msgs), errMsg)
		p.updateResult(fmt.Sprintf("ERROR: %d/%d messages failed", failCount, len(msgs)))
		p.recordTrace(pollStart, functionName, sqsDurationMs, lambdaDurationMs, len(msgs), failCount, subSpans)
	} else {
		p.updateResult("OK")
		p.recordTrace(pollStart, functionName, sqsDurationMs, lambdaDurationMs, len(msgs), 0, subSpans)
	}
}

func (p *poller) recordTrace(start time.Time, functionName string, sqsDurationMs, lambdaDurationMs int64, msgCount, failCount int, subSpans []tracesvc.Span) {
	if p.traceStore == nil {
		return
	}
	status := 200
	queueStatus := "ok"
	lambdaStatus := "ok"
	if failCount > 0 {
		status = 500
		lambdaStatus = "error"
	}
	spans := []tracesvc.Span{
		{
			Kind:       "queue",
			Name:       p.mapping.QueueName,
			DurationMs: sqsDurationMs,
			Status:     queueStatus,
			Meta:       map[string]string{"msgCount": fmt.Sprintf("%d", msgCount)},
		},
		{
			Kind:       "lambda",
			Name:       functionName,
			DurationMs: lambdaDurationMs,
			Status:     lambdaStatus,
		},
	}
	p.traceStore.Add(&tracesvc.Trace{
		ID:         uuid.NewString()[:8],
		StartedAt:  start,
		DurationMs: time.Since(start).Milliseconds(),
		Status:     status,
		Spans:      append(spans, subSpans...),
	})
}

func (p *poller) recordDLQTrace(start time.Time, srcQueue, dlqName string) {
	if p.traceStore == nil {
		return
	}
	p.traceStore.Add(&tracesvc.Trace{
		ID:         uuid.NewString()[:8],
		StartedAt:  start,
		DurationMs: time.Since(start).Milliseconds(),
		Status:     200,
		Spans: []tracesvc.Span{
			{
				Kind:   "queue",
				Name:   srcQueue,
				Status: "ok",
			},
			{
				Kind:   "dlq",
				Name:   dlqName,
				Status: "ok",
			},
		},
	})
}

func (p *poller) updateResult(result string) {
	p.mapping.LastProcessingResult = result
	p.mapping.LastModified = time.Now().UTC()
	_ = p.store.Save(p.mapping)
}

func (p *poller) disableWithError(err error) {
	log.Printf("[eventsource] %s: resource not found, will retry: %v", p.mapping.UUID, err)
	// Record the error but keep the poller running so it retries on the next
	// tick. In a local emulator the queue/function may not yet exist at startup
	// (e.g. SQS state still loading, function being created) but will appear
	// shortly. Stopping permanently caused messages to pile up unprocessed.
	p.updateResult(fmt.Sprintf("ERROR: %v", err))
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

// matchesAnyFilter returns true if the message matches at least one filter in the criteria.
// Multiple filters are OR'd; conditions within a single filter are AND'd.
func matchesAnyFilter(msg *types.SQSMessage, fc *types.FilterCriteria) bool {
	if fc == nil || len(fc.Filters) == 0 {
		return true
	}
	for _, f := range fc.Filters {
		if matchesFilterPattern(msg, f.Pattern) {
			return true
		}
	}
	return false
}

// matchesFilterPattern tests a message against a single JSON filter pattern string.
// Supported top-level keys: "body" (matched against parsed JSON body).
// Values are arrays of allowed values. Each entry can be:
//   - a scalar (exact match)
//   - {"prefix": "..."} for prefix matching
func matchesFilterPattern(msg *types.SQSMessage, pattern string) bool {
	if pattern == "" {
		return true
	}
	var fp map[string]interface{}
	if err := json.Unmarshal([]byte(pattern), &fp); err != nil {
		return false
	}
	for key, rawConditions := range fp {
		conditions, ok := rawConditions.(map[string]interface{})
		if !ok {
			return false
		}
		switch key {
		case "body":
			var body map[string]interface{}
			if err := json.Unmarshal([]byte(msg.Body), &body); err != nil {
				return false // body is not JSON
			}
			if !matchFieldConditions(body, conditions) {
				return false
			}
		// "messageAttributes" support can be added here in the future
		default:
			// Unknown top-level key — treat as no-match
			return false
		}
	}
	return true
}

// matchFieldConditions checks that every key in conditions matches the corresponding
// field in data. Each condition value must be an array of allowed values / matchers.
func matchFieldConditions(data map[string]interface{}, conditions map[string]interface{}) bool {
	for field, rawAllowed := range conditions {
		actual, exists := data[field]
		if !exists {
			return false
		}
		allowed, ok := rawAllowed.([]interface{})
		if !ok {
			return false
		}
		if !matchesAllowedValues(actual, allowed) {
			return false
		}
	}
	return true
}

// matchesAllowedValues returns true if actual satisfies any entry in the allowed list.
func matchesAllowedValues(actual interface{}, allowed []interface{}) bool {
	actualStr := fmt.Sprintf("%v", actual)
	for _, v := range allowed {
		switch cond := v.(type) {
		case map[string]interface{}:
			if prefix, ok := cond["prefix"].(string); ok {
				if strings.HasPrefix(actualStr, prefix) {
					return true
				}
			}
		default:
			// Exact match — compare as strings for simplicity
			if fmt.Sprintf("%v", v) == actualStr {
				return true
			}
		}
	}
	return false
}

type sqsEventRecord struct {
	MessageId      string `json:"messageId"`
	ReceiptHandle  string `json:"receiptHandle"`
	Body           string `json:"body"`
	Md5OfBody      string `json:"md5OfBody"`
	EventSource    string `json:"eventSource"`
	EventSourceARN string `json:"eventSourceARN"`
	AwsRegion      string `json:"awsRegion"`
}

func buildSQSEventPayload(msgs []*types.SQSMessage, eventSourceArn, queueName string) []byte {
	records := make([]sqsEventRecord, len(msgs))
	for i, msg := range msgs {
		records[i] = sqsEventRecord{
			MessageId:      msg.MessageId,
			ReceiptHandle:  msg.ReceiptHandle,
			Body:           msg.Body,
			Md5OfBody:      msg.MD5OfBody,
			EventSource:    "aws:sqs",
			EventSourceARN: eventSourceArn,
			AwsRegion:      "us-east-1",
		}
	}
	data, _ := json.Marshal(map[string]any{"Records": records})
	return data
}
