# Tarn Docs

Documentation site built with [VitePress](https://vitepress.dev/).

## Structure

```
docs/
├── index.md                    # Landing page
├── guide/
│   ├── getting-started.md      # Quick start
│   ├── installation.md         # Install methods
│   ├── configuration.md        # Flags and env vars
│   ├── terraform.md            # Terraform provider setup
│   ├── development.md          # Building from source
│   └── contributing.md         # Contribution guide
├── services/
│   ├── index.md                # Services overview
│   ├── lambda.md               # Lambda
│   ├── api-gateway.md          # API Gateway v1/v2
│   ├── s3.md                   # S3
│   ├── sqs.md                  # SQS
│   ├── sns.md                  # SNS
│   ├── secrets-manager.md      # Secrets Manager
│   ├── eventbridge.md          # EventBridge
│   └── iam.md                  # IAM stubs
└── reference/
    └── api-coverage.md         # API coverage matrix
```

## Development

```bash
cd docs
bun install
bun run docs:dev
```

Then open `http://localhost:5173`.
