<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import { describeSchedule } from "$lib/eventbridge-schedule";
  import type { EventBridgeRuleSummary } from "$lib/types";

  let {
    rules,
    selectedName,
    onselect,
  }: {
    rules: EventBridgeRuleSummary[];
    selectedName: string | null;
    onselect: (name: string) => void;
  } = $props();

  let query = $state("");

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rules;
    return rules.filter((r) => r.name.toLowerCase().includes(q) || r.scheduleExpression.toLowerCase().includes(q));
  });

  function failing(rule: EventBridgeRuleSummary) {
    return (rule.targets ?? []).some((t) => /error|fail/i.test(t.lastResult ?? ""));
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((r) => r.name === selectedName);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    onselect(visible[next].name);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="rule-list" onkeydown={onKeydown}>
  <label class="search">
    <MagnifyingGlassIcon size={12} />
    <input placeholder="Filter rules" bind:value={query} aria-label="Filter rules" />
    <span class="count">{visible.length}</span>
  </label>

  <div class="rows">
    {#each visible as rule (rule.name)}
      <RcListRow
        mono
        title={rule.name}
        sub={describeSchedule(rule.scheduleExpression).label}
        selected={rule.name === selectedName}
        onclick={() => onselect(rule.name)}
      >
        {#snippet trailing()}
          {#if rule.state !== "ENABLED"}
            <span class="flag">off</span>
          {:else if failing(rule)}
            <span class="flag err">failing</span>
          {:else}
            <span class="targets" title="{rule.targets?.length ?? 0} targets">{rule.targets?.length ?? 0}→</span>
          {/if}
        {/snippet}
      </RcListRow>
    {:else}
      <p class="none">No match for “{query}”</p>
    {/each}
  </div>
</div>

<style>
  .rule-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
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
  .targets { font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .flag { font-size: 10px; padding: 1px 6px; border-radius: 6px; color: var(--text-tertiary); background: var(--bg-element); }
  .flag.err { color: var(--accent-red); background: color-mix(in srgb, var(--accent-red) 10%, transparent); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
</style>
