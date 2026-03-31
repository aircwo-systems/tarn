# API Coverage

Tarn implements **180+ AWS API actions** across 11 services, with service-specific compatibility fallbacks to keep Terraform and SDK workflows moving when optional APIs are probed.

## Coverage Matrix

| Service | Protocol | Actions | Terraform Endpoint |
|---------|----------|--------:|-------------------|
| **SNS** | Query/XML | 16 | `sns` |
| **SQS** | Query + JSON | 16 | `sqs` |
| **Lambda** | REST/JSON | 30 | `lambda` |
| **S3** | REST/XML | 30+ | `s3` |
| **DynamoDB** | JSON | 15 | `dynamodb` |
| **Secrets Manager** | JSON-RPC | 10 | `secretsmanager` |
| **EventBridge** | JSON | 13 | `events` |
| **API Gateway v2** | REST/JSON | 18 | `apigatewayv2` |
| **API Gateway v1** | REST/JSON | 19 | `apigateway` |
| **IAM** | Query/XML | 17 | default |
| **Event Source Mapping** | REST/JSON | 5 | `lambda` (shared) |

## Stub Behavior

When Tarn receives an API action it doesn't explicitly implement, the fallback depends on the service protocol. Query and JSON-style services usually return an empty success response for Terraform compatibility, while Lambda and S3 use AWS-style "not configured" responses for unsupported sub-resources.

**Per-protocol stub format:**

| Protocol | Stub Response |
|----------|--------------|
| Query/XML (SNS, SQS, IAM) | `200 OK` with `<{Action}Response><{Action}Result/></...>` |
| JSON-RPC (EventBridge, Secrets) | `200 OK` with `{}` |
| JSON (DynamoDB, DynamoDB Streams) | `200 OK` with `{}` |
| JSON Wire (SQS v2) | `200 OK` with `{}` |
| REST/JSON (Lambda sub-resources) | `404` with `ResourceNotFoundException` |
| REST/XML (S3) | Specific error codes per sub-resource (e.g. `NoSuchCORSConfiguration`) |

All stubbed actions are logged: `[service] unhandled action (returning empty OK): ActionName`

## S3 Sub-Resource Stubs

Terraform's S3 provider probes many bucket sub-resources during every plan/apply. Tarn returns appropriate "not configured" responses:

| Sub-resource | Status | Response |
|---|---|---|
| `?versioning` | 200 | Empty `<VersioningConfiguration/>` |
| `?encryption` | 404 | `ServerSideEncryptionConfigurationNotFoundError` |
| `?cors` | 404 | `NoSuchCORSConfiguration` |
| `?logging` | 200 | Empty `<BucketLoggingStatus/>` |
| `?acl` | 200 | Default private ACL |
| `?replication` | 404 | `ReplicationConfigurationNotFoundError` |
| `?accelerate` | 200 | `Suspended` |
| `?request-payment` | 200 | `BucketOwner` |
| `?object-lock` | 404 | `ObjectLockConfigurationNotFoundError` |
| `?tagging` | 404 | `NoSuchTagSet` |
| `?lifecycle` | 404 | `NoSuchLifecycleConfiguration` |

## Protocol Routing

All services share a single endpoint (`localhost:4566`). Requests are routed by:

1. **`X-Amz-Target` header** — EventBridge (`AWSEvents.*`), SQS JSON (`AmazonSQS.*`), Secrets Manager (`secretsmanager.*`), DynamoDB (`DynamoDB_20120810.*`), DynamoDB Streams (`DynamoDBStreams_20120810.*`)
2. **`Version` form parameter** — IAM (`2010-05-08`), SNS (`2010-03-31`)
3. **URL path** — Lambda (`/2015-03-31/functions/`), S3 (`/_s3/`), API Gateway (`/v2/apis/`, `/restapis/`)
4. **Fallback** — SQS query protocol (default for `POST /`)
