output "sns_topic_arn" {
  value       = aws_sns_topic.events.arn
  description = "SNS topic ARN for publishing test events."
}

output "sqs_queue_url" {
  value       = aws_sqs_queue.worker.url
  description = "SQS queue URL subscribed to the SNS topic."
}

output "lambda_function_name" {
  value       = aws_lambda_function.worker.function_name
  description = "Lambda consumer function name."
}

output "publish_order_created_curl" {
  description = "Publish a sample event to SNS using the Query API (curl)."
  value       = "curl -sS -X POST ${var.endpoint}/ -d Action=Publish -d TopicArn=${aws_sns_topic.events.arn} -d Message=${urlencode(jsonencode({ id = "evt-1001", type = "order.created", source = "demo", body = "sns->sqs->lambda" }))}"
}

output "open_lambda_logs_curl" {
  description = "Inspect Lambda logs from the OpenStack admin endpoint."
  value       = "curl -sS ${var.endpoint}/_openstack/admin/logs/events/%2Faws%2Flambda%2F${aws_lambda_function.worker.function_name}?limit=50"
}

output "overview_traces_curl" {
  description = "Inspect recent traces; spans should include topic + queue after publish."
  value       = "curl -sS ${var.endpoint}/_openstack/admin/overview"
}
