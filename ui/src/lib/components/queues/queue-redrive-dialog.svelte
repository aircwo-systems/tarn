<script lang="ts">
  import { onMount } from "svelte";
  import { XIcon, ArrowsClockwiseIcon, CheckIcon, WarningCircleIcon } from "phosphor-svelte";
  import { redriveQueueMessages } from "$lib/api";
  import type { QueueSummary, RedriveQueueResult } from "$lib/types";

  let {
    queue,
    allQueues = [],
    onclose,
    onredriven,
  }: {
    queue: QueueSummary;
    allQueues?: QueueSummary[];
    onclose: () => void;
    onredriven: (moved: number) => void;
  } = $props();

  let sourceQueue = $state("");
  let targetQueue = $state("");
  let maxMessages = $state(100);
  let busy = $state(false);
  let error = $state("");
  let result = $state<RedriveQueueResult | null>(null);

  onMount(() => {
    sourceQueue = queue.dlqName ? queue.dlqName : queue.name;
    targetQueue = queue.dlqName ? queue.name : "";
  });

  async function handleRedrive() {
    busy = true;
    error = "";
    result = null;

    try {
      const res = await redriveQueueMessages(
        sourceQueue,
        targetQueue.trim() || undefined,
        maxMessages > 0 ? maxMessages : undefined,
      );
      result = res;
      onredriven(res.moved);
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to redrive messages";
    } finally {
      busy = false;
    }
  }
</script>

<div class="overlay" onclick={onclose} role="presentation"></div>

<div class="dialog" role="dialog" aria-modal="true" aria-labelledby="redrive-title">
  <div class="dialog-head">
    <div class="head-title">
      <h2 id="redrive-title">Redrive DLQ Messages</h2>
      <span class="queue-pill">{queue.name}</span>
    </div>
    <button type="button" class="icon-btn" onclick={onclose} aria-label="Close dialog">
      <XIcon size={14} />
    </button>
  </div>

  <div class="dialog-body">
    <p class="description">
      Move messages from a dead-letter queue back to the primary processing queue.
    </p>

    <div class="field">
      <label for="source-queue" class="field-label">Source Queue (DLQ)</label>
      <input
        id="source-queue"
        type="text"
        bind:value={sourceQueue}
        placeholder="Queue to pull messages from"
      />
    </div>

    <div class="field">
      <label for="target-queue" class="field-label">
        Target Queue <span class="dim">(destination for reprocessed messages)</span>
      </label>
      {#if allQueues.length > 0}
        <select id="target-queue" bind:value={targetQueue}>
          {#if !targetQueue}
            <option value="">Auto-infer source queue</option>
          {/if}
          {#each allQueues as q (q.name)}
            <option value={q.name}>{q.name} ({q.fifo ? "FIFO" : "Standard"})</option>
          {/each}
        </select>
      {:else}
        <input
          id="target-queue"
          type="text"
          bind:value={targetQueue}
          placeholder="Leave blank to auto-infer"
        />
      {/if}
    </div>

    <div class="field count-field">
      <label for="max-messages" class="field-label">
        Max Messages to Redrive <span class="dim">(0 for all available)</span>
      </label>
      <input
        id="max-messages"
        type="number"
        min="0"
        max="10000"
        bind:value={maxMessages}
      />
    </div>

    {#if error}
      <div class="status-box err">
        <WarningCircleIcon size={14} />
        <span>{error}</span>
      </div>
    {/if}

    {#if result}
      <div class="status-box ok">
        <CheckIcon size={14} />
        <div class="status-content">
          <p class="status-title">
            Redrive complete: {result.moved} {result.moved === 1 ? "message" : "messages"} moved!
          </p>
          <p class="status-detail">
            From <code>{result.sourceQueue}</code> → <code>{result.targetQueue}</code>
          </p>
        </div>
      </div>
    {/if}
  </div>

  <div class="dialog-foot">
    <button type="button" class="btn ghost" onclick={onclose}>
      {result ? "Done" : "Cancel"}
    </button>
    <button
      type="button"
      class="btn primary"
      onclick={handleRedrive}
      disabled={busy || !sourceQueue.trim()}
    >
      <ArrowsClockwiseIcon size={12} class={busy ? "animate-spin" : ""} />
      {busy ? "Redriving…" : "Start Redrive"}
    </button>
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 70;
    background: rgb(0 0 0 / 0.45);
    backdrop-filter: blur(1px);
  }
  .dialog {
    position: fixed;
    z-index: 75;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    width: min(34rem, 94vw);
    max-height: 86vh;
    display: flex;
    flex-direction: column;
    border-radius: 12px;
    border: 1px solid var(--border-default);
    background: var(--bg-app);
    box-shadow: 0 24px 64px rgb(0 0 0 / 0.4);
  }
  .dialog-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-subtle);
  }
  .head-title {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .dialog-head h2 {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .queue-pill {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
    color: var(--text-secondary);
    background: var(--bg-element);
    padding: 2px 7px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
  }
  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 28px;
    width: 28px;
    border-radius: 8px;
    color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .icon-btn:hover {
    color: var(--text-primary);
    background: var(--bg-element-hover);
  }
  .dialog-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    overflow-y: auto;
  }
  .description {
    font-size: 12px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
  .dialog-foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--border-subtle);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .count-field {
    max-width: 220px;
  }
  .field-label {
    font-size: 11.5px;
    color: var(--text-secondary);
  }
  .field-label .dim {
    color: var(--text-tertiary);
  }

  input[type="number"],
  input[type="text"],
  select {
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    color: var(--text-primary);
    font-size: 12px;
    padding: 7px 9px;
    outline: none;
    width: 100%;
    transition: border-color 120ms ease;
  }
  input:focus-visible,
  select:focus-visible {
    outline: 1px solid var(--border-focus);
    outline-offset: 1px;
    border-color: var(--border-focus);
  }

  .status-box {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 11.5px;
  }
  .status-box.err {
    background: color-mix(in srgb, var(--accent-red, #fb7185) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-red, #fb7185) 30%, transparent);
    color: var(--accent-red, #fb7185);
  }
  .status-box.ok {
    background: color-mix(in srgb, var(--accent-green, #10b981) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-green, #10b981) 30%, transparent);
    color: var(--accent-green, #10b981);
  }
  .status-content {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .status-title {
    font-weight: 600;
  }
  .status-detail code {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    padding: 0 12px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    font-size: 11.5px;
    color: var(--text-secondary);
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .btn:hover:not(:disabled) {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element-hover);
  }
  .btn:active:not(:disabled) {
    transform: scale(0.96);
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .btn.primary:not(:disabled) {
    color: var(--accent-blue, #60a5fa);
    border-color: color-mix(in srgb, var(--accent-blue, #60a5fa) 40%, transparent);
    background: color-mix(in srgb, var(--accent-blue, #60a5fa) 10%, transparent);
  }
  .btn.ghost {
    border-color: transparent;
  }
  .btn.ghost:hover:not(:disabled) {
    border-color: var(--border-subtle);
  }
</style>
