# HTTP API `HTTP_PROXY` integration PRD

Status: Phase 1 implemented  
Target: API Gateway HTTP APIs (v2)  
Primary area: `internal/apigateway`, `internal/api/apigateway`, `pkg/types/apigateway.go`

## 1. Summary

Tarn's HTTP APIs should support `HTTP_PROXY` integrations. A route forwards the request to an
HTTP URL and returns the upstream response as it is. Parameter mapping on the integration can add,
replace or remove request headers, query parameters and the path.

The URL can be any reachable HTTP endpoint, including an ECS service running in Tarn through its
`/_ecs/{account}/{cluster}/{service}/` proxy. That covers the common "API Gateway in front of a
container service" layout without a Lambda in between.

VPC links (`ConnectionType: VPC_LINK`) are phase 2 (section 10). Tarn has no load balancer or
Cloud Map for a VPC link to point at yet.

## 2. Problem

`CreateIntegration` on an HTTP API accepts `AWS_PROXY` (Lambda) and `AWS` (SQS) only
(`internal/apigateway/service.go:225`). `docs/services/api-gateway.md` lists this as a known
limitation. `Invoke` assumes that anything that is not `AWS` is a Lambda.

In AWS, a gateway in front of a container service is an `HTTP_PROXY` integration (public URL or
VPC link) with parameter mapping. To run the same layout in Tarn today, a team has to write a
Lambda that forwards requests. A real case, an admin API behind a gateway that must replace the
caller's `X-API-KEY` with its own and forward the staff Bearer token untouched, needed a 150-line
forwarding Lambda:

- It strips hop-by-hop headers and `x-api-key`, sets the gateway's key, rebuilds the URL, sends
  the request with `node:http`, copies back an allow-list of response headers and maps errors to
  its own JSON body.
- It has its own 5 second timeout, retries reads once, and writes its own log line, because none
  of that came from the gateway. None of this behaviour exists in AWS, so local tests exercise the
  stand-in rather than the gateway's real behaviour.
- It adds a Lambda container per request path. The deploy script pins the Lambda's architecture to
  the host's, because an emulated x86_64 runtime on Apple Silicon crashed now and then. A call
  from the Lambda to Tarn also hung on occasion, which nobody diagnosed. Both problems come from
  the workaround, not from the code being tested.
- It exists only locally. The AWS deployment has no such function, so the local and deployed
  topologies differ.

In AWS the same gateway is one integration with
`"overwrite:header.x-api-key": "<key>"`.

## 3. Product intent

The user is a developer running a service behind an HTTP API in Tarn, configured with the SDK, the
CLI or Terraform. They write the same `CreateIntegration` call (or `aws_apigatewayv2_integration`
resource) they would write for AWS, and requests reach their backend with the mapped headers.
Timeouts and failures should look like API Gateway's, so the caller's error handling is tested
against the gateway and not against a stand-in.

For ECS services in Tarn, phase 1 supports one way in: an `INTERNET` integration whose URL is
Tarn's own `/_ecs/` proxy. Tarn should make that URL easy to write and fast to call (section 5.5).

## 4. Scope

### In scope (phase 1)

- `IntegrationType: HTTP_PROXY` with `ConnectionType: INTERNET` (the AWS default) on HTTP APIs.
- `IntegrationUri` as an absolute `http` or `https` URL, with `{name}` path variables filled from
  the matched route, including a greedy `{proxy}` from a `{proxy+}` route.
- `IntegrationMethod`, including `ANY`. Today Tarn ignores the input and stores `POST`.
- Request parameter mapping for `HTTP_PROXY` (section 5.3).
- `TimeoutInMillis`, already validated to 50 to 30 000 ms, enforced on the upstream call.
- Traces and a request log line for each proxied call (section 5.6).
- Create, get, update and delete round-trip, persistence across restarts, and a clean second
  `terraform apply`.
- Doc updates (section 12).

### Out of scope

- `ConnectionType: VPC_LINK` and the `*VpcLink` operations. See section 10.
- Custom domain names.
- Executing `HTTP` or `HTTP_PROXY` integrations on REST APIs (v1).
  `internal/apigatewayv1/service.go` defines `integrationTypeHTTP` but nothing runs it.
