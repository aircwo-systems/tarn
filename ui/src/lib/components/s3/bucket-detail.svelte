<script lang="ts">
  import {
    ArrowClockwiseIcon,
    CheckIcon,
    CopyIcon,
    FileIcon,
    FolderIcon,
    MagnifyingGlassIcon,
    UploadSimpleIcon,
  } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import ObjectPreview from "./object-preview.svelte";
  import { deleteObject, leafName, listObjects, putObject, type S3Listing } from "$lib/s3";
  import { refresh } from "$lib/state.svelte";
  import { formatBytes, formatDate, timeAgo } from "$lib/utils";
  import type { BucketSummary } from "$lib/types";

  let {
    bucket,
    prefix,
    selectedKey,
    onnavigate,
  }: {
    bucket: BucketSummary;
    prefix: string;
    selectedKey: string | null;
    /** change folder and/or open an object; both live in the URL */
    onnavigate: (prefix: string, key: string | null) => void;
  } = $props();

  let listing = $state<S3Listing | null>(null);
  let loading = $state(false);
  let error = $state("");
  let query = $state("");

  async function load() {
    loading = true;
    error = "";
    try {
      listing = await listObjects(bucket.name, prefix);
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to list objects";
    } finally {
      loading = false;
    }
  }

  // Reload when the folder changes, and when the dashboard reports a different object count.
  $effect(() => {
    void prefix;
    void bucket.objects;
    load();
  });

  const crumbs = $derived.by(() => {
    const parts = prefix.split("/").filter(Boolean);
    return parts.map((name, i) => ({ name, prefix: parts.slice(0, i + 1).join("/") + "/" }));
  });

  const q = $derived(query.trim().toLowerCase());
  const folders = $derived((listing?.prefixes ?? []).filter((p) => !q || leafName(p, prefix).toLowerCase().includes(q)));
  const files = $derived((listing?.objects ?? []).filter((o) => !q || leafName(o.key, prefix).toLowerCase().includes(q)));
  const selected = $derived(listing?.objects.find((o) => o.key === selectedKey) ?? null);

  const largest = $derived(
    (bucket.previewObjects ?? []).reduce<{ key: string; size: number } | null>(
      (max, o) => (!max || o.size > max.size ? o : max),
      null,
    ),
  );
  const lastWrite = $derived(
    (bucket.previewObjects ?? []).map((o) => o.lastModified).sort().at(-1),
  );

  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  async function copyUri() {
    await navigator.clipboard.writeText(`s3://${bucket.name}/${prefix}`);
    copied = true;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied = false), 1600);
  }

  let fileInput = $state<HTMLInputElement>();
  let uploading = $state(0);
  async function upload(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const picked = Array.from(input.files ?? []);
    input.value = "";
    if (picked.length === 0) return;
    uploading = picked.length;
    error = "";
    try {
      for (const file of picked) {
        await putObject(bucket.name, prefix + file.name, file);
        uploading -= 1;
      }
    } catch (err) {
      error = err instanceof Error ? err.message : "Upload failed";
    } finally {
      uploading = 0;
      await Promise.all([load(), refresh()]);
    }
  }

  async function removeSelected() {
    if (!selected) return;
    try {
      await deleteObject(bucket.name, selected.key);
      onnavigate(prefix, null);
    } catch (err) {
      error = err instanceof Error ? err.message : "Delete failed";
    }
    await Promise.all([load(), refresh()]);
  }

  function onRowKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const keys = files.map((f) => f.key);
    if (keys.length === 0) return;
    e.preventDefault();
    const idx = keys.indexOf(selectedKey ?? "");
    const next = e.key === "ArrowDown" ? Math.min(keys.length - 1, idx + 1) : Math.max(0, idx - 1);
    onnavigate(prefix, keys[next]);
  }
</script>

