# DynamoDB

Managed NoSQL tables with DynamoDB Streams support.

<span class="status-badge status-partial">Partial Support</span> — Core table APIs, Query/Scan, secondary indexes, and DynamoDB Streams

## Supported Operations

| Operation | Status | Notes |
|-----------|--------|-------|
| CreateTable | Supported | Tables become `ACTIVE` immediately |
| DeleteTable | Supported | Tables are removed immediately after returning a `DELETING` description |
| DescribeTable | Supported | Includes key schema, indexes, and stream metadata |
| ListTables | Supported | |
| UpdateTable | Supported | Stream configuration updates are applied immediately |
| TagResource | Supported | Table tags are persisted and queryable |
| UntagResource | Supported | |
| ListTagsOfResource | Supported | |
| PutItem | Supported | Conditional writes and return values |
| GetItem | Supported | `ConsistentRead` accepted as a no-op |
| UpdateItem | Supported | `SET`, `REMOVE`, `ADD`, `DELETE` update clauses |
| DeleteItem | Supported | Conditional delete and return values |
| Scan | Supported | Filters, projection, pagination, index scan |
| Query | Supported | Table, LSI, and GSI query paths |
| DescribeTimeToLive | Supported | Compatibility response only |
| UpdateTimeToLive | Supported | Compatibility response only |
| DescribeContinuousBackups | Supported | Compatibility response only |
| UpdateContinuousBackups | Supported | Compatibility response only |
| DescribeContributorInsights | Supported | Compatibility response only |
| UpdateContributorInsights | Supported | Compatibility response only |
| DescribeKinesisStreamingDestination | Supported | Compatibility response only |
| EnableKinesisStreamingDestination | Supported | Compatibility response only |
| DisableKinesisStreamingDestination | Supported | Compatibility response only |
| ListStreams | Supported | One logical shard per stream |
| DescribeStream | Supported | Stream ARN and shard metadata |
| GetShardIterator | Supported | `TRIM_HORIZON`, `LATEST`, `AT_SEQUENCE_NUMBER`, `AFTER_SEQUENCE_NUMBER` |
| GetRecords | Supported | AWS-shaped DynamoDB stream records |

## Features

### Secondary Indexes
Local and global secondary indexes declared at table creation are queryable immediately.

```bash
awslocal dynamodb create-table \
  --table-name orders \
  --attribute-definitions \
    AttributeName=pk,AttributeType=S \
    AttributeName=sk,AttributeType=S \
    AttributeName=status,AttributeType=S \
  --key-schema \
    AttributeName=pk,KeyType=HASH \
    AttributeName=sk,KeyType=RANGE \
  --global-secondary-indexes \
    'IndexName=StatusIndex,KeySchema=[{AttributeName=status,KeyType=HASH},{AttributeName=sk,KeyType=RANGE}],Projection={ProjectionType=ALL}'
```

### Streams
Enable streams when the table is created, then connect the stream ARN to Lambda event source mappings.

```bash
awslocal dynamodb create-table \
  --table-name orders \
  --attribute-definitions AttributeName=pk,AttributeType=S \
  --key-schema AttributeName=pk,KeyType=HASH \
  --stream-specification StreamEnabled=true,StreamViewType=NEW_AND_OLD_IMAGES
```

### Root Endpoint Compatibility
DynamoDB and DynamoDB Streams both use Tarn's shared root endpoint:

```bash
awslocal dynamodb list-tables --endpoint-url http://127.0.0.1:4566
awslocal dynamodbstreams list-streams --endpoint-url http://127.0.0.1:4566
```

## Examples

Terraform example in this repository:

- `examples/terraform/dynamodb-stream-lambda` — stream-enabled table with Lambda event source mapping

### Put and Query Items

```javascript
import {
  DynamoDBClient,
  PutItemCommand,
  QueryCommand,
} from "@aws-sdk/client-dynamodb";

const client = new DynamoDBClient({
  endpoint: "http://127.0.0.1:4566",
  region: "us-east-1",
  credentials: { accessKeyId: "test", secretAccessKey: "test" }
});

await client.send(new PutItemCommand({
  TableName: "orders",
  Item: {
    pk: { S: "user#1" },
    sk: { S: "order#1" },
    status: { S: "PENDING" },
    total: { N: "42" }
  }
}));

const result = await client.send(new QueryCommand({
  TableName: "orders",
  KeyConditionExpression: "#pk = :pk",
  ExpressionAttributeNames: { "#pk": "pk" },
  ExpressionAttributeValues: { ":pk": { S: "user#1" } }
}));

console.log(result.Items);
```

### Terraform Endpoint

```hcl
provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    dynamodb = "http://127.0.0.1:4566"
    lambda   = "http://127.0.0.1:4566"
  }
}
```

## Known Limitations

- No transactions, batch APIs, TTL, backups, restore, global tables, or PartiQL
- Table status transitions are synchronous; throughput settings are stored but not enforced
- `UpdateTable` support is limited to stream configuration changes
- TTL, backups, contributor insights, and Kinesis streaming destination APIs are present for compatibility but do not emulate the full AWS feature behavior
