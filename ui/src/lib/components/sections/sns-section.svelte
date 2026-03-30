<script lang="ts">
  import { BellIcon } from "phosphor-svelte";
  import {
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
    TableCell,
  } from "$lib/components/ui/table";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import SectionHeader from "./section-header.svelte";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import { formatUnixSeconds } from "$lib/utils";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

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
  const fifoTopics = $derived(topics.filter((topic) => topic.fifo).length);
  const lambdaSubscriptions = $derived(
    subscriptions.filter((sub) => sub.protocol.toLowerCase() === "lambda").length,
  );
  const sqsSubscriptions = $derived(
    subscriptions.filter((sub) => sub.protocol.toLowerCase() === "sqs").length,
  );

  function protocolColor(protocol: string): string {
    switch (protocol.toLowerCase()) {
      case "sqs":
        return "text-amber";
      case "lambda":
        return "text-primary";
      default:
        return "text-muted-foreground";
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

<div class="flex min-h-full flex-col gap-4">
  <SectionHeader
    title="SNS topics"
    description="Topic fan-out, subscriber mix and delivery filter rules."
    icon={BellIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet stats()}
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{topics.length}</span>
        <span class="text-muted-foreground/70">topics</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{subscriptions.length}</span>
        <span class="text-muted-foreground/70">subscriptions</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{fifoTopics}</span>
        <span class="text-muted-foreground/70">fifo</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{lambdaSubscriptions}</span>
        <span class="text-muted-foreground/70">lambda targets</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{sqsSubscriptions}</span>
        <span class="text-muted-foreground/70">sqs targets</span>
      </span>
    {/snippet}
  </SectionHeader>

  <div class="space-y-4">
    <section class="overflow-hidden rounded-lg border border-border/70 bg-background/60">
      <div class="border-b border-border px-3 py-2">
        <h3 class="text-sm font-semibold text-foreground">Topics</h3>
      </div>

      {#if dashboard.loading && !dashboard.data}
        <div class="space-y-2 p-3">
          {#each Array(4) as _, index (index)}
            <Skeleton class="h-11 w-full" />
          {/each}
        </div>
      {:else if topics.length === 0}
        <div class="flex min-h-[14rem] items-center justify-center">
          <EmptyState
            message="No SNS topics created yet."
            icon={BellIcon}
          />
        </div>
      {:else}
        <div class="overflow-auto">
          <Table>
            <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
              <TableRow class="hover:bg-transparent">
                <TableHead>Topic</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Subscriptions</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each topics as topic}
                <TableRow>
                  <TableCell>
                    <ArnCell name={topic.name} arn={topic.arn} />
                  </TableCell>
                  <TableCell class={`font-mono text-xs ${topic.fifo ? "text-amber" : "text-muted-foreground"}`}>
                    {topic.fifo ? "FIFO" : "Standard"}
                  </TableCell>
                  <TableCell class="font-mono text-muted-foreground">
                    {topic.subscriptions}
                  </TableCell>
                  <TableCell class="text-xs text-muted-foreground/70">
                    {formatUnixSeconds(topic.createdTimestamp)}
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        </div>
      {/if}
    </section>

    <section class="overflow-hidden rounded-lg border border-border/70 bg-background/60">
      <div class="border-b border-border px-3 py-2">
        <h3 class="text-sm font-semibold text-foreground">Subscriptions</h3>
      </div>

      {#if dashboard.loading && !dashboard.data}
        <div class="space-y-2 p-3">
          {#each Array(5) as _, index (index)}
            <Skeleton class="h-11 w-full" />
          {/each}
        </div>
      {:else if subscriptions.length === 0}
        <div class="flex min-h-[14rem] items-center justify-center">
          <EmptyState
            message="No subscriptions configured yet."
            icon={BellIcon}
          />
        </div>
      {:else}
        <div class="overflow-auto">
          <Table>
            <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
              <TableRow class="hover:bg-transparent">
                <TableHead>Subscription</TableHead>
                <TableHead>Topic</TableHead>
                <TableHead>Protocol</TableHead>
                <TableHead>Endpoint</TableHead>
                <TableHead>Delivery</TableHead>
                <TableHead>Filter</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each subscriptions as sub}
                <TableRow>
                  <TableCell class="max-w-56 break-all font-mono text-xs text-muted-foreground">
                    {sub.subscriptionArn}
                  </TableCell>
                  <TableCell>
                    <ArnCell name={sub.topicName} arn={sub.topicArn} />
                  </TableCell>
                  <TableCell class={`font-mono text-xs ${protocolColor(sub.protocol)}`}>
                    {sub.protocol}
                  </TableCell>
                  <TableCell class="max-w-56 break-all font-mono text-xs text-muted-foreground">
                    {sub.endpoint}
                  </TableCell>
                  <TableCell class={`font-mono text-xs ${sub.rawMessageDelivery ? "text-primary" : "text-muted-foreground"}`}>
                    {sub.rawMessageDelivery ? "Raw" : "Envelope"}
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
            </TableBody>
          </Table>
        </div>
      {/if}
    </section>
  </div>
</div>
