<script lang="ts">
  import { onMount } from "svelte";
  import SectionHeader from "./section-header.svelte";
  import QueueList from "$lib/components/queues/queue-list.svelte";
  import QueueDetail from "$lib/components/queues/queue-detail.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import { fetchQueueMessages } from "$lib/api";
  import {
    getDashboard,
    getDashboardFilters,
    matchesTagFilter,
  } from "$lib/state.svelte";
  import type { QueueMessageSummary } from "$lib/types";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const filters = getDashboardFilters();
  const queues = $derived(
    (dashboard.data?.queues ?? []).filter((queue) =>
      matchesTagFilter(queue.tags, filters.tagFilter),
    ),
  );

  const totalVisible = $derived(
    queues.reduce((total, queue) => total + queue.approxVisible, 0),
  );
  const totalInFlight = $derived(
    queues.reduce((total, queue) => total + queue.approxInFlight, 0),
  );
  const totalProcessed = $derived(
    queues.reduce((total, queue) => total + (queue.processedCount ?? 0), 0),
  );

  // Keyed by name: polling replaces the objects, so holding one would freeze the panel.
  let selectedName = $state<string | null>(null);
  const selected = $derived(
    queues.find((queue) => queue.name === selectedName) ?? queues[0] ?? null,
  );

  let selectedMessages = $state<QueueMessageSummary[]>([]);
  let selectedLoading = $state(false);
  let selectedError = $state("");
  let requestToken = 0;
  let loadedFor = $state("");

  $effect(() => {
    if (
      selectedName &&
      !queues.some((queue) => queue.name === selectedName)
    ) {
      selectedName = null;
      selectedMessages = [];
      selectedError = "";
      loadedFor = "";
    }
  });

  $effect(() => {
    const name = selected?.name ?? "";
    if (name && name !== loadedFor) {
      loadedFor = name;
      void loadQueueMessages(name);
    }
  });

  function select(name: string) {
    selectedName = name;
    history.replaceState(null, "", `#queues?queue=${encodeURIComponent(name)}`);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const queue = qs ? new URLSearchParams(qs).get("queue") : null;
    if (queue) selectedName = queue;
  });

  async function refreshSelectedQueueMessages() {
    if (!selected) return;
    loadedFor = "";
    await loadQueueMessages(selected.name);
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
</script>

<div class="queues">
  <SectionHeader
    title="SQS Queues"
    description="{queues.length} queues · {totalVisible} visible · {totalInFlight} in flight · {totalProcessed.toLocaleString('en-GB')} processed"
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
  {:else if queues.length === 0}
    <div class="blank">
      <h2>No queues yet</h2>
      <p>Create one with <code>aws sqs create-queue</code> or deploy through your IaC, and it appears here.</p>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-queues-list-width">
        <QueueList queues={queues} selectedName={selected?.name ?? null} onselect={select} />
      </RcResizableAside>
      {#if selected}
        {#key selected.name}
          <QueueDetail
            queue={selected}
            messages={selectedMessages}
            loading={selectedLoading}
            error={selectedError}
            onrefresh={() => void refreshSelectedQueueMessages()}
          />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .queues { display: flex; flex-direction: column; min-height: 100%; }
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

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .blank code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px; color: var(--text-primary); }

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
