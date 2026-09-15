<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import type { QueueSummary } from "$lib/types";

  let {
    queues,
    selectedName,
    onselect,
    selection = null,
    ontoggleselect = null,
  }: {
    queues: QueueSummary[];
    selectedName: string | null;
    onselect: (name: string) => void;
    /** When set, rows show checkboxes for bulk disruptor targeting. */
    selection?: Set<string> | null;
    ontoggleselect?: ((name: string) => void) | null;
  } = $props();

  let query = $state("");
  const numberFormatter = new Intl.NumberFormat("en-GB", { notation: "compact" });

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return queues;
    return queues.filter((queue) => queue.name.toLowerCase().includes(q));
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((queue) => queue.name === selectedName);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    onselect(visible[next].name);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="queue-list" onkeydown={onKeydown}>
  <label class="search">
    <MagnifyingGlassIcon size={12} />
    <input placeholder="Filter queues" bind:value={query} aria-label="Filter queues" />
    <span class="count">{visible.length}</span>
  </label>

  <div class="rows">
    {#each visible as queue (queue.name)}
      <div class="row" class:selected={queue.name === selectedName}>
        {#if selection && ontoggleselect}
          {@const toggle = ontoggleselect}
          <input
            type="checkbox"
            class="pick"
            checked={selection.has(queue.name)}
            onchange={() => toggle(queue.name)}
            onclick={(e) => e.stopPropagation()}
            aria-label={`Target ${queue.name} for disruptor`}
            title="Target for disruptor"
          />
        {/if}
        <RcListRow
          mono
          title={queue.name}
          sub="{queue.fifo ? 'FIFO' : 'Standard'} · {queue.approxVisible} visible"
          selected={queue.name === selectedName}
          onclick={() => onselect(queue.name)}
        >
          {#snippet trailing()}
            {#if queue.disruptEnabled}
              <span class="state disrupted" title="Disruptor armed: ~{queue.disruptFailureRate ?? '?'}% sends fail ({queue.disruptCode ?? 'ServiceUnavailable'})">disrupt</span>
            {/if}
            {#if (queue.approxStale ?? 0) > 0}
              <span class="state failed">{numberFormatter.format(queue.approxStale ?? 0)} stale</span>
            {:else if queue.approxVisible > 0}
              <span class="calls" title="{queue.approxVisible} visible messages">{numberFormatter.format(queue.approxVisible)}</span>
            {:else if queue.approxInFlight > 0}
              <span class="calls" title="{queue.approxInFlight} in flight">{numberFormatter.format(queue.approxInFlight)}</span>
            {/if}
          {/snippet}
        </RcListRow>
      </div>
    {:else}
      <p class="none">No match for “{query}”</p>
    {/each}
  </div>
</div>

<style>
  .queue-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }  .search {
    display: flex; align-items: center; gap: 7px; height: 30px; padding: 0 10px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); color: var(--text-tertiary);
    transition: border-color 120ms ease;
  }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 12px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }
  .count { font-size: 10.5px; font-variant-numeric: tabular-nums; }
  .rows { display: flex; flex-direction: column; gap: 2px; }
  .row { display: flex; align-items: center; gap: 6px; min-width: 0; }
  .row > :global(.rc-list-row) { flex: 1; min-width: 0; }
  .pick { flex-shrink: 0; width: 14px; height: 14px; accent-color: var(--accent-red); cursor: pointer; }
  .calls { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
  .state {
    font-size: 10px; padding: 1px 6px; border-radius: 6px; color: var(--accent-amber);
    background: color-mix(in srgb, var(--accent-amber) 10%, transparent);
  }
  .state.failed { color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 10%, transparent); }
  .state.disrupted { color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 12%, transparent); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
</style>