- `ResponseParameters` (response mapping) on HTTP API integrations.
- Stage variables, and the `$stageVariables.*` mapping source.
- `TlsConfig` and mutual TLS.
- Retries. API Gateway does not retry a failed integration call, and Tarn won't either.
- Dashboard changes, beyond the new type showing up wherever integrations are already listed.

## 5. Behaviour

### 5.1 Creating and updating the integration

| Field | Rule |
|---|---|
| `IntegrationType` | `HTTP_PROXY`. |
| `IntegrationUri` | Required. Absolute `http://` or `https://` URL. Anything else returns `BadRequestException`. Tarn does not check that the URL is reachable at create time, as AWS doesn't. |
| `IntegrationMethod` | An HTTP method or `ANY`. Defaults to `ANY` when omitted (see open question 1). |
| `PayloadFormatVersion` | `1.0` only. Omitted means `1.0`. AWS: "For all other integrations, `1.0` is the only supported value." |
| `ConnectionType` | `INTERNET`, which is the default. `VPC_LINK` returns `BadRequestException` naming the phase 2 gap, rather than silently treating the link as a URL. |
| `TimeoutInMillis` | Unchanged: default 30 000, range 50 to 30 000. |
| `RequestParameters` | Validated as mappings (section 5.3). For `AWS` (SQS) integrations the keys keep their current meaning. |

`UpdateIntegration` applies the same rules to whatever fields it changes. `GetIntegration` and
`GetIntegrations` return `IntegrationType`, `IntegrationUri`, `IntegrationMethod`,
`ConnectionType`, `PayloadFormatVersion`, `TimeoutInMillis` and `RequestParameters` as stored.

### 5.2 Forwarding a request

- **Method.** `IntegrationMethod`, or the request's method when it is `ANY`.
- **URL.** `IntegrationUri` with each `{name}` replaced by the matched route's path parameter.
  A greedy value keeps its slashes. The request's query string is appended as received, in order,
  with repeated keys kept. Query string mappings then apply. When the URI has no path variable,
  Tarn sends the URI's own path, unchanged (open question 2).
- **Headers.** All request headers except hop-by-hop ones (`Connection`, `Keep-Alive`, `TE`,
  `Trailer`, `Transfer-Encoding`, `Upgrade`, `Proxy-Authorization`). `Host` is the target's host.
  Tarn adds `X-Forwarded-For` and `X-Forwarded-Proto` (open question 3). Header mappings apply
  last.
- **Body.** Unchanged, byte for byte. Tarn caps request and response bodies at 10 MB, API
  Gateway's HTTP API payload limit, and answers 413 above it.
- **Correlation.** Tarn reads the correlation ID with `CorrelationIDFromHeaders`, as for Lambda
  integrations. If the request has none, Tarn generates one and sends it as `X-Correlation-Id`, so
  the backend's logs join the trace.
- **Response.** Upstream status, headers and body pass through. Repeated headers stay repeated, so
  several `Set-Cookie` values reach the caller as separate headers. Hop-by-hop headers are dropped.
  If the API has a `corsConfiguration`, CORS headers are replaced as they are today for Lambda
  responses (`internal/apigateway/cors.go`).
- **At most once.** Tarn sends each request to the upstream at most once.

### 5.3 Request parameter mapping

Keys, from the AWS parameter mapping reference:

| Key | Effect |
|---|---|
| `append:header.<name>` | Add a value to the header. |
| `overwrite:header.<name>` | Replace every inbound value with this one. |
| `remove:header.<name>` | Drop the header. The value is ignored (AWS expects `""`). |
| `append:querystring.<name>` / `overwrite:querystring.<name>` / `remove:querystring.<name>` | The same, for query parameters. |
| `overwrite:path` | Replace the whole upstream path. |

Values:

- A static string, which is any value that does not start with `$`.
- `$request.header.<name>`, `$request.querystring.<name>`, `$request.path`,
  `$request.path.<name>`.
- `$context.requestId`, `$context.apiId`, `$context.stage`.

Rules:

- Mapping values read from the request as received, not from the result of other mappings.
- Header names are case-insensitive. An `overwrite` and a `remove` for the same header in one
  integration is a `BadRequestException`.
