package eventsource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
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
	IncrementProcessedCount(queueName string, delta int64) error
	// ChangeMessageVisibility updates the visibility timeout of an in-flight message.
	// Setting timeout=0 makes the message immediately visible to other consumers.
	ChangeMessageVisibility(queueName, receiptHandle string, timeout int) error
	// ReleaseMessage resets visibility and undoes this receive attempt so
	// filter misses do not count toward redrive/DLQ thresholds.
	ReleaseMessage(queueName, receiptHandle string) error
	// MoveToDLQIfExceeded checks whether msg has exceeded the queue's maxReceiveCount
	// and, if so, delivers it to the configured DLQ and deletes it from srcQueue.
	// Returns (true, dlqName, nil) when moved, (false, "", nil) when below the threshold.
	MoveToDLQIfExceeded(srcQueue string, msg *types.SQSMessage) (bool, string, error)
}

// waitingReceiver is implemented by SQS services whose long polls can be
// cancelled. The poller uses it to wait for messages instead of re-polling
// every second, and still stops promptly when the mapping is removed.
type waitingReceiver interface {
	ReceiveMessageUntil(queueName string, maxCount, visTimeout, waitTimeSec int, cancel <-chan struct{}) ([]*types.SQSMessage, error)
}

// sqsLongPollSeconds matches the long poll AWS's own SQS pollers use.
const sqsLongPollSeconds = 20

// StreamInterface abstracts DynamoDB Streams operations needed by the poller.
type StreamInterface interface {
	StreamBatch(streamArn, lastSequence string, limit int) ([]*types.StreamRecord, string, error)
}

// LambdaInterface abstracts Lambda operations needed by the poller.
type LambdaInterface interface {
	Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error)
}

type poller struct {
	mapping    *types.EventSourceMapping
	sqs        SQSInterface
	streams    StreamInterface
	lambda     LambdaInterface
	store      *Store
	traceStore *tracesvc.Store
	collector  *tracesvc.Collector
	done       chan struct{}
	stopOnce   sync.Once
	// lambdaMaxConcurrency caps the SQS poll loops at the function's
	// concurrency limit, so extra loops don't just queue for a container.
	// Zero means no cap.
	lambdaMaxConcurrency int

	// mu guards the mapping's mutable fields and notFoundStreak, which the
	// concurrent poll loops share.
	mu             sync.Mutex
	notFoundStreak int

	drainers atomic.Int32 // extra poll loops running while a backlog lasts
}

const (
	maxConsecutiveNotFoundRetries = 5

	// defaultSQSPollers is how many poll loops an SQS mapping runs when it
	// sets no ScalingConfig.MaximumConcurrency.
	defaultSQSPollers = 5

	// invokeTimeout bounds one ESM invoke: the 15-minute Lambda maximum plus
	// room for a cold start. The function's own timeout applies inside it.
	invokeTimeout = 16 * time.Minute
)

func newPoller(mapping *types.EventSourceMapping, sqsSvc SQSInterface, streamsSvc StreamInterface, lambdaSvc LambdaInterface, store *Store, traceStore *tracesvc.Store, collector *tracesvc.Collector) *poller {
	return &poller{
		mapping:    mapping,
		sqs:        sqsSvc,
		streams:    streamsSvc,
		lambda:     lambdaSvc,
		store:      store,
		traceStore: traceStore,
		collector:  collector,
		done:       make(chan struct{}),
	}
}

func (p *poller) start() {
	log.Printf("[eventsource] %s: starting poller (up to %d concurrent) queue=%s function=%s", p.mapping.UUID, p.loops(), p.mapping.QueueName, p.mapping.FunctionName)
	go p.run()
}

// loops returns how many poll loops may run at once. SQS mappings scale out
// like AWS pollers; FIFO ordering is still kept because the queue never hands
// out a message group that is already in flight. DynamoDB streams keep a
// single loop so records are processed in sequence order.
func (p *poller) loops() int {
	if normalizeSourceType(p.mapping) == "dynamodb-stream" {
		return 1
	}
	n := defaultSQSPollers
	if sc := p.mapping.ScalingConfig; sc != nil && sc.MaximumConcurrency > 0 {
		n = sc.MaximumConcurrency
	}
	if p.lambdaMaxConcurrency > 0 && n > p.lambdaMaxConcurrency {
		n = p.lambdaMaxConcurrency
	}
	return n
}

