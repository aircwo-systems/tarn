output "feature_tag" {
  description = "Tag used to filter this stack in the Tarn UI"
  value       = "feature:develop-mvp"
}

output "api_id" {
  description = "API Gateway HTTP API ID"
  value       = aws_apigatewayv2_api.content.id
}

output "api_invoke_base" {
  description = "Invoke base URL for the $default stage"
  value       = "${var.endpoint}/_apigateway/${aws_apigatewayv2_api.content.id}/$default"
}

output "post_campaign_curl" {
  description = "Send a frontend campaign request into API Gateway -> SQS"
  value       = "curl -s -X POST ${var.endpoint}/_apigateway/${aws_apigatewayv2_api.content.id}/$default/campaigns -H 'Content-Type: application/json' -d '{\"campaignId\":\"cmp-42\",\"channel\":\"web\",\"assetKey\":\"incoming/cmp-42.json\"}'"
}

output "get_campaign_status_curl" {
  description = "Check campaign status route (API Gateway -> Lambda) with visible status/body output"
  value       = "curl -sS -i ${var.endpoint}/_apigateway/${aws_apigatewayv2_api.content.id}/$default/campaigns/cmp-42; echo"
}

output "upload_artifact_curl" {
  description = "Put an object in S3 to trigger the S3 listener Lambda"
  value       = "curl -s -X PUT ${var.endpoint}/_s3/${aws_s3_bucket.artifacts.id}/incoming/cmp-42.json -H 'Content-Type: application/json' -d '{\"campaignId\":\"cmp-42\",\"status\":\"uploaded\"}'"
}

output "queue_url" {
  description = "SQS queue URL"
  value       = aws_sqs_queue.review_jobs.url
}

output "bucket_name" {
  description = "S3 artifacts bucket"
  value       = aws_s3_bucket.artifacts.id
}

output "secret_name" {
  description = "Secrets Manager config secret"
  value       = aws_secretsmanager_secret.pipeline.name
}

output "lambda_functions" {
  description = "Lambda functions in this example"
  value = [
    aws_lambda_function.ingest_status.function_name,
    aws_lambda_function.queue_worker.function_name,
    aws_lambda_function.s3_listener.function_name,
  ]
}