- A mapping cannot target a reserved header. The AWS list is: `access-control-*`, `apigw-*`,
  `Authorization`, `Connection`, `Content-Encoding`, `Content-Length`, `Content-Location`,
  `Forwarded`, `Keep-Alive`, `Origin`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`,
  `Trailers`, `Transfer-Encoding`, `Upgrade`, `x-amz-*`, `x-amzn-*`, `X-Forwarded-For`,
  `X-Forwarded-Host`, `X-Forwarded-Proto`, `Via`. Create and update return `BadRequestException`
  for these. `x-api-key` is not reserved, and `Authorization` passes through unmapped.
- A source Tarn does not support (`$request.body.*`, `$stageVariables.*`, other `$context.*`
  variables, or a value mixing text and an expression) returns `BadRequestException` naming the
  source. Tarn must never forward the literal text `$request.body.id` to a backend.

A static value is stored in the integration and returned by `GetIntegration`, as in AWS. Docs
should say so, because a static API key is readable by anyone who can read the API's config.

### 5.4 Timeouts and failures

| Case | Response |
|---|---|
| No response headers within `TimeoutInMillis` | 504 |
| Connection refused, DNS failure, TLS failure, reset before a response | 502 |
| Upstream responded, with any status | That status and body, unchanged |
| Body over 10 MB | 413 for a request; 502 for a response |

Error bodies are `{"message": "..."}` with `Content-Type: application/json`, the same shape as
Tarn's existing Lambda error. The exact text should match AWS once confirmed (open question 4).

When the target is Tarn's `/_ecs/` proxy, the proxy's own answers (503 for no running task, 502
and 504 for upstream failures) are upstream responses and pass through unchanged. The proxy's
`ecsProxyResponseHeaderTimeout` is 10 s, so an integration timeout above 10 s never fires for an
ECS target. The ECS proxy's 504 arrives first. Document this.

### 5.5 Calling Tarn's own services

The integration call starts in Tarn, so the URI has to be reachable from Tarn. Teams will often
copy the URL their containers use, such as `http://host.docker.internal:4566/_ecs/...`, and that
name may not resolve on the host.

Tarn treats a URI whose host is Tarn's own listener as local. That means `127.0.0.1`, `localhost`,
`host.docker.internal` or the configured `--host`, on Tarn's port. Tarn dispatches a local
request to its own handler in process, without opening a socket. A local call therefore cannot
hang on a loopback connection and costs no extra connection.

Account scoping is unchanged: the `/_ecs/{account}/...` path still names the account.

### 5.6 Observability

- **Trace.** One trace per request under the correlation ID, as for Lambda integrations. The
  span for an ECS target is kind `ecs`, named after the service (`ecs:<service>`). For any other
  target it is kind `http`, named after the URI's host. The span status comes from the upstream
  status (`StatusForHTTP`).
- **Log.** One line per proxied request in `/tarn/api`, with API ID, stage, route key, upstream
  target, upstream status, duration in ms and correlation ID. A failed request (502, 504, 413) also
  records the cause (`timeout`, `connection refused`, and so on).

Stage access logging (`AccessLogSettings`) is the AWS feature for this and is a follow-up.

### 5.7 Persistence and Terraform

The new fields (`ConnectionType`, and `IntegrationMethod` as given) persist in
`internal/apigateway/store.go` and survive a restart. An `aws_apigatewayv2_integration` with
`integration_type = "HTTP_PROXY"`, `integration_method = "ANY"` and a `request_parameters` block
applies, then shows no diff on a second plan.

## 6. Reference scenario

An admin frontend calls an identity service running as an ECS service in Tarn. The gateway must
replace any inbound `X-API-KEY` with its own and forward the staff `Authorization: Bearer` token
untouched.

```ts
const api = await apigw.send(new CreateApiCommand({ Name: "admin-gateway", ProtocolType: "HTTP" }));
const integration = await apigw.send(
  new CreateIntegrationCommand({
    ApiId: api.ApiId,
    IntegrationType: "HTTP_PROXY",
    IntegrationMethod: "ANY",
    IntegrationUri: "http://127.0.0.1:4566/_ecs/000000000000/admin/identity/admin/{proxy}",
    PayloadFormatVersion: "1.0",
    TimeoutInMillis: 5000,
    RequestParameters: { "overwrite:header.x-api-key": gatewayKey },
  }),
);
await apigw.send(
  new CreateRouteCommand({
    ApiId: api.ApiId,
    RouteKey: "ANY /admin/{proxy+}",
    Target: `integrations/${integration.IntegrationId}`,
  }),
);
```

