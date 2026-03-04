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
    apigateway   = var.endpoint
    apigatewayv2 = var.endpoint
    lambda       = var.endpoint
    sqs          = var.endpoint
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
# SQS queue that receives enqueued orders
# ──────────────────────────────────────────────
resource "aws_sqs_queue" "orders" {
  name = "orders"
}

# ──────────────────────────────────────────────
# HTTP API (v2)
# ──────────────────────────────────────────────
resource "aws_apigatewayv2_api" "orders" {
  name          = "orders-api"
  protocol_type = "HTTP"
  description   = "Orders intake API — routes POST /orders into SQS"
}

# ──────────────────────────────────────────────
# SQS integration with request parameter mapping
#
# Equivalent to the REST API v1 pattern:
#   request_templates = {
#     "application/json" = "Action=SendMessage&MessageBody=$input.body"
#   }
#
# Supported expressions evaluated by OpenStack:
#   $request.body              — full request body
#   $request.body.<field>      — top-level JSON field
#   $request.header.<name>     — request header
#   $request.querystring.<name>— query string param
#   $request.path.<name>       — route path parameter
#   'literal'                  — static string
# ──────────────────────────────────────────────
resource "aws_apigatewayv2_integration" "orders_sqs" {
  api_id           = aws_apigatewayv2_api.orders.id
  integration_type = "AWS"
  integration_uri  = aws_sqs_queue.orders.arn

  # Map the full HTTP request body as the SQS MessageBody.
  # Change to "$request.body.payload" to forward only a nested field.
  request_parameters = {
    "MessageBody" = "$request.body"
  }
}

# ──────────────────────────────────────────────
# Route: POST /orders → SQS integration
# ──────────────────────────────────────────────
resource "aws_apigatewayv2_route" "post_orders" {
  api_id    = aws_apigatewayv2_api.orders.id
  route_key = "POST /orders"
  target    = "integrations/${aws_apigatewayv2_integration.orders_sqs.id}"
}

# ──────────────────────────────────────────────
# Optional: GET /orders/{id} route as a
# reminder that path params also work in mappings
# ──────────────────────────────────────────────
resource "aws_apigatewayv2_route" "get_order" {
  api_id    = aws_apigatewayv2_api.orders.id
  route_key = "GET /orders/{orderId}"
  target    = "integrations/${aws_apigatewayv2_integration.orders_sqs.id}"
}

# ──────────────────────────────────────────────
# Lambda: order-logger
# Reads from the orders queue and logs each message body.
# ──────────────────────────────────────────────
data "archive_file" "order_logger" {
  type        = "zip"
  source_file = "${path.module}/lambda/index.js"
  output_path = "${path.module}/.build/order-logger.zip"
}

resource "aws_lambda_function" "order_logger" {
  function_name    = "order-logger"
  filename         = data.archive_file.order_logger.output_path
  source_code_hash = data.archive_file.order_logger.output_base64sha256
  handler          = "index.handler"
  runtime          = "nodejs24.x"
  # OpenStack does not enforce IAM; a placeholder ARN is sufficient.
  role = "arn:aws:iam::000000000000:role/lambda-exec"
}

# ──────────────────────────────────────────────
# Event source mapping: orders queue → order-logger
# ──────────────────────────────────────────────
resource "aws_lambda_event_source_mapping" "orders_to_logger" {
  event_source_arn = aws_sqs_queue.orders.arn
  function_name    = aws_lambda_function.order_logger.arn
  batch_size       = 5
  enabled          = true
}
