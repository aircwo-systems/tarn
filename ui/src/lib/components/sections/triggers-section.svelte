<script lang="ts">
  import {
    ArrowsClockwise,
    Bell,
    Bridge,
    BridgeIcon,
    ChatCircle,
    GlobeHemisphereWest,
    Lightning,
    StackIcon,
  } from "phosphor-svelte";
  import { TableCell, TableRow } from "$lib/components/ui/table";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import ResourceTable from "$lib/components/common/resource-table.svelte";
  import SectionHeader from "./section-header.svelte";
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

  <ResourceTable
    title="Trigger Mappings"
    count={triggerRows.length}
    loading={dashboard.loading && !dashboard.data}
    empty={triggerRows.length === 0}
    emptyMessage="No triggers configured yet."
    emptyIcon={ArrowsClockwise}
    columns={["Type", "Source", "Target", "State", "Details"]}
  >
    {#each triggerRows as trigger}
      <TableRow>
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
          <div class="space-y-0.5">
            <p class="text-xs text-muted-foreground">{trigger.detail}</p>
            {#if trigger.lastResult}
              <p class="font-mono text-[11px] text-muted-foreground/70">
                {trigger.lastResult}
              </p>
            {/if}
          </div>
        </TableCell>
      </TableRow>
    {/each}
  </ResourceTable>
</div>
