output "feature_tag" {
  description = "Tag used to filter this stack in the Tarn UI"
  value       = "feature:develop-mvp"
}

output "api_id" {
  description = "API Gateway HTTP API ID"
  value       = aws_apigatewayv2_api.media.id
}

output "api_invoke_base" {
  description = "Invoke base URL for the $default stage"
  value       = "${var.endpoint}/_apigateway/${aws_apigatewayv2_api.media.id}/$default"
}

output "upload_source_image_curl" {
  description = "Upload a sample source SVG image to S3"
  value       = "curl -s -X PUT ${var.endpoint}/_s3/${aws_s3_bucket.media_assets.id}/uploads/img-42.svg -H 'Content-Type: image/svg+xml' --data-binary '<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 320 180\"><defs><linearGradient id=\"g\" x1=\"0\" y1=\"0\" x2=\"1\" y2=\"1\"><stop offset=\"0%\" stop-color=\"#0ea5e9\"/><stop offset=\"100%\" stop-color=\"#1d4ed8\"/></linearGradient></defs><rect width=\"320\" height=\"180\" fill=\"url(#g)\"/><text x=\"18\" y=\"95\" font-size=\"28\" fill=\"white\" font-family=\"Arial\">img-42 hero</text></svg>'"
}

output "post_image_job_curl" {
  description = "Send an image processing request into API Gateway -> SQS"
  value       = "curl -s -X POST ${var.endpoint}/_apigateway/${aws_apigatewayv2_api.media.id}/$default/images/jobs -H 'Content-Type: application/json' -d '{\"imageId\":\"img-42\",\"sourceKey\":\"uploads/img-42.svg\",\"title\":\"Hero Banner\",\"requestedBy\":\"studio-ui\"}'"
}

output "get_image_status_curl" {
  description = "Check image processing status route (API Gateway -> Lambda -> Redis)"
  value       = "curl -sS -i ${var.endpoint}/_apigateway/${aws_apigatewayv2_api.media.id}/$default/images/img-42; echo"
}

output "list_image_objects_curl" {
  description = "List uploaded and generated image objects in the bucket"
  value       = "curl -s ${var.endpoint}/_s3/${aws_s3_bucket.media_assets.id}"
}

output "fetch_generated_web_image_curl" {
  description = "Fetch the generated web rendition SVG"
  value       = "curl -s ${var.endpoint}/_s3/${aws_s3_bucket.media_assets.id}/images/img-42/renditions/web.svg"
}

output "redis_status_command" {
  description = "Inspect Redis status key for img-42 from the host machine"
  value       = "redis-cli -u ${replace(var.redis_url, "host.docker.internal", "localhost")} GET image:asset:img-42:status"
}

output "redis_indexed_command" {
  description = "Inspect Redis indexed metadata written by the S3 indexer"
  value       = "redis-cli -u ${replace(var.redis_url, "host.docker.internal", "localhost")} GET image:asset:img-42:indexed"
}

output "redis_manifest_command" {
  description = "Inspect Redis manifest pointer set by the queue worker"
  value       = "redis-cli -u ${replace(var.redis_url, "host.docker.internal", "localhost")} GET image:asset:img-42:manifest"
}

output "queue_url" {
  description = "SQS queue URL"
  value       = aws_sqs_queue.media_jobs.url
}

output "bucket_name" {
  description = "S3 image assets bucket"
  value       = aws_s3_bucket.media_assets.id
}

output "secret_name" {
  description = "Secrets Manager config secret"
  value       = aws_secretsmanager_secret.pipeline.name
}

output "lambda_functions" {
  description = "Lambda functions in this example"
  value = [
    aws_lambda_function.status_api.function_name,
    aws_lambda_function.queue_worker.function_name,
    aws_lambda_function.s3_indexer.function_name,
  ]
}
