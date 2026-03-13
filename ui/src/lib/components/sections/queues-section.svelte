<script lang="ts">
  import { ChatCircleIcon, CaretDownIcon, CaretUpIcon } from "phosphor-svelte";
  import { TableRow, TableCell } from "$lib/components/ui/table";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import ResourceTable from "$lib/components/common/resource-table.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import { fetchQueueMessages } from "$lib/api";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import type { QueueMessageSummary } from "$lib/types";
  import { formatUnixSeconds } from "$lib/utils";

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const queues = $derived(
    (dashboard.data?.queues ?? []).filter((queue) =>
      matchesTagFilter(queue.tags, filters.tagFilter),
    ),
  );

  let selectedQueueName = $state("");
  let selectedMessages = $state<QueueMessageSummary[]>([]);
  let selectedLoading = $state(false);
  let selectedError = $state("");
  let requestToken = 0;

  const selectedQueue = $derived(
    queues.find((queue) => queue.name === selectedQueueName) ?? null,
  );

  $effect(() => {
    if (
      selectedQueueName &&
      !queues.some((queue) => queue.name === selectedQueueName)
    ) {
      selectedQueueName = "";
      selectedMessages = [];
      selectedError = "";
    }
  });

  async function selectQueue(queueName: string) {
    selectedQueueName = queueName;
    expandedMessages = new Set();
    await loadQueueMessages(queueName);
  }

  async function refreshSelectedQueueMessages() {
    if (!selectedQueueName) return;
    await loadQueueMessages(selectedQueueName);
  }

  async function loadQueueMessages(queueName: string) {
    const token = ++requestToken;
    selectedLoading = true;
    selectedError = "";
    try {
      const messages = await fetchQueueMessages(queueName);
      if (token !== requestToken) return;
      selectedMessages = messages;
    } catch (error) {
      if (token !== requestToken) return;
      const queue = queues.find((item) => item.name === queueName);
      selectedMessages = queue?.recentMessages ?? [];
      selectedError =
        error instanceof Error
          ? error.message
          : "Failed to load queue messages";
    } finally {
      if (token === requestToken) {
        selectedLoading = false;
      }
    }
  }

  function onQueueRowKeydown(event: KeyboardEvent, queueName: string) {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      void selectQueue(queueName);
    }
  }

  function formatSentAt(ms: number): string {
    if (!ms) return "--";
    return new Date(ms).toLocaleString();
  }

  function messageBadgeVariant(
    state: string,
  ): "default" | "amber" | "secondary" | "destructive" {
    if (state === "visible") return "default";
    if (state === "inflight") return "amber";
    if (state === "stale") return "destructive";
    return "secondary";
  }

  const PREVIEW_LENGTH = 280;
  let expandedMessages = $state(new Set<string>());

  function toggleExpanded(id: string) {
    const next = new Set(expandedMessages);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    expandedMessages = next;
  }

  function tryFormatJSON(body: string): string | null {
    try {
      return JSON.stringify(JSON.parse(body), null, 2);
    } catch {
      return null;
    }
  }

  function isLargeBody(body: string): boolean {
    return body.length > PREVIEW_LENGTH;
  }

  function bodyPreview(body: string): string {
    if (body.length <= PREVIEW_LENGTH) return body;
    return body.slice(0, PREVIEW_LENGTH) + "…";
  }

</script>

