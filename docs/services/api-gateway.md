# API Gateway

Create HTTP and REST APIs with Tarn.

<span class="status-badge status-partial">Partial Support</span>

## Supported Operations

### HTTP APIs (v2)

| Operation | Status | Notes |
|-----------|--------|-------|
| CreateApi | Supported | |
| DeleteApi | Supported | |
| GetApi | Supported | |
| ListApis | Supported | |
| UpdateApi | Supported | |
| CreateRoute | Supported | $default route, path parameters |
| DeleteRoute | Supported | |
| CreateIntegration | Supported | Lambda (`AWS_PROXY`) and SQS (`AWS`) |
| DeleteIntegration | Supported | |
| CreateStage | Supported | |
| DeleteStage | Supported | |
| GetStage | Supported | |
| ListStages | Supported | |

### REST APIs (v1)

| Operation | Status | Notes |
|-----------|--------|-------|
| CreateRestApi | Supported | |
| DeleteRestApi | Supported | |
| GetRestApi | Supported | |
| GetResources | Supported | |
| CreateResource | Supported | |
| CreateMethod | Supported | GET, POST, PUT, DELETE, PATCH |
| PutIntegration | Supported | Lambda proxy and SQS AWS integrations |
| CreateDeployment | Supported | |
| CreateStage | Supported | |

## Examples

### HTTP API with Lambda

<div class="example-block">
<div class="lang">JavaScript (AWS SDK)</div>

```javascript
import { ApiGatewayV2Client, CreateApiCommand } from "@aws-sdk/client-apigatewayv2";
import { LambdaClient, CreateFunctionCommand } from "@aws-sdk/client-lambda";

const apiGw = new ApiGatewayV2Client({ endpoint: "http://127.0.0.1:4566" });
const lambda = new LambdaClient({ endpoint: "http://127.0.0.1:4566" });

// Create API
const apiRes = await apiGw.send(new CreateApiCommand({
  Name: "my-api",
  ProtocolType: "HTTP"
}));

// Create Lambda function and integrate...
```
</div>

### REST API with Terraform

<div class="example-block">
<div class="lang">HCL</div>

```hcl
resource "aws_api_gateway_rest_api" "example" {
  name = "example-api"
}

resource "aws_api_gateway_resource" "items" {
  rest_api_id = aws_api_gateway_rest_api.example.id
  parent_id   = aws_api_gateway_rest_api.example.root_resource_id
  path_part   = "items"
}

resource "aws_api_gateway_method" "items_get" {
  rest_api_id      = aws_api_gateway_rest_api.example.id
  resource_id      = aws_api_gateway_resource.items.id
  http_method      = "GET"
  authorization    = "NONE"
}

resource "aws_api_gateway_integration" "items_lambda" {
  rest_api_id      = aws_api_gateway_rest_api.example.id
  resource_id      = aws_api_gateway_resource.items.id
  http_method      = aws_api_gateway_method.items_get.http_method
  type             = "AWS_PROXY"
  integration_http_method = "POST"
  uri              = aws_lambda_function.example.invoke_arn
}
```
</div>

## Invoke URLs

Once deployed, invoke your API:

```bash
# HTTP API (v2)
curl https://{api-id}.execute-api.{region}.amazonaws.com/{stage}/path

# REST API (v1)
curl https://{api-id}.execute-api.{region}.amazonaws.com/{stage}/path
```

In Tarn:
```bash
curl http://127.0.0.1:4566/_apigateway/{api-id}/{stage}/path
```

## Known Limitations

- HTTP APIs support `ProtocolType=HTTP` only
- HTTP APIs support Lambda and SQS integrations only
- REST APIs execute Lambda proxy and SQS AWS integrations only
- No CORS configuration
- No request/response transformations
- No API keys or usage plans
- No caching
