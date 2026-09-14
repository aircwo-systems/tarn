<script lang="ts">
  import { onMount } from "svelte";
  import SectionHeader from "./section-header.svelte";
  import TriggerList, { type TriggerRow } from "$lib/components/triggers/trigger-list.svelte";
  import TriggerDetail from "$lib/components/triggers/trigger-detail.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();

  const gateways = $derived(
    (dashboard.data?.gateways ?? []).filter((gateway) =>
      matchesTagFilter(gateway.tags, filters.tagFilter),
    ),
  );
  const functions = $derived(
    (dashboard.data?.functions ?? []).filter((fn) =>
      matchesTagFilter(fn.tags, filters.tagFilter),
    ),
  );
  const queues = $derived(
    (dashboard.data?.queues ?? []).filter((queue) =>
      matchesTagFilter(queue.tags, filters.tagFilter),
    ),
  );
  const topics = $derived(
    (dashboard.data?.topics ?? []).filter((topic) =>
      matchesTagFilter(topic.tags, filters.tagFilter),
    ),
  );
  const subscriptions = $derived(dashboard.data?.subscriptions ?? []);
  const mappings = $derived(dashboard.data?.eventSourceMappings ?? []);
  const eventBridgeRules = $derived(dashboard.data?.eventBridgeRules ?? []);
  const connections = $derived(dashboard.data?.connections ?? []);

  const functionsByName = $derived(
    new Map(functions.map((fn) => [fn.name, fn])),
  );
  const queuesByName = $derived(
    new Map(queues.map((queue) => [queue.name, queue])),
  );
  const topicsByName = $derived(
    new Map(topics.map((topic) => [topic.name, topic])),
  );
  const gatewaysByID = $derived(
    new Map(gateways.map((gateway) => [gateway.apiId, gateway])),
  );

  const filteredMappings = $derived(
    mappings.filter((mapping) => {
      if (!filters.tagFilter.trim()) {
        return true;
      }
      return (
        functionsByName.has(mapping.functionName) ||
        queuesByName.has(mapping.queueName) ||
        (mapping.sourceType === "dynamodb-stream" &&
          (mapping.sourceName ?? "").length > 0)
      );
    }),
  );

  const sqsTriggers = $derived<TriggerRow[]>(
    filteredMappings
      .filter((mapping) => (mapping.sourceType ?? "sqs") !== "dynamodb-stream")
      .map((mapping) => {
      const queue = queuesByName.get(mapping.queueName);
      const fn = functionsByName.get(mapping.functionName);
      return {
        id: mapping.uuid,
        type: "SQS",
        sourceName: mapping.queueName,
        sourceArn: queue?.arn ?? queue?.url ?? mapping.queueName,
        targetName: mapping.functionName,
        targetArn: fn?.arn ?? mapping.functionName,
        state: mapping.state,
        detail: `Batch ×${mapping.batchSize}`,
        detailLabel: "sqs",
        detailFields: [
          { label: "Source", value: mapping.queueName },
          { label: "Target", value: mapping.functionName },
          { label: "State", value: mapping.state },
          { label: "Batch Size", value: String(mapping.batchSize) },
          { label: "Mapping UUID", value: mapping.uuid },
          { label: "Source ARN", value: mapping.eventSourceArn ?? queue?.arn ?? queue?.url ?? "" },
        ],
        lastResult: mapping.lastResult,
      };
      }),
  );

  const dynamodbTriggers = $derived<TriggerRow[]>(
    filteredMappings
      .filter((mapping) => (mapping.sourceType ?? "") === "dynamodb-stream")
      .map((mapping) => {
        const fn = functionsByName.get(mapping.functionName);
        const sourceName = mapping.sourceName || mapping.queueName || mapping.eventSourceArn || "--";
        return {
          id: `dynamodb-${mapping.uuid}`,
          type: "DYNAMODB",
          sourceName,
          sourceArn: mapping.eventSourceArn ?? sourceName,
          targetName: mapping.functionName,
          targetArn: fn?.arn ?? mapping.functionName,
          state: mapping.state,
          detail: `Stream mapping · Batch ×${mapping.batchSize}`,
          detailLabel: "stream",
          detailFields: [
            { label: "Source", value: sourceName },
            { label: "Target", value: mapping.functionName },
            { label: "State", value: mapping.state },
            { label: "Batch Size", value: String(mapping.batchSize) },
            { label: "Mapping UUID", value: mapping.uuid },
            { label: "Stream ARN", value: mapping.eventSourceArn ?? "" },
          ],
          lastResult: mapping.lastResult,
        };
      }),
  );

  const filteredSubscriptions = $derived(
    subscriptions.filter((subscription) => {
      if (!filters.tagFilter.trim()) {
        return true;
      }
      return topicsByName.has(subscription.topicName);
    }),
  );

  const snsTriggers = $derived<TriggerRow[]>(
    filteredSubscriptions.map((subscription) => {
      const topic = topicsByName.get(subscription.topicName);
      const protocol = subscription.protocol.toLowerCase();
      const targetName =
        protocol === "lambda"
          ? lambdaNameFromEndpoint(subscription.endpoint)
          : protocol === "sqs"
            ? queueNameFromEndpoint(subscription.endpoint)
            : subscription.endpoint;
      const targetArn =
        protocol === "lambda"
          ? (functionsByName.get(targetName)?.arn ?? subscription.endpoint)
          : protocol === "sqs"
            ? (queuesByName.get(targetName)?.arn ??
              queuesByName.get(targetName)?.url ??
              subscription.endpoint)
            : subscription.endpoint;

      return {
        id: `sns-${subscription.subscriptionArn}`,
        type: "SNS",
        sourceName: subscription.topicName,
        sourceArn: topic?.arn ?? subscription.topicArn,
        targetName,
        targetArn,
        state: "Configured",
        detail: `${subscription.protocol.toUpperCase()} subscription${
          subscription.filterPolicy ? ` · filter` : ""
        }`,
        detailLabel: "sns",
        detailFields: [
          { label: "Topic", value: subscription.topicName },
          { label: "Protocol", value: subscription.protocol.toUpperCase() },
          { label: "Endpoint", value: subscription.endpoint },
          { label: "Raw Delivery", value: subscription.rawMessageDelivery ? "true" : "false" },
          { label: "Filter Scope", value: subscription.filterPolicyScope ?? "" },
          { label: "Subscription ARN", value: subscription.subscriptionArn },
        ],
        lastResult: subscription.filterPolicy,
      };
    }),
  );

  const apiTriggers = $derived<TriggerRow[]>(
    connections
      .filter(
        (connection) =>
          connection.targetKind === "apigw-lambda" ||
          connection.targetKind === "apigw-sqs",
      )
      .flatMap((connection) => {
        const gateway = gatewaysByID.get(connection.sourceFunction);
        if (!gateway) return [] as TriggerRow[];

        if (connection.targetKind === "apigw-lambda") {
          const targetName = connection.targetId || connection.targetName;
          const fn = functionsByName.get(targetName);
          return [
            {
              id: `api-${connection.source}-${targetName}`,
              type: "API",
              sourceName: gateway.name,
              sourceArn:
                gateway.apiEndpoint || gateway.invokeUrl || gateway.arn,
              targetName,
              targetArn: fn?.arn ?? targetName,
              state: "Configured",
              detail: `Integration AWS_PROXY · Stage ${gateway.defaultStage}`,
              detailLabel: "api",
              detailFields: [
                { label: "Gateway", value: gateway.name },
                { label: "Target", value: targetName },
                { label: "Integration", value: "AWS_PROXY" },
                { label: "Stage", value: gateway.defaultStage },
                { label: "Endpoint", value: gateway.apiEndpoint || gateway.invokeUrl || gateway.arn },
              ],
            },
          ];
        }

        const targetName = connection.targetId || connection.targetName;
        const queue = queuesByName.get(targetName);
        return [
          {
            id: `api-${connection.source}-${targetName}`,
            type: "API",
            sourceName: gateway.name,
            sourceArn: gateway.apiEndpoint || gateway.invokeUrl || gateway.arn,
            targetName,
            targetArn: queue?.arn ?? queue?.url ?? targetName,
            state: "Configured",
            detail: `Integration AWS(SQS) · Stage ${gateway.defaultStage}`,
            detailLabel: "api",
            detailFields: [
              { label: "Gateway", value: gateway.name },
              { label: "Target", value: targetName },
              { label: "Integration", value: "AWS(SQS)" },
              { label: "Stage", value: gateway.defaultStage },
              { label: "Endpoint", value: gateway.apiEndpoint || gateway.invokeUrl || gateway.arn },
            ],
          },
        ];
      }),
  );

  const eventBridgeTriggers = $derived<TriggerRow[]>(
    eventBridgeRules.flatMap((rule) => {
      const targets = rule.targets ?? [];
      if (targets.length === 0) return [] as TriggerRow[];
      if (filters.tagFilter.trim()) {
        const hasFilteredTarget = targets.some((target) =>
          functionsByName.has(lambdaNameFromEndpoint(target.arn)),
        );
        if (!hasFilteredTarget) return [] as TriggerRow[];
      }
      return targets.map((target) => {
        const targetName = lambdaNameFromEndpoint(target.arn);
        const fn = functionsByName.get(targetName);
        return {
          id: `eventbridge-${rule.name}-${target.id}`,
          type: "EVENTBRIDGE",
          sourceName: rule.name,
          sourceArn: rule.arn,
          targetName,
          targetArn: fn?.arn ?? target.arn,
          state: rule.state,
          detail: `${rule.scheduleExpression} · target ${target.id}`,
          detailLabel: "rule",
          detailFields: [
            { label: "Rule", value: rule.name },
            { label: "Target", value: targetName },
            { label: "State", value: rule.state },
            { label: "Schedule", value: rule.scheduleExpression },
            { label: "Target ID", value: target.id },
            { label: "Rule ARN", value: rule.arn },
          ],
          lastResult: target.lastResult ?? rule.lastResult,
        };
      });
    }),
  );

  const triggerRows = $derived<TriggerRow[]>([
    ...sqsTriggers,
    ...dynamodbTriggers,
    ...snsTriggers,
    ...eventBridgeTriggers,
    ...apiTriggers,
  ]);
  const sqsCount = $derived(sqsTriggers.length);
  const snsCount = $derived(snsTriggers.length);
  const dynamodbCount = $derived(dynamodbTriggers.length);
  const eventBridgeCount = $derived(eventBridgeTriggers.length);
  const apiCount = $derived(apiTriggers.length);

  type TypeFilter = "ALL" | TriggerRow["type"];
  let typeFilter = $state<TypeFilter>("ALL");

  const typeOptions = $derived<Array<{ id: TypeFilter; label: string; count: number }>>([
    { id: "ALL", label: "All", count: triggerRows.length },
    { id: "SQS", label: "SQS", count: sqsCount },
    { id: "SNS", label: "SNS", count: snsCount },
    { id: "DYNAMODB", label: "DDB", count: dynamodbCount },
    { id: "EVENTBRIDGE", label: "EB", count: eventBridgeCount },
    { id: "API", label: "API", count: apiCount },
  ]);

  const filteredTriggers = $derived(
    typeFilter === "ALL" ? triggerRows : triggerRows.filter((t) => t.type === typeFilter),
  );

  // Keyed by id: polling replaces the objects, so holding one would freeze the panel.
  let selectedTriggerID = $state<string | null>(null);
  const selectedTrigger = $derived(
    filteredTriggers.find((trigger) => trigger.id === selectedTriggerID) ??
      filteredTriggers[0] ??
      null,
  );

  function lambdaNameFromEndpoint(endpoint: string): string {
    const marker = ":function:";
    const index = endpoint.indexOf(marker);
    if (index < 0) return endpoint;
    const tail = endpoint.slice(index + marker.length);
    return tail.split(":")[0] || endpoint;
  }

  function queueNameFromEndpoint(endpoint: string): string {
    if (endpoint.startsWith("arn:aws:sqs:")) {
      const parts = endpoint.split(":");
      return parts[parts.length - 1] || endpoint;
    }
    const slash = endpoint.lastIndexOf("/");
    if (slash >= 0 && slash + 1 < endpoint.length) {
      return endpoint.slice(slash + 1);
    }
    return endpoint;
  }

  function select(id: string) {
    selectedTriggerID = id;
    history.replaceState(null, "", `#triggers?id=${encodeURIComponent(id)}`);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const id = qs ? new URLSearchParams(qs).get("id") : null;
    if (id) selectedTriggerID = id;
  });
