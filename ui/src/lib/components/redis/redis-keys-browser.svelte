<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import {
    ArrowClockwiseIcon,
    CaretDownIcon,
    CaretRightIcon,
    CheckIcon,
    CopyIcon,
    MagnifyingGlassIcon,
    PlusIcon,
    TrashIcon,
    WarningCircleIcon,
  } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import RcTonePill from "#lib/components/rack/rc-tone-pill.svelte";
  import RedisKeyDrawer from "./redis-key-drawer.svelte";
  import { deleteRedisKeys, scanRedisKeys } from "#lib/api";
  import {
    formatRedisTTL,
    groupRedisKeysByPrefix,
    notifyRedisMutation,
    redisTypeTone,
  } from "#lib/redis";
  import { formatBytes } from "#lib/utils";
  import type { RedisAuthOptions, RedisKeySummary } from "#lib/types";

  let {
    targetId,
    auth,
    connected = true,
    externalMutationTick = 0,
    onMutated = () => {},
  }: {
    targetId: string;
    auth: RedisAuthOptions;
    connected?: boolean;
    externalMutationTick?: number;
    onMutated?: () => void;
  } = $props();

  let pattern = $state("*");
  let keyType = $state("all");
  let count = $state(100);
  let viewMode = $state<"flat" | "tree">("flat");

  let keys = $state<RedisKeySummary[]>([]);
  let nextCursor = $state("0");
  let hasMore = $state(false);
  let dbSize = $state(0);
  let loading = $state(false);
  let error = $state("");
  let hasRun = $state(false);

  let collapsedGroups = $state<Record<string, boolean>>({});
  let drawerOpen = $state(false);
  let editingKey = $state<string | null>(null);
  let copiedKey = $state<string | null>(null);
  let armedDeleteKey = $state<string | null>(null);

  let controller: AbortController | null = null;
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let armTimer: ReturnType<typeof setTimeout> | undefined;
  let lastHandledTick = 0;

  const grouped = $derived(viewMode === "tree" ? groupRedisKeysByPrefix(keys) : []);

  async function runScan(append = false) {
    if (!connected && !hasRun) return;
    controller?.abort();
    const ac = new AbortController();
    controller = ac;
    loading = true;
    error = "";
    try {
      const res = await scanRedisKeys(
        targetId,
        {
          ...auth,
          pattern: pattern.trim() || "*",
          type: keyType,
          cursor: append ? nextCursor : "0",
          count,
        },
        ac.signal,
      );
      if (ac.signal.aborted) return;
      if (append) {
        const seen = new Set(keys.map((k) => k.key));
        const merged = [...keys];
        for (const k of res.keys) {
          if (!seen.has(k.key)) {
            seen.add(k.key);
            merged.push(k);
          }
        }
        keys = merged;
      } else {
        keys = res.keys ?? [];
      }
      nextCursor = res.cursor ?? "0";
      hasMore = !!res.hasMore;
      dbSize = res.dbSize ?? keys.length;
      hasRun = true;
    } catch (e) {
      if (ac.signal.aborted) return;
      error = e instanceof Error ? e.message : "Failed to scan keys";
    } finally {
      if (controller === ac) {
        loading = false;
        controller = null;
      }
    }
  }

  onMount(() => {
    lastHandledTick = externalMutationTick;
    if (connected) void runScan(false);
  });

  onDestroy(() => {
    controller?.abort();
    if (copyTimer) clearTimeout(copyTimer);
    if (armTimer) clearTimeout(armTimer);
  });

  $effect(() => {
    if (externalMutationTick !== lastHandledTick) {
      lastHandledTick = externalMutationTick;
      if (connected) void runScan(false);
    }
  });

  function openCreate() {
    editingKey = null;
    drawerOpen = true;
  }

  function openInspect(key: string) {
    editingKey = key;
    drawerOpen = true;
  }

  async function copyName(key: string, e: MouseEvent) {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(key);
      copiedKey = key;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copiedKey = null), 1400);
    } catch {}
  }

  async function handleRowDelete(key: string, e: MouseEvent) {
    e.stopPropagation();
    if (armedDeleteKey !== key) {
      armedDeleteKey = key;
      if (armTimer) clearTimeout(armTimer);
      armTimer = setTimeout(() => (armedDeleteKey = null), 3000);
      return;
    }
    armedDeleteKey = null;
    try {
      await deleteRedisKeys(targetId, { ...auth, keys: [key] });
      keys = keys.filter((k) => k.key !== key);
      dbSize = Math.max(0, dbSize - 1);
      notifyRedisMutation(targetId, auth.db ?? 0);
      onMutated();
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to delete key";
    }
  }

  function toggleGroup(prefix: string) {
    collapsedGroups = { ...collapsedGroups, [prefix]: !collapsedGroups[prefix] };
  }
</script>

