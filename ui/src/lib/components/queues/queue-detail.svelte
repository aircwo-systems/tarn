<script lang="ts">
  import { ArrowsClockwiseIcon, CaretDownIcon, CaretUpIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import { formatUnixSeconds } from "$lib/utils";
  import type { QueueMessageSummary, QueueSummary } from "$lib/types";

  let {
    queue,
    messages,
    loading,
    error,
    onrefresh,
  }: {
    queue: QueueSummary;
    messages: QueueMessageSummary[];
    loading: boolean;
    error: string;
    onrefresh: () => void;
  } = $props();

  const numberFormatter = new Intl.NumberFormat("en-GB");
  const staleCount = $derived(queue.approxStale ?? 0);

  const config = $derived([
    { label: "Visibility", value: `${queue.visibilitySec}s`, mono: true },
    { label: "Long poll", value: `${queue.waitTimeSec}s`, mono: true },
    { label: "Created", value: formatUnixSeconds(queue.createdTimestamp), mono: true },
    {
      label: "Redrive",
      value: queue.dlqName ? `${queue.dlqName} (max ${queue.maxReceiveCount ?? "?"} receives)` : "none",
      mono: !!queue.dlqName,
      dim: !queue.dlqName,
    },
    { label: "URL", value: queue.url, mono: true, dim: true },
    { label: "ARN", value: queue.arn, mono: true, dim: true },
  ]);

  const tags = $derived(Object.entries(queue.tags ?? {}));

  function messageTone(state: string): Tone {
    if (state === "visible") return "green";
    if (state === "inflight") return "amber";
    if (state === "stale") return "red";
    return "neutral";
  }

  function formatSentAt(ms: number): string {
    if (!ms) return "--";
    return new Date(ms).toLocaleString();
  }

  const PREVIEW_LENGTH = 280;
  let expandedMessages = $state(new Set<string>());

  function toggleExpanded(id: string) {
    const next = new Set(expandedMessages);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    expandedMessages = next;
  }

  function isLargeBody(body: string): boolean {
    return body.length > PREVIEW_LENGTH;
  }

  function bodyPreview(body: string): string {
    if (body.length <= PREVIEW_LENGTH) return body;
    return body.slice(0, PREVIEW_LENGTH) + "…";
  }
</script>

<div class="detail">
  <!-- Hero -->
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={queue.name}>{queue.name}</h1>
        <RcTonePill tone={queue.fifo ? "amber" : "neutral"}>{queue.fifo ? "fifo" : "standard"}</RcTonePill>
        {#if staleCount > 0}
          <RcTonePill tone="red">{staleCount} stale</RcTonePill>
        {/if}
      </div>
      <p class="subline">
        <span>{queue.approxVisible} visible</span><i></i><span>{queue.approxInFlight} in flight</span><i></i><span>{queue.approxDelayed} delayed</span><i></i><span>{queue.processedCount ?? 0} processed</span>
      </p>
    </div>
    <div class="hero-actions">
      <button type="button" class="btn" onclick={onrefresh} disabled={loading}>
        <ArrowsClockwiseIcon size={12} class={loading ? "animate-spin" : ""} />Refresh
      </button>
    </div>
  </header>

  <!-- Numbers -->
  <div class="stats">
    <RcStat
      label="Visible"
      value={numberFormatter.format(queue.approxVisible)}
      sub="available for consumption"
      tone={queue.approxVisible > 0 ? "amber" : undefined}
    />
    <RcStat
      label="In Flight"
      value={numberFormatter.format(queue.approxInFlight)}
      sub="locked by consumers"
    />
    <RcStat
      label="Delayed"
      value={numberFormatter.format(queue.approxDelayed)}
      sub="not yet visible"
    />
    <RcStat
      label="Processed"
      value={numberFormatter.format(queue.processedCount ?? 0)}
      sub="since start"
    />
    <RcStat
      label="Stale"
      value={numberFormatter.format(staleCount)}
      sub={staleCount > 0 ? "parked, no DLQ" : "none parked"}
      tone={staleCount > 0 ? "red" : undefined}
    />
  </div>

  <RcPanel title="Messages" description="Live payloads currently on this queue." index={0}>
    {#snippet actions()}
      <button type="button" class="btn ghost" onclick={onrefresh} disabled={loading}>Refresh</button>
    {/snippet}
    {#if error}
      <p class="error">{error}</p>
    {/if}
    {#if loading}
      <p class="empty">Loading messages...</p>
    {:else if messages.length === 0}
      <p class="empty">No messages available for this queue.</p>
    {:else}
      <div class="messages">
        {#each messages as message (message.id)}
          {@const expanded = expandedMessages.has(message.id)}
          {@const large = isLargeBody(message.body ?? "")}
          {@const formatted = formatJSONForViewer(message.body ?? "")}
          {@const canExpand = large || formatted !== null}
          <div class="message">
            <div class="message-meta">
              <RcTonePill tone={messageTone(message.state)}>{message.state}</RcTonePill>
              <span class="message-id">{message.id}</span>
              {#if message.receiveCount > 0}
                <span class="message-flag">receives: {message.receiveCount}</span>
              {/if}
              {#if (message.retryCount ?? 0) > 0}
                <span class="message-flag">retried: {message.retryCount}</span>
              {/if}
              {#if large}
                <span class="message-flag">{(message.body ?? "").length} chars</span>
              {/if}
            </div>

            {#if expanded && formatted}
              <FormattedMessageViewer
                raw={message.body || "(empty)"}
                formatted={formatted.formatted}
                formattedHtml={formatted.formattedHtml}
                formattedContentClass="text-[11px] text-muted-foreground"
                rawContentClass="text-[11px] text-muted-foreground"
                formattedMaxHeightClass="max-h-96"
                rawMaxHeightClass="max-h-96"
              />
            {:else if expanded}
              <p class="message-body-scroll">{message.body || "(empty)"}</p>
            {:else}
              <p class="message-body">{bodyPreview(message.body ?? "") || "(empty)"}</p>
            {/if}

            {#if message.state === "stale"}
              <p class="stale-note">
                Parked. Failed {message.receiveCount} times with no DLQ. Tarn
                halts retries to prevent wasted invocations.
              </p>
            {/if}

            <div class="message-foot">
              <p class="message-time">{formatSentAt(message.sentAt)}</p>
              {#if canExpand}
                <button
                  type="button"
                  class="expand-btn"
                  onclick={() => toggleExpanded(message.id)}
                >
                  {#if expanded}
                    <CaretUpIcon size={12} />
                    Collapse
                  {:else}
                    <CaretDownIcon size={12} />
                    Expand
                  {/if}
                </button>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </RcPanel>

  <RcPanel title="Configuration" index={1}>
    <RcKv items={config} />
    {#if tags.length > 0}
      <div class="tags">
        {#each tags as [k, v] (k)}
          <span class="tag"><span>{k}</span>{v}</span>
        {/each}
      </div>
    {/if}
  </RcPanel>
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .hero { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; flex-wrap: wrap; padding: 4px 2px 2px; }
  .identity { min-width: 0; }
  .title-row { display: flex; align-items: center; gap: 8px; min-width: 0; flex-wrap: wrap; }
  h1 {
    font: 600 19px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }
  .hero-actions { display: flex; gap: 6px; }

  .btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary); text-decoration: none;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .btn:hover:not(:disabled) { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .btn:active:not(:disabled) { transform: scale(0.96); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn.ghost { height: 24px; padding: 0 9px; font-size: 11px; border-color: transparent; }
  .btn.ghost:hover:not(:disabled) { border-color: var(--border-subtle); }
  .btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  .stats {
    display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(3, minmax(0, 1fr)); } }

  .empty { font-size: 11.5px; color: var(--text-tertiary); }
  .error { margin-bottom: 10px; font-size: 11.5px; color: var(--accent-red); }

  .messages { display: flex; flex-direction: column; gap: 10px; }
  .message {
    border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--bg-app);
    padding: 10px 12px;
  }
  .message-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 8px; font-size: 11px; color: var(--text-tertiary); }
  .message-id { font-family: var(--font-mono, ui-monospace, monospace); }
  .message-flag { font-family: var(--font-mono, ui-monospace, monospace); }
  .message-body { word-break: break-all; font-size: 12px; color: var(--text-secondary); }
  .message-body-scroll {
    max-height: 24rem; overflow-y: auto; word-break: break-all; white-space: pre-wrap;
    font-size: 12px; color: var(--text-secondary);
  }
  .stale-note { margin-top: 6px; font-size: 11px; color: var(--accent-red); }
  .message-foot { display: flex; align-items: center; justify-content: space-between; margin-top: 8px; }
  .message-time { font-size: 11px; color: var(--text-tertiary); }
  .expand-btn {
    display: inline-flex; align-items: center; gap: 4px; font-size: 11px; color: var(--text-tertiary);
    transition: color 120ms ease;
  }
  .expand-btn:hover { color: var(--text-primary); }

  .tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; }
  .tag {
    display: inline-flex; gap: 6px; height: 22px; align-items: center; padding: 0 8px; border-radius: 8px;
    background: var(--bg-element); font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
  }
  .tag span { color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) { .stats { animation: none; } }
</style>
