<script lang="ts">
  import {
    ArrowsClockwise,
    Bell,
    BridgeIcon,
    ChatCircle,
    GlobeHemisphereWest,
    Lightning,
    StackIcon,
    XIcon,
  } from "phosphor-svelte";
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from "$lib/components/ui/table";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import SectionHeader from "./section-header.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
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

  type TriggerRow = {
    id: string;
    type: "SQS" | "SNS" | "API" | "EVENTBRIDGE" | "DYNAMODB";
    sourceName: string;
    sourceArn: string;
    targetName: string;
    targetArn: string;
    state: string;
    detail: string;
    detailLabel: string;
    detailFields: Array<{ label: string; value: string }>;
    lastResult?: string;
  };

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
  let selectedTriggerID = $state<string | null>(null);

  $effect(() => {
    if (selectedTriggerID && !triggerRows.some((trigger) => trigger.id === selectedTriggerID)) {
      selectedTriggerID = null;
    }
  });

  const selectedTrigger = $derived(
    triggerRows.find((trigger) => trigger.id === selectedTriggerID) ?? null,
  );
  const selectedTriggerPayload = $derived(
    selectedTrigger?.lastResult ? formatJSONForViewer(selectedTrigger.lastResult) : null,
  );

  function stateColor(state: string): "green" | "amber" | "red" | "gray" {
    const normalized = state.toLowerCase();
    if (normalized === "enabled" || normalized === "active" || normalized === "configured")
      return "green";
    if (normalized === "creating" || normalized === "updating" || normalized === "pending")
      return "amber";
    if (normalized.includes("fail") || normalized === "disabled")
      return "red";
    return "gray";
  }

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

  function selectTrigger(id: string) {
    selectedTriggerID = selectedTriggerID === id ? null : id;
  }
</script>

