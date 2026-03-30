# SNS

Simple Notification Service — publish-subscribe messaging.

<span class="status-badge status-full">Fully Supported</span>

Full SNS emulation using the AWS Query/XML protocol. Create topics, manage subscriptions, publish messages with fanout to SQS queues and Lambda functions. Supports FIFO topics, message attribute filtering, and subscription filter policies.

::: info Terraform Compatible
Unknown SNS actions return empty success responses, so production `.tf` files work without modification. Route detection uses `Version=2010-03-31`.
:::

## Supported Operations

| Operation | Description | Status |
|-----------|-------------|--------|
| `CreateTopic` | Create a new SNS topic | Full |
| `DeleteTopic` | Delete a topic and all subscriptions | Full |
| `ListTopics` | List all topics in the account | Full |
| `GetTopicAttributes` | Get topic configuration and metadata | Full |
| `SetTopicAttributes` | Update topic attributes | Full |
| `Subscribe` | Subscribe an endpoint to a topic | Full |
| `Unsubscribe` | Remove a subscription | Full |
| `ListSubscriptions` | List all subscriptions | Full |
| `ListSubscriptionsByTopic` | List subscriptions for a specific topic | Full |
| `GetSubscriptionAttributes` | Get subscription configuration | Full |
| `SetSubscriptionAttributes` | Update subscription attributes | Full |
| `Publish` | Publish a message to a topic | Full |
| `PublishBatch` | Publish up to 10 messages in a single call | Full |
| `TagResource` | Add tags to a topic | Full |
| `UntagResource` | Remove tags from a topic | Full |
| `ListTagsForResource` | List tags on a topic | Full |

::: warning Stubbed Actions
Actions not listed above (e.g. `ConfirmSubscription`, `AddPermission`, `RemovePermission`) return `200 OK` with an empty result. Check server logs for `[sns] unhandled action` entries.
:::

## Subscription Protocols

- **SQS** — Messages delivered with SNS envelope (or raw via `RawMessageDelivery=true`)
- **Lambda** — Asynchronous invocation with SNS event record

## Features

- FIFO topics with content-based deduplication
- Message attribute filtering via `FilterPolicy`
- Filter policy scopes (`MessageAttributes` or `MessageBody`)
- Tag management for all topics

## Usage

::: code-group

```bash [Tarn]
# Create a topic
tarn sns create-topic --name events

# Subscribe an SQS queue
tarn sns subscribe \
  --topic-arn arn:aws:sns:us-east-1:000000000000:events \
  --protocol sqs \
  --endpoint arn:aws:sqs:us-east-1:000000000000:worker

# Publish a message
tarn sns publish \
  --topic-arn arn:aws:sns:us-east-1:000000000000:events \
  --message '{"orderId":"12345"}'
```

```bash [AWS CLI]
# Create a topic
aws sns create-topic --name events \
  --endpoint-url http://localhost:4566

# Subscribe an SQS queue
aws sns subscribe \
  --topic-arn arn:aws:sns:us-east-1:000000000000:events \
  --protocol sqs \
  --notification-endpoint arn:aws:sqs:us-east-1:000000000000:worker \
  --endpoint-url http://localhost:4566

# Publish a message
aws sns publish \
  --topic-arn arn:aws:sns:us-east-1:000000000000:events \
  --message '{"orderId":"12345"}' \
  --endpoint-url http://localhost:4566
```

:::

## Usage: Terraform

```hcl
resource "aws_sns_topic" "events" {
  name = "events"
  tags = {
    Environment = "dev"
  }
}

resource "aws_sns_topic_subscription" "worker" {
  topic_arn            = aws_sns_topic.events.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.worker.arn
  raw_message_delivery = true
}
```

<div class="tf-grid">
  <div class="tf-grid-item">
    <div class="label">Endpoint</div>
    <div class="value">http://localhost:4566</div>
  </div>
  <div class="tf-grid-item">
    <div class="label">Terraform key</div>
    <div class="value">endpoints.sns</div>
  </div>
</div>

See [Terraform guide](/guide/terraform) for the full provider configuration.

## Usage: AWS SDK

<div class="example-block">
<div class="lang">JavaScript</div>

```javascript
import { SNSClient, CreateTopicCommand, SubscribeCommand, PublishCommand } from "@aws-sdk/client-sns";

const sns = new SNSClient({ endpoint: "http://127.0.0.1:4566" });

// Create topic
const topicRes = await sns.send(new CreateTopicCommand({ Name: "orders" }));

// Subscribe Lambda
await sns.send(new SubscribeCommand({
  TopicArn: topicRes.TopicArn,
  Protocol: "lambda",
  Endpoint: "arn:aws:lambda:us-east-1:000000000000:function:process-order"
}));

// Publish with attributes
await sns.send(new PublishCommand({
  TopicArn: topicRes.TopicArn,
  Message: JSON.stringify({ orderId: "123", total: 99.99 }),
  MessageAttributes: {
    event: { DataType: "String", StringValue: "order-placed" }
  }
}));
```
</div>

## Filter Policies

Subscriptions support message attribute filtering. Set a `FilterPolicy` on the subscription to control which messages are delivered.

```json
{
  "eventType": ["order.created", "order.updated"],
  "priority": [{ "numeric": [">", 0, "<=", 5] }]
}
```

```javascript
await sns.send(new SetSubscriptionAttributesCommand({
  SubscriptionArn: "...",
  AttributeName: "FilterPolicy",
  AttributeValue: JSON.stringify(filterPolicy)
}));
```

## Protocol

- **Protocol:** Query/XML (`POST /` with `Action=...`)
- **Version:** `2010-03-31`
- **Namespace:** `http://sns.amazonaws.com/doc/2010-03-31/`

## Known Limitations

- Subscription protocols limited to **SQS** and **Lambda** (no HTTP/S, email, or SMS)
- No cross-account topic/subscription support
- Topic policies (`AddPermission` / `RemovePermission`) are stubbed
