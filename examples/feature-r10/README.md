# Feature `r10` Example

This example provisions a small feature-scoped stack and tags every resource with `feature=r10` so the dashboard can be filtered down to that slice of infrastructure.

Resources created:
- API Gateway HTTP API: `r10-public-api`
- Lambda function: `r10-catalog-api`
- Lambda function: `r10-fulfillment-worker`
- SQS queue: `r10-orders`
- Secrets Manager secret: `r10-shared-config`

The setup script prefers the local `./build/openstack` CLI for resource creation. It uses direct AWS-compatible API calls only for tag operations and API Gateway management, because those flows are not exposed in the CLI yet.

## Usage

Start OpenStack first:

```bash
make build
./build/openstack start --ui
```

Provision the tagged feature stack:

```bash
./examples/feature-r10/setup.sh
```

Exercise the stack:

```bash
./examples/feature-r10/invoke.sh
```

Then open the dashboard and filter by:

```text
feature:r10
```

That should reduce the resource views to just this feature bundle.

Remove the example resources:

```bash
./examples/feature-r10/cleanup.sh
```

If a previous run left stale `r10` resources behind, flush them directly from the CLI:

```bash
./build/openstack flush --tag feature=r10
```
