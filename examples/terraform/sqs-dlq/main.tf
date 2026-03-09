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

variable "endpoint" {
  type    = string
  default = "http://localhost:4566"
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  s3_use_path_style           = true

  endpoints {
    sqs    = var.endpoint
    lambda = var.endpoint
    s3     = var.endpoint
  }
}

# Dead-letter queue — receives messages the processor couldn't handle
resource "aws_sqs_queue" "dlq" {
  name                       = "orders-dlq"
  message_retention_seconds  = 1209600 # 14 days
}

# Main queue — after 3 failed deliveries, messages go to the DLQ
resource "aws_sqs_queue" "orders" {
  name                       = "orders"
  visibility_timeout_seconds = 30

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}

# Lambda that processes orders (intentionally fails to demo DLQ routing)
data "archive_file" "processor_zip" {
  type = "zip"
  source {
    content  = <<-JS
      exports.handler = async (event) => {
        console.log("Processing", event.Records?.length, "record(s)");
        throw new Error("simulated processing failure");
      };
    JS
    filename = "index.js"
  }
  output_path = "${path.module}/processor.zip"
}

resource "aws_s3_bucket" "artifacts" {
  bucket        = "sqs-dlq-artifacts"
  force_destroy = true
}

resource "aws_s3_object" "processor_code" {
  bucket = aws_s3_bucket.artifacts.id
  key    = "processor.zip"
  source = data.archive_file.processor_zip.output_path
  etag   = data.archive_file.processor_zip.output_md5
}

resource "aws_lambda_function" "processor" {
  function_name    = "order-processor"
  runtime          = "nodejs20.x"
  handler          = "index.handler"
  role             = "arn:aws:iam::000000000000:role/lambda-role"
  s3_bucket        = aws_s3_bucket.artifacts.id
  s3_key           = aws_s3_object.processor_code.key
  source_code_hash = data.archive_file.processor_zip.output_base64sha256
  timeout          = 10

  dead_letter_config {
    target_arn = aws_sqs_queue.dlq.arn
  }
}

resource "aws_lambda_event_source_mapping" "orders_to_processor" {
  event_source_arn = aws_sqs_queue.orders.arn
  function_name    = aws_lambda_function.processor.arn
  batch_size       = 1
  enabled          = true
}

output "orders_queue_url" {
  value = aws_sqs_queue.orders.url
}

output "dlq_url" {
  value = aws_sqs_queue.dlq.url
}
