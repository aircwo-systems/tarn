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
  description = "Tarn endpoint URL"
  type        = string
  default     = "http://localhost:4566"
}

variable "account_id" {
  description = "Emulated AWS account ID"
  type        = string
  default     = "000000000000"
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "sns-trace-demo"
}

locals {
  tags = {
    feature = "sns-trace-demo"
    example = "sns-sqs-lambda"
  }
}

provider "aws" {
  region                      = var.region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    lambda = var.endpoint
    sns    = var.endpoint
    sqs    = var.endpoint
  }
}

resource "aws_sns_topic" "events" {
  name = "${var.name_prefix}-events"
  tags = local.tags
}

resource "aws_sqs_queue" "worker" {
  name = "${var.name_prefix}-queue"
  tags = local.tags
}

resource "aws_sqs_queue_policy" "allow_sns_publish" {
  queue_url = aws_sqs_queue.worker.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AllowSNSPublish"
        Effect    = "Allow"
        Principal = { Service = "sns.amazonaws.com" }
        Action    = "sqs:SendMessage"
        Resource  = aws_sqs_queue.worker.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_sns_topic.events.arn
          }
        }
      }
    ]
  })
}

resource "aws_sns_topic_subscription" "queue" {
  topic_arn            = aws_sns_topic.events.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.worker.arn
  raw_message_delivery = true

  depends_on = [aws_sqs_queue_policy.allow_sns_publish]
}

data "archive_file" "worker_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/index.js"
  output_path = "${path.module}/.build/sns-worker.zip"
}

resource "aws_lambda_function" "worker" {
  function_name    = "${var.name_prefix}-worker"
  filename         = data.archive_file.worker_zip.output_path
  source_code_hash = data.archive_file.worker_zip.output_base64sha256
  handler          = "index.handler"
  runtime          = "nodejs24.x"
  role             = "arn:aws:iam::${var.account_id}:role/lambda-exec"
  tags             = local.tags
}

resource "aws_lambda_event_source_mapping" "queue_to_worker" {
  event_source_arn = aws_sqs_queue.worker.arn
  function_name    = aws_lambda_function.worker.arn
  batch_size       = 5
  enabled          = true
}
