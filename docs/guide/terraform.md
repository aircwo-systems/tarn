# Terraform

Tarn works with real Terraform configurations for the services it currently supports. Point the AWS provider at `localhost:4566` and apply, but expect some service gaps and compatibility stubs while coverage continues to improve.

## Provider Configuration

```hcl
provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  s3_use_path_style           = true

  endpoints {
    apigateway     = "http://localhost:4566"
    apigatewayv2   = "http://localhost:4566"
    dynamodb       = "http://localhost:4566"
    events         = "http://localhost:4566"
    lambda         = "http://localhost:4566"
    s3             = "http://localhost:4566/_s3"
    secretsmanager = "http://localhost:4566"
    sns            = "http://localhost:4566"
    sqs            = "http://localhost:4566"
  }
}
```

::: info S3 Endpoint
S3 requires the `/_s3` path prefix. All other services use the root endpoint. You must also set `s3_use_path_style = true` since Tarn only supports path-style S3 URLs.
:::

<div class="tf-grid">
  <div class="tf-grid-item">
    <div class="label">Default Endpoint</div>
    <div class="value">http://localhost:4566</div>
  </div>
  <div class="tf-grid-item">
    <div class="label">Provider Compatibility</div>
    <div class="value">AWS Provider v5 &amp; v6</div>
  </div>
</div>

## Production Compatibility

Tarn includes compatibility stubs for a number of Terraform-driven control-plane actions, especially around IAM, SNS, SQS, DynamoDB, Secrets Manager, and EventBridge. This helps production-shaped `.tf` files apply with fewer local-only changes.

Check server logs for `[service] unhandled action (returning empty OK)` to see which stubs were hit during an apply.

::: warning Stub Behavior
Stubbed actions are compatibility shims, not full implementations. They may succeed without persisting full state, and some APIs still return explicit "not configured" or unsupported responses depending on the service. Tarn aims to keep Terraform flows moving, but not every AWS feature is fully emulated yet.
:::

## Endpoint Mapping

| Terraform Key | Service | Endpoint |
|---|---|---|
| `lambda` | Lambda | `http://localhost:4566` |
| `dynamodb` | DynamoDB | `http://localhost:4566` |
| `s3` | S3 | `http://localhost:4566/_s3` |
| `sqs` | SQS | `http://localhost:4566` |
| `sns` | SNS | `http://localhost:4566` |
| `secretsmanager` | Secrets Manager | `http://localhost:4566` |
| `events` | EventBridge | `http://localhost:4566` |
| `apigatewayv2` | API Gateway v2 | `http://localhost:4566` |
| `apigateway` | API Gateway v1 | `http://localhost:4566` |

IAM does not require an endpoint override — Tarn auto-stubs IAM actions via protocol detection (`Version=2010-05-08`).

## Examples

A small set of curated Terraform examples still lives in this repository under `examples/terraform/`.

Useful starting points:

- `examples/terraform/dynamodb-stream-lambda` — DynamoDB table with streams wired to Lambda
- `examples/terraform/eventbridge-scheduled-lambda` — scheduled EventBridge rule targeting Lambda
- `examples/terraform/apigw-sqs-fanout-secrets-s3-dlq` — larger multi-service integration flow

See the [project roadmap](https://github.com/aircwo-systems/tarn/blob/develop-docs/ROADMAP.md) for planned Terraform compatibility work and SDK testing coverage.
