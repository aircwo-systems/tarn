# Infrastructure disruptions PRD

Status: Proposed  
Target: Post-MVP operator tooling  
Primary area: Tarn dashboard and account-local service modules

## 1. Summary

Tarn should provide one dashboard section where a developer can deliberately make local AWS resources fail. The first release will move the existing SQS send-failure controls into this section and extend the same workflow to SNS publishes, Lambda invokes, and DynamoDB data operations.

The feature is intended for testing retries, fallbacks, dead-letter handling, partial failures, and recovery behavior before an application reaches a shared environment. Injected failures must look like failures from the relevant AWS service. They must affect direct SDK calls and calls made internally between Tarn services.

Rules remain account-local and in memory. Tarn clears them when the process restarts. The dashboard must make every active rule visible and provide an immediate way to disarm all rules.

## 2. Problem

Tarn currently supports failure injection only for SQS `SendMessage`. The implementation is useful but tied to queues throughout the stack:

- The rule store lives inside the SQS module.
- The admin routes use `/_tarn/admin/sqs/disruptor`.
- The request and dashboard types contain queue-specific fields.
- The controls appear inside queue detail and bulk-selection views.
- The available error codes are fixed to SQS.

This prevents the dashboard from presenting one coherent view of active disruptions. Adding more services by copying the SQS implementation would also duplicate rule stores, admin handlers, client functions, state projection, and UI forms.

The existing dashboard "Chaos" section solves a different problem. It probes API Gateway routes with generated inputs to discover validation behavior. Infrastructure failure injection must not replace or silently change that workflow.

## 3. Product intent

The primary user is a developer running Tarn locally while testing an event-driven application. They have just deployed or started the stack and want to answer a specific question, such as:

- Does the publisher retry when SQS returns `ServiceUnavailable`?
- Does an API route return the expected fallback when Lambda is throttled?
- Does a workflow handle a failed DynamoDB write without losing the request?
- Does SNS batch publishing report individual failed entries correctly?
- Does the application recover as soon as the disruption is removed?

The workflow should feel controlled and explicit. The user must always know what is armed, which calls it affects, and how to return the environment to normal.

## 4. Goals

- Provide one dashboard section for creating, inspecting, updating, and removing disruption rules.
- Support SQS, SNS, Lambda, and DynamoDB in the first release.
- Apply rules at service method seams so internal Tarn calls are affected as well as direct HTTP calls.
- Return AWS-shaped codes, statuses, and batch failures through each service protocol.
- Keep rules isolated by account and clear them on restart.
- Make the affected resource, operation, probability, error, and expiry visible before a rule is armed.
- Provide a global disarm action that restores normal behavior immediately.
- Preserve current SQS behavior and existing admin-route compatibility during migration.

## 5. Non-goals

- Persisting disruption rules across Tarn restarts.
- Simulating AWS-wide regional incidents.
- Enforcing IAM permissions for disruption management.
- Network packet loss, DNS poisoning, bandwidth limits, or host-level network shaping.
- Mutating or corrupting stored resource data.
- Killing ECS tasks, changing ECS desired counts, or interrupting Step Functions executions in the first release.
- Injecting response latency in the first release.
- Coordinated multi-step fault scenarios or scheduled experiments.
- Production or remotely hosted chaos management.

### 5.1 Success measures

- A developer who knows the target resource can arm a supported disruption in under 30 seconds.
- Every MVP service has automated healthy, disrupted, and recovered coverage through both direct calls and at least one internal Tarn call where such a path exists.
- A 100 percent error rule produces no unintended resource mutation in service tests.
- **Disarm all** completes in under one second with 1,000 rules and prevents disruption decisions for calls started after the response.
- Existing SQS clients and tests require no changes during the compatibility period.

## 6. Terminology

