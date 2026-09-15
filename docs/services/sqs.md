# SQS

Message queues for asynchronous processing.

<span class="status-badge status-full">Fully Supported</span>

## Supported Operations

| Operation | Status | Notes |
|-----------|--------|-------|
| CreateQueue | Supported | Standard and FIFO |
| DeleteQueue | Supported | |
| ListQueues | Supported | |
| GetQueueAttributes | Supported | |
| SetQueueAttributes | Supported | Visibility timeout, message retention |
| SendMessage | Supported | With attributes and deduplication |
| SendMessageBatch | Supported | Up to 10 messages |
| ReceiveMessage | Supported | Long polling, message attributes |
| DeleteMessage | Supported | |
| DeleteMessageBatch | Supported | |
| PurgeQueue | Supported | Clear all messages |
| CreateEventSourceMapping | Supported | Trigger Lambda on messages |

## Features

### FIFO Queues
Process messages in order with deduplication:

```javascript
await sqs.send(new SendMessageCommand({
  QueueUrl: "https://queue.fifo",
  MessageBody: "data",
  MessageGroupId: "group1",
  MessageDeduplicationId: "unique-id"
}));
```

### Event Source Mappings
Automatically invoke Lambda when messages arrive:

```bash
awslocal lambda create-event-source-mapping \
  --event-source-arn arn:aws:sqs:us-east-1:000000000000:my-queue \
  --function-name processor \
  --batch-size 10
```

### Dead Letter Queues
Capture messages that fail processing:
<div class="example-block">
<div class="lang">HCL (Terraform)</div>

```hcl
resource "aws_sqs_queue" "main" {
  name = "main-queue"
  message_retention_seconds = 86400
}

resource "aws_sqs_queue" "dlq" {
  name = "main-queue-dlq"
}

resource "aws_sqs_queue_redrive_policy" "main" {
  queue_url = aws_sqs_queue.main.id
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}
```
</div>

### Disruptor (publish failure injection)
Deliberately fail `SendMessage` calls to test how publishers handle partial
failures. Arm it from the dashboard (SQS section, per queue or bulk) or via
the admin API. Rules are in-memory only and do not survive restarts.

```bash
# Fail ~50% of sends to one queue with 503 ServiceUnavailable
curl -X PUT http://127.0.0.1:4566/_tarn/admin/sqs/disruptor \
  -H 'Content-Type: application/json' \
  -d '{"queue":"my-queue","enabled":true,"failureRate":50,"code":"ServiceUnavailable"}'

# Target several queues at once
curl -X PUT http://127.0.0.1:4566/_tarn/admin/sqs/disruptor \
  -H 'Content-Type: application/json' \
  -d '{"queues":["a","b"],"enabled":true,"failureRate":100,"code":"InternalError"}'

# List rules, disarm one queue, disarm everything
curl http://127.0.0.1:4566/_tarn/admin/sqs/disruptor
curl -X DELETE 'http://127.0.0.1:4566/_tarn/admin/sqs/disruptor?queue=my-queue'
curl -X DELETE 'http://127.0.0.1:4566/_tarn/admin/sqs/disruptor?all=true'
```

Supported codes: `ServiceUnavailable` (503), `InternalError` (500),
`Throttling` (400), `OverLimit` (400). Batch sends report per-entry failures,
so partial batch failure is exercised realistically.

## Examples

### Send and Receive

<div class="example-block">
<div class="lang">JavaScript</div>

```javascript
import { SQSClient, SendMessageCommand, ReceiveMessageCommand, DeleteMessageCommand } from "@aws-sdk/client-sqs";

const sqs = new SQSClient({ endpoint: "http://127.0.0.1:4566" });

// Send
const sendRes = await sqs.send(new SendMessageCommand({
  QueueUrl: "https://sqs.us-east-1.amazonaws.com/000000000000/my-queue",
  MessageBody: JSON.stringify({ task: "process-order", orderId: "123" }),
  MessageAttributes: {
    Priority: { DataType: "Number", StringValue: "5" }
  }
}));

// Receive
const receiveRes = await sqs.send(new ReceiveMessageCommand({
  QueueUrl: "https://sqs.us-east-1.amazonaws.com/000000000000/my-queue",
  MaxNumberOfMessages: 10,
  WaitTimeSeconds: 20, // Long polling
  MessageAttributeNames: ["All"]
}));

for (const msg of receiveRes.Messages || []) {
  console.log("Message:", msg.Body);

  // Process and delete
  await sqs.send(new DeleteMessageCommand({
    QueueUrl: "...",
    ReceiptHandle: msg.ReceiptHandle
  }));
}
```
</div>

## Known Limitations

- No KMS encryption
- SQS-managed server-side encryption is a compatibility stub, not real encryption
