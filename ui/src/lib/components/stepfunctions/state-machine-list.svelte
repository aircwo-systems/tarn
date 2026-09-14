<script lang="ts">
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import type { StateMachineSummary } from "$lib/types";

  let {
    machines,
    selectedArn,
    onselect,
  }: {
    machines: StateMachineSummary[];
    selectedArn: string | null;
    onselect: (arn: string) => void;
  } = $props();

  let query = $state("");

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return machines;
    return machines.filter((machine) =>
      `${machine.name} ${machine.type} ${machine.status}`.toLowerCase().includes(q),
    );
  });

  function statusTone(status: string): Tone {
    if (status === "ACTIVE") return "green";
    if (status === "DELETING") return "amber";
    return "neutral";
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((machine) => machine.arn === selectedArn);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    onselect(visible[next].arn);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="machine-list" onkeydown={onKeydown}>
  <label class="search">
    <MagnifyingGlassIcon size={12} />
    <input placeholder="Filter state machines" bind:value={query} aria-label="Filter state machines" />
    <span class="count">{visible.length}</span>
  </label>

  <div class="rows">
    {#each visible as machine (machine.arn)}
      <RcListRow
        mono
        title={machine.name}
        sub="{machine.type} · {machine.executions?.length ?? 0} executions"
        selected={machine.arn === selectedArn}
        onclick={() => onselect(machine.arn)}
      >
        {#snippet trailing()}
          <RcTonePill tone={statusTone(machine.status)}>{machine.status.toLowerCase()}</RcTonePill>
        {/snippet}
      </RcListRow>
    {:else}
      <p class="none">No match for “{query}”</p>
    {/each}
  </div>
</div>

<style>
  .machine-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
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