Expected:

- `GET /_apigateway/{id}/$default/admin/users?q=smith&q=jones` with `Authorization: Bearer t` and
  `X-API-KEY: wrong` reaches the service as `GET /admin/users?q=smith&q=jones`, with
  `Authorization: Bearer t` and exactly one `x-api-key` header carrying the gateway's key.
- A `POST` with a JSON body reaches the service byte for byte. The service's 201 response,
  including two `Set-Cookie` headers, comes back unchanged.
- With the service scaled to 0, the caller gets the ECS proxy's 503.
- With the service answering after 6 s, the caller gets 504 after about 5 s. The service received
  the request once.
- The trace for the request's correlation ID contains an `ecs:identity` span. `/tarn/api` holds
  one line for the request with the same correlation ID.

The forwarding Lambda this replaces maps to Tarn features like this:

| Forwarding Lambda | Tarn with this PRD |
|---|---|
| Strip inbound `x-api-key`, set its own | `overwrite:header.x-api-key` |
| Drop hop-by-hop headers, forward the rest | Section 5.2 |
| Copy back an allow-list of response headers | All response headers pass through |
| 5 s socket timeout, 504 | `TimeoutInMillis`, 504 |
| Retry a failed `GET` once | Dropped. API Gateway doesn't retry. |
| Own error JSON body | API Gateway's `{"message": ...}` body |
| One JSON log line per request | `/tarn/api` line and the trace span |

## 7. Acceptance criteria

1. **Create.** `CreateIntegration` with `HTTP_PROXY` and a valid URL succeeds. `GetIntegration`
   returns the type, URI, method, `ConnectionType: INTERNET`, `PayloadFormatVersion: 1.0`,
   timeout and mappings.
2. **Validation.** Missing or non-HTTP URI, `PayloadFormatVersion: 2.0`, `ConnectionType:
   VPC_LINK`, a reserved header mapping, an unsupported source and a conflicting
   overwrite/remove pair each return `BadRequestException` with a message naming the problem.
3. **Method.** `IntegrationMethod: ANY` forwards the request's method. A fixed method, such as
   `POST`, is used whatever the request's method was.
4. **Path and query.** A `{proxy+}` route with a `{proxy}` URI forwards nested paths with their
   slashes. Query strings arrive in order with repeated keys kept.
5. **Headers.** Inbound headers arrive except hop-by-hop ones. `Host` is the target's.
   `Authorization` is unchanged.
6. **Mapping.** Each key form in 5.3 works against an echo server: `overwrite` leaves exactly one
   value, `append` adds one, `remove` drops it. Static, header, query string, path and `$context`
   values resolve.
7. **Response.** Status, body and every response header pass through, including two `Set-Cookie`
   headers as two headers.
8. **Timeout.** An upstream slower than `TimeoutInMillis` produces 504 within 10% of the timeout.
   The upstream sees one request.
9. **Unreachable.** A closed port produces 502 with a JSON `message`. No second attempt is made.
10. **ECS target.** A URI on Tarn's own listener and `/_ecs/` reaches the service's task without a
    socket to Tarn's own port. `127.0.0.1`, `localhost` and `host.docker.internal` all work.
    Scaling the service to 0 passes the proxy's 503 through.
11. **Observability.** Each proxied request leaves a trace with an `ecs:` or `http:` span and one
    `/tarn/api` line, both under the request's correlation ID.
12. **Persistence.** Integrations survive a Tarn restart unchanged. `UpdateIntegration` changes
    the URI, method, timeout and mappings, and validates them as create does.
13. **Terraform.** A `compatibility-test/terraform-aws` case with an `HTTP_PROXY` integration and a
    `request_parameters` block applies, serves a request through the gateway, and plans no changes
    on a second run.
14. **No regression.** Lambda and SQS integrations behave as before, including `Invoke`'s error
    for an integration type it doesn't know.

## 8. Design notes

- **Where it lives.** Add `invokeHTTPProxyIntegration` in `internal/apigateway/service.go` next
  to `invokeSQSIntegration`. Make `Invoke`'s switch explicit: `AWS_PROXY` gets its own case, and
  `default` returns an error. Today `default` means Lambda, so a new type falls into the Lambda
  path.