<div class="detail">
  <header class="hero">
    <div class="identity">
      <h1 title={bucket.name}>{bucket.name}</h1>
      <p class="subline">
        <span>s3://{bucket.name}</span><i></i><span>created {formatDate(bucket.createdDate)}</span>
      </p>
    </div>
    <div class="hero-actions">
      <RcButton onclick={copyUri}>
        {#if copied}<CheckIcon size={12} />Copied{:else}<CopyIcon size={12} />S3 URI{/if}
      </RcButton>
      <RcButton variant="primary" disabled={uploading > 0} onclick={() => fileInput?.click()}>
        <UploadSimpleIcon size={12} />{uploading > 0 ? `Uploading ${uploading}…` : "Upload"}
      </RcButton>
      <input bind:this={fileInput} type="file" multiple hidden onchange={upload} />
    </div>
  </header>

  <div class="stats">
    <RcStat label="Objects" value={bucket.objects.toLocaleString("en-GB")} sub="across all prefixes" />
    <RcStat label="Size" value={formatBytes(bucket.totalSize)} sub="{bucket.totalSize.toLocaleString('en-GB')} bytes" />
    <RcStat
      label="Average"
      value={bucket.objects ? formatBytes(Math.round(bucket.totalSize / bucket.objects)) : "--"}
      sub={largest ? `largest ${formatBytes(largest.size)}` : "per object"}
    />
    <RcStat label="Last write" value={lastWrite ? timeAgo(lastWrite) : "--"} sub={lastWrite ? formatDate(lastWrite) : "no objects"} />
  </div>

  {#if error}<p class="error">{error}</p>{/if}

  <RcPanel index={0}>
    <div class="toolbar">
      <nav class="crumbs" aria-label="Prefix">
        <button type="button" class:current={crumbs.length === 0} onclick={() => onnavigate("", null)}>{bucket.name}</button>
        {#each crumbs as c (c.prefix)}
          <span class="sep">/</span>
          <button type="button" class:current={c.prefix === prefix} onclick={() => onnavigate(c.prefix, null)}>{c.name}</button>
        {/each}
      </nav>
      <label class="search">
        <MagnifyingGlassIcon size={12} />
        <input placeholder="Filter this level" bind:value={query} aria-label="Filter objects" />
      </label>
      <RcButton small variant="ghost" title="Refresh" onclick={load}>
        <span class:spin={loading}><ArrowClockwiseIcon size={12} /></span>
      </RcButton>
    </div>

    {#if !listing && loading}
      <div class="skeleton">{#each Array(4) as _, i (i)}<span style:--i={i}></span>{/each}</div>
    {:else if folders.length === 0 && files.length === 0}
      {#if q}
        <p class="empty">Nothing at this level matches “{query}”.</p>
      {:else}
        <div class="blank">
          <h3>{prefix ? "No objects under this prefix" : "This bucket has no objects yet"}</h3>
          <p>Use <strong>Upload</strong> above, or copy a file in from your terminal:</p>
          <pre>tarn s3 cp --bucket {bucket.name} --key {prefix}hello.txt --file ./hello.txt</pre>
        </div>
      {/if}
    {:else}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="rows" onkeydown={onRowKeydown}>
        <div class="row head"><span>Name</span><span>Size</span><span>Modified</span></div>
        {#each folders as p (p)}
          <button type="button" class="row" onclick={() => onnavigate(p, null)}>
            <span class="name"><FolderIcon size={13} /><span>{leafName(p, prefix)}</span></span>
            <span class="dim">—</span><span class="dim">—</span>
          </button>
        {/each}
        {#each files as o (o.key)}
          <button type="button" class="row" class:selected={o.key === selectedKey} aria-pressed={o.key === selectedKey} onclick={() => onnavigate(prefix, o.key === selectedKey ? null : o.key)}>
            <span class="name"><FileIcon size={13} /><span title={o.key}>{leafName(o.key, prefix)}</span></span>
            <span>{formatBytes(o.size)}</span>
            <span class="dim" title={formatDate(o.lastModified)}>{timeAgo(o.lastModified)}</span>
          </button>
        {/each}
      </div>
      {#if listing?.truncated}<p class="empty">Showing the first 1,000 keys at this level.</p>{/if}
    {/if}
  </RcPanel>

  {#if selected}
    {#key selected.key}
      <ObjectPreview bucket={bucket.name} object={selected} onclose={() => onnavigate(prefix, null)} ondelete={removeSelected} />
    {/key}
  {/if}
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .hero { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; flex-wrap: wrap; padding: 4px 2px 2px; }
  .identity { min-width: 0; }
  h1 {
    font: 600 19px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }
  .hero-actions { display: flex; gap: 6px; }

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .error {
    padding: 8px 12px; border-radius: 8px; font-size: 11.5px; color: var(--accent-red);
    border: 1px solid color-mix(in srgb, var(--accent-red) 45%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
  }

  .toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
  .crumbs { display: flex; align-items: center; gap: 2px; flex: 1; min-width: 0; overflow-x: auto; font: 12px var(--font-mono, ui-monospace, monospace); }
  .crumbs button { padding: 2px 6px; border-radius: 6px; color: var(--text-tertiary); white-space: nowrap; transition: color 120ms ease, background 120ms ease; }
  .crumbs button:hover { color: var(--text-primary); background: var(--bg-element-hover); }
  .crumbs button.current { color: var(--text-primary); }
  .sep { color: var(--text-tertiary); opacity: 0.6; }

  .search {
    display: flex; align-items: center; gap: 7px; width: 200px; height: 26px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app); color: var(--text-tertiary);
    transition: border-color 120ms ease;
  }
  .search:hover { border-color: var(--border-default); }
  .search:focus-within { border-color: var(--border-focus); }
  .search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: none; font-size: 11.5px; color: var(--text-primary); }
  .search input::placeholder { color: var(--text-tertiary); }
  .spin { display: inline-flex; animation: spin 900ms linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  .rows { display: flex; flex-direction: column; gap: 2px; }
  .row {
    position: relative; display: grid; grid-template-columns: minmax(0, 1fr) 90px 110px; align-items: center; gap: 12px;
    width: 100%; padding: 6px 10px; border-radius: 8px; border: 1px solid transparent; text-align: left;
    font: 11.5px var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums; color: var(--text-secondary);
    transition: background 120ms ease, border-color 120ms ease, padding-left 220ms var(--ease-snappy);
  }
  .row > span:not(:first-child) { text-align: right; white-space: nowrap; }
  .row.head {
    padding-top: 0; padding-bottom: 4px; font: 10px var(--font-sans, inherit); letter-spacing: 0.06em; text-transform: uppercase;
    color: var(--text-tertiary);
  }
  button.row:hover { background: var(--bg-element-hover); color: var(--text-primary); }
  button.row:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 1px; }
  .row.selected { padding-left: 18px; background: var(--bg-element); border-color: var(--border-default); color: var(--text-primary); }
  .row.selected::before {
    content: ""; position: absolute; left: 6px; top: 8px; bottom: 8px; width: 2.5px;
    border-radius: 2px; background: var(--text-primary); opacity: 0.9;
  }
  .name { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .name :global(svg) { flex-shrink: 0; color: var(--text-tertiary); }
  .name span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .dim { color: var(--text-tertiary); }

  .empty { padding: 10px; font-size: 11.5px; color: var(--text-tertiary); }
  .blank { display: flex; flex-direction: column; gap: 4px; padding: 18px 10px 8px; }
  .blank h3 { font-size: 12.5px; font-weight: 600; color: var(--text-primary); }
  .blank p { font-size: 11.5px; color: var(--text-secondary); }
  .blank strong { font-weight: 500; color: var(--text-primary); }
  .blank pre {
    margin-top: 8px; padding: 8px 12px; border-radius: 8px; background: var(--bg-app); overflow-x: auto;
    font: 11.5px/1.6 var(--font-mono, ui-monospace, monospace); color: var(--text-primary); user-select: all;
  }

  .skeleton { display: flex; flex-direction: column; gap: 4px; }
  .skeleton span {
    height: 28px; border-radius: 8px; background: var(--bg-element);
    animation: pulse 1.4s ease-in-out infinite; animation-delay: calc(var(--i) * 80ms);
  }
  @keyframes pulse { 50% { opacity: 0.5; } }
  @media (prefers-reduced-motion: reduce) { .stats, .skeleton span, .spin { animation: none; } }
</style>
