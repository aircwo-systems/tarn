# EventBridge

Scheduled and event-driven workflows.

<span class="status-badge status-partial">Partial Support</span> Scheduled rules, event-pattern rules, `PutEvents`, custom event buses, and Lambda, ECS and SQS targets.

## Supported Operations

| Operation | Status | Notes |
|-----------|--------|-------|
| PutRule | Supported | Scheduled rules or event-pattern rules |
| DescribeRule | Supported | |
| ListRules | Supported | |
| DeleteRule | Supported | |
| EnableRule | Supported | |
| DisableRule | Supported | |
| PutTargets | Supported | Lambda, ECS and SQS queue targets. `Input`, `InputPath`, `InputTransformer`, `SqsParameters`, `DeadLetterConfig` and `RetryPolicy` are accepted and echoed by `ListTargetsByRule` |
| RemoveTargets | Supported | |
| ListTargetsByRule | Supported | |
| ListRuleNamesByTarget | Supported | |
| PutEvents | Supported | Each entry matches enabled event-pattern rules on its `EventBusName` (default bus if unset) |
| CreateEventBus | Supported | Custom buses. Partner event sources are not supported |
| DescribeEventBus | Supported | Name or ARN. Describes `default` too |
| ListEventBuses | Supported | Lists `default` and every custom bus |
| DeleteEventBus | Supported | Fails while rules remain on the bus. `default` cannot be deleted |
| ListTagsForResource | Supported | Rules and custom buses |
| TagResource | Supported | Rules and custom buses |
| UntagResource | Supported | Rules and custom buses |

## Supported Schedule Expressions

### Rate Expressions
Execute at regular intervals:

```
rate(5 minutes)
rate(1 hour)
rate(7 days)
```

### Cron Expressions
AWS 6-field format:

```
cron(0 12 * * ? *)         # Every day at noon UTC
cron(0 0 ? * MON-FRI *)    # Every weekday at midnight
cron(0 0 1 * ? *)          # First day of each month
cron(0 0 ? * * *)          # Every day at midnight UTC
```

Supports:
- Ranges: `1-3`, `MON-FRI`
- Lists: `1,2,3`
- Steps: `*/5` (every 5th)
- Last day: `L` (in day field)
- Last weekday: `3L` (last Wednesday)
- Nth weekday: `3#2` (2nd Wednesday)

## Examples

### Scheduled Lambda Invocation

<div class="example-block">
<div class="lang">JavaScript (AWS SDK)</div>

```javascript
import { EventBridgeClient, PutRuleCommand, PutTargetsCommand } from "@aws-sdk/client-eventbridge";

const events = new EventBridgeClient({ endpoint: "http://127.0.0.1:4566" });

// Create rule that fires every 5 minutes
const ruleRes = await events.send(new PutRuleCommand({
  Name: "process-tasks",
  ScheduleExpression: "rate(5 minutes)",
  State: "ENABLED"
}));

// Target a Lambda function
await events.send(new PutTargetsCommand({
  Rule: "process-tasks",
  Targets: [
    {
      Id: "1",
      Arn: "arn:aws:lambda:us-east-1:000000000000:function:process-task",
      RoleArn: "arn:aws:iam::000000000000:role/eventbridge-role"
    }
  ]
}));
```
</div>

### Daily Reports with Terraform

<div class="example-block">
<div class="lang">HCL</div>

```hcl
resource "aws_cloudwatch_event_rule" "daily_report" {
  name                = "daily-report"
  schedule_expression = "cron(0 8 * * ? *)"  # Every day at 8 AM UTC
}

resource "aws_cloudwatch_event_target" "daily_report_lambda" {
  rule      = aws_cloudwatch_event_rule.daily_report.name
  target_id = "SendReport"
  arn       = aws_lambda_function.generate_report.arn
  role_arn  = aws_iam_role.eventbridge.arn
}

resource "aws_lambda_permission" "allow_eventbridge" {
  statement_id  = "AllowExecutionFromEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.generate_report.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.daily_report.arn
}
```
</div>

### Custom Events with Event Patterns

<div class="example-block">
<div class="lang">JavaScript (AWS SDK)</div>

```javascript
import {
  EventBridgeClient,
  PutEventsCommand,
  PutRuleCommand,
  PutTargetsCommand,
} from "@aws-sdk/client-eventbridge";

const events = new EventBridgeClient({ endpoint: "http://127.0.0.1:4566" });

await events.send(new PutRuleCommand({
  Name: "order-created",
  EventPattern: JSON.stringify({
    source: ["app.orders"],
    "detail-type": ["OrderCreated"]
  }),
  State: "ENABLED"
}));

await events.send(new PutTargetsCommand({
  Rule: "order-created",
  Targets: [{ Id: "1", Arn: "arn:aws:lambda:us-east-1:000000000000:function:process-order" }]
}));

await events.send(new PutEventsCommand({
  Entries: [
    {
      Source: "app.orders",
      DetailType: "OrderCreated",
      Detail: JSON.stringify({ orderId: "ord_123" })
    }
  ]
}));
```
</div>

## SQS targets

A target whose `Arn` is an SQS queue ARN, `arn:aws:sqs:<region>:<account>:<queue>`, enqueues the event on that queue. The body is the payload a Lambda target would get. That is the full EventBridge envelope by default, or the result of `Input`, `InputPath` or `InputTransformer`. Event-pattern rules matched by `PutEvents` and scheduled rules both deliver to queues. One rule can fan out to several queues and functions at once.

