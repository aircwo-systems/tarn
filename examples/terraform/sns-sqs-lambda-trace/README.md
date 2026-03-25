# Terraform Example: SNS -> SQS Subscription -> Lambda Consumer (Traceable)

This example provisions a minimal but fully working SNS flow in Tarn:

1. Publish event to SNS topic.
2. SNS fans out to subscribed SQS queue.
3. Lambda is triggered by SQS event source mapping and processes the message.

It is designed to exercise the new SNS support and make the SNS component visible in traces/topology.

## Resources Created

- SNS topic (`aws_sns_topic`)
- SQS queue (`aws_sqs_queue`)
- Queue policy allowing SNS publishes (`aws_sqs_queue_policy`)
- SNS -> SQS subscription (`aws_sns_topic_subscription`)
- Lambda consumer (`aws_lambda_function`)
- SQS -> Lambda trigger (`aws_lambda_event_source_mapping`)

## Usage

Start Tarn:

```bash
make build
./build/tarn start --ui
```

Provision infrastructure:

```bash
cd examples/terraform/sns-sqs-lambda-trace
terraform init
terraform apply -auto-approve
```

Publish an event:

```bash
terraform output -raw publish_order_created_curl | sh
```

Inspect Lambda logs:

```bash
terraform output -raw open_lambda_logs_curl | sh
```

Inspect overview/traces payload:

```bash
terraform output -raw overview_traces_curl | sh
```

## Verify in UI

Open the dashboard and verify:

- **SNS** tab shows topic + subscription.
- **Triggers** view shows SNS wiring.
- **Topology** shows SNS connections.
- **Traces/Xray** shows topic and queue spans for published events.

## Destroy

```bash
terraform destroy -auto-approve
```
