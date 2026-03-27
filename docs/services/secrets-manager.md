# Secrets Manager

Store and manage sensitive data.

<span class="status-badge status-full">Fully Supported</span>

## Supported Operations

| Operation | Status | Notes |
|-----------|--------|-------|
| CreateSecret | Supported | String and binary values |
| GetSecretValue | Supported | |
| PutSecretValue | Supported | Creates a new current value |
| GetResourcePolicy | Supported | Compatibility read returns a default policy document |
| UpdateSecret | Supported | Update secret value or metadata |
| DeleteSecret | Supported | Immediate or scheduled deletion |
| ListSecrets | Supported | |
| DescribeSecret | Supported | |
| TagResource | Supported | |
| UntagResource | Supported | |

## Features

### Lambda Extension
Secrets are automatically available inside Lambda containers via the AWS Secrets Manager Lambda Extension:

```javascript
// Inside your Lambda function
const response = await fetch(
  'http://localhost:2773/secretsmanager/get?secretId=my-database-password',
  {
    headers: {
      'X-Aws-Parameters-Secrets-Token': process.env.AWS_SESSION_TOKEN
    }
  }
);

const secret = await response.json();
console.log(secret.SecretString);
```

### Lambda Extension Surface
The local extension proxy forwards `GetSecretValue` requests to Tarn and supports `secretId`, `versionId`, and `versionStage` query parameters. Parameter Store endpoints are present but return `501 Not Implemented`.

## Examples

### Create and Retrieve

<div class="example-block">
<div class="lang">JavaScript (AWS SDK)</div>

```javascript
import { SecretsManagerClient, CreateSecretCommand, GetSecretValueCommand } from "@aws-sdk/client-secrets-manager";

const secrets = new SecretsManagerClient({ endpoint: "http://127.0.0.1:4566" });

// Create secret
const createRes = await secrets.send(new CreateSecretCommand({
  Name: "database-password",
  SecretString: "super-secret-password"
}));

console.log("Created:", createRes.ARN);

// Retrieve secret
const getRes = await secrets.send(new GetSecretValueCommand({
  SecretId: "database-password"
}));

console.log("Password:", getRes.SecretString);
```
</div>

### With Terraform

<div class="example-block">
<div class="lang">HCL</div>

```hcl
resource "aws_secretsmanager_secret" "db_password" {
  name = "prod/database/password"
}

resource "aws_secretsmanager_secret_version" "db_password" {
  secret_id     = aws_secretsmanager_secret.db_password.id
  secret_string = "my-secure-password"
}

resource "aws_lambda_function" "app" {
  filename = "function.zip"
  handler  = "index.handler"
  runtime  = "nodejs20.x"

  environment {
    variables = {
      SECRET_ID = aws_secretsmanager_secret.db_password.name
    }
  }
}
```
</div>

## Lambda Integration

In your Lambda function, access secrets without additional setup:

```javascript
// database.js
import { SecretsManagerClient, GetSecretValueCommand } from "@aws-sdk/client-secrets-manager";

const secrets = new SecretsManagerClient();

export async function getDbPassword() {
  const result = await secrets.send(new GetSecretValueCommand({
    SecretId: process.env.SECRET_ID
  }));

  return JSON.parse(result.SecretString);
}
```

The extension forwards requests directly to Tarn Secrets Manager, so SDK-based access works without changing application code.

## Known Limitations

- No versioning (latest version only)
- No lambda rotation policies
- Resource policy reads are compatibility stubs; custom policy management is not implemented
