<script lang="ts">
  import {
    ArrowsClockwiseIcon,
    CaretDownIcon,
    CaretUpIcon,
    PaperPlaneTiltIcon,
    TrashIcon,
    FloppyDiskIcon,
    CopyIcon,
    WarningCircleIcon,
  } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import QueueDisruptor from "./queue-disruptor.svelte";
  import QueueSendDialog from "./queue-send-dialog.svelte";
  import QueueRedriveDialog from "./queue-redrive-dialog.svelte";
  import QueueSaveEventDialog from "./queue-save-event-dialog.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import { formatUnixSeconds } from "$lib/utils";
  import { purgeQueue, deleteQueueMessage } from "$lib/api";
  import type { QueueMessageSummary, QueueSummary, FunctionSummary } from "$lib/types";

  let {
    queue,
    messages,
    loading,
    error,
    allQueues = [],
    functions = [],
    onrefresh,
  }: {
    queue: QueueSummary;
    messages: QueueMessageSummary[];
    loading: boolean;
    error: string;
    allQueues?: QueueSummary[];
    functions?: FunctionSummary[];
    onrefresh: () => void;
  } = $props();

  const numberFormatter = new Intl.NumberFormat("en-GB");
  const staleCount = $derived(queue.approxStale ?? 0);
  const isDLQ = $derived(allQueues.some((q) => q.dlqName === queue.name));
  const hasDLQRelationship = $derived(Boolean(queue.dlqName) || isDLQ);

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

  // Dialog & Action States
  let sendDialogOpen = $state(false);
  let sendDialogInitialBody = $state("");
  let redriveDialogOpen = $state(false);
  let saveEventOpen = $state(false);
  let saveEventBody = $state("");

  let purgeConfirmOpen = $state(false);
  let purging = $state(false);
  let purgeError = $state<string | null>(null);

  let deletingIds = $state(new Set<string>());
  let bannerMessage = $state<string | null>(null);

  function openSendDialog(initial = "") {
    sendDialogInitialBody = initial;
    sendDialogOpen = true;
  }

  function openSaveEvent(body: string) {
    saveEventBody = body;
    saveEventOpen = true;
  }

  async function handlePurge() {
    purging = true;
    purgeError = null;
    try {
      await purgeQueue(queue.name);
      purgeConfirmOpen = false;
      bannerMessage = `Queue "${queue.name}" purged successfully.`;
      setTimeout(() => (bannerMessage = null), 4000);
      onrefresh();
    } catch (err) {
      purgeError = err instanceof Error ? err.message : "Failed to purge queue";
    } finally {
      purging = false;
    }
  }

  async function handleDeleteMessage(id: string) {
    const next = new Set(deletingIds);
    next.add(id);
    deletingIds = next;

    try {
      await deleteQueueMessage(queue.name, id);
      onrefresh();
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to delete message");
    } finally {
      const updated = new Set(deletingIds);
      updated.delete(id);
      deletingIds = updated;
    }
  }
</script>

