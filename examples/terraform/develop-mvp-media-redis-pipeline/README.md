# Terraform Example: `develop-mvp` Image Pipeline with Redis + S3

This example provisions a frontend/API-driven image workflow that uses the full OpenStack stack in one slice:

- API Gateway HTTP API
- SQS queue
- Lambda functions
- S3 bucket + S3 trigger
- Secrets Manager secret
- Redis cache/status store

## Use Case

A content studio frontend uploads source images and submits processing jobs:

1. Frontend uploads source image `uploads/img-42.svg` to S3.
2. Frontend sends `POST /images/jobs`.
3. API Gateway enqueues the job to SQS.
4. Queue worker Lambda consumes the message, generates image renditions (`web.svg`, `thumb.svg`), writes them to S3, and updates Redis status.
5. S3 object-created events invoke the indexer Lambda.
6. Indexer Lambda updates Redis indexing metadata.
7. Frontend polls `GET /images/{imageId}` for status and rendition pointers.

All taggable resources are tagged with:

- `feature=develop-mvp`
- `example=develop-mvp-images-redis`

Note: S3 bucket tag APIs are not fully emulated yet, so bucket scoping is done via the `develop-mvp-images-redis-...` name prefix.

## Prerequisites

1. OpenStack running locally:

```bash
make build
./build/openstack start --ui
```

2. Redis running on host port `6379`:

```bash
docker run --name openstack-redis -p 6379:6379 -d redis:7-alpine
```

Default Lambda Redis endpoint: `redis://host.docker.internal:6379/0`.

## Provision

```bash
cd examples/terraform/develop-mvp-media-redis-pipeline
terraform init
terraform apply -auto-approve
```

## Drive the Image Flow

Upload sample source image:

```bash
terraform output -raw upload_source_image_curl | sh
```

Submit image processing job (API Gateway -> SQS):

```bash
terraform output -raw post_image_job_curl | sh
```

Check image status (API Gateway -> Lambda -> Redis):

```bash
terraform output -raw get_image_status_curl | sh
```

List objects (source + generated renditions + manifest):

```bash
terraform output -raw list_image_objects_curl | sh
```

Fetch generated web rendition SVG:

```bash
terraform output -raw fetch_generated_web_image_curl | sh
```

Inspect Redis keys:

```bash
terraform output -raw redis_status_command | sh
terraform output -raw redis_indexed_command | sh
terraform output -raw redis_manifest_command | sh
```

Inspect Lambda logs:

```bash
curl -s "http://localhost:4566/_openstack/admin/logs/events/%2Faws%2Flambda%2Fdevelop-mvp-images-redis-queue-worker?limit=50"
curl -s "http://localhost:4566/_openstack/admin/logs/events/%2Faws%2Flambda%2Fdevelop-mvp-images-redis-s3-indexer?limit=50"
curl -s "http://localhost:4566/_openstack/admin/logs/events/%2Faws%2Flambda%2Fdevelop-mvp-images-redis-status-api?limit=50"
```

In the OpenStack UI:

- Filter by `feature:develop-mvp`
- Verify API -> queue -> worker -> bucket trigger -> indexer edges
- Verify Lambda -> Redis connection evidence from `REDIS_URL`
- Check bucket objects and generated `images/img-42/...` files

## Destroy

```bash
terraform destroy -auto-approve
```
