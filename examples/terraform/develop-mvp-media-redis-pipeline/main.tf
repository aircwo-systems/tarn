terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.7.1"
    }
  }
}

variable "region" {
  description = "Emulated AWS region"
  type        = string
  default     = "us-east-1"
}

variable "endpoint" {
  description = "OpenStack API endpoint"
  type        = string
  default     = "http://localhost:4566"
}

variable "account_id" {
  description = "OpenStack emulated account ID"
  type        = string
  default     = "000000000000"
}

variable "name_prefix" {
  description = "Prefix for all resource names in this example"
  type        = string
  default     = "develop-mvp-images-redis"
}

variable "redis_url" {
  description = "Redis endpoint used by Lambda functions"
  type        = string
  default     = "redis://host.docker.internal:6379/0"
}

locals {
  shared_tags = {
    feature = "develop-mvp"
    example = "develop-mvp-images-redis"
    stack   = "terraform"
    process = "image-processing"
  }

  lambda_role_arn = "arn:aws:iam::${var.account_id}:role/lambda-exec"
}

provider "aws" {
  region                      = var.region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  s3_use_path_style           = true

  endpoints {
    apigateway     = var.endpoint
    apigatewayv2   = var.endpoint
    lambda         = var.endpoint
    sqs            = var.endpoint
    s3             = "${var.endpoint}/_s3"
    secretsmanager = var.endpoint
  }
}

resource "aws_sqs_queue" "media_jobs" {
  name = "${var.name_prefix}-jobs"

  tags = merge(local.shared_tags, {
    component = "queue"
  })
}

resource "aws_s3_bucket" "media_assets" {
  bucket        = "${var.name_prefix}-assets"
  force_destroy = true
}

resource "aws_secretsmanager_secret" "pipeline" {
  name        = "${var.name_prefix}-config"
  description = "Shared config for develop-mvp image processing workflow"

  tags = merge(local.shared_tags, {
    component = "secret"
  })
}

resource "aws_secretsmanager_secret_version" "pipeline_v1" {
  secret_id = aws_secretsmanager_secret.pipeline.id
  secret_string = jsonencode({
    mode           = "develop-mvp"
    workflow       = "image-processing"
    cacheNamespace = "image:asset"
    frontendClient = "studio-web"
  })
}

resource "aws_apigatewayv2_api" "media" {
  name          = "${var.name_prefix}-api"
  protocol_type = "HTTP"
  description   = "Frontend image-processing API for develop-mvp"

  tags = merge(local.shared_tags, {
    component = "gateway"
  })
}

data "archive_file" "status_api_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/status-api/index.js"
  output_path = "${path.module}/.build/status-api.zip"
}

resource "aws_lambda_function" "status_api" {
  function_name    = "${var.name_prefix}-status-api"
  filename         = data.archive_file.status_api_zip.output_path
  source_code_hash = data.archive_file.status_api_zip.output_base64sha256
  runtime          = "nodejs24.x"
  handler          = "index.handler"
  role             = local.lambda_role_arn

  environment {
    variables = {
      SHARED_SECRET_NAME = aws_secretsmanager_secret.pipeline.name
      ARTIFACT_BUCKET    = aws_s3_bucket.media_assets.id
      REDIS_URL          = var.redis_url
      CACHE_NAMESPACE    = "image:asset"
      OPENSTACK_ENDPOINT = var.endpoint
    }
  }

  tags = merge(local.shared_tags, {
    component = "function"
    role      = "status-api"
  })
}

data "archive_file" "queue_worker_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/queue-worker/index.js"
  output_path = "${path.module}/.build/queue-worker.zip"
}

resource "aws_lambda_function" "queue_worker" {
  function_name    = "${var.name_prefix}-queue-worker"
  filename         = data.archive_file.queue_worker_zip.output_path
  source_code_hash = data.archive_file.queue_worker_zip.output_base64sha256
  runtime          = "nodejs24.x"
  handler          = "index.handler"
  role             = local.lambda_role_arn

  environment {
    variables = {
      SHARED_SECRET_NAME = aws_secretsmanager_secret.pipeline.name
      ARTIFACT_BUCKET    = aws_s3_bucket.media_assets.id
      REDIS_URL          = var.redis_url
      CACHE_NAMESPACE    = "image:asset"
      OPENSTACK_ENDPOINT = var.endpoint
    }
  }

  tags = merge(local.shared_tags, {
    component = "function"
    role      = "queue-worker"
  })
}

data "archive_file" "s3_indexer_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/s3-indexer/index.js"
  output_path = "${path.module}/.build/s3-indexer.zip"
}

resource "aws_lambda_function" "s3_indexer" {
  function_name    = "${var.name_prefix}-s3-indexer"
  filename         = data.archive_file.s3_indexer_zip.output_path
  source_code_hash = data.archive_file.s3_indexer_zip.output_base64sha256
  runtime          = "nodejs24.x"
  handler          = "index.handler"
  role             = local.lambda_role_arn

  environment {
    variables = {
      SHARED_SECRET_NAME = aws_secretsmanager_secret.pipeline.name
      ARTIFACT_BUCKET    = aws_s3_bucket.media_assets.id
      REDIS_URL          = var.redis_url
      CACHE_NAMESPACE    = "image:asset"
      OPENSTACK_ENDPOINT = var.endpoint
    }
  }

  tags = merge(local.shared_tags, {
    component = "function"
    role      = "s3-indexer"
  })
}

resource "aws_apigatewayv2_integration" "media_enqueue" {
  api_id           = aws_apigatewayv2_api.media.id
  integration_type = "AWS"
  integration_uri  = aws_sqs_queue.media_jobs.arn

  request_parameters = {
    MessageBody = "$request.body"
  }
}

resource "aws_apigatewayv2_route" "post_media_jobs" {
  api_id    = aws_apigatewayv2_api.media.id
  route_key = "POST /images/jobs"
  target    = "integrations/${aws_apigatewayv2_integration.media_enqueue.id}"
}

resource "aws_apigatewayv2_integration" "media_status" {
  api_id                 = aws_apigatewayv2_api.media.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.status_api.arn
  payload_format_version = "2.0"
  timeout_milliseconds   = 30000
}

resource "aws_apigatewayv2_route" "get_media_job" {
  api_id    = aws_apigatewayv2_api.media.id
  route_key = "GET /images/{imageId}"
  target    = "integrations/${aws_apigatewayv2_integration.media_status.id}"
}

resource "aws_lambda_event_source_mapping" "queue_to_worker" {
  event_source_arn = aws_sqs_queue.media_jobs.arn
  function_name    = aws_lambda_function.queue_worker.arn
  batch_size       = 5
  enabled          = true
}

resource "aws_s3_bucket_notification" "media_notifications" {
  bucket = aws_s3_bucket.media_assets.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.s3_indexer.arn
    events              = ["s3:ObjectCreated:*"]
  }

  depends_on = [aws_lambda_function.s3_indexer]
}