| Term | Meaning |
|---|---|
| Disruption rule | An account-local instruction that may alter calls matching one resource operation. |
| Target | A service and resource pair, such as Lambda function `resize-image`. |
| Operation | The service method affected by the rule, such as `Invoke` or `PutItem`. |
| Effect | The failure returned when the rule fires. The MVP supports returned errors only. |
| Probability | The percentage chance that an otherwise valid matching call is disrupted. |
| Armed | A rule that is enabled, unexpired, and has a probability greater than zero. |
| Expiry | The time after which Tarn stops applying the rule automatically. |
| Disarm | Disable or remove a rule so matching calls proceed normally. |
| Blast radius | The exact set of account, resources, operations, and calls affected by armed rules. |

The UI and documentation should use "disruption" for injected infrastructure failures. "Chaos probe" remains the name of the API Gateway input-probing workflow.

## 7. MVP scope

### 7.1 Service support

| Service | Target | Operations | Initial effects |
|---|---|---|---|
| SQS | Queue | `SendMessage` | Existing `ServiceUnavailable`, `InternalError`, `Throttling`, and `OverLimit` behavior. |
| SNS | Topic | `Publish` | Retriable internal failure, service unavailable, and throttling. `PublishBatch` must report failures per entry. |
| Lambda | Function | `Invoke` | Service error, too many requests, and resource not ready. |
| DynamoDB | Table | `GetItem`, `PutItem`, `UpdateItem`, `DeleteItem`, `Query`, and `Scan` | Internal error, throttling, and provisioned-throughput failure. |

The implementation must define an error catalog for each service. The UI may only offer combinations that the corresponding adapter can render through the AWS-compatible protocol.

### 7.2 Existing SQS behavior

The migration must preserve these behaviors:

- Rules can target one or many queues.
- Each send is evaluated independently.
- A failed send writes no message.
- Batch sends can contain successful and failed entries.
- SNS fanout, API Gateway integrations, and DLQ sends pass through the same SQS rule evaluation.
- Existing `/_tarn/admin/sqs/disruptor` callers continue to work for at least one release cycle.

### 7.3 Rule lifetime

- A new rule defaults to a 15-minute expiry.
- The UI offers 5, 15, 30, and 60 minutes, plus "until restart".
- An expired rule stops affecting calls without requiring dashboard activity.
- Tarn may retain expired rules briefly for display, but they must not be reported as armed.
- No disruption rule is written to the account data directory.
- Restarting Tarn clears all rules.

## 8. User journeys

### 8.1 Arm one disruption

1. The user opens **Disruptions** under **Tools**.
2. The user selects a service, resource, and operation.
3. The user chooses an error, probability, and expiry.
4. The UI shows a plain-language preview of the blast radius.
5. The user arms the rule.
6. The rule appears immediately in the active-rules ledger.
7. Matching requests begin receiving the selected AWS-shaped failure.

### 8.2 Target several resources

1. The user filters resources within one service.
2. The user selects several resources that support the same operation and effect.
3. The user arms the disruption once.
4. Tarn validates every target before changing any rule.
5. The ledger shows one row per resulting rule so each target can be disarmed independently.

### 8.3 Recover the environment

1. The user opens the active-rules ledger.
2. The user disarms one rule or selects **Disarm all**.
3. Tarn stops applying the selected rules before returning success.
4. The ledger updates immediately and the normal dashboard refresh confirms the state.

### 8.4 Follow a resource link

1. A resource detail view shows that the resource is disrupted.
2. The user follows the status link to the Disruptions section.
3. The matching rule is selected and visible without further filtering.

## 9. Dashboard requirements

### 9.1 Navigation

- Add **Disruptions** as a separate item under **Tools**.
- Keep the existing **Chaos** item and route-probing behavior unchanged.
- The Disruptions item shows the number of armed rules when the count is greater than zero.
- Deep links use `#disruptions` and may include a rule or target query parameter.

A future information-architecture change may group Chaos and Disruptions under a Reliability area. That rename is outside this PRD.

### 9.2 Layout

The page uses continuous panes and rows rather than a grid of service cards:

1. A status strip shows the active-rule count, affected services, nearest expiry, and **Disarm all**.
2. An active-rules ledger lists every armed rule.
3. A split target explorer lists services and resources on the left and the rule composer on the right.

