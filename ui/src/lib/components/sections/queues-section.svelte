<script lang="ts">
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon, SidebarSimpleIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import QueueList from "$lib/components/queues/queue-list.svelte";
  import QueueDetail from "$lib/components/queues/queue-detail.svelte";
  import QueueDisruptor from "$lib/components/queues/queue-disruptor.svelte";
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

  let listCollapsed = $state(false);

  function toggleListCollapse() {
    listCollapsed = !listCollapsed;
    try {
      localStorage.setItem("tarn-queues-list-collapsed", String(listCollapsed));
    } catch {}
  }

  let query = $state("");
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return queues;
    return queues.filter((queue) => queue.name.toLowerCase().includes(q));
  });

  const disruptedCount = $derived(queues.filter((q) => q.disruptEnabled).length);

  // Keyed by name: polling replaces the objects, so holding one would freeze the panel.
  let selectedName = $state<string | null>(null);
  const selected = $derived(
    queues.find((queue) => queue.name === selectedName) ?? queues[0] ?? null,
  );

  // Multi-queue targeting for the bulk disruptor action. A Set in $state is
  // not deeply reactive, so every mutation replaces the instance.
  let disruptTargets = $state<Set<string>>(new Set());
  const disruptQueues = $derived(
    queues.filter((queue) => disruptTargets.has(queue.name)),
  );

  function toggleDisruptTarget(name: string) {
    const next = new Set(disruptTargets);
    if (next.has(name)) {
      next.delete(name);
    } else {
      next.add(name);
    }
    disruptTargets = next;
  }

  function clearDisruptTargets() {
    disruptTargets = new Set();
  }

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
    // Drop bulk-disruptor targets for queues that no longer exist.
    if (disruptTargets.size > 0) {
      const alive = new Set(queues.map((queue) => queue.name));
      let pruned: Set<string> | null = null;
      for (const name of disruptTargets) {
        if (!alive.has(name)) {
          pruned ??= new Set(disruptTargets);
          pruned.delete(name);
        }
      }
      if (pruned) disruptTargets = pruned;
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

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((queue) => queue.name === selected?.name);
    const forward = e.key === "ArrowDown" || e.key === "ArrowRight";
    const next = forward ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].name);
  }

  onMount(() => {
    const qs = window.location.hash.split("?")[1];
    const queue = qs ? new URLSearchParams(qs).get("queue") : null;
    if (queue) selectedName = queue;
    try {
      const saved = localStorage.getItem("tarn-queues-list-collapsed");
      if (saved !== null) {
        listCollapsed = saved === "true";
      }
    } catch {}
  });

  async function refreshSelectedQueueMessages() {
    if (!selected) return;
    loadedFor = "";
    await loadQueueMessages(selected.name);
  }

  async function loadQueueMessages(name: string) {
    const token = ++requestToken;
    selectedLoading = true;
    selectedError = "";
    try {
      const messages = await fetchQueueMessages(name);
      if (token === requestToken) {
        selectedMessages = messages;
      }
    } catch (err) {
      if (token === requestToken) {
        selectedError =
          err instanceof Error ? err.message : "Failed to load messages";
      }
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
      <div class="header-filter">
        <MagnifyingGlassIcon size={12} />
        <input
          placeholder="Filter queues..."
          bind:value={query}
          aria-label="Filter queues"
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

  {#if dashboard.loading && !dashboard.data}\
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
    {#if disruptQueues.length > 0}
      <div class="bulk">
        <QueueDisruptor targets={disruptQueues} />
        <button type="button" class="clear-targets" onclick={clearDisruptTargets}>
          Clear selection ({disruptQueues.length})
        </button>
      </div>
    {/if}
    <div class="layout" class:list-collapsed={listCollapsed}>
      {#if listCollapsed}
        <div class="list-toolbar" role="toolbar" aria-label="Queues overview">
          <div class="toolbar-leading">
            <button
              type="button"
              class="expand-list-btn"
              onclick={toggleListCollapse}
              title="Expand queue list"
              aria-label="Expand queue list"
            >
              <SidebarSimpleIcon size={13} weight="fill" />
              <span class="expand-label">Queue list</span>
              <span class="count-badge">{queues.length}</span>
            </button>

            {#if sidebarCollapsed}
              <span class="toolbar-divider" aria-hidden="true"></span>
              <div class="toolbar-stats">
                <span class="toolbar-stat" title="{totalVisible} visible messages">
                  <span class="status-dot green"></span>
                  <span>{totalVisible} visible</span>
                </span>
                {#if totalInFlight > 0}
                  <span class="toolbar-stat" title="{totalInFlight} in flight">
                    <span class="status-dot amber"></span>
                    <span>{totalInFlight} in flight</span>
                  </span>
                {/if}
                {#if disruptedCount > 0}
                  <span class="toolbar-stat" title="{disruptedCount} queues disrupted">
                    <span class="status-dot red"></span>
                    <span>{disruptedCount} disrupted</span>
                  </span>
                {/if}
              </div>
            {/if}
          </div>

          <div class="toolbar-trailing">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="chips-row" role="tablist" tabindex="0" aria-label="Queue switcher" onkeydown={onKeydown}>
              {#each visible as q (q.name)}
                <button
                  type="button"
                  role="tab"
                  class="item-chip"
                  class:selected={q.name === selected?.name}
                  aria-selected={q.name === selected?.name}
                  onclick={() => select(q.name)}
                  title="{q.name} ({q.fifo ? 'FIFO' : 'Standard'} · {q.approxVisible} visible)"
                >
                  <span
                    class="chip-dot"
                    style:background={q.disruptEnabled ? "var(--accent-red, #fb7185)" : (q.approxStale ?? 0) > 0 ? "var(--accent-amber, #f59e0b)" : q.approxVisible > 0 ? "var(--accent-green, #10b981)" : "var(--text-tertiary)"}
                  ></span>
                  <span class="chip-name">{q.name}</span>
                  {#if q.approxVisible > 0}
                    <span class="chip-badge">{q.approxVisible}</span>
                  {:else if q.fifo}
                    <span class="chip-badge">FIFO</span>
                  {/if}
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
          storageKey="tarn-queues-list-width"
          collapsible={true}
          onToggleCollapse={toggleListCollapse}
        >
          <QueueList
            queues={queues}
            selectedName={selected?.name ?? null}
            onselect={select}
            selection={disruptTargets}
            ontoggleselect={toggleDisruptTarget}
            bind:query
            onToggleCollapse={toggleListCollapse}
          />
          {#if disruptTargets.size === 0}
            <p class="bulk-hint">Tick queues to target several at once with the disruptor.</p>
          {/if}
        </RcResizableAside>
      {/if}

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

  .bulk {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
    padding: 10px 14px; margin-bottom: 8px; border-radius: 8px;
    background: color-mix(in srgb, var(--accent-red) 6%, var(--bg-element));
    border: 1px solid color-mix(in srgb, var(--accent-red) 22%, transparent);
    animation: fadeUp 180ms var(--ease-snappy) both;
  }
  .clear-targets {
    font-size: 11px; color: var(--text-tertiary); background: transparent; border: 0;
    cursor: pointer; padding: 2px 6px; border-radius: 4px;
    transition: color 100ms ease, background 100ms ease;
  }
  .clear-targets:hover { color: var(--text-primary); background: var(--bg-element); }

  .bulk-hint {
    margin: 8px 4px 0; font-size: 11px; color: var(--text-tertiary); line-height: 1.4;
  }

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
    .blank, .bulk, .skeleton-list span { animation: none; }
  }
</style>
