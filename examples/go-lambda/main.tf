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

provider "aws" {
  region                      = var.region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    lambda = var.endpoint
  }
}

data "archive_file" "lambda_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/go-lambda.zip"
}

resource "aws_lambda_function" "go_lambda" {
  function_name    = "go-lambda-handler"
  runtime          = "provided.al2023"
  handler          = "bootstrap" # Required but ignored by OS
  
  # Point to the archive_file output
  filename         = data.archive_file.lambda_zip.output_path
  source_code_hash = data.archive_file.lambda_zip.output_base64sha256

  role             = "arn:aws:iam::123456789012:role/lambda-role"
  
  environment {
    variables = {
      EXAMPLE_ENV = "value"
    }
  }
}

output "lambda_function_name" {
  value = aws_lambda_function.go_lambda.function_name
}
