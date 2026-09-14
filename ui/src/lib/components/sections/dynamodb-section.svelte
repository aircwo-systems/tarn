<script lang="ts">
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import DynamoTableDetail from "$lib/components/dynamodb/dynamodb-table-detail.svelte";
  import { getDashboard } from "$lib/state.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const tables = $derived(dashboard.data?.dynamodbTables ?? []);
  const streams = $derived(dashboard.data?.dynamodbStreams ?? []);
  const config = $derived(dashboard.data?.config ?? null);
  const streamEnabledCount = $derived(tables.filter((table) => table.streamEnabled).length);

  let selectedTableName = $state<string | null>(null);
  let query = $state("");

  // Keyed by name: polling replaces the objects, so holding one would freeze the detail panel.
  const selected = $derived(
    tables.find((table) => table.name === selectedTableName) ?? tables[0] ?? null,
  );

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return tables;
    return tables.filter((table) =>
      [table.name, table.status, table.keySchema, table.billingMode ?? ""]
        .join(" ")
        .toLowerCase()
        .includes(q),
    );
  });

  const selectedStreams = $derived(
    selected ? streams.filter((stream) => stream.tableName === selected.name) : [],
  );

  function select(name: string) {
    selectedTableName = name;
    history.replaceState(null, "", `#dynamodb?table=${encodeURIComponent(name)}`);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((table) => table.name === selected?.name);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].name);
  }

  function indexLabel(table: { localIndexes: number; globalIndexes: number }) {
    const total = table.localIndexes + table.globalIndexes;
    return total === 0 ? "no indexes" : `${total} index${total === 1 ? "" : "es"}`;
  }

  onMount(() => {
    const table = new URLSearchParams(window.location.hash.split("?")[1] ?? "").get("table");
    if (table) selectedTableName = table;
  });
</script>

<div class="dynamodb">
  <SectionHeader
    title="DynamoDB"
    description="{tables.length} table{tables.length === 1 ? '' : 's'} · {streams.length} stream{streams.length === 1 ? '' : 's'} · {streamEnabledCount} streaming"
    {sidebarCollapsed}
    {onToggleSidebar}
  />

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(5) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if tables.length === 0}
    <div class="blank">
      <h2>No DynamoDB tables yet</h2>
      <p>Create one with the AWS SDK, AWS CLI, or your IaC, and it shows up here.</p>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-dynamodb-list-width">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="table-list" onkeydown={onKeydown}>
          <div class="list-intro">
            <div>
              <span class="eyebrow">Tables</span>
              <p>Keys, indexes, and stream state.</p>
            </div>
            <span class="list-count">{tables.length}</span>
          </div>

          <label class="search">
            <MagnifyingGlassIcon size={12} />
            <input placeholder="Filter tables" bind:value={query} aria-label="Filter tables" />
            <span class="count">{visible.length}</span>
          </label>

          <div class="rows">
            {#each visible as table (table.name)}
              <RcListRow
                mono
                title={table.name}
                sub={`${table.keySchema} · ${indexLabel(table)}`}
                selected={table.name === selected?.name}
                onclick={() => select(table.name)}
              >
                {#snippet trailing()}
                  <span class="row-meta">
                    <span class="status" data-state={table.status.toLowerCase()}>{table.status.toLowerCase()}</span>
                    {#if table.streamEnabled}
                      <span class="stream-dot" role="img" aria-label="Stream enabled" title="Stream enabled"></span>
                    {/if}
                    <span class="item-count" title={`${table.itemCount.toLocaleString("en-GB")} items`}>
                      {table.itemCount.toLocaleString("en-GB")}
                    </span>
                  </span>
                {/snippet}
              </RcListRow>
            {:else}
              <p class="none">No match for “{query}”</p>
            {/each}
          </div>
        </div>
      </RcResizableAside>

      {#if selected}
        {#key selected.name}
          <DynamoTableDetail table={selected} streams={selectedStreams} {config} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .dynamodb { display: flex; flex-direction: column; min-height: 100%; }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  .table-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .list-intro { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding: 1px 10px 3px; }
  .eyebrow { display: block; font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-tertiary); }
  .list-intro p { margin-top: 3px; font-size: 11px; color: var(--text-secondary); }
  .list-count { font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

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
  .row-meta { display: flex; align-items: center; gap: 7px; }
  .status { font-size: 10px; color: var(--text-tertiary); text-transform: lowercase; }
  .status[data-state="active"] { color: var(--accent-green); }
  .status[data-state="creating"], .status[data-state="updating"] { color: var(--accent-amber); }
  .status[data-state="failed"], .status[data-state="error"] { color: var(--accent-red); }
  .stream-dot { width: 5px; height: 5px; border-radius: 50%; background: var(--topology-dynamodb); box-shadow: 0 0 0 2px color-mix(in srgb, var(--topology-dynamodb) 16%, transparent); }
  .item-count { font: 10.5px var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums; color: var(--text-tertiary); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; width: 260px; }
  .skeleton-list span {
    height: 42px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } }
  @media (prefers-reduced-motion: reduce) {
    .blank, .skeleton-list span { animation: none; }
  }
</style>
