<script lang="ts">
  import { onMount } from "svelte";
  import { MagnifyingGlassIcon } from "phosphor-svelte";
  import SectionHeader from "./section-header.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import RcResizableAside from "$lib/components/rack/rc-resizable-aside.svelte";
  import BucketDetail from "$lib/components/s3/bucket-detail.svelte";
  import { getDashboard } from "$lib/state.svelte";
  import { formatBytes } from "$lib/utils";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const buckets = $derived(dashboard.data?.buckets ?? []);

  // Keyed by name: polling replaces the objects, so holding one would freeze the panel.
  let selectedName = $state<string | null>(null);
  let prefix = $state("");
  let objectKey = $state<string | null>(null);
  const selected = $derived(buckets.find((b) => b.name === selectedName) ?? buckets[0] ?? null);

  const totalObjects = $derived(buckets.reduce((n, b) => n + b.objects, 0));
  const totalSize = $derived(buckets.reduce((n, b) => n + b.totalSize, 0));

  let query = $state("");
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return q ? buckets.filter((b) => b.name.toLowerCase().includes(q)) : buckets;
  });

  function syncHash() {
    const qs = new URLSearchParams();
    if (selectedName) qs.set("bucket", selectedName);
    if (prefix) qs.set("prefix", prefix);
    if (objectKey) qs.set("key", objectKey);
    const s = qs.toString();
    history.replaceState(null, "", `#storage${s ? `?${s}` : ""}`);
  }

  function select(name: string) {
    selectedName = name;
    prefix = "";
    objectKey = null;
    syncHash();
  }

  function navigate(nextPrefix: string, key: string | null) {
    selectedName = selected?.name ?? null;
    prefix = nextPrefix;
    objectKey = key;
    syncHash();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    if (visible.length === 0) return;
    e.preventDefault();
    const idx = visible.findIndex((b) => b.name === selected?.name);
    const next = e.key === "ArrowDown" ? Math.min(visible.length - 1, idx + 1) : Math.max(0, idx - 1);
    select(visible[next].name);
  }

  onMount(() => {
    const qs = new URLSearchParams(window.location.hash.split("?")[1] ?? "");
    selectedName = qs.get("bucket");
    prefix = qs.get("prefix") ?? "";
    objectKey = qs.get("key");
  });
</script>

<div class="storage">
  <SectionHeader
    title="S3 Storage"
    description="{buckets.length} bucket{buckets.length === 1 ? '' : 's'} · {totalObjects.toLocaleString('en-GB')} objects · {formatBytes(totalSize)}"
    {sidebarCollapsed}
    {onToggleSidebar}
  />

  {#if dashboard.loading && !dashboard.data}
    <div class="layout">
      <div class="skeleton-list">
        {#each Array(5) as _, i (i)}<span style:--i={i}></span>{/each}
      </div>
    </div>
  {:else if buckets.length === 0}
    <div class="blank">
      <h2>No buckets yet</h2>
      <p>Create one with <code>tarn s3 mb --name my-bucket</code>, or deploy through your IaC, and it shows up here.</p>
    </div>
  {:else}
    <div class="layout">
      <RcResizableAside storageKey="tarn-storage-list-width">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="bucket-list" onkeydown={onKeydown}>
          <label class="search">
            <MagnifyingGlassIcon size={12} />
            <input placeholder="Filter buckets" bind:value={query} aria-label="Filter buckets" />
            <span class="count">{visible.length}</span>
          </label>
          <div class="rows">
            {#each visible as b (b.name)}
              <RcListRow
                mono
                title={b.name}
                sub="{b.objects.toLocaleString('en-GB')} object{b.objects === 1 ? '' : 's'}"
                selected={b.name === selected?.name}
                onclick={() => select(b.name)}
              >
                {#snippet trailing()}<span class="size">{formatBytes(b.totalSize)}</span>{/snippet}
              </RcListRow>
            {:else}
              <p class="none">No match for “{query}”</p>
            {/each}
          </div>
        </div>
      </RcResizableAside>
      {#if selected}
        {#key selected.name}
          <BucketDetail bucket={selected} {prefix} selectedKey={objectKey} onnavigate={navigate} />
        {/key}
      {/if}
    </div>
  {/if}
</div>

<style>
  .storage { display: flex; flex-direction: column; min-height: 100%; }
  .layout {
    display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 28px; padding: 20px 0 48px;
    max-width: 1320px; align-items: start;
  }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); }
  }

  .bucket-list { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
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
  .size { font: 10.5px var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums; color: var(--text-tertiary); }
  .none { padding: 12px 10px; font-size: 11.5px; color: var(--text-tertiary); }

  .blank { padding: 64px 0; text-align: center; animation: fadeUp 320ms var(--ease-snappy) both; }
  .blank h2 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .blank p { margin-top: 4px; font-size: 12px; color: var(--text-secondary); }
  .blank code { font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px; color: var(--text-primary); }

  .skeleton-list { display: flex; flex-direction: column; gap: 6px; width: 260px; }
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