<div class="keys-browser">
  <RcPanel
    title="Keys"
    description="Cursor-based SCAN on db{auth.db ?? 0} · {dbSize.toLocaleString('en-GB')} total keys in database"
    index={0}
  >
    {#snippet actions()}
      <div class="toolbar-actions">
        <div class="view-toggle" role="group" aria-label="Key display mode">
          <button type="button" class:active={viewMode === "flat"} onclick={() => (viewMode = "flat")}>Flat</button>
          <button type="button" class:active={viewMode === "tree"} onclick={() => (viewMode = "tree")}>Prefixes</button>
        </div>
        <RcButton small variant="ghost" disabled={loading} onclick={() => runScan(false)}>
          <ArrowClockwiseIcon size={12} /> {loading ? "Scanning..." : "Scan"}
        </RcButton>
        <RcButton small onclick={openCreate}>
          <PlusIcon size={12} weight="bold" /> New key
        </RcButton>
      </div>
    {/snippet}

    <form class="scan-bar" onsubmit={(e) => { e.preventDefault(); void runScan(false); }}>
      <label class="pattern-input">
        <MagnifyingGlassIcon size={12} />
        <input
          class="mono"
          bind:value={pattern}
          placeholder="Pattern (e.g. *, user:*, session:*)"
          aria-label="Key match pattern"
        />
      </label>
      <select bind:value={keyType} aria-label="Filter by Redis type" onchange={() => void runScan(false)}>
        <option value="all">All types</option>
        <option value="string">string</option>
        <option value="hash">hash</option>
        <option value="list">list</option>
        <option value="set">set</option>
        <option value="zset">zset</option>
        <option value="stream">stream</option>
      </select>
      <select bind:value={count} aria-label="Scan batch size">
        <option value={50}>50 / page</option>
        <option value={100}>100 / page</option>
        <option value={250}>250 / page</option>
      </select>
      <RcButton small type="submit" disabled={loading}>Run SCAN</RcButton>
    </form>

    {#if error}
      <div class="error-banner" role="alert">
        <WarningCircleIcon size={14} />
        <span>{error}</span>
      </div>
    {/if}

    {#if keys.length === 0 && !loading}
      <p class="empty">
        {#if !connected}
          Redis target is unreachable. Start Redis or adjust connection credentials above.
        {:else}
          No keys matched <code>{pattern || "*"}</code> in <code>db{auth.db ?? 0}</code>.
        {/if}
      </p>
    {:else if viewMode === "flat"}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Key</th>
              <th>Type</th>
              <th>TTL</th>
              <th>Memory</th>
              <th class="col-actions"></th>
            </tr>
          </thead>
          <tbody>
            {#each keys as item (item.key)}
              <tr onclick={() => openInspect(item.key)}>
                <td class="key-cell mono" title={item.key}>{item.key}</td>
                <td><RcTonePill tone={redisTypeTone(item.type)}>{item.type}</RcTonePill></td>
                <td class="mono muted">{formatRedisTTL(item.ttlMs)}</td>
                <td class="mono muted">{item.memoryBytes ? formatBytes(item.memoryBytes) : "--"}</td>
                <td class="col-actions">
                  <button type="button" class="row-btn" onclick={(e) => copyName(item.key, e)} title="Copy key">
                    {#if copiedKey === item.key}<CheckIcon size={12} />{:else}<CopyIcon size={12} />{/if}
                  </button>
                  <button
                    type="button"
                    class="row-btn danger"
                    class:armed={armedDeleteKey === item.key}
                    onclick={(e) => handleRowDelete(item.key, e)}
                    title={armedDeleteKey === item.key ? "Click again to delete" : "Delete key"}
                  >
                    <TrashIcon size={12} />
                    {#if armedDeleteKey === item.key}<span>Confirm</span>{/if}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <div class="tree-list">
        {#each grouped as group (group.prefix)}
          {@const collapsed = !!collapsedGroups[group.prefix]}
          <div class="tree-group">
            <button type="button" class="group-head" onclick={() => toggleGroup(group.prefix)}>
              {#if collapsed}<CaretRightIcon size={12} />{:else}<CaretDownIcon size={12} />{/if}
              <span class="mono">{group.prefix}{group.prefix === "(root)" ? "" : ":*"}</span>
              <span class="group-count">{group.keys.length}</span>
            </button>
            {#if !collapsed}
              <div class="group-rows">
                {#each group.keys as item (item.key)}
                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <div class="tree-row" onclick={() => openInspect(item.key)}>
                    <span class="key-cell mono" title={item.key}>{item.key}</span>
                    <span class="tree-meta">
                      <RcTonePill tone={redisTypeTone(item.type)}>{item.type}</RcTonePill>
                      <span class="mono muted">{formatRedisTTL(item.ttlMs)}</span>
                      <button type="button" class="row-btn" onclick={(e) => copyName(item.key, e)} title="Copy key">
                        {#if copiedKey === item.key}<CheckIcon size={12} />{:else}<CopyIcon size={12} />{/if}
                      </button>
                      <button
                        type="button"
                        class="row-btn danger"
                        class:armed={armedDeleteKey === item.key}
                        onclick={(e) => handleRowDelete(item.key, e)}
                        title={armedDeleteKey === item.key ? "Click again to delete" : "Delete key"}
                      >
                        <TrashIcon size={12} />
                      </button>
                    </span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}

    {#if hasMore}
      <div class="pager">
        <span class="muted">Showing {keys.length} scanned keys · cursor <code>{nextCursor}</code></span>
        <RcButton small variant="ghost" disabled={loading} onclick={() => runScan(true)}>
          {loading ? "Loading..." : "Load more"}
        </RcButton>
      </div>
    {/if}
  </RcPanel>
</div>

{#if drawerOpen}
  <RedisKeyDrawer
    {targetId}
    {auth}
    initialKey={editingKey}
    onclose={() => (drawerOpen = false)}
    onmutated={() => {
      void runScan(false);
      onMutated();
    }}
  />
{/if}

<style>
  .keys-browser { display: flex; flex-direction: column; gap: 14px; }
  .toolbar-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .view-toggle {
    display: inline-flex; padding: 2px; border-radius: 7px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
  }
  .view-toggle button {
    border: 0; background: transparent; color: var(--text-tertiary);
    padding: 3px 8px; border-radius: 5px; font-size: 11px; cursor: pointer;
  }
  .view-toggle button.active { background: var(--bg-panel); color: var(--text-primary); }
  .scan-bar {
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 12px;
  }
  .pattern-input {
    flex: 1; min-width: 190px; display: flex; align-items: center; gap: 7px;
    height: 30px; padding: 0 9px; border-radius: 7px;
    border: 1px solid var(--border-subtle); background: var(--bg-element); color: var(--text-tertiary);
  }
  .pattern-input:focus-within { border-color: var(--border-focus); }
  .pattern-input input {
    flex: 1; min-width: 0; border: 0; outline: none; background: transparent;
    color: var(--text-primary); font-size: 12px;
  }
  .scan-bar select {
    height: 30px; padding: 0 8px; border-radius: 7px;
    border: 1px solid var(--border-subtle); background: var(--bg-element);
    color: var(--text-primary); font-size: 11.5px;
  }
  .error-banner {
    display: flex; align-items: center; gap: 8px; padding: 9px 11px; margin-bottom: 12px;
    border-radius: 7px; border: 1px solid color-mix(in srgb, var(--accent-red) 40%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red); font-size: 12px;
  }
  .empty { padding: 24px 8px; font-size: 12px; color: var(--text-tertiary); text-align: center; }
  .table-wrap { overflow-x: auto; border: 1px solid var(--border-subtle); border-radius: 8px; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th {
    text-align: left; padding: 8px 10px; font-size: 10.5px; text-transform: uppercase;
    letter-spacing: 0.05em; color: var(--text-tertiary); border-bottom: 1px solid var(--border-subtle);
    background: var(--bg-element);
  }
  td { padding: 8px 10px; border-bottom: 1px solid var(--border-subtle); color: var(--text-primary); }
  tbody tr { cursor: pointer; }
  tbody tr:hover { background: var(--bg-element); }
  tbody tr:last-child td { border-bottom: 0; }
  .key-cell { max-width: 420px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .muted { color: var(--text-tertiary); font-size: 11.5px; }
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }
  .col-actions { text-align: right; white-space: nowrap; width: 1%; }
  .row-btn {
    display: inline-flex; align-items: center; gap: 4px; padding: 4px 6px; border-radius: 5px;
    border: 1px solid transparent; background: transparent; color: var(--text-tertiary);
    cursor: pointer; font-size: 11px;
  }
  .row-btn:hover { background: var(--bg-panel); border-color: var(--border-subtle); color: var(--text-primary); }
  .row-btn.danger:hover, .row-btn.danger.armed {
    color: var(--accent-red);
    border-color: color-mix(in srgb, var(--accent-red) 45%, transparent);
    background: color-mix(in srgb, var(--accent-red) 12%, transparent);
  }
  .tree-list { display: flex; flex-direction: column; gap: 8px; }
  .tree-group { border: 1px solid var(--border-subtle); border-radius: 8px; overflow: hidden; }
  .group-head {
    width: 100%; display: flex; align-items: center; gap: 8px; padding: 8px 10px;
    border: 0; background: var(--bg-element); color: var(--text-primary);
    font-size: 12px; cursor: pointer; text-align: left;
  }
  .group-count {
    margin-left: auto; font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary);
  }
  .group-rows { display: flex; flex-direction: column; }
  .tree-row {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding: 7px 12px 7px 26px; border-top: 1px solid var(--border-subtle); cursor: pointer; font-size: 12px;
  }
  .tree-row:hover { background: var(--bg-element); }
  .tree-meta { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
  .pager {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding-top: 12px; font-size: 11.5px;
  }
</style>