The active-rules ledger is the page's signature element. A user should be able to read the blast radius by scanning its columns:

| Column | Content |
|---|---|
| State | Armed, expiring soon, expired, or off. |
| Target | Service icon, resource name, and account context. |
| Operation | The affected service operation. |
| Effect | AWS error code and HTTP status. |
| Rate | Probability using tabular numbers. |
| Expires | Remaining time or "restart". |
| Actions | Edit and disarm. |

### 9.3 Rule composer

- Service selection filters the resource list and available operations.
- Operation selection filters the available effects.
- Probability accepts integer values from 1 through 100.
- Expiry defaults to 15 minutes.
- The arm action stays disabled until every required value is valid.
- The preview names the target count, operation, error, probability, and expiry.
- Arming a 100 percent rule or more than one target requires an explicit confirmation step.
- The UI reports validation and network errors without losing the composed rule.
- The UI exposes loading, empty, error, disabled, hover, active, and keyboard-focus states.

### 9.4 Resource views

- Queue, topic, function, and table detail views show an "armed" status when any rule targets that resource.
- The status links to the matching rule in the Disruptions section.
- Remove queue-specific rule editing and bulk-target selection after the new section reaches parity.
- Keep the queue status pill so existing queue inspection still exposes disruption state.

### 9.5 Visual direction

Domain concepts include blast radius, arming, targets, operations, expiry, recovery, retries, and circuit breaking.

The color world comes from a local infrastructure rack: charcoal console surfaces, steel dividers, amber warning lamps, red armed indicators, green recovery signals, and blue trace links. Color communicates state only. It does not decorate service groups.

The interface rejects these common defaults:

- Service cards are replaced by a searchable resource ledger and split explorer.
- A generic settings form is replaced by an operation-aware composer.
- Hidden per-resource controls are replaced by one active-rule ledger with deep links back to resource views.

Use the existing Tarn typography, spacing, rack controls, and border-led depth. Do not introduce a separate visual theme.

## 10. Functional requirements

### 10.1 Shared disruption module

Create one account-local disruption module with a small interface for rule management and call evaluation. Its implementation owns:

- Thread-safe in-memory rule storage.
- Target matching.
- Probability evaluation.
- Expiry evaluation.
- Stable rule ordering for dashboard responses.
- Rule validation shared by the admin handler and service adapters.

Service adapters call the module at their existing service method seams. The module returns a decision. The adapter translates that decision into its service's native error type.

The evaluator must not depend on HTTP request objects. Internal calls such as SNS to SQS, EventBridge to Lambda, S3 to Lambda, and Step Functions to Lambda must receive the same disruption behavior as direct SDK calls.

### 10.2 Rule model

A rule contains:

```text
id
service
resource kind
resource identifier
operation
effect kind
service error code
HTTP status
message
probability
enabled
created at
expires at, or until restart
```

The external type must represent effects as explicit variants. Future latency or delivery-drop effects must not be added as unrelated optional fields on the error effect.

Only one rule may exist for a service, resource, and operation tuple. Updating that tuple replaces its configuration while retaining its rule ID, creation time, and deep link.

### 10.3 Evaluation order

For an otherwise valid service call:

1. Resolve the resource and validate the request enough to identify the operation and target.
2. Ask the disruption module for a decision.
3. If the rule fires, return the configured service-native failure without changing stored resource data.
4. If the rule does not fire, continue through the existing implementation.

Dry-run or validation-only operations must not be disrupted unless the rule explicitly supports them.

### 10.4 Bulk behavior

- The admin handler validates all requested targets before applying any changes.
- A failed validation changes no rules.
- Batch service operations preserve their protocol semantics. A matching rule may fail individual entries when the protocol supports per-entry results.
- The evaluator makes an independent probability decision for each eligible item or call.

### 10.5 Account isolation

- Each account bundle owns one disruption module.
- Rules created under one account ID are invisible to all other accounts.
- Switching accounts in the dashboard refreshes the rule ledger and available targets.
- **Disarm all** affects only the active account.

