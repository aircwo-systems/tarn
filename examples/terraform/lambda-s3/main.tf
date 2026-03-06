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
  description = "OpenStack API endpoint"
  type        = string
  default     = "http://localhost:4566"
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
    lambda = var.endpoint
    s3     = var.endpoint
  }
}

data "archive_file" "lambda_zip" {
  type = "zip"
  source {
    content  = <<-JS
      exports.handler = async () => ({
        statusCode: 200,
        body: `hello, lambda running from s3 ($${process.env.AWS_LAMBDA_FUNCTION_VERSION})`,
      });
    JS
    filename = "index.js"
  }
  output_path = "${path.module}/lambda.zip"
}

resource "aws_s3_bucket" "artifacts" {
  bucket        = "lambda-s3-artifacts"
  force_destroy = true
}

resource "aws_s3_object" "lambda_code" {
  bucket = aws_s3_bucket.artifacts.id
  key    = "lambda.zip"
  source = data.archive_file.lambda_zip.output_path
  etag   = data.archive_file.lambda_zip.output_md5
}

resource "aws_lambda_function" "hello" {
  function_name    = "hello-from-s3"
  runtime          = "nodejs24.x"
  handler          = "index.handler"
  role             = "arn:aws:iam::000000000000:role/lambda-role"
  s3_bucket        = aws_s3_bucket.artifacts.id
  s3_key           = aws_s3_object.lambda_code.key
  source_code_hash = data.archive_file.lambda_zip.output_base64sha256

  environment {
    variables = {
      AWS_LAMBDA_FUNCTION_VERSION = "1-test"
    }
  }
}

output "function_name" {
  value = aws_lambda_function.hello.function_name
}
