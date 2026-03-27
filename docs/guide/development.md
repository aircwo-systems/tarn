# Development

Build Tarn from source and contribute to development.

## Prerequisites

- **Go 1.26+**
- **Docker** (for Lambda execution during tests)
- **make** and standard Unix tools
- **golangci-lint** (for code quality checks)

## Initial Setup

```bash
git clone https://github.com/aircwo-systems/tarn.git
cd tarn

# Build the binary
make build

# Run the server
./build/tarn start
```

The server will start on `http://127.0.0.1:4566` and wait for requests.

## Development Commands

```bash
# Build binary
make build

# Run tests
make test

# Run tests (fast mode, skip long tests)
make test-short

# Format code
make fmt

# Lint code
make lint

# Format + lint
make fmt lint

# Build and start in dev mode
make dev

# Install binary to $GOPATH/bin
make install
```

## Project Structure

```
tarn/
├── cmd/                    # Entry points
│   ├── tarn/              # Main CLI
│   ├── secrets-proxy/     # Lambda extension
│   └── db-proxy/          # Database proxy
├── internal/              # Core packages
│   ├── api/               # HTTP API handlers
│   ├── lambda/            # Lambda service
│   ├── sqs/               # SQS service
│   ├── sns/               # SNS service
│   ├── s3/                # S3 service
│   ├── secrets/           # Secrets Manager
│   ├── eventbridge/       # EventBridge
│   ├── apigateway*/       # API Gateway v1/v2
│   ├── config/            # Configuration
│   ├── cli/               # CLI commands
│   └── trace/             # Tracing/logging
├── ui/                    # SvelteKit dashboard (optional)
└── docs/                  # VitePress documentation
```

## Code Style

Tarn follows Go conventions:

- **Naming**: `camelCase` for functions, `PascalCase` for exported
- **Comments**: Explain the "why", not the "what"
- **Tests**: Every exported function has tests (aim for >80% coverage)
- **Errors**: Use `fmt.Errorf` with context, wrap with `%w`

## Testing

Run all tests:

```bash
make test
```

Run tests for a specific package:

```bash
go test ./internal/lambda -v
```

Run with race detector (slower but catches concurrency bugs):

```bash
go test -race ./...
```

## Common Development Tasks

### Adding a New AWS Service

1. Create `internal/{service}/` package
2. Implement API handlers in `internal/api/{service}/`
3. Add CLI commands in `internal/cli/{service}/`
4. Write tests
5. Update documentation in `docs/services/`

### Fixing a Bug

1. Create a branch: `git checkout -b fix/description`
2. Write a failing test first
3. Fix the code to pass the test
4. Run `make test lint`
5. Commit: `git commit -m "fix(service): description"`

### Adding a Feature

1. Create a branch: `git checkout -b feat/description`
2. Implement the feature with tests
3. Update relevant docs
4. Commit: `git commit -m "feat(service): description"`

## Running Tests with Coverage

Generate a coverage report:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Debugging

There is no dedicated debug flag or `TARN_DEBUG` environment variable at the moment. For local debugging:

```bash
./build/tarn start
go test ./... -run TestName -v
```

Use `log.Printf` or structured logging in code for debugging.

## CI/CD

Tests run automatically on every push via GitHub Actions:

- **CI Workflow** (`.github/workflows/ci.yml`): Tests, lint, build
- **Release Workflow** (`.github/workflows/release.yml`): Build binaries and deploy to GitHub Releases

Check status in GitHub Actions tab.

## Documentation

API documentation is in code comments. User documentation is in `docs/`.

To preview docs locally:

```bash
cd docs
bun install
bun run docs:dev
```

Then open `http://localhost:5173`.

## Questions?

Open an issue on [GitHub](https://github.com/aircwo-systems/tarn/issues) or check existing issues for help.
