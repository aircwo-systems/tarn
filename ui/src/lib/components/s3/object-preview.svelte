<script lang="ts">
  import { CheckIcon, CopyIcon, DownloadSimpleIcon, TrashIcon, XIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import {
    headObject,
    isImage,
    isText,
    objectPath,
    MAX_IMAGE_PREVIEW_BYTES,
    MAX_TEXT_PREVIEW_BYTES,
    type S3Object,
    type S3ObjectHead,
  } from "$lib/s3";
  import { formatJSONForViewer } from "$lib/json-format";
  import { formatBytes, formatDate } from "$lib/utils";

  let {
    bucket,
    object,
    onclose,
    ondelete,
  }: {
    bucket: string;
    object: S3Object;
    onclose: () => void;
    ondelete: () => Promise<void>;
  } = $props();

  let head = $state<S3ObjectHead | null>(null);
  let content = $state<string | null>(null);
  let notice = $state("");
  let error = $state("");
  let loading = $state(true);
  let showRaw = $state(false);

  const href = $derived(objectPath(bucket, object.key));
  const json = $derived(content !== null ? formatJSONForViewer(content) : null);

  $effect(() => {
    const key = object.key;
    let cancelled = false;
    (async () => {
      try {
        const h = await headObject(bucket, key);
        if (cancelled) return;
        head = h;
        const size = h.contentLength || object.size;
        if (isImage(h.contentType)) {
          if (size > MAX_IMAGE_PREVIEW_BYTES) notice = `Images over ${formatBytes(MAX_IMAGE_PREVIEW_BYTES)} aren't previewed.`;
        } else if (!isText(h.contentType)) {
          notice = "Binary content — download to inspect.";
        } else if (size > MAX_TEXT_PREVIEW_BYTES) {
          notice = `Objects over ${formatBytes(MAX_TEXT_PREVIEW_BYTES)} aren't previewed.`;
        } else {
          const resp = await fetch(objectPath(bucket, key));
          if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
          const text = await resp.text();
          if (!cancelled) content = text;
        }
      } catch (err) {
        if (!cancelled) error = err instanceof Error ? err.message : "Failed to load object";
      } finally {
        if (!cancelled) loading = false;
      }
    })();
    return () => (cancelled = true);
  });

  const meta = $derived([
    { label: "Key", value: object.key, mono: true },
    { label: "Size", value: `${formatBytes(object.size)} (${object.size.toLocaleString("en-GB")} B)`, mono: true },
    { label: "Content type", value: head?.contentType ?? "--", mono: true },
    { label: "Modified", value: formatDate(object.lastModified) },
    { label: "ETag", value: object.etag || head?.etag || "--", mono: true, dim: true },
    { label: "URI", value: `s3://${bucket}/${object.key}`, mono: true, dim: true },
  ]);

  let copied = $state<"uri" | "content" | null>(null);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  async function copy(kind: "uri" | "content") {
    await navigator.clipboard.writeText(kind === "uri" ? `s3://${bucket}/${object.key}` : (content ?? ""));
    copied = kind;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied = null), 1600);
  }

  let confirming = $state(false);
  let deleting = $state(false);
  async function remove() {
    deleting = true;
    try {
      await ondelete();
    } finally {
      deleting = false;
      confirming = false;
    }
  }
</script>

<RcPanel title={object.key.split("/").pop() || object.key} description="Object" index={1}>
  {#snippet actions()}
    <RcButton small variant="ghost" onclick={() => copy("uri")}>
      {#if copied === "uri"}<CheckIcon size={11} />Copied{:else}<CopyIcon size={11} />URI{/if}
    </RcButton>
    <RcButton small href={href}><DownloadSimpleIcon size={11} />Open</RcButton>
    {#if confirming}
      <RcButton small variant="danger" disabled={deleting} onclick={remove}>{deleting ? "Deleting…" : "Confirm delete"}</RcButton>
      <RcButton small variant="ghost" onclick={() => (confirming = false)}>Cancel</RcButton>
    {:else}
      <RcButton small variant="ghost" title="Delete object" onclick={() => (confirming = true)}><TrashIcon size={11} /></RcButton>
    {/if}
    <RcButton small variant="ghost" title="Close" onclick={onclose}><XIcon size={11} /></RcButton>
  {/snippet}

  <div class="body">
    <RcKv items={meta} labelWidth="6.5rem" />

    <div class="preview">
      <div class="preview-bar">
        <span>Preview</span>
        {#if content !== null}
          <div class="modes">
            {#if json}
              <button type="button" class:on={!showRaw} onclick={() => (showRaw = false)}>JSON</button>
              <button type="button" class:on={showRaw} onclick={() => (showRaw = true)}>Raw</button>
            {/if}
            <button type="button" onclick={() => copy("content")}>{copied === "content" ? "Copied" : "Copy"}</button>
          </div>
        {/if}
      </div>

      {#if loading}
        <p class="note">Loading…</p>
      {:else if error}
        <p class="note err">{error}</p>
      {:else if notice}
        <p class="note">{notice}</p>
      {:else if head && isImage(head.contentType)}
        <div class="image"><img src={href} alt={object.key} /></div>
      {:else if content !== null}
        {#if content === ""}
          <p class="note">Empty object.</p>
        {:else if json && !showRaw}
          <pre>{@html json.formattedHtml}</pre>
        {:else}
          <pre>{content}</pre>
        {/if}
      {/if}
    </div>
  </div>
</RcPanel>

<style>
  .body { display: flex; flex-direction: column; gap: 16px; }
  .preview { display: flex; flex-direction: column; gap: 8px; }
  .preview-bar { display: flex; align-items: center; justify-content: space-between; }
  .preview-bar > span { font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary); }
  .modes { display: flex; gap: 2px; }
  .modes button {
    height: 22px; padding: 0 8px; border-radius: 6px; font-size: 11px; color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .modes button:hover { color: var(--text-primary); }
  .modes button.on { color: var(--text-primary); background: var(--bg-element); }
  pre {
    max-height: 480px; overflow: auto; padding: 10px 12px; border-radius: 8px; background: var(--bg-app);
    font: 11.5px/1.6 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
    white-space: pre-wrap; word-break: break-word;
  }
  .image { display: flex; justify-content: center; padding: 14px; border-radius: 8px; background: var(--bg-app); }
  .image img { max-width: 100%; max-height: 420px; border-radius: 4px; }
  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  .note.err { color: var(--accent-red); }
</style>