## 11. Admin HTTP interface

Introduce an account-resolved interface:

| Method and path | Behavior |
|---|---|
| `GET /_tarn/admin/disruptions` | List rules and supported service capabilities. |
| `PUT /_tarn/admin/disruptions` | Create or replace rules for one or more targets. |
| `DELETE /_tarn/admin/disruptions/{id}` | Remove one rule. |
| `DELETE /_tarn/admin/disruptions?all=true` | Remove all rules for the active account. |

The list response includes capabilities so the dashboard does not hard-code service error catalogs:

```json
{
  "rules": [],
  "capabilities": [
    {
      "service": "lambda",
      "resourceKind": "function",
      "operations": [
        {
          "name": "Invoke",
          "effects": [
            { "kind": "error", "code": "TooManyRequestsException", "httpStatus": 429 }
          ]
        }
      ]
    }
  ]
}
```

The mutation request accepts one operation and effect with several compatible targets. The response returns one rule per target.

The existing SQS admin routes remain available as compatibility adapters. They read and write the shared rule store and return the existing payload shape.

## 12. Observability

- Log rule creation, update, expiry, and removal with account, rule ID, service, target, and operation.
- Log each injected failure at debug level or through a bounded aggregate so a 100 percent rule cannot flood normal logs.
- Record an `injected` marker in traces when a disrupted call already participates in Tarn tracing.
- The dashboard should show a per-rule injected-call count when it can be maintained without unbounded memory.
- Metrics and traces must never expose secret values, request bodies, or message payloads through rule metadata.

Injected-call counts are useful but not required to ship the first vertical slice. The rule state and trace marker are required.

## 13. Safety requirements

- Rules are disabled by restart and are never persisted.
- New rules expire after 15 minutes unless the user chooses another duration.
- **Disarm all** remains visible whenever any rule is armed.
- The UI confirms rules with a 100 percent probability or several targets.
- Mutation requests validate every target before changing state.
- An invalid or unsupported error code cannot fall back silently to a different error.
- Rule evaluation must never mutate payloads or stored resources.
- Deleting a resource removes or deactivates its disruption rules.
- The admin interface must not permit wildcard account targets.

## 14. Performance and reliability

- Rule evaluation adds less than 1 millisecond at the 95th percentile for a local call with up to 1,000 rules in the account.
- Reads and evaluations remain safe under concurrent service calls and dashboard mutations.
- A dashboard polling failure does not alter active rules.
- Expiry does not require the dashboard to be open.
- Rule ordering remains stable across repeated list calls.
- A stopped or nil optional service does not prevent other services' rules from being listed or disarmed.

## 15. Acceptance criteria

### 15.1 Shared behavior

- A user can create, edit, inspect, and remove rules from the Disruptions section.
- The navigation count matches the number of armed rules for the active account.
- A 100 percent rule fails every eligible call with the selected AWS code and status.
- A zero-rule environment behaves exactly as it did before this feature.
- A rule expires without a page refresh and no longer affects calls.
- Restarting Tarn clears every rule.
- **Disarm all** prevents disruption decisions for calls started after its response returns.
- Rules do not cross account IDs.

### 15.2 SQS

- Existing SQS disruptor tests pass against the shared module.
- Direct sends, SNS fanout, API Gateway integration sends, and DLQ sends use the same rule.
- Failed sends do not persist messages.
- XML and JSON protocol clients receive the configured error.
- Batch sends retain per-entry failure behavior.
- Existing SQS admin endpoints remain compatible.

### 15.3 SNS

- A topic rule affects direct `Publish` and each eligible `PublishBatch` entry.
- Failed publishes do not fan out to any subscription.
- Batch responses distinguish successful and failed entries.

### 15.4 Lambda

- A function rule affects direct invokes and invokes originating from other Tarn services.
- A failed invoke does not start or acquire a Lambda container.
- The caller receives the selected Lambda error code and status.
- Existing invocation tracing marks the call as injected.