<div class="space-y-4">
  <SectionHeader
    title="Triggers"
    description="Event sources wired to functions and APIs."
    icon={ArrowsClockwise}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet actions()}
      <div class="flex items-center gap-4 text-xs text-muted-foreground font-mono">
      <span class="inline-flex items-center gap-1.5">
        <ChatCircle size={12} class="text-amber" />
        {sqsCount} SQS
      </span>
      <span class="inline-flex items-center gap-1.5">
        <Bell size={12} class="text-primary" />
        {snsCount} SNS
      </span>
      <span class="inline-flex items-center gap-1.5">
        <StackIcon size={12} class="text-[var(--topology-dynamodb)]" />
        {dynamodbCount} DDB
      </span>
      <span class="inline-flex items-center gap-1.5">
        <BridgeIcon size={12} class="text-blue" />
        {eventBridgeCount} EB
      </span>
      <span class="inline-flex items-center gap-1.5">
        <GlobeHemisphereWest size={12} class="text-blue" />
        {apiCount} API
      </span>
      </div>
    {/snippet}
  </SectionHeader>

  <div class="overflow-hidden rounded-lg border border-border/70 bg-background/50">
    <div class="relative min-h-0" style="height: calc(100vh - 10rem);">
      <div class="min-h-0">
        <div class="flex items-center justify-between border-b border-border/70 px-4 py-3">
          <div>
            <h3 class="text-sm font-semibold text-foreground">Trigger Mappings</h3>
            <p class="mt-1 text-[11px] text-muted-foreground/70">
              Select a mapping row to inspect its source, target, and payload details.
            </p>
          </div>
          <span class="text-xs text-muted-foreground/70 font-mono">{triggerRows.length} items</span>
        </div>

        {#if dashboard.loading && !dashboard.data}
          <div class="space-y-2 p-4">
            {#each Array(7) as _, index (index)}
              <Skeleton class="h-11 w-full" />
            {/each}
          </div>
        {:else if triggerRows.length === 0}
          <div class="flex min-h-[18rem] items-center justify-center text-sm text-muted-foreground/70">
            No triggers configured yet.
          </div>
        {:else}
          <div class="overflow-auto">
            <Table>
              <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
                <TableRow class="hover:bg-transparent">
                  <TableHead>Type</TableHead>
                  <TableHead>Source</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Details</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each triggerRows as trigger}
                  <TableRow
                    class={`cursor-pointer ${trigger.id === selectedTriggerID ? "bg-muted/50" : ""}`}
                    role="button"
                    tabindex={0}
                    onclick={() => selectTrigger(trigger.id)}
                    onkeydown={(event: KeyboardEvent) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        selectTrigger(trigger.id);
                      }
                    }}
                  >
                    <TableCell class="font-mono text-xs text-muted-foreground">
                      {trigger.type}
                    </TableCell>
                    <TableCell>
                      <ArnCell name={trigger.sourceName} arn={trigger.sourceArn} />
                    </TableCell>
                    <TableCell>
                      {#if trigger.type === "SQS" || trigger.type === "SNS" || trigger.type === "EVENTBRIDGE"}
                        <div class="flex items-start gap-2">
                          <Lightning size={13} class="mt-[2px] text-primary" />
                          <ArnCell name={trigger.targetName} arn={trigger.targetArn} />
                        </div>
                      {:else}
                        <div class="space-y-0.5 max-w-[22rem]">
                          <p class="text-xs font-medium text-foreground">
                            {trigger.targetName}
                          </p>
                          <p class="font-mono text-[11px] text-muted-foreground/70">
                            {trigger.targetArn}
                          </p>
                        </div>
                      {/if}
                    </TableCell>
                    <TableCell>
                      <span class="inline-flex items-center gap-1.5 text-xs">
                        <LedDot color={stateColor(trigger.state)} />
                        <span class="text-muted-foreground">{trigger.state}</span>
                      </span>
                    </TableCell>
                    <TableCell>
                      <div class="space-y-1">
                        <p class="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground/60">
                          {trigger.detailLabel}
                        </p>
                        <p class="text-xs text-muted-foreground">{trigger.detail}</p>
                      </div>
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          </div>
        {/if}
      </div>

      <div
        class="absolute inset-y-0 right-0 z-10 overflow-hidden border-l border-border bg-card shadow-xl transition-[width,opacity] duration-200 ease-out {selectedTrigger ? 'opacity-100' : 'pointer-events-none opacity-0'}"
        style="width: {selectedTrigger ? '420px' : '0px'}"
      >
        {#if selectedTrigger}
          <div class="flex h-full min-w-[420px] flex-col">
            <div class="flex items-center justify-between gap-3 border-b border-border px-4 py-2.5 shrink-0 bg-card">
              <div class="min-w-0">
                <p class="truncate font-mono text-sm text-foreground">
                  {selectedTrigger.sourceName} → {selectedTrigger.targetName}
                </p>
                <p class="mt-1 text-[11px] text-muted-foreground/70">
                  {selectedTrigger.type} mapping
                </p>
              </div>
              <button
                type="button"
                onclick={() => (selectedTriggerID = null)}
                class="flex h-6 w-6 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:bg-muted hover:text-foreground shrink-0"
                aria-label="Close detail panel"
              >
                <XIcon size={14} />
              </button>
            </div>

            <div class="flex-1 overflow-y-auto p-4 space-y-4">
              <div>
                <p class="mb-2 text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Summary</p>
                <div class="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                  <div>
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Type</p>
                    <p class="mt-1 font-mono text-foreground">{selectedTrigger.type}</p>
                  </div>
                  <div>
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">State</p>
                    <p class="mt-1 font-mono text-foreground">{selectedTrigger.state}</p>
                  </div>
                  <div class="col-span-2">
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Source ARN</p>
                    <p class="mt-1 break-all font-mono text-foreground">{selectedTrigger.sourceArn}</p>
                  </div>
                  <div class="col-span-2">
                    <p class="text-[10px] uppercase tracking-[0.2em] text-muted-foreground/55">Target ARN</p>
                    <p class="mt-1 break-all font-mono text-foreground">{selectedTrigger.targetArn}</p>
                  </div>
                </div>
              </div>

              <div>
                <p class="mb-2 text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Mapping Details</p>
                <div class="rounded-md border border-border overflow-hidden divide-y divide-border/60">
                  {#each selectedTrigger.detailFields as field (field.label)}
                    <div class="flex items-start gap-4 px-3 py-2">
                      <span class="w-24 shrink-0 pt-px text-[10px] uppercase tracking-wider text-muted-foreground/70">
                        {field.label}
                      </span>
                      <span class="break-all font-mono text-[12px] leading-snug text-muted-foreground">
                        {field.value}
                      </span>
                    </div>
                  {/each}
                </div>
              </div>

              <div>
                <p class="mb-2 text-[10px] uppercase tracking-[0.24em] text-muted-foreground/55">Payload</p>
                {#if selectedTrigger.lastResult}
                  {#if selectedTriggerPayload}
                    <FormattedMessageViewer
                      raw={selectedTrigger.lastResult}
                      formatted={selectedTriggerPayload.formatted}
                      formattedHtml={selectedTriggerPayload.formattedHtml}
                      formattedLabel="Formatted"
                      rawLabel="Raw"
                      formattedOpenByDefault={true}
                      rawOpenByDefault={false}
                      formattedContentClass="text-[11px] text-foreground"
                      rawContentClass="text-[11px] text-muted-foreground"
                      formattedMaxHeightClass="max-h-[24rem]"
                      rawMaxHeightClass="max-h-[20rem]"
                    />
                  {:else}
                    <pre
                      class="max-h-[24rem] overflow-y-auto rounded-md border border-border bg-[var(--code-bg)] px-3 py-3 font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-all"
                    >{selectedTrigger.lastResult}</pre>
                  {/if}
                {:else}
                  <p class="text-xs text-muted-foreground/70">
                    No payload or result details were captured for this trigger.
                  </p>
                {/if}
              </div>
            </div>
          </div>
        {/if}
      </div>
    </div>
  </div>
</div>
