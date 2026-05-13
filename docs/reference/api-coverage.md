# API Coverage

Tarn implements **225+ AWS API actions** across 11 services, with service-specific compatibility fallbacks to keep Terraform and SDK workflows moving when optional APIs are probed.

## Coverage Matrix

| Service | Protocol | Actions | Terraform Endpoint |
|---------|----------|--------:|-------------------|
| **SNS** | Query/XML | 16 | `sns` |
| **SQS** | Query + JSON | 16 | `sqs` |
| **Lambda** | REST/JSON | 30 | `lambda` |
| **S3** | REST/XML | 55+ | `s3` |
| **DynamoDB** | JSON | 27 | `dynamodb` |
| **Secrets Manager** | JSON-RPC | 10 | `secretsmanager` |
| **EventBridge** | JSON | 14 | `events` |
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

## S3 Bucket Sub-Resources

Terraform's S3 provider probes many bucket sub-resources during every plan/apply. Most are now fully implemented and persisted to disk. A few remain as stubs for Terraform compatibility:

| Sub-resource | Implementation | Notes |
|---|---|---|
| `?versioning` | Full | GET/PUT persisted |
| `?encryption` | Full | GET/PUT/DELETE persisted |
| `?cors` | Full | GET/PUT/DELETE persisted |
| `?logging` | Full | GET/PUT persisted |
| `?acl` | Full | GET/PUT persisted |
| `?tagging` | Full | GET/PUT/DELETE persisted |
| `?lifecycle` | Full | GET/PUT/DELETE persisted |
| `?policy` | Full | GET/PUT/DELETE persisted |
| `?publicAccessBlock` | Full | GET/PUT/DELETE persisted |
| `?ownershipControls` | Full | GET/PUT/DELETE persisted |
| `?object-lock` | Full | GET/PUT persisted |
| `?replication` | Stub | GET → `ReplicationConfigurationNotFoundError`; PUT/DELETE accept and discard |
| `?accelerate` | Stub | GET → `Suspended`; PUT accepts and discards |
| `?request-payment` | Stub | GET → `BucketOwner`; PUT accepts and discards |

## Protocol Routing

All services share a single endpoint (`localhost:4566`). Requests are routed by:

1. **`X-Amz-Target` header** — EventBridge (`AWSEvents.*`), SQS JSON (`AmazonSQS.*`), Secrets Manager (`secretsmanager.*`), DynamoDB (`DynamoDB_20120810.*`), DynamoDB Streams (`DynamoDBStreams_20120810.*`)
2. **`Version` form parameter** — IAM (`2010-05-08`), SNS (`2010-03-31`)
3. **URL path** — Lambda (`/2015-03-31/functions/`), S3 (`/_s3/`), API Gateway (`/v2/apis/`, `/restapis/`)
4. **Fallback** — SQS query protocol (default for `POST /`)
