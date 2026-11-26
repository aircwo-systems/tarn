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
| CreateIntegration | Supported | Lambda (`AWS_PROXY`), SQS (`AWS`) and HTTP URLs (`HTTP_PROXY`) |
| UpdateIntegration | Supported | Validates changes as `CreateIntegration` does |
| DeleteIntegration | Supported | |
| CreateStage | Supported | Creates an auto-deployed stage with an optional description |
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

### HTTP API with an HTTP backend

An `HTTP_PROXY` integration forwards each request to a URL and returns the upstream response unchanged. Put a container service behind a gateway this way without a forwarding Lambda. Point the integration at Tarn's ECS proxy, `/_ecs/{account}/{cluster}/{service}/`.

<div class="example-block">
<div class="lang">JavaScript (AWS SDK)</div>

```javascript
import {
  ApiGatewayV2Client, CreateApiCommand, CreateIntegrationCommand, CreateRouteCommand,
} from "@aws-sdk/client-apigatewayv2";

const apigw = new ApiGatewayV2Client({ endpoint: "http://127.0.0.1:4566" });

const api = await apigw.send(new CreateApiCommand({ Name: "admin-gateway", ProtocolType: "HTTP" }));
const integration = await apigw.send(new CreateIntegrationCommand({
  ApiId: api.ApiId,
  IntegrationType: "HTTP_PROXY",
  IntegrationMethod: "ANY",
  IntegrationUri: "http://127.0.0.1:4566/_ecs/000000000000/admin/identity/admin/{proxy}",
  TimeoutInMillis: 5000,
  RequestParameters: { "overwrite:header.x-api-key": gatewayKey },
}));
await apigw.send(new CreateRouteCommand({
  ApiId: api.ApiId,
  RouteKey: "ANY /admin/{proxy+}",
  Target: `integrations/${integration.IntegrationId}`,
}));
```
</div>

`GET /_apigateway/{api-id}/$default/admin/users?q=smith` reaches the `identity` service as `GET /admin/users?q=smith`. The caller's `Authorization` header passes through unchanged, and the service sees exactly one `x-api-key`, the gateway's.

How Tarn forwards a request:

- `IntegrationMethod: ANY` (the default) sends the caller's method. Any other method replaces it.
- Each `{name}` in the URI's path takes the matched route's path parameter, escaped. A `{proxy+}` value keeps its slashes. Variables may appear only in the path, so a request cannot change the upstream host or query. Without a path variable Tarn sends the URI's own path.
- The query string arrives as the caller sent it, in order and with repeated keys.
- Tarn forwards every request header except hop-by-hop ones, sets `Host` to the target's, and adds `X-Forwarded-For` and `X-Forwarded-Proto`. A request without a correlation ID gets an `X-Correlation-Id`.
- Status, headers and body come back unchanged, including several `Set-Cookie` headers. Tarn does not follow redirects or decompress bodies.
- Tarn calls the upstream once and never retries, as API Gateway does.

`RequestParameters` takes `append:`, `overwrite:` and `remove:` on `header.<name>` and `querystring.<name>`, plus `overwrite:path`. A value is a static string, `$request.header.<name>`, `$request.querystring.<name>`, `$request.path`, `$request.path.<name>`, `$context.requestId`, `$context.apiId` or `$context.stage`. Values read the request as received, not the result of other mappings. Tarn rejects a reserved header such as `Authorization`, `X-Forwarded-For` or `x-amz-*`, a source it does not support such as `$request.body.id`, a value that mixes text and an expression, and an `overwrite` and `remove` for the same header. A query mapping changes only the parameters it names. The rest keep their exact spelling and order.

`UpdateIntegration` merges `RequestParameters` into the stored mappings, as AWS does. A key with a value adds or replaces that mapping, a key sent with an empty value deletes it, and keys left out are kept. So an update cannot set an empty static value. Create can.

::: warning Static values are readable
`GetIntegration` returns static mapping values as stored, as AWS does. Anyone who can read the API's configuration can read an API key set with `overwrite:header.x-api-key`.
:::

| Upstream result | Caller gets |
|---|---|
| Any response | Its status, headers and body |
| No response headers within `TimeoutInMillis` | `504` |
| Connection refused, DNS or TLS failure, connection reset | `502` |
| Request body over 10 MB | `413`, without calling the upstream. This applies to every HTTP API integration. |
| Response body over 10 MB | `502` |

Tarn's own errors are JSON, `{"message": "..."}`.

A URI on Tarn's own port with host `127.0.0.1`, `localhost`, `host.docker.internal` or the `--host` value runs in process, without opening a socket. So a URL copied from a container's configuration works from the gateway too. Answers from the ECS proxy pass through like any upstream response, such as `503` when the service has no running task. The ECS proxy gives up waiting for response headers after 10 seconds, so with an ECS target a `TimeoutInMillis` above 10 000 never fires and the proxy's `504` arrives first.

Each proxied request leaves a trace under its correlation ID, with an `ecs` span named after the service for an ECS target and an `http` span named after the host otherwise. Its `/tarn/api` line adds the API ID, stage, route key, upstream URL, upstream status, upstream time and correlation ID, plus a `cause` such as `timeout` when the call failed.

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

Tarn creates an HTTP API's `$default` stage with the API. Use `CreateStage` or `aws_apigatewayv2_stage` with `auto_deploy = true` to create a named stage such as `dev`. Every stage serves the API's current routes immediately. Tarn always enables automatic deployment for HTTP API stages. Deleting a stage removes its invoke URL.

HTTP API Lambda proxy responses can set multiple cookies with the payload 2.0 `cookies` array. Tarn sends each entry as a separate `Set-Cookie` header alongside any cookie already in `headers`. Payload 1.0 proxy responses can use `multiValueHeaders` on both HTTP and REST APIs.

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
- HTTP APIs have no VPC links. `ConnectionType: VPC_LINK` returns `BadRequestException`. Use an `INTERNET` integration with an `http://` URL, such as Tarn's ECS proxy.
- `HTTP_PROXY` integrations support payload format `1.0` only, and no `ResponseParameters`, `TlsConfig` or stage variables
- An update deletes a mapping by sending it with an empty value, which is how Terraform removes a `request_parameters` entry. A `remove:` entry always has an empty value, so an update cannot delete one, and deleting one from Terraform configuration leaves it in place. Delete and recreate the integration to drop it.
- REST APIs execute Lambda proxy and SQS AWS integrations only
- Integration response mappings apply static (single-quoted) header values only; `integration.response.*` mappings are ignored
- Gateway responses (`DEFAULT_4XX`, `DEFAULT_5XX`) are not configurable, so REST API errors carry no CORS headers
- No request/response transformations
- No API keys or usage plans
- No caching
