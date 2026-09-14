<script lang="ts">
  import { CheckIcon, CopyIcon, EyeIcon, EyeSlashIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import { fetchSecretValue } from "$lib/api";
  import { formatJSONForViewer } from "$lib/json-format";
  import type { SecretSummary, SecretValueResult } from "$lib/types";
  import { formatDate, timeAgo } from "$lib/utils";

  let { secret }: { secret: SecretSummary } = $props();

  // Value is fetched on demand and dropped when the panel unmounts (parent keys on name).
  let value = $state<SecretValueResult | null>(null);
  let visible = $state(false);
  let loading = $state(false);
  let error = $state("");
  let showRaw = $state(false);

  const json = $derived(value && value.valueType === "string" && value.value ? formatJSONForViewer(value.value) : null);
  const tags = $derived(Object.entries(secret.tags ?? {}).sort(([a], [b]) => a.localeCompare(b)));

  async function toggle() {
    if (visible) {
      visible = false;
      return;
    }
    if (!value) {
      loading = true;
      error = "";
      try {
        value = await fetchSecretValue(secret.name);
      } catch (err) {
        error = err instanceof Error ? err.message : "Failed to load secret value";
        return;
      } finally {
        loading = false;
      }
    }
    visible = true;
  }

  let copied = $state<string | null>(null);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  async function copy(text: string, field: string) {
    try {
      await navigator.clipboard.writeText(text);
      copied = field;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = null), 1600);
    } catch {}
  }

  const details = $derived([
    { label: "Name", value: secret.name, mono: true },
    { label: "ARN", value: secret.arn, mono: true, dim: true },
    { label: "Description", value: secret.description || "--" },
    { label: "Version", value: secret.versionId || "--", mono: true, dim: true },
    { label: "Created", value: formatDate(secret.createdDate) },
    { label: "Last changed", value: formatDate(secret.lastChangedDate) },
  ]);
</script>

<div class="detail">
  <header class="hero">
    <div class="identity">
      <h1 title={secret.name}>{secret.name}</h1>
      <div class="subline">
        <span>Secret</span><i></i>
        <button type="button" class="arn" title="Copy ARN" onclick={() => copy(secret.arn, "arn")}>
          <span>{secret.arn}</span>
          {#if copied === "arn"}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
        </button>
      </div>
    </div>
  </header>

  <div class="stats">
    <RcStat label="Tags" value={secret.tagCount} sub={tags.length ? tags.map(([k]) => k).join(", ") : "untagged"} />
    <RcStat label="Version" value={secret.versionId ? secret.versionId.slice(0, 8) : "--"} sub="current" />
    <RcStat label="Created" value={timeAgo(secret.createdDate)} sub={formatDate(secret.createdDate)} />
    <RcStat label="Last changed" value={timeAgo(secret.lastChangedDate)} sub={formatDate(secret.lastChangedDate)} />
  </div>

  <RcPanel title="Value" description={visible && value ? value.valueType : "Hidden until revealed"} index={0}>
    {#snippet actions()}
      {#if visible && value?.value}
        <RcButton small variant="ghost" onclick={() => copy(value!.value, "value")}>
          {#if copied === "value"}<CheckIcon size={11} />Copied{:else}<CopyIcon size={11} />Copy{/if}
        </RcButton>
      {/if}
      <RcButton small variant={visible ? "default" : "primary"} disabled={loading} onclick={toggle}>
        {#if visible}<EyeSlashIcon size={11} />Hide{:else}<EyeIcon size={11} />{loading ? "Loading…" : "Reveal"}{/if}
      </RcButton>
    {/snippet}

    {#if error}
      <p class="note err">{error}</p>
    {:else if !visible}
      <button type="button" class="masked" onclick={toggle} disabled={loading}>
        <span>••••••••••••••••</span>
        <em>{loading ? "Loading…" : "Click to reveal"}</em>
      </button>
    {:else if value}
      {#if value.valueType === "binary"}
        <div class="bar"><span>Base64</span></div>
        <pre>{value.value || "(empty binary)"}</pre>
      {:else if !value.value}
        <p class="note">Empty value.</p>
      {:else if json}
        <div class="bar">
          <span>{showRaw ? "Raw" : "JSON"}</span>
          <div class="modes">
            <button type="button" class:on={!showRaw} onclick={() => (showRaw = false)}>JSON</button>
            <button type="button" class:on={showRaw} onclick={() => (showRaw = true)}>Raw</button>
          </div>
        </div>
        {#if showRaw}<pre>{value.value}</pre>{:else}<pre>{@html json.formattedHtml}</pre>{/if}
      {:else}
        <pre>{value.value}</pre>
      {/if}
    {/if}
  </RcPanel>

  <RcPanel title="Details" index={1}>
    <RcKv items={details} labelWidth="7rem" />
  </RcPanel>

  <RcPanel title="Tags" description="{secret.tagCount} tag{secret.tagCount === 1 ? '' : 's'}" index={2}>
    {#if tags.length}
      <div class="tags">
        {#each tags as [k, v] (k)}
          <span class="tag"><b>{k}</b>{v}</span>
        {/each}
      </div>
    {:else}
      <p class="note">No tags on this secret.</p>
    {/if}
  </RcPanel>
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .hero { padding: 4px 2px 2px; }
  .identity { min-width: 0; }
  h1 {
    font: 600 19px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); min-width: 0; }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); flex-shrink: 0; }
  .arn {
    display: inline-flex; align-items: center; gap: 6px; min-width: 0; padding: 1px 4px; margin-left: -4px; border-radius: 6px;
    font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); transition: color 120ms ease, background 120ms ease;
  }
  .arn span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .arn:hover { color: var(--text-primary); background: var(--bg-element-hover); }

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .masked {
    display: flex; align-items: center; justify-content: space-between; width: 100%; padding: 14px 12px; border-radius: 8px;
    background: var(--bg-app); border: 1px dashed var(--border-subtle); text-align: left; transition: border-color 120ms ease;
  }
  .masked:hover:not(:disabled) { border-color: var(--border-default); }
  .masked span { font: 13px var(--font-mono, ui-monospace, monospace); letter-spacing: 0.1em; color: var(--text-tertiary); }
  .masked em { font-style: normal; font-size: 11px; color: var(--text-tertiary); }

  .bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
  .bar > span { font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary); }
  .modes { display: flex; gap: 2px; }
  .modes button {
    height: 22px; padding: 0 8px; border-radius: 6px; font-size: 11px; color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .modes button:hover { color: var(--text-primary); }
  .modes button.on { color: var(--text-primary); background: var(--bg-element); }
  pre {
    max-height: 420px; overflow: auto; padding: 10px 12px; border-radius: 8px; background: var(--bg-app);
    font: 11.5px/1.6 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
    white-space: pre-wrap; word-break: break-word;
  }
  .note { padding: 14px 12px; border-radius: 8px; background: var(--bg-app); font-size: 11.5px; color: var(--text-tertiary); }
  .note.err { color: var(--accent-red); }

  .tags { display: flex; flex-wrap: wrap; gap: 6px; }
  .tag {
    display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 9px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
  }
  .tag b { font-weight: 500; color: var(--text-primary); }
  .tag b::after { content: "="; margin-left: 6px; color: var(--text-tertiary); }
</style>
