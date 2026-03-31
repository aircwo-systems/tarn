# EventBridge

Scheduled and event-driven workflows.

<span class="status-badge status-partial">Partial Support</span> — Scheduled rules, event-pattern rules, `PutEvents`, and Lambda targets

## Supported Operations

| Operation | Status | Notes |
|-----------|--------|-------|
| PutRule | Supported | Scheduled rules or event-pattern rules |
| DescribeRule | Supported | |
| ListRules | Supported | |
| DeleteRule | Supported | |
| EnableRule | Supported | |
| DisableRule | Supported | |
| PutTargets | Supported | Lambda targets only; `Input`, `InputPath`, and `InputTransformer` are supported |
| RemoveTargets | Supported | |
| ListTargetsByRule | Supported | |
| ListRuleNamesByTarget | Supported | |
| PutEvents | Supported | Matches enabled event-pattern rules on the default bus |
| ListTagsForResource | Supported | |
| TagResource | Supported | |
| UntagResource | Supported | |

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

## Known Limitations

- No dead letter queues
- No SQS or SNS targets
- Default event bus only
