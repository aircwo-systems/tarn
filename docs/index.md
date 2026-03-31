---
layout: home

hero:
  name: "Tarn"
  text: "Local cloud, free and fast."
  tagline: "Open-source AWS emulator. Single binary, zero config. Free forever under Apache 2.0."
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: View on GitHub
      link: https://github.com/aircwo-systems/tarn

features:
  - title: Single Binary
    details: One binary, no dependencies. Build and start in seconds. No Docker-in-Docker, no JVM, no Python runtime.
  - title: AWS Compatible
    details: Works with AWS CLI, SDK v2, and Terraform. Drop-in replacement — point your endpoint and go.
  - title: Docker Native Lambda
    details: Lambda containers run in Docker with AWS Runtime Interface Emulator. Layers, extensions, and secrets cache built in.
  - title: Dashboard UI
    details: Built-in web console to manage resources, view topology, inspect logs, and monitor event flows.
  - title: Event Driven
    details: SQS and DynamoDB Streams event source mappings, SNS fanout to SQS and Lambda, EventBridge scheduled rules with cron and rate expressions.
  - title: Terraform Ready
    details: 180+ API actions implemented. Unknown actions return empty success responses so production .tf files just work.
---
