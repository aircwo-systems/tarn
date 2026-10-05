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
| UpdateApi | Supported | Includes `corsConfiguration` |
| DeleteCorsConfiguration | Supported | |
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
| PutIntegration | Supported | Lambda proxy, SQS AWS and `MOCK` integrations |
| PutMethodResponse | Supported | `responseParameters` stored |
| PutIntegrationResponse | Supported | Static `responseParameters` header values applied |
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

## CORS

Browser apps can call Tarn's API Gateway endpoints directly, without a dev-server proxy, when the API is configured for CORS the way it would be in AWS.

**HTTP APIs (v2).** Set `corsConfiguration` on `CreateApi` or `UpdateApi`, or `cors_configuration` on `aws_apigatewayv2_api` in Terraform. API Gateway then:

- Answers preflight requests (`OPTIONS` with `Origin` and `Access-Control-Request-Method`) with `204`. No `OPTIONS` route is needed. An allowed origin, method and header set gets the `Access-Control-*` headers. Anything else gets none, so the browser blocks the request.
- Adds `Access-Control-Allow-Origin`, `Access-Control-Expose-Headers` and `Access-Control-Allow-Credentials` to every response for an allowed origin, including API Gateway's own errors such as a missing route.
- Ignores `Access-Control-*` headers returned by the Lambda.

Without `corsConfiguration`, preflights are routed like any other request, so an `ANY /{proxy+}` route or an `OPTIONS` route can handle them.

**REST APIs (v1).** REST APIs have no CORS switch. Add an `OPTIONS` method with a `MOCK` integration, as SAM, the Serverless Framework and the console's "Enable CORS" do:

```bash
aws apigateway put-integration --rest-api-id $API --resource-id $RES --http-method OPTIONS \
  --type MOCK --request-templates '{"application/json":"{\"statusCode\": 200}"}'
aws apigateway put-integration-response --rest-api-id $API --resource-id $RES --http-method OPTIONS \
  --status-code 200 --response-parameters '{
    "method.response.header.Access-Control-Allow-Origin": "'"'"'http://localhost:5173'"'"'",
    "method.response.header.Access-Control-Allow-Methods": "'"'"'GET,POST,OPTIONS'"'"'",
    "method.response.header.Access-Control-Allow-Headers": "'"'"'content-type,authorization'"'"'"
  }'
```

The `MOCK` integration reads `statusCode` from the request template, picks the integration response whose `selectionPattern` matches it (or the default response), and returns that response's static header values and `application/json` response template. SQS (`AWS`) integrations apply static header values from the `200` integration response as well. Lambda proxy integrations must return CORS headers from the function, as in AWS.

Preflights never carry credentials, so Tarn checks them against the default account, as it does unsigned `fetch` calls. Use the default account for buckets and APIs a browser calls directly.

## Known Limitations

- HTTP APIs support `ProtocolType=HTTP` only
- HTTP APIs support Lambda and SQS integrations only
- REST APIs execute Lambda proxy and SQS AWS integrations only
- Integration response mappings apply static (single-quoted) header values only; `integration.response.*` mappings are ignored
- Gateway responses (`DEFAULT_4XX`, `DEFAULT_5XX`) are not configurable, so REST API errors carry no CORS headers
- No request/response transformations
- No API keys or usage plans
- No caching
