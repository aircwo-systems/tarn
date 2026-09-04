package mcp

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultAccountID is the account Tarn uses when no 12-digit access key is
// supplied. Queue URLs are account-scoped, so it has to appear in the path.
const defaultAccountID = "000000000000"

// PeekQueueInput selects messages to inspect.
type PeekQueueInput struct {
	Queue   string `json:"queue" jsonschema:"Queue name, not a URL."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum messages to return. Defaults to 20, capped at 100."`
	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// QueueMessage is one message sitting in a queue.
type QueueMessage struct {
	ID           string `json:"id"`
	Body         string `json:"body"`
	State        string `json:"state" jsonschema:"visible, or in flight while a consumer holds it."`
	ReceiveCount int    `json:"receiveCount" jsonschema:"How many times this message has been delivered. A climbing count on a queue with a dead letter target usually means the consumer keeps failing."`
	RetryCount   int    `json:"retryCount,omitempty"`
}

// PeekQueueOutput reports queue contents without consuming anything.
type PeekQueueOutput struct {
	Queue    string         `json:"queue"`
	Messages []QueueMessage `json:"messages"`
	Returned int            `json:"returned"`
}

const peekQueueDescription = `Inspect messages sitting in an SQS queue on the local Tarn instance.

This is a read-only peek. Messages are not received, their visibility is not
changed, and nothing is deleted, so it is safe to call while a consumer is
running.

Use it to debug asynchronous paths, where no caller sees an invocation result.
A message with a climbing receiveCount means its consumer keeps failing; check
the consumer's logs with tarn_get_logs. Messages piling up in a queue whose name
ends in -dlq are ones that already exhausted their retries.`

func addPeekQueueTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_peek_queue",
		Description: peekQueueDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Peek SQS queue",
			ReadOnlyHint: true,
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in PeekQueueInput) (
		*mcp.CallToolResult, PeekQueueOutput, error,
	) {
		if strings.TrimSpace(in.Queue) == "" {
			return nil, PeekQueueOutput{}, errors.New("queue is required")
		}

		q := url.Values{"limit": {strconv.Itoa(clampInt(in.Limit, 20, 100))}}

		var raw struct {
			Queue    string         `json:"queue"`
			Messages []QueueMessage `json:"messages"`
		}
		path := "/_tarn/admin/queues/" + url.PathEscape(in.Queue) + "/messages"
		if err := c.get(ctx, path, in.Account, q, &raw); err != nil {
			return nil, PeekQueueOutput{}, err
		}

		return nil, PeekQueueOutput{
			Queue:    defaultString(raw.Queue, in.Queue),
			Messages: raw.Messages,
			Returned: len(raw.Messages),
		}, nil
	}

	mcp.AddTool(s, tool, handler)
}

// SendMessageInput describes a message to enqueue.
type SendMessageInput struct {
	Queue   string `json:"queue" jsonschema:"Queue name, not a URL."`
	Body    string `json:"body" jsonschema:"Message body. Pass JSON as a string if the consumer expects it."`
	GroupID string `json:"groupId,omitempty" jsonschema:"Message group ID. Required for FIFO queues, whose names end in .fifo."`
	DedupID string `json:"dedupId,omitempty" jsonschema:"Deduplication ID for FIFO queues."`
	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// SendMessageOutput reports the enqueued message.
type SendMessageOutput struct {
	MessageID string `json:"messageId"`
	Queue     string `json:"queue"`
}

const sendMessageDescription = `Send a message to an SQS queue on the local Tarn instance.

Use this to exercise an event-driven path end to end: enqueue a message, then
read the consumer's logs with tarn_get_logs to see what it did. Nothing here
reaches real AWS.

If the queue has a Lambda event source mapping, the consumer is invoked
automatically and no invocation result comes back to you, so logs are the only
way to see the outcome.

FIFO queues, whose names end in .fifo, require groupId.`

func addSendMessageTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_send_message",
		Description: sendMessageDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Send SQS message",
			DestructiveHint: boolPtr(true),
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in SendMessageInput) (
		*mcp.CallToolResult, SendMessageOutput, error,
	) {
		if strings.TrimSpace(in.Queue) == "" {
			return nil, SendMessageOutput{}, errors.New("queue is required")
		}

		account := defaultString(in.Account, defaultAccountID)
		form := url.Values{
			"Action":      {"SendMessage"},
			"QueueUrl":    {fmt.Sprintf("%s/%s/%s", c.endpoint, account, in.Queue)},
			"MessageBody": {in.Body},
		}
		if in.GroupID != "" {
			form.Set("MessageGroupId", in.GroupID)
		}
		if in.DedupID != "" {
			form.Set("MessageDeduplicationId", in.DedupID)
		}

		body, err := c.postForm(ctx, "/"+account+"/"+in.Queue, in.Account, form)
		if err != nil {
			return nil, SendMessageOutput{}, err
		}

		var result struct {
			XMLName   xml.Name `xml:"SendMessageResponse"`
			MessageID string   `xml:"SendMessageResult>MessageId"`
		}
		if err := xml.Unmarshal(body, &result); err != nil {
			return nil, SendMessageOutput{}, fmt.Errorf("failed to parse SendMessage response: %w", err)
		}

		return nil, SendMessageOutput{MessageID: result.MessageID, Queue: in.Queue}, nil
	}

	mcp.AddTool(s, tool, handler)
}

// PublishInput describes an SNS publish.
type PublishInput struct {
	TopicARN string `json:"topicArn" jsonschema:"Full topic ARN, as reported by tarn_status."`
	Message  string `json:"message" jsonschema:"Message body."`
	Subject  string `json:"subject,omitempty"`
	Account  string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// PublishOutput reports the published message.
type PublishOutput struct {
	MessageID string `json:"messageId"`
	TopicARN  string `json:"topicArn"`
}

const publishDescription = `Publish a message to an SNS topic on the local Tarn instance.

Subscribers receive it as they would on AWS, so this is the way to exercise a
fan-out path. Subscribed queues can then be inspected with tarn_peek_queue, and
subscribed functions leave their output in logs.

Pass the full topic ARN. tarn_status reports the topics that exist.`

func addPublishTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_publish",
		Description: publishDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Publish to SNS topic",
			DestructiveHint: boolPtr(true),
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in PublishInput) (
		*mcp.CallToolResult, PublishOutput, error,
	) {
		if strings.TrimSpace(in.TopicARN) == "" {
			return nil, PublishOutput{}, errors.New("topicArn is required")
		}

		form := url.Values{
			"Action":   {"Publish"},
			"TopicArn": {in.TopicARN},
			"Message":  {in.Message},
		}
		if in.Subject != "" {
			form.Set("Subject", in.Subject)
		}

		body, err := c.postForm(ctx, "/", in.Account, form)
		if err != nil {
			return nil, PublishOutput{}, err
		}

		var result struct {
			XMLName   xml.Name `xml:"PublishResponse"`
			MessageID string   `xml:"PublishResult>MessageId"`
		}
		if err := xml.Unmarshal(body, &result); err != nil {
			return nil, PublishOutput{}, fmt.Errorf("failed to parse Publish response: %w", err)
		}

		return nil, PublishOutput{MessageID: result.MessageID, TopicARN: in.TopicARN}, nil
	}

	mcp.AddTool(s, tool, handler)
}