func (p *poller) stop() {
	p.stopOnce.Do(func() {
		close(p.done)
	})
}

func (p *poller) stopped() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// run is the main poll loop. It polls once per tick while the source is idle.
// When full batches come back it keeps polling and starts extra drain loops,
// so a backlog is worked at invoke speed across up to loops() invokes at once.
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
		}
		for p.poll() {
			if p.stopped() {
				return
			}
			p.scaleUp()
		}
	}
}

// scaleUp starts drain loops until loops() are running in total.
func (p *poller) scaleUp() {
	for {
		n := p.drainers.Load()
		if int(n) >= p.loops()-1 {
			return
		}
		if p.drainers.CompareAndSwap(n, n+1) {
			go p.drain()
		}
	}
}

// drain polls back to back until a batch comes back short, then exits.
func (p *poller) drain() {
	defer p.drainers.Add(-1)
	for !p.stopped() && p.poll() {
	}
}

// poll receives one batch and invokes the function with it. It returns true
// when the batch was full and was processed, meaning more work is likely
// waiting. Filter misses alone never count, because they are released and
// would otherwise be received again straight away.
func (p *poller) poll() bool {
	if normalizeSourceType(p.mapping) == "dynamodb-stream" {
		return p.pollDynamoStream()
	}
	var msgs []*types.SQSMessage
	var err error
	if w, ok := p.sqs.(waitingReceiver); ok {
		msgs, err = w.ReceiveMessageUntil(p.mapping.QueueName, p.mapping.BatchSize, -1, sqsLongPollSeconds, p.done)
	} else {
		msgs, err = p.sqs.ReceiveMessage(p.mapping.QueueName, p.mapping.BatchSize, -1, 1)
	}
	// Measured from receipt: time spent waiting on an empty queue is not
	// part of handling the batch.
	pollStart := time.Now()
	if err != nil {
		if isNotFoundError(err) {
			p.disableWithError(err)
			return false
		}
		log.Printf("[eventsource] %s: receive error: %v", p.mapping.UUID, err)
		p.updateResult(fmt.Sprintf("ERROR: %v", err))
		return false
	}
	p.resetNotFound()
	if len(msgs) == 0 {
		return false
	}
	fullBatch := len(msgs) >= p.mapping.BatchSize

	// Apply filter criteria: partition messages into matching and non-matching.
	// Non-matching messages are released so other pollers (with different
	// filter policies on the same queue) can process them, without incrementing
	// receive count toward DLQ redrive thresholds.
	matching := msgs
	if p.mapping.FilterCriteria != nil && len(p.mapping.FilterCriteria.Filters) > 0 {
		matching = make([]*types.SQSMessage, 0, len(msgs))
		for _, msg := range msgs {
			if matchesAnyFilter(msg, p.mapping.FilterCriteria) {
				matching = append(matching, msg)
			} else {
				// Release the message immediately for other consumers, without
				// counting this filter miss as a processing attempt.
				if err := p.sqs.ReleaseMessage(p.mapping.QueueName, msg.ReceiptHandle); err != nil {
					log.Printf("[eventsource] %s: release filtered-out message %s: %v", p.mapping.UUID, msg.MessageId, err)
				}
			}
		}
	}
	if len(matching) == 0 {
		return false
	}
	msgs = matching

	payload := buildSQSEventPayload(msgs, p.mapping.EventSourceArn, p.mapping.QueueName)

	ctx, cancel := context.WithTimeout(context.Background(), invokeTimeout)
	defer cancel()

	functionName := p.functionName()

	inv := p.collector.Begin()
	lambdaStart := time.Now()
	sqsDurationMs := lambdaStart.Sub(pollStart).Milliseconds()
	output, err := p.lambda.Invoke(ctx, &types.InvokeInput{
		FunctionName:   functionName,
		Payload:        payload,
		InvocationType: "RequestResponse",
	})
	lambdaDurationMs := time.Since(lambdaStart).Milliseconds()

	if err != nil {
		if isNotFoundError(err) {
			inv.Finish(func([]tracesvc.Span) {})
			p.disableWithError(err)
			return false
		}
		log.Printf("[eventsource] %s: invoke error: %v", p.mapping.UUID, err)
		p.updateResult(fmt.Sprintf("ERROR: %v", err))
		p.recordTrace(pollStart, functionName, msgs, sqsDurationMs, lambdaDurationMs, len(msgs), len(msgs), inv)

		// Check DLQ for all messages — ReceiveMessage already incremented their
		// receive count, so without this check messages bypass maxReceiveCount
		// and retry indefinitely (the background reaper races with the poller).
		for _, msg := range msgs {
			moved, dlqName, dlqErr := p.sqs.MoveToDLQIfExceeded(p.mapping.QueueName, msg)
			if dlqErr != nil {
				log.Printf("[eventsource] %s: DLQ check error for message %s: %v", p.mapping.UUID, msg.MessageId, dlqErr)
			} else if moved {
				log.Printf("[eventsource] %s: message %s moved to DLQ after %d attempt(s)", p.mapping.UUID, msg.MessageId, msg.ApproximateReceiveCount)
				p.recordDLQTrace(pollStart, p.mapping.QueueName, dlqName)
			}
		}
		return false
	}
	p.resetNotFound()

	log.Printf("[eventsource] %s: invoke response: statusCode=%d functionError=%q payload=%s",
		p.mapping.UUID, output.StatusCode, output.FunctionError, truncate(output.Payload, 120))

	// Determine which messages failed. Failed messages are NOT deleted so their
	// visibility timeout expires, they become visible again for retry.
	// Messages that exceed the queue's maxReceiveCount are moved to the DLQ directly
	// by the poller — this avoids a race condition between the poller and the reaper.
	failed := parseBatchItemFailures(output, msgs)

	failCount := 0
	successCount := int64(0)
	for _, msg := range msgs {
		if !failed[msg.MessageId] {
			if err := p.sqs.DeleteMessage(p.mapping.QueueName, msg.ReceiptHandle); err != nil {
				log.Printf("[eventsource] %s: delete message error: %v", p.mapping.UUID, err)
			} else {
				successCount++
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

	if successCount > 0 {
		if err := p.sqs.IncrementProcessedCount(p.mapping.QueueName, successCount); err != nil {
			log.Printf("[eventsource] %s: increment processed count error: %v", p.mapping.UUID, err)
		}
	}

	if failCount > 0 {
		errMsg := lambdaErrorMessage(output.Payload)
		log.Printf("[eventsource] %s: %d/%d message(s) failed (%s)", p.mapping.UUID, failCount, len(msgs), errMsg)
		p.updateResult(fmt.Sprintf("ERROR: %d/%d messages failed", failCount, len(msgs)))
		p.recordTrace(pollStart, functionName, msgs, sqsDurationMs, lambdaDurationMs, len(msgs), failCount, inv)
	} else {
		p.updateResult("OK")
		p.recordTrace(pollStart, functionName, msgs, sqsDurationMs, lambdaDurationMs, len(msgs), 0, inv)
	}
	// Back off to the tick after failures rather than retrying at full speed.
	return fullBatch && failCount == 0
}

func (p *poller) pollDynamoStream() bool {
	pollStart := time.Now()
	records, nextSeq, err := p.streams.StreamBatch(p.mapping.EventSourceArn, p.mapping.LastStreamSequence, p.mapping.BatchSize)
	if err != nil {
		if isNotFoundError(err) {
			p.disableWithError(err)
			return false
		}
		log.Printf("[eventsource] %s: stream batch error: %v", p.mapping.UUID, err)
		p.updateResult(fmt.Sprintf("ERROR: %v", err))
		return false
	}
	p.resetNotFound()
	if len(records) == 0 {
		return false
	}

	payload := buildDynamoDBEventPayload(records)
	ctx, cancel := context.WithTimeout(context.Background(), invokeTimeout)
	defer cancel()

	functionName := p.functionName()
	inv := p.collector.Begin()
	streamDurationMs := time.Since(pollStart).Milliseconds()
	lambdaStart := time.Now()
	output, err := p.lambda.Invoke(ctx, &types.InvokeInput{
		FunctionName:   functionName,
		Payload:        payload,
		InvocationType: "RequestResponse",
	})
	lambdaDurationMs := time.Since(lambdaStart).Milliseconds()

	if err != nil || output == nil || output.FunctionError != "" || lambdaReturnedError(output) {
		if err != nil && isNotFoundError(err) {
			inv.Finish(func([]tracesvc.Span) {})
			p.disableWithError(err)
			return false
		}
		msg := "stream invoke failed"
		if err != nil {
			msg = err.Error()
		} else if output != nil && len(output.Payload) > 0 {
			msg = lambdaErrorMessage(output.Payload)
		}
		log.Printf("[eventsource] %s: %s", p.mapping.UUID, msg)
		p.updateResult("ERROR: " + msg)
		p.recordStreamTrace(pollStart, functionName, len(records), streamDurationMs, lambdaDurationMs, true, inv)
		return false
	}

	p.mu.Lock()
	p.mapping.LastStreamSequence = nextSeq
	p.saveLocked()
	p.mu.Unlock()
	p.updateResult("OK")
	p.recordStreamTrace(pollStart, functionName, len(records), streamDurationMs, lambdaDurationMs, false, inv)
	return len(records) >= p.mapping.BatchSize
}

// recordTrace records the poll's trace once the invocation's late telemetry
// has arrived, without holding up the next poll.
func (p *poller) recordTrace(start time.Time, functionName string, msgs []*types.SQSMessage, sqsDurationMs, lambdaDurationMs int64, msgCount, failCount int, inv *tracesvc.Invocation) {
	if p.traceStore == nil {
		inv.Finish(func([]tracesvc.Span) {})
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
			Meta: map[string]string{
				"msgCount":     fmt.Sprintf("%d", msgCount),
				"receiveCount": fmt.Sprintf("%d", maxApproximateReceiveCount(msgs)),
			},
		},
		{
			Kind:       "lambda",
			Name:       functionName,
			DurationMs: lambdaDurationMs,
			Status:     lambdaStatus,
		},
	}
	correlationID := tracesvc.NewCorrelationID()
	if value := correlationIDFromSQSMessageAttributes(msgs); value != "" {
		correlationID = value
	}
	durationMs := time.Since(start).Milliseconds()
	inv.Finish(func(subSpans []tracesvc.Span) {
		p.traceStore.Add(&tracesvc.Trace{
			ID:            uuid.NewString()[:8],
			CorrelationID: correlationID,
			StartedAt:     start,
			DurationMs:    durationMs,
			Status:        status,
			Spans:         append(spans, subSpans...),
		})
	})
}

func maxApproximateReceiveCount(msgs []*types.SQSMessage) int {
	maxCount := 0
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.ApproximateReceiveCount > maxCount {
			maxCount = msg.ApproximateReceiveCount
		}
	}
	return maxCount
}

func correlationIDFromSQSMessageAttributes(msgs []*types.SQSMessage) string {
	for _, msg := range msgs {
		if msg == nil || len(msg.MessageAttributes) == 0 {
			continue
		}
		for _, key := range []string{"correlationId", "CorrelationId", "x-correlation-id"} {
			if attr, ok := msg.MessageAttributes[key]; ok && attr != nil && strings.TrimSpace(attr.StringValue) != "" {
				return strings.TrimSpace(attr.StringValue)
			}
		}
	}
	return ""
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
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setResultLocked(result)
}

func (p *poller) setResultLocked(result string) {
	p.mapping.LastProcessingResult = result
	p.mapping.LastModified = time.Now().UTC()
	p.saveLocked()
}

// saveLocked persists the mapping unless the poller has been stopped: a
// stopped poller's copy is stale, and an in-flight poll finishing after
// UpdateMapping or DeleteMapping must not write it back.
func (p *poller) saveLocked() {
	if !p.stopped() {
		_ = p.store.Save(p.mapping)
	}
}

// functionName returns the mapping's function name, healing persisted
// mappings that were saved with FunctionName as an ARN.
func (p *poller) functionName() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	name := normalizeLambdaFunctionName(p.mapping.FunctionName)
	if name != p.mapping.FunctionName {
		p.mapping.FunctionName = name
		p.saveLocked()
	}
	return name
}

