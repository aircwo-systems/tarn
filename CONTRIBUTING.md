# Contributing to OpenStack

Thanks for contributing.

## 1) Prerequisites

- Go 1.26+
- Docker (required for Lambda execution)
- `make` + standard Unix shell tools

## 2) Local Setup

```bash
make build && make start
```

For UI work:

```bash
make ui-install && make ui-dev
```

## 3) Development Workflow

1. Create a branch from the current main development branch.
2. Keep changes scoped to one feature/fix per PR.
3. Add or update tests with code changes.
4. Run checks before opening a PR.

## 4) Required Checks

```bash
make test
make lint
```

If you changed Go code, also ensure formatting is clean:

```bash
make fmt
```

## 5) Commit & PR Guidance

- Use clear commit messages (e.g. `fix(eventbridge): handle DisableRule on prefixed paths`).
- Include a concise PR description:
  - what changed
  - why it changed
  - how it was tested
- Link related issues/tasks when available.

## 6) Scope Notes

- Keep API behavior AWS-compatible where feasible.
- Avoid breaking existing Terraform/AWS SDK flows.
- Prefer small, reviewable PRs over large mixed changes.

