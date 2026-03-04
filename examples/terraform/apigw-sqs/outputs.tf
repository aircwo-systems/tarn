output "api_id" {
  description = "API Gateway HTTP API ID"
  value       = aws_apigatewayv2_api.orders.id
}

output "api_endpoint" {
  description = "Base URL for the $default stage"
  value       = "${var.endpoint}/_apigateway/${aws_apigatewayv2_api.orders.id}/$default"
}

output "queue_url" {
  description = "SQS queue URL"
  value       = aws_sqs_queue.orders.url
}

output "queue_arn" {
  description = "SQS queue ARN"
  value       = aws_sqs_queue.orders.arn
}

output "post_orders_curl" {
  description = "Example curl command to enqueue an order"
  value       = "curl -s -X POST ${var.endpoint}/_apigateway/${aws_apigatewayv2_api.orders.id}/$default/orders -H 'Content-Type: application/json' -d '{\"orderId\":\"abc-123\",\"amount\":49.99}'"
}

output "lambda_function_name" {
  description = "Name of the order-logger Lambda function"
  value       = aws_lambda_function.order_logger.function_name
}

output "lambda_logs_curl" {
  description = "Fetch Lambda log events from the OpenStack dashboard API"
  value       = "curl -s ${var.endpoint}/_openstack/admin/logs/groups/%2Faws%2Flambda%2Forder-logger/events | jq ."
}