<div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
  <ResourceTable
    title="SQS Queues"
    count={queues.length}
    loading={dashboard.loading && !dashboard.data}
    empty={queues.length === 0}
    emptyMessage="No queues created yet."
    emptyIcon={ChatCircleIcon}
    columns={[
      "Queue",
      "Type",
      "Visible",
      "In Flight",
      "Delayed",
      "Stale",
      "Messages",
      "Visibility",
      "Long Poll",
      "Created",
    ]}
  >
    {#each queues as queue}
      <TableRow
        class={`cursor-pointer focus-within:bg-muted/60 ${queue.name === selectedQueueName ? "bg-muted/60" : ""}`}
        role="button"
        tabindex="0"
        aria-label={`Open messages for queue ${queue.name}`}
        onclick={() => void selectQueue(queue.name)}
        onkeydown={(event: KeyboardEvent) =>
          onQueueRowKeydown(event, queue.name)}
      >
        <TableCell><ArnCell name={queue.name} arn={queue.url} /></TableCell>
        <TableCell>
          <Badge variant={queue.fifo ? "amber" : "secondary"}>
            {queue.fifo ? "FIFO" : "Standard"}
          </Badge>
        </TableCell>
        <TableCell class="font-mono text-muted-foreground"
          >{queue.approxVisible}</TableCell
        >
        <TableCell class="font-mono text-muted-foreground"
          >{queue.approxInFlight}</TableCell
        >
        <TableCell class="font-mono text-muted-foreground"
          >{queue.approxDelayed}</TableCell
        >
        <TableCell class="font-mono">
          {#if (queue.approxStale ?? 0) > 0}
            <span
              class="text-destructive"
              title="Parked by OpenStack after repeated failures (no DLQ). On real AWS these would retry indefinitely."
              >{queue.approxStale}</span
            >
          {:else}
            <span class="text-muted-foreground">0</span>
          {/if}
        </TableCell>
        <TableCell class="min-w-[18rem]">
          {#if queue.recentMessages?.length}
            <div class="space-y-1.5">
              {#each queue.recentMessages.slice(0, 3) as message}
                <div
                  class="rounded-md border border-border bg-background-subtle/70 px-2 py-1.5"
                >
                  <div
                    class="mb-1 flex items-center gap-2 text-[10px] uppercase tracking-wide text-muted-foreground/70"
                  >
                    <Badge variant={messageBadgeVariant(message.state)}>
                      {message.state}
                    </Badge>
                    <span class="font-mono">{message.id.slice(0, 8)}</span>
                    {#if message.receiveCount > 0}
                      <span class="font-mono">x{message.receiveCount}</span>
                    {/if}
                  </div>
                  <p
                    class="max-h-9 overflow-hidden break-all text-xs text-muted-foreground"
                    title={message.body}
                  >
                    {message.body || "(empty)"}
                  </p>
                </div>
              {/each}
              {#if queue.recentMessages.length > 3}
                <p class="text-[11px] text-muted-foreground/70">
                  +{queue.recentMessages.length - 3} more messages
                </p>
              {/if}
            </div>
          {:else}
            <span class="text-xs text-muted-foreground/70">No messages</span>
          {/if}
        </TableCell>
        <TableCell class="font-mono text-muted-foreground"
          >{queue.visibilitySec}s</TableCell
        >
        <TableCell class="font-mono text-muted-foreground"
          >{queue.waitTimeSec}s</TableCell
        >
        <TableCell class="text-muted-foreground/70 text-xs"
          >{formatUnixSeconds(queue.createdTimestamp)}</TableCell
        >
      </TableRow>
    {/each}
  </ResourceTable>

  <section class="rounded-lg border border-border bg-card">
    <div
      class="flex items-center justify-between border-b border-border px-3 py-2"
    >
      <h3 class="text-sm font-semibold text-foreground">Queue Messages</h3>
      {#if selectedQueue}
        <button
          type="button"
          class="rounded-md border border-border px-2 py-1 text-xs text-muted-foreground hover:bg-background-subtle"
          onclick={() => void refreshSelectedQueueMessages()}
          disabled={selectedLoading}
        >
          Refresh
        </button>
      {/if}
    </div>

    {#if !selectedQueue}
      <p class="px-3 py-5 text-sm text-muted-foreground/70">
        Click a queue row to inspect pending messages.
      </p>
    {:else}
      <div
        class="border-b border-border px-3 py-2 text-xs text-muted-foreground/70"
      >
        <p class="truncate font-mono text-foreground">{selectedQueue.name}</p>
        <p class="mt-1">
          {selectedQueue.approxVisible} visible, {selectedQueue.approxInFlight} in
          flight, {selectedQueue.approxDelayed} delayed{#if (selectedQueue.approxStale ?? 0) > 0},
            <span class="text-destructive"
              >{selectedQueue.approxStale} stale</span
            >{/if}
        </p>
      </div>

      {#if selectedError}
        <p
          class="border-b border-border px-3 py-2 text-xs text-destructive-300"
        >
          {selectedError}
        </p>
      {/if}

      <div class="max-h-[36rem] space-y-2 overflow-y-auto px-3 py-3">
        {#if selectedLoading}
          <p class="text-sm text-muted-foreground/70">Loading messages...</p>
        {:else if selectedMessages.length === 0}
          <p class="text-sm text-muted-foreground/70">
            No messages available for this queue.
          </p>
        {:else}
          {#each selectedMessages as message}
            {@const expanded = expandedMessages.has(message.id)}
            {@const large = isLargeBody(message.body ?? "")}
            {@const formatted = tryFormatJSON(message.body ?? "")}
            {@const canExpand = large || formatted !== null}
            <div
              class="rounded-md border border-border bg-background-subtle/70 p-2"
            >
              <div
                class="mb-1.5 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground/70"
              >
                <Badge variant={messageBadgeVariant(message.state)}>
                  {message.state}
                </Badge>
                <span class="font-mono">{message.id}</span>
                {#if message.receiveCount > 0}
                  <span class="font-mono">receives: {message.receiveCount}</span
                  >
                {/if}
                {#if large}
                  <span>{(message.body ?? "").length} chars</span>
                {/if}
              </div>

              {#if expanded && formatted}
                <FormattedMessageViewer
                  raw={message.body || "(empty)"}
                  formatted={formatted}
                  formattedContentClass="text-[11px] text-muted-foreground"
                  rawContentClass="text-[11px] text-muted-foreground"
                  formattedMaxHeightClass="max-h-96"
                  rawMaxHeightClass="max-h-96"
                />
              {:else if expanded}
                <p
                  class="max-h-96 overflow-y-auto break-all whitespace-pre-wrap text-xs text-muted-foreground"
                >
                  {message.body || "(empty)"}
                </p>
              {:else}
                <p class="break-all text-xs text-muted-foreground">
                  {bodyPreview(message.body ?? "") || "(empty)"}
                </p>
              {/if}

              {#if message.state === "stale"}
                <p class="mt-1.5 text-[11px] text-destructive/80">
                  Parked — failed {message.receiveCount}× with no DLQ. On AWS
                  this would retry indefinitely; OpenStack parks it to prevent
                  wasted invocations.
                </p>
              {/if}

              <div class="mt-1.5 flex items-center justify-between">
                <p class="text-[11px] text-muted-foreground/70">
                  {formatSentAt(message.sentAt)}
                </p>
                {#if canExpand}
                  <button
                    type="button"
                    class="flex items-center gap-1 text-[11px] text-muted-foreground/70 hover:text-foreground"
                    onclick={() => toggleExpanded(message.id)}
                  >
                    {#if expanded}
                      <CaretUpIcon size={12} />
                      Collapse
                    {:else}
                      <CaretDownIcon size={12} />
                      Expand
                    {/if}
                  </button>
                {/if}
              </div>
            </div>
          {/each}
        {/if}
      </div>
    {/if}
  </section>
</div>
