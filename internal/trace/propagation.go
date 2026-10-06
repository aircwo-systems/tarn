package trace

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/pkg/types"
	"github.com/google/uuid"
)

// Context identifies the distributed trace and the span that published a
// message. TraceID is the full 128-bit hex ID; ParentID is decimal, matching
// the agent intake's span IDs. Sampling tags remain in the original carrier.
type Context struct {
	TraceID  string
	ParentID string
}

func (c *Context) ID(accountID string) string { return "dd:" + accountID + ":" + c.TraceID }

func (c *Context) correlationID() string {
	low, _ := strconv.ParseUint(c.TraceID[16:], 16, 64)
	return strconv.FormatUint(low, 10)
}

// ContextFromHeaders accepts Datadog text-map headers and W3C traceparent.
func ContextFromHeaders(headers http.Header) *Context {
	low, lowErr := strconv.ParseUint(headers.Get("x-datadog-trace-id"), 10, 64)
	parent, parentErr := strconv.ParseUint(headers.Get("x-datadog-parent-id"), 10, 64)
	if lowErr == nil && parentErr == nil && low != 0 && parent != 0 {
		high := "0000000000000000"
		for _, tag := range strings.Split(headers.Get("x-datadog-tags"), ",") {
			key, value, ok := strings.Cut(strings.TrimSpace(tag), "=")
			if ok && key == "_dd.p.tid" {
				if len(value) != 16 {
					return nil
				}
				if _, err := strconv.ParseUint(value, 16, 64); err != nil {
					return nil
				}
				high = strings.ToLower(value)
			}
		}
		return &Context{TraceID: fmt.Sprintf("%s%016x", high, low), ParentID: strconv.FormatUint(parent, 10)}
	}
	parts := strings.Split(headers.Get("traceparent"), "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return nil
	}
	high, err := strconv.ParseUint(parts[1][:16], 16, 64)
	if err != nil {
		return nil
	}
	low, err = strconv.ParseUint(parts[1][16:], 16, 64)
	if err != nil || high == 0 && low == 0 {
		return nil
	}
	parent, err = strconv.ParseUint(parts[2], 16, 64)
	if err != nil || parent == 0 {
		return nil
	}
	if _, err := strconv.ParseUint(parts[3], 16, 8); err != nil {
		return nil
	}
	return &Context{TraceID: strings.ToLower(parts[1]), ParentID: strconv.FormatUint(parent, 10)}
}

func contextFromCarrier(raw []byte) *Context {
	// AWS attributes are bounded, and malformed context must never prevent a
	// publish or invoke. Do not scan arbitrary application payloads for IDs.
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil
	}
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	headers := make(http.Header, len(values))
	for key, value := range values {
		headers.Set(key, value)
	}
	return ContextFromHeaders(headers)
}

func ContextFromSNSAttributes(attrs map[string]types.SNSMessageAttribute) *Context {
	attr, ok := attrs["_datadog"]
	if !ok {
		return nil
	}
	if strings.Split(attr.DataType, ".")[0] == "Binary" {
		raw, err := base64.StdEncoding.DecodeString(attr.BinaryValue)
		if err != nil {
			return nil
		}
		return contextFromCarrier(raw)
	}
	return contextFromCarrier([]byte(attr.StringValue))
}

func ContextFromSQSMessage(msg *types.SQSMessage) *Context {
	if msg == nil {
		return nil
	}
	if attr := msg.MessageAttributes["_datadog"]; attr != nil {
		raw := []byte(attr.StringValue)
		if strings.Split(attr.DataType, ".")[0] == "Binary" {
			raw = attr.BinaryValue
		}
		if ctx := contextFromCarrier(raw); ctx != nil {
			return ctx
		}
	}
	// Non-raw SNS delivery keeps the carrier inside the notification body.
	var envelope struct {
		Type              string
		MessageAttributes map[string]struct{ Type, Value string }
	}
	if json.Unmarshal([]byte(msg.Body), &envelope) != nil || envelope.Type != "Notification" {
		return nil
	}
	attr, ok := envelope.MessageAttributes["_datadog"]
	if !ok {
		return nil
	}
	raw := []byte(attr.Value)
	if attr.Type == "Binary" {
		var err error
		raw, err = base64.StdEncoding.DecodeString(attr.Value)
		if err != nil {
			return nil
		}
	}
	return contextFromCarrier(raw)
}

// QueueSpanID ties a delivery to its successful send without adding private
// message attributes or changing the Datadog carrier.
func QueueSpanID(messageID string) string { return "tarn:sqs:" + messageID }

// RecordJoined keeps legacy traces when no valid carrier exists. With context,
// native spans merge into the same account-scoped trace as agent chunks,
// regardless of which arrives first.
func (s *Store) RecordJoined(ctx *Context, accountID string, t *Trace) {
	if s == nil {
		return
	}
	if ctx == nil {
		s.Add(t)
		return
	}
	t.ID, t.AccountID, t.CorrelationID = ctx.ID(accountID), accountID, ctx.correlationID()
	rootID := ""
	for i := range t.Spans {
		span := &t.Spans[i]
		if span.ID == "" {
			span.ID = "tarn:" + uuid.NewString()
		}
		if i == 0 {
			rootID = span.ID
		}
		if span.ParentID == "" {
			span.ParentID = rootID
			if i == 0 {
				span.ParentID = ctx.ParentID
			}
		}
		if span.StartedAt == nil {
			start := t.StartedAt
			span.StartedAt = &start
		}
		if span.DurationNs == 0 {
			span.DurationNs = span.DurationMs * int64(time.Millisecond)
		}
	}
	if err := s.Merge(t); err != nil {
		log.Printf("[trace] failed to join messaging trace: %v", err)
	}
}
