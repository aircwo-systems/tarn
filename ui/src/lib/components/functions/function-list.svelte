<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import type { FunctionSummary } from "$lib/types";

  let {
    functions,
    selectedName,
    onselect,
  }: {
    functions: FunctionSummary[];
    selectedName: string | null;
    onselect: (name: string) => void;
  } = $props();

  let query = $state("");
  const numberFormatter = new Intl.NumberFormat("en-GB", { notation: "compact" });

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return functions;
    return functions.filter((fn) => fn.name.toLowerCase().includes(q) || fn.runtime.toLowerCase().includes(q));
  });

  function isInactive(fn: FunctionSummary) {
    return fn.state.toLowerCase() !== "active";
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((fn) => fn.name === selectedName);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    onselect(visible[next].name);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="fn-list" onkeydown={onKeydown}>
  <label class="search">
    <MagnifyingGlassIcon size={12} />
    <input placeholder="Filter functions" bind:value={query} aria-label="Filter functions" />
    <span class="count">{visible.length}</span>
  </label>

  <div class="rows">
    {#each visible as fn (fn.name)}
      <RcListRow
        mono
        title={fn.name}
        sub="{fn.runtime} · {fn.memoryMB} MB"
        selected={fn.name === selectedName}
        onclick={() => onselect(fn.name)}
      >
        {#snippet trailing()}
          {#if isInactive(fn)}
            <span class="state" class:failed={fn.state.toLowerCase() === "failed"}>{fn.state.toLowerCase()}</span>
          {:else if fn.invocations}
            <span class="calls" title="{fn.invocations} invocations">{numberFormatter.format(fn.invocations)}</span>
          {/if}
        {/snippet}
      </RcListRow>
    {:else}
      <p class="none">No match for “{query}”</p>
    {/each}
  </div>
</div>

<style>
  .fn-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .search {
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
  .calls { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
  .state {
    font-size: 10px; padding: 1px 6px; border-radius: 6px; color: var(--accent-amber);
    background: color-mix(in srgb, var(--accent-amber) 10%, transparent);
  }
  .state.failed { color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 10%, transparent); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
</style>
