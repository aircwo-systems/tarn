# Terraform Example: `develop-mvp` Content Review Pipeline

This example provisions a non-orders workflow where a frontend request enters API Gateway and fans into queue-based processing.

Flow:
- Frontend `POST /campaigns` -> API Gateway HTTP API
- API Gateway integration -> SQS queue
- SQS event source mapping -> queue worker Lambda
- S3 object uploads -> S3 notification trigger -> S3 listener Lambda
- Shared configuration stored in Secrets Manager

All taggable resources are tagged with:
- `feature=develop-mvp`
- `example=develop-mvp`

Note: S3 bucket tag APIs are not fully emulated yet, so the bucket is name-scoped with the `develop-mvp-content-...` prefix instead of tag-filtered.

## Resources Created

- API Gateway HTTP API
- 3 Lambda functions (`ingest-status`, `queue-worker`, `s3-listener`)
- 1 SQS queue
- 1 S3 bucket
- 1 Secrets Manager secret (+ initial version)
- 1 Lambda event source mapping (SQS -> Lambda trigger)
- 1 S3 bucket notification (S3 -> Lambda trigger)

## End-to-End Data Flow

This example has two ingress paths that converge on the same feature slice:

1. Frontend sends `POST /campaigns` to the API Gateway `$default` stage.
2. API Gateway integration (`AWS`) forwards request body to SQS queue `develop-mvp-content-jobs`.
3. Event source mapping polls the queue and invokes Lambda `develop-mvp-content-queue-worker`.
4. Worker logs each SQS record payload and can use shared env config (`SHARED_SECRET_NAME`, `ARTIFACT_BUCKET`).
5. Frontend (or operator) can call `GET /campaigns/{campaignId}` on the same API.
6. API Gateway `AWS_PROXY` integration invokes Lambda `develop-mvp-content-ingest-status`, which returns campaign status JSON.
7. When an object is uploaded to bucket `develop-mvp-content-artifacts`, S3 emits `s3:ObjectCreated:Put`.
8. Bucket notification rule (`s3:ObjectCreated:*`) matches that event and invokes Lambda `develop-mvp-content-s3-listener`.
9. S3 listener logs bucket/key event details for the uploaded artifact.

## Verify the Flow

Run the outputs in this order:

```bash
terraform output -raw post_campaign_curl | sh
terraform output -raw get_campaign_status_curl | sh
terraform output -raw upload_artifact_curl | sh
```

Then inspect logs:

```bash
curl -s "http://localhost:4566/_tarn/admin/logs/events/%2Faws%2Flambda%2Fdevelop-mvp-content-queue-worker?limit=50"
curl -s "http://localhost:4566/_tarn/admin/logs/events/%2Faws%2Flambda%2Fdevelop-mvp-content-ingest-status?limit=50"
curl -s "http://localhost:4566/_tarn/admin/logs/events/%2Faws%2Flambda%2Fdevelop-mvp-content-s3-listener?limit=50"
```

## Usage

Start Tarn:

```bash
make build
./build/tarn start --ui
```

Provision:

```bash
cd examples/terraform/develop-mvp-content-pipeline
terraform init
terraform apply -auto-approve
```

Send a frontend-style request into the pipeline:

```bash
terraform output -raw post_campaign_curl | sh
```

Hit the API -> Lambda status route:

```bash
terraform output -raw get_campaign_status_curl | sh
```

Trigger the S3 -> Lambda path:

```bash
terraform output -raw upload_artifact_curl | sh
```

In the Tarn UI:
- Filter by `feature:develop-mvp`
- Check the canvas and triggers view for API, queue trigger, and S3 trigger wiring

Destroy:

```bash
terraform destroy -auto-approve
```