func (p *poller) resetNotFound() {
	p.mu.Lock()
	p.notFoundStreak = 0
	p.mu.Unlock()
}

func (p *poller) disableWithError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notFoundStreak++
	if p.notFoundStreak < maxConsecutiveNotFoundRetries {
		log.Printf("[eventsource] %s: resource not found, will retry (%d/%d): %v", p.mapping.UUID, p.notFoundStreak, maxConsecutiveNotFoundRetries, err)
		p.setResultLocked(fmt.Sprintf("ERROR: %v", err))
		return
	}
	if !p.mapping.Enabled && p.mapping.State == "Disabled" {
		return // another loop already disabled the mapping
	}

	p.mapping.Enabled = false
	p.mapping.State = "Disabled"
	msg := fmt.Sprintf("DISABLED: resource not found after %d retries: %v", maxConsecutiveNotFoundRetries, err)
	log.Printf("[eventsource] %s: %s", p.mapping.UUID, msg)
	p.setResultLocked(msg)
	p.stop()
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

func lambdaReturnedError(output *types.InvokeOutput) bool {
	if output == nil || len(output.Payload) == 0 {
		return false
	}
	var payload struct {
		ErrorType string `json:"errorType"`
	}
	return json.Unmarshal(output.Payload, &payload) == nil && payload.ErrorType != ""
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
			body, ok := decodeMessageBodyForFiltering(msg.Body)
			if !ok {
				return false
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

// decodeMessageBodyForFiltering parses message bodies for filter evaluation.
// It supports:
// 1) regular JSON objects
// 2) JSON strings containing an object payload
// 3) shell-escaped JSON objects like {\"type\":\"type2\"}
func decodeMessageBodyForFiltering(raw string) (map[string]interface{}, bool) {
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &body); err == nil {
		return body, true
	}

	// Some clients may double-encode message bodies as JSON strings.
	var wrapped string
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		if err := json.Unmarshal([]byte(wrapped), &body); err == nil {
			return body, true
		}
	}

	// Be tolerant of common shell-escaped JSON input, e.g. {\"type\":\"type2\"}.
	if strings.Contains(raw, `\"`) {
		normalized := strings.ReplaceAll(raw, `\"`, `"`)
		if err := json.Unmarshal([]byte(normalized), &body); err == nil {
			return body, true
		}
	}

	return nil, false
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

// sqsMessageAttribute is the Lambda-event representation of a user-defined
// SQS message attribute. We emit the AWS SQS event keys plus compatibility
// aliases used by existing SNS-style consumers.
type sqsMessageAttribute struct {
	StringValue            string   `json:"stringValue,omitempty"`
	StringValueCompat      string   `json:"StringValue,omitempty"`
	Value                  string   `json:"Value,omitempty"`
	BinaryValue            string   `json:"binaryValue,omitempty"`
	BinaryValueCompat      string   `json:"BinaryValue,omitempty"`
	DataType               string   `json:"dataType"`
	DataTypeCompat         string   `json:"DataType,omitempty"`
	StringListValues       []string `json:"stringListValues"`
	BinaryListValues       []string `json:"binaryListValues"`
	StringListValuesCompat []string `json:"StringListValues,omitempty"`
	BinaryListValuesCompat []string `json:"BinaryListValues,omitempty"`
}

type sqsEventRecord struct {
	MessageId      string `json:"messageId"`
	ReceiptHandle  string `json:"receiptHandle"`
	Body           string `json:"body"`
	Md5OfBody      string `json:"md5OfBody"`
	EventSource    string `json:"eventSource"`
	EventSourceARN string `json:"eventSourceARN"`
	AwsRegion      string `json:"awsRegion"`
	// Attributes holds SQS system attributes. The AWS Java SDK calls
	// getAttributes() and NPEs if this field is null/missing in the JSON,
	// so we always emit it as a non-null object even when it has no entries.
	Attributes              map[string]string              `json:"attributes"`
	MessageAttributes       map[string]sqsMessageAttribute `json:"messageAttributes"`
	MessageAttributesCompat map[string]sqsMessageAttribute `json:"MessageAttributes,omitempty"`
}

func buildSQSEventPayload(msgs []*types.SQSMessage, eventSourceArn, queueName string) []byte {
	records := make([]sqsEventRecord, len(msgs))
	for i, msg := range msgs {
		// System attributes — always present (non-null) so Java SDKs can call
		// getAttributes().get(...) without a NullPointerException.
		attrs := map[string]string{
			"ApproximateReceiveCount":          fmt.Sprintf("%d", msg.ApproximateReceiveCount),
			"SentTimestamp":                    fmt.Sprintf("%d", msg.SentTimestamp),
			"ApproximateFirstReceiveTimestamp": fmt.Sprintf("%d", msg.ApproximateFirstReceiveTimestamp),
			"SenderId":                         "AIDAIENQZJOLO23YVJ4VO",
		}
		// FIFO-specific system attributes
		if msg.MessageGroupId != "" {
			attrs["MessageGroupId"] = msg.MessageGroupId
		}
		if msg.MessageDeduplicationId != "" {
			attrs["MessageDeduplicationId"] = msg.MessageDeduplicationId
		}

		// User-defined message attributes (different field/format from system attrs)
		msgAttrs := make(map[string]sqsMessageAttribute, len(msg.MessageAttributes))
		for k, v := range msg.MessageAttributes {
			if v != nil {
				var binVal string
				if len(v.BinaryValue) > 0 {
					binVal = base64.StdEncoding.EncodeToString(v.BinaryValue)
				}
				msgAttrs[k] = sqsMessageAttribute{
					StringValue:            v.StringValue,
					StringValueCompat:      v.StringValue,
					Value:                  v.StringValue,
					BinaryValue:            binVal,
					BinaryValueCompat:      binVal,
					DataType:               v.DataType,
					DataTypeCompat:         v.DataType,
					StringListValues:       []string{},
					BinaryListValues:       []string{},
					StringListValuesCompat: []string{},
					BinaryListValuesCompat: []string{},
				}
			}
		}

		records[i] = sqsEventRecord{
			MessageId:               msg.MessageId,
			ReceiptHandle:           msg.ReceiptHandle,
			Body:                    msg.Body,
			Md5OfBody:               msg.MD5OfBody,
			EventSource:             "aws:sqs",
			EventSourceARN:          eventSourceArn,
			AwsRegion:               "us-east-1",
			Attributes:              attrs,
			MessageAttributes:       msgAttrs,
			MessageAttributesCompat: msgAttrs,
		}
	}
	data, _ := json.Marshal(map[string]any{"Records": records})
	return data
}

func buildDynamoDBEventPayload(records []*types.StreamRecord) []byte {
	payload := map[string]any{"Records": records}
	data, _ := json.Marshal(payload)
	return data
}

// recordStreamTrace records the poll's trace once the invocation's late
// telemetry has arrived, without holding up the next poll.
func (p *poller) recordStreamTrace(start time.Time, functionName string, recordCount int, streamDurationMs, lambdaDurationMs int64, failed bool, inv *tracesvc.Invocation) {
	if p.traceStore == nil {
		inv.Finish(func([]tracesvc.Span) {})
		return
	}
	status := 200
	streamStatus := "ok"
	lambdaStatus := "ok"
	if failed {
		status = 500
		lambdaStatus = "error"
	}
	durationMs := time.Since(start).Milliseconds()
	sourceName := p.mapping.SourceName
	inv.Finish(func(subSpans []tracesvc.Span) {
		p.traceStore.Add(&tracesvc.Trace{
			ID:         uuid.NewString()[:8],
			StartedAt:  start,
			DurationMs: durationMs,
			Status:     status,
			Spans: append([]tracesvc.Span{
				{
					Kind:       "dynamodb",
					Name:       sourceName,
					DurationMs: streamDurationMs,
					Status:     streamStatus,
					Meta:       map[string]string{"recordCount": fmt.Sprintf("%d", recordCount)},
				},
				{
					Kind:       "lambda",
					Name:       functionName,
					DurationMs: lambdaDurationMs,
					Status:     lambdaStatus,
				},
			}, subSpans...),
		})
	})
}