### 15.5 DynamoDB

- A table rule can target any supported operation exposed by the capability catalog.
- Failed write operations do not alter items or stream records.
- Failed reads return no data.
- The DynamoDB JSON response contains the selected error type and status.

### 15.6 Dashboard quality

- The page is usable at desktop and narrow viewport widths.
- Every control is keyboard accessible and has a visible focus state.
- Loading, empty, error, expired, and unavailable-service states are implemented.
- Deep links select the intended target or rule.
- The page uses the current Tarn design tokens and introduces no service-card grid.

## 16. Testing requirements

- Unit tests cover rule validation, matching, probability bounds, expiry, stable ordering, concurrent access, and account isolation.
- Each service adapter has tests for every exposed effect and verifies that failed calls produce no side effects.
- Handler tests cover list, bulk validation, atomic mutation, single disarm, and account-local disarm-all behavior.
- Compatibility tests cover the existing SQS admin routes and payloads.
- UI checks cover rule composition, validation, bulk selection, editing, disarming, expiry display, deep links, and account switching.
- At least one end-to-end test drives an application through healthy, disrupted, and recovered phases for each MVP service.
- The existing ECS publisher to SQS end-to-end test continues to pass.

## 17. Delivery plan

| Phase | Deliverable | Estimate |
|---|---|---|
| 0 | Shared rule module, account wiring, capability model, admin interface, and unit tests. | 1 to 2 days |
| 1 | Migrate SQS, retain compatibility routes, and preserve existing tests. | 1 day |
| 2 | Add the Disruptions section, active-rules ledger, composer, deep links, and resource status links. Remove duplicate queue editing after parity. | 2 to 3 days |
| 3 | Add SNS and Lambda adapters with protocol and end-to-end tests. | 2 to 3 days |
| 4 | Add DynamoDB operation targeting and tests. | 1 to 2 days |
| 5 | Documentation, accessibility pass, narrow-layout verification, and release notes. | 1 day |

Expected total for one engineer is 8 to 12 working days. ECS lifecycle faults, S3 operation failures, latency effects, and saved experiments should be estimated separately.

## 18. Risks and mitigations

| Risk | Mitigation |
|---|---|
| A generic rule becomes too weak for service-specific behavior. | Keep matching and lifetime generic, but translate decisions through service-specific adapters and capability catalogs. |
| Injection at the HTTP layer misses internal calls. | Evaluate rules inside service methods before side effects. |
| A developer forgets an armed rule. | Default expiry, persistent navigation count, visible disarm-all action, and in-memory-only state. |
| Bulk creation leaves partial state after validation fails. | Validate every target and effect before acquiring the write lock and applying changes. |
| High-rate rules flood logs. | Aggregate or sample injection logs and expose bounded counters. |
| Existing SQS callers break during migration. | Keep the old routes and payload shapes as adapters for at least one release cycle. |
| "Chaos" and "Disruptions" confuse users. | Keep distinct navigation labels and descriptions. Chaos probes inputs; Disruptions inject infrastructure failures. |

## 19. Deferred extensions

- S3 `GetObject`, `PutObject`, and delete failures.
- EventBridge event acceptance versus target-delivery failures.
- ECS task-launch failure, forced task stop, and unhealthy-container simulation.
- Step Functions task-state failures scoped to a state machine or execution.
- Fixed and ranged latency effects.
- Drop or black-hole effects for asynchronous delivery.
- A finite hit count, such as "fail the next three calls".
- Scheduled scenarios and reusable experiment templates.
- Import and export of rule sets.
- A disruption timeline correlated with traces and logs.

## 20. Open decisions

- Whether the first release exposes grouped DynamoDB operations such as "all writes" or requires one explicit operation per rule.
- Whether expired rules remain visible in a short history or disappear immediately.
- Whether injected-call counts belong in the first release or the first follow-up.
- How long the legacy SQS admin routes remain supported after the new interface ships.
- Whether a future Reliability navigation group should contain Chaos, Disruptions, and related diagnostics.
