<script lang="ts" module>
  export interface TriggerRow {
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
  }

  export function triggerStateTone(state: string): "green" | "amber" | "red" | "neutral" {
    const normalized = state.toLowerCase();
    if (normalized === "enabled" || normalized === "active" || normalized === "configured")
      return "green";
    if (normalized === "creating" || normalized === "updating" || normalized === "pending")
      return "amber";
    if (normalized.includes("fail") || normalized === "disabled") return "red";
    return "neutral";
  }
</script>

<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcTonePill from "$lib/components/rack/rc-tone-pill.svelte";

  let {
    triggers,
    selectedId,
    onselect,
  }: {
    triggers: TriggerRow[];
    selectedId: string | null;
    onselect: (id: string) => void;
  } = $props();

  let query = $state("");

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return triggers;
    return triggers.filter((trigger) =>
      `${trigger.sourceName} ${trigger.targetName} ${trigger.type} ${trigger.state}`
        .toLowerCase()
        .includes(q),
    );
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((trigger) => trigger.id === selectedId);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    onselect(visible[next].id);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="trigger-list" onkeydown={onKeydown}>
  <label class="search">
    <MagnifyingGlassIcon size={12} />
    <input placeholder="Filter triggers" bind:value={query} aria-label="Filter triggers" />
    <span class="count">{visible.length}</span>
  </label>

  <div class="rows">
    {#each visible as trigger (trigger.id)}
      <RcListRow
        mono
        title="{trigger.sourceName} → {trigger.targetName}"
        sub="{trigger.type} · {trigger.detail}"
        selected={trigger.id === selectedId}
        onclick={() => onselect(trigger.id)}
      >
        {#snippet trailing()}
          <RcTonePill tone={triggerStateTone(trigger.state)}>{trigger.state.toLowerCase()}</RcTonePill>
        {/snippet}
      </RcListRow>
    {:else}
      <p class="none">No match for “{query}”</p>
    {/each}
  </div>
</div>

<style>
  .trigger-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
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
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }
</style>
