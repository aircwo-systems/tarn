# Agent Builder & Operator Surface PRD

Status: Proposed  
Target: Operator tooling (UI Topology Canvas, EventBridge, Lambda, DynamoDB, MCP)  
Primary area: `ui/src/lib/components/topology`, `internal/api/admin`, `internal/cli/mcp`  
Prototype: `agent-builder-poc.html` (repo root)  
Reference Demo: [Outglow Studio — Product Discovery](https://agent-builder-ui-one.vercel.app/#/agents/product-discovery)  
Reference Video: [Original Demo Video by @arknow91](https://t.co/aVQENXqow8)

---

## 1. Summary

This proposal explores adding an **Agent Builder & Operator Surface** to Tarn, inspired by the Outglow Studio interface prototype ("an agent builder where the chat and the canvas are the same thing").

Modern cloud applications built on AWS are increasingly agentic: compositions of scheduled triggers, webhook listeners, LLM reasoning workers, memory stores, external API tools, and multi-agent workflows. Today, developers building these architectures locally with Tarn must stitch together raw CLI outputs, scattered CloudWatch logs, and separate resource lists.

This feature introduces a unified **dual-pane operator canvas** for Tarn:
1. **Left Telemetry Rail (395px)**: A live execution feed showing the active pass, reasoning narrative, step-by-step tool invocation status, dispatched sub-agents, token consumption, and budget metrics.
2. **Right Visual Topology Canvas**: An infinite, interactive dot-grid canvas with dynamic SVG bezier wires connecting **Ingress/Triggers** → **Core Reasoning Agent** → **Downstream Capabilities/Tools**.

---

## 2. References & Origins

- **Original Video**: [https://t.co/aVQENXqow8](https://t.co/aVQENXqow8) (demo shared by Arek / [@arknow91](https://x.com/arknow91))
- **Live Vercel Application**: [https://agent-builder-ui-one.vercel.app/#/agents/product-discovery](https://agent-builder-ui-one.vercel.app/#/agents/product-discovery)
- **Local Interactive Prototype**: `agent-builder-poc.html` (repo root)

---

## 3. The Problem

When developers build agentic workflows on AWS (e.g. Bedrock agents, LangGraph/CrewAI pipelines running on Lambda/ECS):
- **Invisible Execution Paths**: When an EventBridge cron rule or webhook triggers an agent run, observing which tools were invoked, what memory was read, and which sub-agents were spawned requires grepping across disparate CloudWatch log streams.
- **Disconnected Context**: The definition of the agent (prompts, model configs, tool schemas) is isolated from its runtime telemetry (token spend, duration, errors, and hop-by-hop traces).
- **Missing Local Visual Feedback**: Local cloud emulators typically provide flat tables of resources (a list of Lambdas, a list of SQS queues). They lack an operator surface that shows **relationships and live data flow**.

---

## 4. Product Intent & Architecture

### 4.1 Node Topology Model

The canvas organizes an agent into three connected columns:

```
┌─────────────────────────────────┐       ┌────────────────────────┐       ┌────────────────────────┐
│     INPUTS & TRIGGERS           │       │    REASONING CORE      │       │     CAPABILITIES       │
│                                 │       │                        │       │                        │
│ • Schedule (cron/interval)      │──────>│ Product Discovery      │──────>│ • Tools                │
│ • Webhook / Event Triggers      │──────>│ Agent Core             │──────>│   (6 API integrations) │
│ • Channels (Identity / Router)  │──────>│                        │──────>│ • Sub-agents (5)       │
│ • Memory (Knowledge Stores)     │──────>│ System Prompt (v1.1)   │──────>│ • Skills (3)           │
└─────────────────────────────────┘       └────────────────────────┘       └────────────────────────┘
```

### 4.2 Mapping Outglow Studio to Tarn Local Cloud Primitives

Unlike pure client-side UI mocks, Tarn can back every node with **real, runnable local cloud infrastructure**:

| Visual Canvas Node | Outglow Reference Concept | Tarn Local AWS Implementation |
| :--- | :--- | :--- |
| **Schedule** | `Mon 08:00 AM`, `cron 0 8 * * 1` | `EventBridge Rule` (scheduled rule with target) |
| **Triggers** | Zendesk webhook (`tag=enterprise`) | `API Gateway Route` + `SQS Queue` |
| **Channels** | Agent identity / dispatch channels | `SNS Topic` / `SES stub` |
| **Memory** | `product-strategy-h2`, `opportunities-archive` | `DynamoDB Table` / `S3 Vector Bucket` |
| **Agent Core** | Model instructions & reasoning engine | `Lambda Function` / `ECS Container` (LLM runner) |
| **Tools** | Zendesk, Notion, Gong, ClickUp, Metabase, Slack | `Tarn MCP Tools` / Downstream `Lambda Functions` |
| **Sub-agents** | `signal_collector`, `theme_clusterer`, etc. | `Step Functions State Machine` / Worker Lambdas |
| **Skills** | `opportunity-solution-tree`, `rice-scoring` | Modular system prompt extensions / Lambdas |

---

## 5. Scope

### In Scope for MVP
- **Self-contained HTML prototype** (`agent-builder-poc.html`) showcasing:
  - Dot-grid canvas with smooth pan, zoom (`40%`–`200%`), and auto-framing on node selection.
  - Dynamically computed SVG bezier connector paths with linear gradients and status states (`live`, `warn`, `rest`, `tarn-mapped`).
  - Left telemetry rail with animated run pass simulation, thinking timer, token meter, and step cards.
  - "Tarn Cloud Mapping" toggle highlighting local AWS services.
  - Light and Dark theme support.
- **Porting Canvas Primitives into Tarn SvelteKit UI**:
  - Incorporate into `ui/src/lib/components/topology/` as a dedicated operator canvas.
  - Connect step pipeline and narrative feeds to Tarn's admin API (`docs/log-summary-prd.md`).

### Out of Scope for MVP
- Direct cloud deployment to real AWS (Tarn remains local-first).
- Visual drag-and-drop node creation (nodes are declared via config or inferred from provisioned resources).

---

## 6. How to Run the Prototype

Open `agent-builder-poc.html` in any browser:
```bash
open agent-builder-poc.html
```

- Click **Run Pass** in the top bar to run a simulated execution pass.
- Click **Tarn Cloud Mapping** to toggle the AWS service annotations.
- Drag anywhere on the background to pan; use mouse-wheel or bottom controls to zoom.
- Click any node card header to center and focus the viewport on it.
