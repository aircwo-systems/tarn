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
  s3_use_path_style           = true

  endpoints {
    lambda = var.endpoint
    s3     = var.endpoint
  }
}

data "archive_file" "lambda_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/go-lambda.zip"
}

# S3 bucket to store Lambda deployment packages
resource "aws_s3_bucket" "lambda_artifacts" {
  bucket        = "go-lambda-artifacts"
  force_destroy = true
}

# Upload the deployment package to S3
resource "aws_s3_object" "lambda_code" {
  bucket = aws_s3_bucket.lambda_artifacts.id
  key    = "go-lambda.zip"
  source = data.archive_file.lambda_zip.output_path
  etag   = data.archive_file.lambda_zip.output_md5
}

resource "aws_lambda_function" "go_lambda" {
  function_name    = "go-lambda-handler"
  runtime          = "provided.al2023"
  handler          = "bootstrap" # Required but ignored by OS

  # Deploy from S3 rather than a local filename
  s3_bucket        = aws_s3_bucket.lambda_artifacts.id
  s3_key           = aws_s3_object.lambda_code.key
  source_code_hash = data.archive_file.lambda_zip.output_base64sha256

  role = "arn:aws:iam::123456789012:role/lambda-role"

  environment {
    variables = {
      EXAMPLE_ENV = "value"
    }
  }
}

output "lambda_function_name" {
  value = aws_lambda_function.go_lambda.function_name
}