</script>

<div class="triggers">
  <SectionHeader
    title="Triggers"
    description="{triggerRows.length} mappings · {sqsCount} sqs · {snsCount} sns · {dynamodbCount} ddb · {eventBridgeCount} eb · {apiCount} api"
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      {#if filters.tagFilter}
        <span class="filter" title={filters.tagFilter}>Tag <span>{filters.tagFilter}</span></span>
      {/if}
    {/snippet}
  </SectionHeader>

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(6) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if triggerRows.length === 0}
    <div class="blank">
      <h2>No triggers yet</h2>
      <p>Wire a queue, topic, rule or route to a function and the mapping appears here.</p>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-triggers-list-width">
        <div class="type-filter" role="group" aria-label="Trigger type filter">
          {#each typeOptions as option (option.id)}
            <button
              type="button"
              class:active={typeFilter === option.id}
              onclick={() => (typeFilter = option.id)}
              aria-pressed={typeFilter === option.id}
            >
              {option.label}
              <span>{option.count}</span>
            </button>
          {/each}
        </div>
        <TriggerList
          triggers={filteredTriggers}
          selectedId={selectedTrigger?.id ?? null}
          onselect={select}
        />
      </RcResizableAside>
      {#if selectedTrigger}
        {#key selectedTrigger.id}
          <TriggerDetail trigger={selectedTrigger} />
        {/key}
      {:else}
        <div class="blank">
          <h2>Nothing selected</h2>
          <p>No {typeFilter === "ALL" ? "" : `${typeFilter.toLowerCase()} `}triggers match. Pick another type.</p>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .triggers { display: flex; flex-direction: column; min-height: 100%; }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  .filter {
    display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-tertiary);
  }
  .filter span { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }

  .type-filter { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 8px; }
  .type-filter button {
    display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 10px;
    border-radius: 8px; border: 1px solid var(--border-subtle); font-size: 11.5px;
    color: var(--text-secondary); background: transparent;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .type-filter button:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .type-filter button:active { transform: scale(0.96); }
  .type-filter button.active {
    color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element);
  }
  .type-filter button span {
    font: 10.5px var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums;
    color: var(--text-tertiary);
  }
  .type-filter button.active span { color: var(--text-secondary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; }
  .skeleton-list span {
    height: 34px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) {
    .blank, .skeleton-list span { animation: none; }
  }
</style>