`PutTargets` checks queue targets and reports a `ValidationException` in `FailedEntries` for each one it rejects. A FIFO queue, one whose name ends in `.fifo`, needs `SqsParameters.MessageGroupId`. A standard queue rejects it. Tarn also rejects `SqsParameters` on a target that is not a queue.

Tarn does not check that the queue exists at `PutTargets` time, so Terraform can create the rule and the queue in either order. A missing queue shows up at delivery time as a failed target. If the target has a DLQ, the event lands there.

For FIFO targets Tarn sets a `MessageDeduplicationId`. On a queue with `ContentBasedDeduplication` it is the SHA-256 of the body, as SQS computes it. Otherwise it is derived from the rule, the target ID and the event `id`, so the same event delivered twice to one target dedupes, while two rules that send the same event to one queue still produce two messages.

Each queued message carries a `correlationId` String message attribute. AWS does not add it. Tarn does, the same way it does for SNS to SQS delivery. The event source mapping that consumes the queue reads it, so the EventBridge delivery trace and the consumer Lambda's trace share a correlation ID in the trace viewer. Delivery spans have kind `sqs`.

### EventBridge to SQS to Lambda with Terraform

<div class="example-block">
<div class="lang">HCL</div>

```hcl
resource "aws_cloudwatch_event_bus" "domain" {
  name = "domain-events"
}

resource "aws_sqs_queue" "signins" {
  name = "user-signins"
}

resource "aws_sqs_queue" "signins_dlq" {
  name = "user-signins-dlq"
}

resource "aws_cloudwatch_event_rule" "user_signed_in" {
  name           = "user-signed-in"
  event_bus_name = aws_cloudwatch_event_bus.domain.name
  event_pattern = jsonencode({
    source        = ["app.cognito"]
    "detail-type" = ["UserSignedIn"]
  })
}

resource "aws_cloudwatch_event_target" "signins" {
  rule           = aws_cloudwatch_event_rule.user_signed_in.name
  event_bus_name = aws_cloudwatch_event_bus.domain.name
  target_id      = "signins"
  arn            = aws_sqs_queue.signins.arn

  dead_letter_config {
    arn = aws_sqs_queue.signins_dlq.arn
  }
}

resource "aws_lambda_event_source_mapping" "signins" {
  event_source_arn = aws_sqs_queue.signins.arn
  function_name    = aws_lambda_function.record_signin.arn
}
```
</div>

A producer, for example a Cognito `PostAuthentication` trigger, then calls `PutEvents` with `EventBusName: "domain-events"`. The consumer Lambda receives the EventBridge envelope as the string in `Records[0].body`.

FIFO queue targets need a message group:

```hcl
resource "aws_cloudwatch_event_target" "audit" {
  rule = aws_cloudwatch_event_rule.user_signed_in.name
  arn  = aws_sqs_queue.audit_fifo.arn

  sqs_target {
    message_group_id = "users"
  }
}
```

## Dead-letter queues

`DeadLetterConfig.Arn` must be an SQS queue ARN. When Tarn cannot deliver an event to a target, it sends the original event to that queue with the message attributes AWS sets. The body is the event itself, before any `Input`, `InputPath` or `InputTransformer`.

| Attribute | Value |
|-----------|-------|
| `ERROR_CODE` | `NO_RESOURCE`, `THROTTLING`, `TIMEOUT`, `INVALID_PARAMETER` or `SDK_CLIENT_ERROR` |
| `ERROR_MESSAGE` | The delivery error, cut to 256 characters |
| `RULE_ARN` | The rule that matched |
| `TARGET_ARN` | The target that failed |
| `EXHAUSTED_RETRY_CONDITION` | `MaximumRetryAttempts` |
| `RETRY_ATTEMPTS` | `0` |

Only delivery failures count. A missing queue or function, an invoke error, or an ECS `RunTask` that starts nothing goes to the DLQ. A Lambda handler that throws does not, because AWS invokes Lambda targets asynchronously and the event was delivered. An ECS task that starts and later exits non-zero does not either. Both still mark the target's `LastResult` as an error.

Tarn tries each delivery once. It stores and echoes `RetryPolicy` so Terraform sees no drift, but it does not retry. A failed delivery goes straight to the DLQ.

## Custom event buses

`CreateEventBus` creates a bus, and every rule and target operation takes `EventBusName` as a name or ARN. Rule names are unique per bus, so `users` on `default` and `users` on `domain-events` are separate rules. Rule ARNs follow AWS: `rule/<name>` on the default bus and `rule/<bus>/<name>` on a custom one.

`PutEvents` routes each entry only to rules on its own bus. An entry naming a bus that does not exist fails with `ResourceNotFoundException` in its result entry, and the other entries still go through.

Scheduled rules are only allowed on the default bus, as on AWS.

## Local tooling

- `tarn flush` removes targets and rules on every bus, then deletes custom buses, before it deletes queues. With `--tag` or `--group` it also selects rules whose targets point at a selected SQS queue. It selects buses whose tags or name match, and every rule on those buses goes with them.
- The MCP tool `tarn_fire_rule` takes an optional `eventBus`. Without it, the default-bus rule with that name fires. If there is none, the first custom-bus rule with that name fires.
- The admin overview lists rules on every bus with their `eventBusName`, and lists custom buses under `eventBuses`.

## Known Limitations

- One delivery attempt per target. `RetryPolicy` is stored but not applied.
- No SNS, Kinesis, Step Functions or API destination targets.
- No archives, replays, `TestEventPattern` or `PutPermission` bus policies.
- `DeleteEventBus` refuses while rules remain on the bus.
