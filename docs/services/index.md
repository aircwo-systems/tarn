# Services Overview

Tarn emulates the following AWS services for local development and testing.

## Supported Services

| Service | Status | Details |
|---------|--------|---------|
| **Lambda** | <span class="status-badge status-partial">Partial</span> | Core Lambda APIs, layers, and SQS/DynamoDB Streams event source mappings |
| **API Gateway v2** | <span class="status-badge status-partial">Partial</span> | HTTP APIs with Lambda and SQS integrations |
| **API Gateway v1** | <span class="status-badge status-partial">Partial</span> | REST APIs with Lambda proxy and SQS integrations |
| **S3** | <span class="status-badge status-partial">Partial</span> | Path-style operations, multipart upload, Lambda notifications |
| **SQS** | <span class="status-badge status-full">Full</span> | Queues, event source mappings, DLQ |
| **SNS** | <span class="status-badge status-full">Full</span> | Topics, subscriptions, fanout to SQS/Lambda |
| **DynamoDB** | <span class="status-badge status-partial">Partial</span> | Core table APIs, Query/Scan, secondary indexes, Streams |
| **Secrets Manager** | <span class="status-badge status-full">Full</span> | CRUD, tagging, compatibility policy reads, Lambda extension |
| **EventBridge** | <span class="status-badge status-partial">Partial</span> | Scheduled rules, event-pattern rules, and `PutEvents` with Lambda targets |

## Service Guides

Each service has detailed documentation with:
- **Supported Operations** — Exactly what's implemented
- **Operation Table** — Which API calls work
- **Code Examples** — AWS SDK usage
- **Known Limitations** — What's not yet supported
- **See Also** — Related resources

Pick a service below to get started:

<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px; margin-top: 24px;">

<a href="/services/lambda" class="service-card">
  <h3>Lambda</h3>
  <p>Deploy and invoke functions. Containers, layers, event mappings.</p>
  <small>Partial Support</small>
</a>

<a href="/services/api-gateway" class="service-card">
  <h3>API Gateway</h3>
  <p>HTTP and REST APIs. Lambda and SQS integrations, routes, stages.</p>
  <small>Partial Support</small>
</a>

<a href="/services/s3" class="service-card">
  <h3>S3</h3>
  <p>Object storage. CRUD, multipart upload, Lambda notifications.</p>
  <small>Partial Support</small>
</a>

<a href="/services/sqs" class="service-card">
  <h3>SQS</h3>
  <p>Message queues. Event source mappings to Lambda.</p>
  <small>Fully Supported</small>
</a>

<a href="/services/sns" class="service-card">
  <h3>SNS</h3>
  <p>Pub/sub messaging. Fanout to SQS and Lambda.</p>
  <small>Fully Supported</small>
</a>

<a href="/services/dynamodb" class="service-card">
  <h3>DynamoDB</h3>
  <p>Tables, indexes, item CRUD, Query/Scan, and Streams.</p>
  <small>Partial Support</small>
</a>

<a href="/services/secrets-manager" class="service-card">
  <h3>Secrets Manager</h3>
  <p>Secrets storage. Tagging, compatibility policy reads, Lambda extension.</p>
  <small>Fully Supported</small>
</a>

<a href="/services/eventbridge" class="service-card">
  <h3>EventBridge</h3>
  <p>Scheduled rules and custom events. Rate/cron, event patterns, and Lambda targets.</p>
  <small>Partial Support</small>
</a>

</div>
