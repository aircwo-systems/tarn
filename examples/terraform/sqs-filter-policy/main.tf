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

provider "aws" {
  region                      = var.region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    lambda = var.endpoint
    sqs    = var.endpoint
  }
}

variable "region" {
  description = "Emulated AWS region"
  default     = "us-east-1"
}

variable "endpoint" {
  description = "OpenStack endpoint URL"
  default     = "http://localhost:4566"
}

# ──────────────────────────────────────────────
# Single shared SQS queue
# Messages carry a "type" field in their body:
#   { "type": "order",   ... }
#   { "type": "payment", ... }
# ──────────────────────────────────────────────
resource "aws_sqs_queue" "events" {
  name = "events"
}

# ──────────────────────────────────────────────
# Lambda: order-processor
# Only receives messages where body.type = "order"
# ──────────────────────────────────────────────
data "archive_file" "order_processor" {
  type        = "zip"
  source_file = "${path.module}/lambda/order-processor/index.js"
  output_path = "${path.module}/.build/order-processor.zip"
}

resource "aws_lambda_function" "order_processor" {
  function_name    = "order-processor"
  filename         = data.archive_file.order_processor.output_path
  source_code_hash = data.archive_file.order_processor.output_base64sha256
  handler          = "index.handler"
  runtime          = "nodejs24.x"
  role             = "arn:aws:iam::000000000000:role/lambda-exec"
}

resource "aws_lambda_event_source_mapping" "orders" {
  event_source_arn = aws_sqs_queue.events.arn
  function_name    = aws_lambda_function.order_processor.arn
  batch_size       = 5
  enabled          = true

  filter_criteria {
    filter {
      pattern = jsonencode({ body = { type = ["order"] } })
    }
  }
}

# ──────────────────────────────────────────────
# Lambda: payment-processor
# Only receives messages where body.type = "payment"
# ──────────────────────────────────────────────
data "archive_file" "payment_processor" {
  type        = "zip"
  source_file = "${path.module}/lambda/payment-processor/index.js"
  output_path = "${path.module}/.build/payment-processor.zip"
}

resource "aws_lambda_function" "payment_processor" {
  function_name    = "payment-processor"
  filename         = data.archive_file.payment_processor.output_path
  source_code_hash = data.archive_file.payment_processor.output_base64sha256
  handler          = "index.handler"
  runtime          = "nodejs24.x"
  role             = "arn:aws:iam::000000000000:role/lambda-exec"
}

resource "aws_lambda_event_source_mapping" "payments" {
  event_source_arn = aws_sqs_queue.events.arn
  function_name    = aws_lambda_function.payment_processor.arn
  batch_size       = 5
  enabled          = true

  filter_criteria {
    filter {
      pattern = jsonencode({ body = { type = ["payment"] } })
    }
  }
}
