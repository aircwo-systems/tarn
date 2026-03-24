<script lang="ts">
  import { BellIcon } from "phosphor-svelte";
  import { TableCell, TableRow } from "$lib/components/ui/table";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import ResourceTable from "$lib/components/common/resource-table.svelte";
  import { getDashboard, getDashboardFilters, matchesTagFilter } from "$lib/state.svelte";
  import { formatUnixSeconds } from "$lib/utils";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();

  const topics = $derived(
    (dashboard.data?.topics ?? []).filter((topic) =>
      matchesTagFilter(topic.tags, filters.tagFilter),
    ),
  );

  const topicNames = $derived(new Set(topics.map((topic) => topic.name)));
  const subscriptions = $derived(
    (dashboard.data?.subscriptions ?? []).filter((sub) => {
      if (!filters.tagFilter.trim()) return true;
      return topicNames.has(sub.topicName);
    }),
  );

  function protocolBadgeVariant(
    protocol: string,
  ): "default" | "secondary" | "amber" | "outline" {
    switch (protocol.toLowerCase()) {
      case "sqs":
        return "amber";
      case "lambda":
        return "default";
      default:
        return "secondary";
    }
  }

  function formatFilterPolicy(value: string): string | null {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return null;
    }
  }
</script>

<div class="space-y-4">
  <ResourceTable
    title="SNS Topics"
    count={topics.length}
    loading={dashboard.loading && !dashboard.data}
    empty={topics.length === 0}
    emptyMessage="No SNS topics created yet."
    emptyIcon={BellIcon}
    columns={["Topic", "Type", "Subscriptions", "Created"]}
  >
    {#each topics as topic}
      <TableRow>
        <TableCell>
          <ArnCell name={topic.name} arn={topic.arn} />
        </TableCell>
        <TableCell>
          <Badge variant={topic.fifo ? "amber" : "secondary"}>
            {topic.fifo ? "FIFO" : "Standard"}
          </Badge>
        </TableCell>
        <TableCell class="font-mono text-muted-foreground">
          {topic.subscriptions}
        </TableCell>
        <TableCell class="text-muted-foreground/70 text-xs">
          {formatUnixSeconds(topic.createdTimestamp)}
        </TableCell>
      </TableRow>
    {/each}
  </ResourceTable>

  <ResourceTable
    title="SNS Subscriptions"
    count={subscriptions.length}
    loading={dashboard.loading && !dashboard.data}
    empty={subscriptions.length === 0}
    emptyMessage="No subscriptions configured yet."
    emptyIcon={BellIcon}
    columns={["Subscription", "Topic", "Protocol", "Endpoint", "Delivery", "Filter"]}
  >
    {#each subscriptions as sub}
      <TableRow>
        <TableCell class="font-mono text-xs text-muted-foreground">
          {sub.subscriptionArn}
        </TableCell>
        <TableCell>
          <ArnCell name={sub.topicName} arn={sub.topicArn} />
        </TableCell>
        <TableCell>
          <Badge variant={protocolBadgeVariant(sub.protocol)}>{sub.protocol}</Badge>
        </TableCell>
        <TableCell class="font-mono text-xs text-muted-foreground">
          {sub.endpoint}
        </TableCell>
        <TableCell>
          <Badge variant={sub.rawMessageDelivery ? "default" : "secondary"}>
            {sub.rawMessageDelivery ? "Raw" : "Envelope"}
          </Badge>
        </TableCell>
        <TableCell class="w-[34rem] min-w-[28rem] align-top">
          {#if sub.filterPolicy}
            {@const formattedPolicy = formatFilterPolicy(sub.filterPolicy)}
            <FormattedMessageViewer
              raw={sub.filterPolicy}
              formatted={formattedPolicy}
              formattedLabel="Filter (JSON)"
              rawLabel="Raw Filter"
              formattedOpenByDefault={true}
              rawOpenByDefault={false}
              formattedContentClass="text-[11px] text-muted-foreground"
              rawContentClass="text-[11px] text-muted-foreground"
              formattedMaxHeightClass="max-h-52"
              rawMaxHeightClass="max-h-40"
            />
          {:else}
            <span class="text-xs text-muted-foreground/70">None</span>
          {/if}
        </TableCell>
      </TableRow>
    {/each}
  </ResourceTable>
</div>
