# Tarn Roadmap

This document outlines where Tarn should go next as the project moves from MVP toward a more complete local AWS development platform.

## Goals

- Make Tarn easier to install and start on every supported platform
- Increase production compatibility for infrastructure workflows
- Improve the UI so it becomes a useful operator surface, not just a basic dashboard

## 1. Installation and Distribution

Tarn should be easier to download, trust, install, and run without manual setup steps.

### Priorities

- Publish cleaner release assets for every supported OS and architecture
- Introduce better install paths:
  - Homebrew tap
  - installer script
  - prebuilt Docker image
- Reduce platform friction on macOS:
  - signed binaries
  - notarization
  - fewer manual `chmod` and quarantine removal steps
- Improve release metadata:
  - checksums
  - clearer versioned download URLs
  - simpler upgrade guidance

### Outcome

Users should be able to go from download to `tarn start` with minimal manual intervention.

## 2. Production-Grade Terraform Compatibility

Tarn already supports core AWS-shaped APIs, but the next step is broader compatibility with real production Terraform configurations.

### Priorities

- Add fuller compatibility stubs across all currently supported services
- Ensure current supported services can accept production-grade Terraform with minimal or no modification
- Expand coverage for control-plane APIs commonly exercised by Terraform providers
- Improve unsupported-action behavior so provider flows continue cleanly where possible
- Preserve AWS-shaped responses, idempotency, and tagging behavior closely enough for real infrastructure plans and applies
- Keep the service coverage docs aligned with actual implementation and stub behavior

### Focus Areas

- API surfaces Terraform calls during create, read, update, and delete flows
- Attribute and policy endpoints required for drift-free plans
- Compatibility responses for APIs that do not need full runtime behavior yet
- Better coverage around dependencies between Lambda, API Gateway, SQS, SNS, S3, Secrets Manager, and EventBridge

### Outcome

Teams should be able to point existing Terraform at Tarn and get useful, high-fidelity local applies for supported services.

## 3. UI and Operator Experience

The UI should evolve from a lightweight dashboard into a practical control plane for local cloud development.

### Canvas and Topology

- Add a richer canvas view for service relationships
- Visualize flows between Lambda, queues, topics, routes, buckets, and event targets
- Make resource-to-resource connections easier to inspect and debug

### Tables and Data Views

- Improve table layouts for resources, events, logs, and traces
- Add stronger filtering, sorting, and empty-state handling
- Make high-volume resource views easier to scan
- Improve status, timing, and error visibility

### Settings and Configuration

- Add better in-app settings management
- Surface environment, region, endpoint, persistence, and runtime settings clearly
- Make feature toggles and local behavior easier to understand without command-line digging

### Probing and Diagnostics

- Improve infrastructure probing and resource discovery
- Expose better diagnostics for what Tarn has detected, provisioned, or inferred
- Make connectivity and compatibility issues more obvious inside the UI

### API Tooling

- Generate Postman collections from the current local surface
- Improve export/share workflows for local API testing
- Make it easier to move from discovered routes and resources into test clients

### Outcome

The UI should become a faster way to understand, inspect, and operate a local Tarn environment.

## 4. Documentation and Product Clarity

The docs should stay tightly coupled to the shipped product.

### Priorities

- Keep service docs aligned with real implementation
- Version docs with releases
- Surface current support levels clearly
- Make recommended workflows obvious:
  - Tarn native CLI
  - AWS-compatible CLI usage
  - Terraform
  - SDKs

### Outcome

Users should be able to trust the docs as a direct reflection of what the current release actually supports.

## 5. Execution Principle

The project should favor practical compatibility over breadth for its own sake.

- Ship deeper support for the services Tarn already exposes before adding many new services
- Prioritize workflows developers actually run:
  - local deploy
  - Terraform apply
  - event debugging
  - secret access
  - queue and topic inspection
- Keep the roadmap grounded in features that improve daily local development, not just checklist parity
