# Contributing

Thanks for your interest in contributing to Tarn! This page explains how to get started.

## Prerequisites

- **Go 1.26+**
- **Docker** (for Lambda execution)
- **make** and standard Unix tools

## Local Setup

```bash
# Clone and build
git clone https://github.com/aircwo-systems/tarn.git
cd tarn
make build

# Run the server
./build/tarn start

# In another terminal, run tests
make test
```

## Development Workflow

1. Create a branch from `develop-mvp`
2. Make focused changes (one feature/fix per PR)
3. Add or update tests with your changes
4. Run checks before pushing

## Required Checks

```bash
# Run tests
make test

# Check formatting and lint
make lint
make fmt
```

## Commit Messages

Use [conventional commits](https://www.conventionalcommits.org/):

```
feat(lambda): add support for custom entrypoints
fix(sqs): handle batch deletion correctly
docs(readme): update quick start example
chore: update dependencies
```

## Pull Request Checklist

- [ ] Branch off `develop-mvp`
- [ ] Tests pass (`make test`)
- [ ] Lint passes (`make lint`)
- [ ] Clear commit messages
- [ ] PR description explains what and why
- [ ] Linked to related issues

## Areas to Contribute

### Services
- Expanding S3 (virtual-hosted style, versioning)
- EventBridge patterns and `PutEvents`
- Cross-account SNS/SQS support

### Features
- Additional runtimes
- Lambda@Edge
- VPC integration

### Documentation
- Service guides
- Examples and tutorials
- Architecture docs

### UI
- Dashboard improvements
- Mobile responsiveness
- New visualizations

## Questions?

Open an issue or discussion on [GitHub](https://github.com/aircwo-systems/tarn).