<div class="detail">
  <!-- Hero -->
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={queue.name}>{queue.name}</h1>
        <RcTonePill tone={queue.fifo ? "amber" : "neutral"}>{queue.fifo ? "fifo" : "standard"}</RcTonePill>
        {#if queue.disruptEnabled}
          <RcTonePill tone="red">disruptor {queue.disruptFailureRate ?? ""}%</RcTonePill>
        {/if}
        {#if staleCount > 0}
          <RcTonePill tone="red">{staleCount} stale</RcTonePill>
        {/if}
      </div>
      <p class="subline">
        <span>{queue.approxVisible} visible</span><i></i><span>{queue.approxInFlight} in flight</span><i></i><span>{queue.approxDelayed} delayed</span><i></i><span>{queue.processedCount ?? 0} processed</span>
      </p>
    </div>
    <div class="hero-actions">
      <button
        type="button"
        class="btn primary-action"
        onclick={() => openSendDialog()}
        title="Send a new message to this queue"
      >
        <PaperPlaneTiltIcon size={12} weight="fill" />Send Message
      </button>

      {#if hasDLQRelationship}
        <button
          type="button"
          class="btn dlq-action"
          onclick={() => (redriveDialogOpen = true)}
          title="Redrive messages between DLQ and source queue"
        >
          <ArrowsClockwiseIcon size={12} />Redrive DLQ
        </button>
      {/if}

      <button
        type="button"
        class="btn danger-action"
        onclick={() => (purgeConfirmOpen = !purgeConfirmOpen)}
        title="Purge all messages in this queue"
      >
        <TrashIcon size={12} />Purge
      </button>

      <button type="button" class="btn" onclick={onrefresh} disabled={loading}>
        <ArrowsClockwiseIcon size={12} class={loading ? "animate-spin" : ""} />Refresh
      </button>
    </div>
  </header>

  {#if purgeConfirmOpen}
    <div class="purge-banner" role="alert">
      <div class="purge-info">
        <WarningCircleIcon size={16} class="purge-icon" />
        <div>
          <p class="purge-title">Purge queue "{queue.name}"?</p>
          <p class="purge-sub">
            This permanently deletes all {queue.approxVisible + queue.approxInFlight + queue.approxDelayed} messages.
            Inflight and delayed messages will also be removed.
          </p>
          {#if purgeError}
            <p class="purge-err">{purgeError}</p>
          {/if}
        </div>
      </div>
      <div class="purge-actions">
        <button
          type="button"
          class="btn ghost"
          onclick={() => { purgeConfirmOpen = false; purgeError = null; }}
          disabled={purging}
        >
          Cancel
        </button>
        <button
          type="button"
          class="btn danger-confirm"
          onclick={handlePurge}
          disabled={purging}
        >
          <TrashIcon size={12} />
          {purging ? "Purging…" : "Confirm Purge"}
        </button>
      </div>
    </div>
  {/if}

  {#if bannerMessage}
    <div class="success-banner" role="status">
      <span>{bannerMessage}</span>
    </div>
  {/if}

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
    {#if error}
      <p class="error">{error}</p>
    {/if}
    {#if loading}
      <p class="empty">Loading messages...</p>
    {:else if messages.length === 0}
      <div class="empty-box">
        <p class="empty">No messages currently in queue.</p>
        <button type="button" class="btn primary-action small" onclick={() => openSendDialog()}>
          <PaperPlaneTiltIcon size={12} weight="fill" />Send a test message
        </button>
      </div>
    {:else}
      <div class="messages">
        {#each messages as message (message.id)}
          {@const expanded = expandedMessages.has(message.id)}
          {@const large = isLargeBody(message.body ?? "")}
          {@const formatted = formatJSONForViewer(message.body ?? "")}
          {@const canExpand = large || formatted !== null}
          {@const isDeleting = deletingIds.has(message.id)}
          <div class="message" class:is-deleting={isDeleting}>
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

              <div class="message-actions">
                <button
                  type="button"
                  class="action-btn"
                  onclick={() => openSaveEvent(message.body ?? "")}
                  title="Save payload as a Lambda test event fixture"
                >
                  <FloppyDiskIcon size={11} />
                  <span>Save as test event</span>
                </button>

                <button
                  type="button"
                  class="action-btn"
                  onclick={() => openSendDialog(message.body ?? "")}
                  title="Clone message and send again"
                >
                  <CopyIcon size={11} />
                  <span>Re-send</span>
                </button>

                <button
                  type="button"
                  class="action-btn delete-btn"
                  onclick={() => handleDeleteMessage(message.id)}
                  disabled={isDeleting}
                  title="Delete message from queue"
                >
                  <TrashIcon size={11} class={isDeleting ? "animate-spin" : ""} />
                  <span>{isDeleting ? "Deleting…" : "Delete"}</span>
                </button>

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
          </div>
        {/each}
      </div>
    {/if}
  </RcPanel>

  <QueueDisruptor targets={[queue]} index={1} />

  <RcPanel title="Configuration" index={2}>
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

<!-- Send Message Dialog -->
{#if sendDialogOpen}
  <QueueSendDialog
    {queue}
    initialBody={sendDialogInitialBody}
    onclose={() => {
      sendDialogOpen = false;
      sendDialogInitialBody = "";
    }}
    onsent={() => {
      onrefresh();
    }}
  />
{/if}

<!-- Redrive DLQ Dialog -->
{#if redriveDialogOpen}
  <QueueRedriveDialog
    {queue}
    {allQueues}
    onclose={() => (redriveDialogOpen = false)}
    onredriven={() => onrefresh()}
  />
{/if}

<!-- Save as Lambda Event Dialog -->
{#if saveEventOpen}
  <QueueSaveEventDialog
    messageBody={saveEventBody}
    sourceQueue={queue.name}
    {functions}
    onclose={() => {
      saveEventOpen = false;
      saveEventBody = "";
    }}
    onsaved={() => {}}
  />
{/if}

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
  .hero-actions { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }

  .btn {
    display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px;
    border: 1px solid var(--border-subtle); font-size: 11.5px; color: var(--text-secondary); text-decoration: none;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .btn:hover:not(:disabled) { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .btn:active:not(:disabled) { transform: scale(0.96); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  .primary-action {
    color: var(--accent-green, #10b981);
    border-color: color-mix(in srgb, var(--accent-green, #10b981) 40%, transparent);
    background: color-mix(in srgb, var(--accent-green, #10b981) 8%, transparent);
  }
  .primary-action:hover:not(:disabled) {
    color: var(--accent-green, #10b981);
    background: color-mix(in srgb, var(--accent-green, #10b981) 16%, transparent);
    border-color: var(--accent-green, #10b981);
  }
  .primary-action.small {
    height: 24px;
    padding: 0 9px;
    font-size: 11px;
    margin-top: 8px;
  }

  .dlq-action {
    color: var(--accent-blue, #60a5fa);
    border-color: color-mix(in srgb, var(--accent-blue, #60a5fa) 35%, transparent);
    background: color-mix(in srgb, var(--accent-blue, #60a5fa) 8%, transparent);
  }
  .dlq-action:hover:not(:disabled) {
    color: var(--accent-blue, #60a5fa);
    background: color-mix(in srgb, var(--accent-blue, #60a5fa) 16%, transparent);
    border-color: var(--accent-blue, #60a5fa);
  }

  .danger-action {
    color: var(--text-tertiary);
  }
  .danger-action:hover:not(:disabled) {
    color: var(--accent-red, #fb7185);
    border-color: color-mix(in srgb, var(--accent-red, #fb7185) 40%, transparent);
  }

  .purge-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    padding: 10px 14px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--accent-red, #fb7185) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-red, #fb7185) 30%, transparent);
  }
  .purge-info {
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }
  :global(.purge-icon) {
    color: var(--accent-red, #fb7185);
    margin-top: 2px;
    flex-shrink: 0;
  }
  .purge-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--accent-red, #fb7185);
  }
  .purge-sub {
    font-size: 11px;
    color: var(--text-secondary);
    margin-top: 2px;
  }
  .purge-err {
    font-size: 11px;
    color: var(--accent-red, #fb7185);
    margin-top: 4px;
  }
  .purge-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .danger-confirm {
    color: #fff;
    background: var(--accent-red, #fb7185);
    border-color: var(--accent-red, #fb7185);
  }
  .danger-confirm:hover:not(:disabled) {
    color: #fff;
    background: color-mix(in srgb, var(--accent-red, #fb7185) 85%, black);
  }

  .success-banner {
    padding: 8px 12px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--accent-green, #10b981) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-green, #10b981) 30%, transparent);
    font-size: 11.5px;
    color: var(--accent-green, #10b981);
  }

  .stats {
    display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(3, minmax(0, 1fr)); } }

  .empty-box {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
  }
  .empty { font-size: 11.5px; color: var(--text-tertiary); }
  .error { margin-bottom: 10px; font-size: 11.5px; color: var(--accent-red); }

  .messages { display: flex; flex-direction: column; }
  .message {
    padding: 14px 2px;
    transition: opacity 120ms ease;
  }
  .message + .message { border-top: 1px solid var(--border-subtle); }
  .message.is-deleting {
    opacity: 0.4;
    pointer-events: none;
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
  .message-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-top: 10px;
    flex-wrap: wrap;
    /* No rule here: inside a divided row it would read as a row boundary. */
    padding-top: 2px;
  }
  .message-time { font-size: 11px; color: var(--text-tertiary); }

  .message-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .action-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 22px;
    padding: 0 7px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    font-size: 10.5px;
    color: var(--text-secondary);
    transition: all 120ms ease;
  }
  .action-btn:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element-hover);
  }
  .delete-btn:hover {
    color: var(--accent-red, #fb7185);
    border-color: color-mix(in srgb, var(--accent-red, #fb7185) 40%, transparent);
    background: color-mix(in srgb, var(--accent-red, #fb7185) 10%, transparent);
  }

  .expand-btn {
    display: inline-flex; align-items: center; gap: 4px; font-size: 11px; color: var(--text-tertiary);
    transition: color 120ms ease;
    margin-left: 4px;
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
