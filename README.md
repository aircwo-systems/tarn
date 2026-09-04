# Tarn

<p align="center">
  <img src="ui/static/favicon.svg" alt="Tarn logo" width="140"/>
</p>

<p align="center">
  <strong>Local cloud, free and fast.</strong>
</p>

<p align="center">
  <a href="https://github.com/aircwo-systems/tarn/actions/workflows/ci.yml"><img src="https://github.com/aircwo-systems/tarn/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/aircwo-systems/tarn/releases/latest"><img src="https://img.shields.io/github/v/release/aircwo-systems/tarn?include_prereleases&label=version" alt="Latest Release" /></a>
  <a href="https://aircwo-systems.github.io/tarn/"><img src="https://img.shields.io/badge/docs-online-007a5a" alt="Docs" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License" /></a>
</p>

Open-source AWS cloud emulator for local development and testing. Single binary, zero config. Free forever under Apache 2.0.

## Status

⚠️ **Beta Project** — Tarn is under active development. Expect breaking changes and API updates as we finalize the MVP. Not recommended for production workloads yet.

**Known Limitations:**
- **EventBridge**: Only scheduled rules (no event patterns or `PutEvents`)
- **S3**: Path-style only (no virtual-hosted style)

See [GitHub issues](https://github.com/aircwo-systems/tarn/issues) for planned features and roadmap.

## Quick Start

```bash
make build
./build/tarn start
```

Endpoint: `http://127.0.0.1:4566`

Point the AWS CLI, SDK, or Terraform at it:

```bash
export AWS_ENDPOINT_URL=http://127.0.0.1:4566
export AWS_ACCESS_KEY_ID=test        # or a 12-digit ID for multi-account
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1
```

To use multiple isolated accounts, set `AWS_ACCESS_KEY_ID` to a 12-digit numeric string — each unique value gets its own resource namespace. See [Multi-Account](docs/guide/configuration.md#multi-account) for details.

## Documentation

- Published docs: [aircwo-systems.github.io/tarn](https://aircwo-systems.github.io/tarn/)
- [Docs Home](docs/index.md)
- [Getting Started](docs/guide/getting-started.md)
- [Installation](docs/guide/installation.md)
- [Configuration](docs/guide/configuration.md)
- [Terraform](docs/guide/terraform.md)
- [MCP](docs/guide/mcp.md)
- [Services Overview](docs/services/index.md)
- [API Coverage](docs/reference/api-coverage.md)

## Development

```bash
make build       # Build binary
make test        # Run tests with race detector
make lint        # Run golangci-lint
make dev         # Build and start on :4566
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 — see [LICENSE](LICENSE).