- **Validation.** `RequestParameters` means different things for each type: SQS reads
  `MessageBody` and friends, `HTTP_PROXY` reads mapping keys. Validate per type in
  `CreateIntegration` and `UpdateIntegration`. Parse mappings once into a typed form at create
  time, and keep the raw map for `GetIntegration`.
- **IntegrationMethod.** `CreateIntegration` sets `IntegrationMethod: http.MethodPost` whatever
  the input says. Add the field to `IntegrationCreateInput` and `IntegrationUpdateInput`, and keep
  `POST` as the stored value for Lambda and SQS.
- **Transport.** Use a dedicated `http.Transport`, with the request context's deadline set from
  `TimeoutInMillis`. Go's transport may resend an idempotent request (`GET`, `HEAD`, or one with
  an `Idempotency-Key`) on a stale pooled connection. That is acceptable, because the request
  never reached the upstream. Tarn adds no retry of its own.
- **Local dispatch.** Recognise Tarn's own host and port and call the server's mux with an
  `httptest`-style recorder, or call `ecsProxyHandler` directly. That removes the loopback hop
  from the case teams will use most.
- **Buffering.** `InvokeOutput` holds the body in memory. That is fine with the 10 MB cap.
  Streaming can come later if anyone needs it.

## 9. Open questions

These are AWS behaviours the docs don't settle. One check against a real HTTP API with an echo
backend would answer 1 to 5 cheaply.

1. Does AWS require `IntegrationMethod` for `HTTP_PROXY`? The API reference marks it optional, and
   every example passes it.
2. When the URI has no path variable, does AWS send the URI's path as is, or append the request
   path?
3. Which headers does API Gateway add or rewrite on the way to the backend (`Host`,
   `X-Forwarded-*`, `Via`)? Does it keep several `Set-Cookie` values on the way back? (Assumed yes.)
4. What status and body does an HTTP API return when an `HTTP_PROXY` call times out, and when the
   backend is unreachable?
5. Can a mapping value mix text and an expression, such as `Bearer $request.header.token`?
6. Which private integration do real deployments of this layout use: ALB listener, NLB listener
   or Cloud Map? The answer decides the shape of phase 2, and whether phase 1 is enough for
   Terraform parity or only removes the local workaround.

## 10. Phase 2: VPC links

In AWS, a container service behind an HTTP API is usually a private integration:
`ConnectionType: VPC_LINK`, a `ConnectionId`, and an `IntegrationUri` that is "the ARN of an
Application Load Balancer listener, Network Load Balancer listener, or AWS Cloud Map service". AWS
also notes that "for private integrations, API Gateway includes the stage portion of the API
endpoint in the request to your backend resources", which teams undo with
`overwrite:path = $request.path`.

Tarn has neither ELBv2 nor Cloud Map, so there is nothing for such an ARN to resolve to. Options,
to pick after open question 6:

- A minimal Cloud Map: namespaces and services, with ECS `serviceRegistries` registering running
  tasks. A Cloud Map ARN then resolves the way the `/_ecs/` proxy resolves a service today.
- A minimal ELBv2: load balancers, listeners and target groups, with ECS `loadBalancers`
  registering tasks. This is more work, and is also useful outside API Gateway.

Either way, phase 2 adds `CreateVpcLink`, `GetVpcLink(s)` and `DeleteVpcLink` as records Tarn
stores without networking behind them, plus the stage-in-path behaviour above.

## 11. Follow-ups (not this PRD)

- Custom domain names, so an HTTP API can sit in front of a browser at a stable host.
- Stage access logging (`AccessLogSettings`) into a log group.
- `HTTP` and `HTTP_PROXY` execution on REST APIs (v1), sharing the forwarder.
- `ResponseParameters` on HTTP API integrations.
- Stage variables and `$stageVariables.*`.

## 12. Doc changes

- `docs/services/api-gateway.md`: the `CreateIntegration` row (line 21) lists `HTTP_PROXY`. The
  limitation "HTTP APIs support Lambda and SQS integrations only" (line 155) changes to name
  VPC links as the remaining gap. Add a "HTTP API with an HTTP backend" example using an ECS
  service and `overwrite:header`, plus a note that static mapping values are readable through
  `GetIntegration`.
- `docs/guide/terraform.md`: an `aws_apigatewayv2_integration` example with
  `integration_type = "HTTP_PROXY"`.
