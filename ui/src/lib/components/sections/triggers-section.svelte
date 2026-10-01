<script lang="ts">
  import { matchesResourceType } from "$lib/filter-utils";
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon, SidebarSimpleIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import TriggerList, { type TriggerRow, triggerStateTone } from "$lib/components/triggers/trigger-list.svelte";
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
  const data = $derived(matchesResourceType("trigger", filters.tagFilter) ? dashboard.data : null);

  const gateways = $derived(
    (data?.gateways ?? []).filter((gateway) =>
      matchesTagFilter(gateway.tags, filters.tagFilter),
    ),
  );
  const functions = $derived(
    (data?.functions ?? []).filter((fn) =>
      matchesTagFilter(fn.tags, filters.tagFilter),
    ),
  );
  const queues = $derived(
    (data?.queues ?? []).filter((queue) =>
      matchesTagFilter(queue.tags, filters.tagFilter),
    ),
  );
  const topics = $derived(
    (data?.topics ?? []).filter((topic) =>
      matchesTagFilter(topic.tags, filters.tagFilter),
    ),
  );
  const subscriptions = $derived(data?.subscriptions ?? []);
  const mappings = $derived(data?.eventSourceMappings ?? []);
  const eventBridgeRules = $derived(data?.eventBridgeRules ?? []);
  const connections = $derived(data?.connections ?? []);

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

  let listCollapsed = $state(false);

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-triggers-list-collapsed", String(listCollapsed));
    } catch {}
  }

  let query = $state("");

  const visibleTriggers = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const base = typeFilter === "ALL" ? triggerRows : triggerRows.filter((t) => t.type === typeFilter);
    if (!q) return base;
    return base.filter((trigger) =>
      `${trigger.sourceName} ${trigger.targetName} ${trigger.type} ${trigger.state}`
        .toLowerCase()
        .includes(q),
    );
  });

  // Keyed by id: polling replaces the objects, so holding one would freeze the panel.
  let selectedTriggerID = $state<string | null>(null);
  const selectedTrigger = $derived(
    visibleTriggers.find((trigger) => trigger.id === selectedTriggerID) ??
      visibleTriggers[0] ??
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

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (visibleTriggers.length === 0) return;
    e.preventDefault();
    const idx = visibleTriggers.findIndex((t) => t.id === selectedTrigger?.id);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(visibleTriggers.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visibleTriggers[next].id);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const id = qs ? new URLSearchParams(qs).get("id") : null;
    if (id) selectedTriggerID = id;
    try {
      const saved = localStorage.getItem("tarn-triggers-list-collapsed");
      if (saved !== null) {
        listCollapsed = saved === "true";
      }
    } catch {}
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
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input
          placeholder="Filter triggers..."
          bind:value={query}
          aria-label="Filter triggers"
        />
        {#if query}
          <button
            type="button"
            class="clear-query-btn"
            onclick={() => (query = "")}
            aria-label="Clear filter"
          >
            &times;
          </button>
        {/if}
      </div>
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
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="Triggers overview">
          <div class="toolbar-leading">
            <button
              type="button"
              class="expand-list-btn"
              onclick={toggleListCollapse}
              title="Expand trigger list"
              aria-label="Expand trigger list"
            >
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">Trigger list</span>
              <span class="count-badge">{triggerRows.length}</span>
            </button>

            {#if sidebarCollapsed}
              <span class="toolbar-divider" aria-hidden="true"></span>
              <div class="toolbar-stats">
                <span class="toolbar-stat" title="{sqsCount} SQS triggers">
                  <span class="status-dot green"></span>
                  <span>{sqsCount} sqs</span>
                </span>
                <span class="toolbar-stat" title="{snsCount} SNS triggers">
                  <span class="status-dot red"></span>
                  <span>{snsCount} sns</span>
                </span>
                <span class="toolbar-stat" title="{dynamodbCount} DynamoDB triggers">
                  <span class="status-dot purple"></span>
                  <span>{dynamodbCount} ddb</span>
                </span>
                <span class="toolbar-stat" title="{apiCount} API triggers">
                  <span class="status-dot blue"></span>
                  <span>{apiCount} api</span>
                </span>
              </div>
            {/if}
          </div>

          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="Trigger switcher" onkeydown={onKeydown}>
              {#each visibleTriggers as trigger (trigger.id)}
                {@const tone = triggerStateTone(trigger.state)}
                <button
                  type="button"
                  role="tab"
                  class="item-chip"
                  class:selected={trigger.id === selectedTrigger?.id}
                  aria-selected={trigger.id === selectedTrigger?.id}
                  onclick={() => select(trigger.id)}
                  title="{trigger.sourceName} → {trigger.targetName} ({trigger.type} · {trigger.state})"
                >
                  <span
                    class="chip-dot"
                    style:background={tone === "green" ? "var(--accent-green, #10b981)" : tone === "amber" ? "var(--accent-amber, #f59e0b)" : tone === "red" ? "var(--accent-red, #fb7185)" : "var(--text-tertiary)"}
                  ></span>
                  <span class="chip-name">{trigger.sourceName} → {trigger.targetName}</span>
                  <span class="chip-badge">{trigger.type}</span>
                </button>
              {:else}
                <span class="chips-none">No match for "{query}"</span>
              {/each}
            </div>
          </div>
        </div>
      {/if}

      {#if !listCollapsed}
        <RcResizableAside
          storageKey="tarn-triggers-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
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
            triggers={visibleTriggers}
            selectedId={selectedTrigger?.id ?? null}
            onselect={select}
            bind:query
            onToggleCollapse={toggleListCollapse}
          />
        </RcResizableAside>
      {/if}

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
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-tertiary);
    background: #ffffff;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
  }
  .filter span { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }

  :global(.dark) .filter {
    background: var(--bg-element);
    box-shadow: none;
  }

  .type-filter {
    display: flex; gap: 4px; padding: 3px; border-radius: 8px;
    background: var(--bg-element); border: 1px solid var(--border-subtle);
    margin-bottom: 8px;
  }
  .type-filter button {
    flex: 1; display: inline-flex; align-items: center; justify-content: center; gap: 5px;
    height: 26px; border: 0; border-radius: 6px; background: transparent;
    font-size: 11px; font-weight: 500; color: var(--text-secondary); cursor: pointer;
    transition: background 120ms ease, color 120ms ease, box-shadow 120ms ease;
  }
  .type-filter button.active {
    background: #ffffff; color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  }
  .type-filter button span {
    font-size: 10px; font-variant-numeric: tabular-nums; opacity: 0.7;
  }

  :global(.dark) .type-filter button.active {
    background: var(--bg-surface, #1e1e24);
    box-shadow: none;
  }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; width: 260px; }
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
