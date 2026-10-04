<script lang="ts">
  import { onMount } from "svelte";
  import { XIcon, FloppyDiskIcon, CheckIcon } from "phosphor-svelte";
  import { saveTestEvent } from "$lib/saved-events";
  import { getAccountSettings } from "$lib/state.svelte";
  import type { FunctionSummary } from "$lib/types";

  let {
    messageBody,
    sourceQueue,
    functions = [],
    onclose,
    onsaved,
  }: {
    messageBody: string;
    sourceQueue: string;
    functions?: FunctionSummary[];
    onclose: () => void;
    onsaved: () => void;
  } = $props();

  const accountSettings = getAccountSettings();

  let targetFunction = $state("");
  let eventName = $state("");
  let saved = $state(false);

  onMount(() => {
    targetFunction = functions[0]?.name ?? "";
    eventName = `SQS: ${sourceQueue} (${new Date().toLocaleTimeString()})`;
  });

  const canSave = $derived(targetFunction.trim().length > 0 && eventName.trim().length > 0);

  function handleSave() {
    if (!canSave) return;
    saveTestEvent(accountSettings.activeAccountId, targetFunction.trim(), {
      name: eventName.trim(),
      payload: messageBody,
      source: `sqs:${sourceQueue}`,
    });
    saved = true;
    setTimeout(() => {
      onsaved();
      onclose();
    }, 600);
  }
</script>

<div class="overlay" onclick={onclose} role="presentation"></div>

<div class="dialog" role="dialog" aria-modal="true" aria-labelledby="save-event-title">
  <div class="dialog-head">
    <div class="head-title">
      <h2 id="save-event-title">Save as Lambda Test Event</h2>
      <span class="queue-pill">{sourceQueue}</span>
    </div>
    <button type="button" class="icon-btn" onclick={onclose} aria-label="Close dialog">
      <XIcon size={14} />
    </button>
  </div>

  <div class="dialog-body">
    <p class="description">
      Save this message payload as a reusable test fixture for one of your Lambda functions.
    </p>

    <div class="field">
      <label for="target-func" class="field-label">Target Lambda Function</label>
      {#if functions.length > 0}
        <select id="target-func" bind:value={targetFunction}>
          {#each functions as fn (fn.name)}
            <option value={fn.name}>{fn.name} ({fn.runtime})</option>
          {/each}
        </select>
      {:else}
        <input
          id="target-func"
          type="text"
          bind:value={targetFunction}
          placeholder="e.g. process-orders"
        />
      {/if}
    </div>

    <div class="field">
      <label for="event-name" class="field-label">Test Event Name</label>
      <input
        id="event-name"
        type="text"
        bind:value={eventName}
        placeholder="e.g. SQS sample event"
      />
    </div>

    <div class="field">
      <label for="preview-body" class="field-label">Payload Preview</label>
      <pre id="preview-body" class="payload-preview">{messageBody}</pre>
    </div>

    {#if saved}
      <div class="status-box ok">
        <CheckIcon size={14} />
        <span>Saved to function test fixtures!</span>
      </div>
    {/if}
  </div>

  <div class="dialog-foot">
    <button type="button" class="btn ghost" onclick={onclose}>Cancel</button>
    <button
      type="button"
      class="btn primary"
      onclick={handleSave}
      disabled={!canSave || saved}
    >
      <FloppyDiskIcon size={12} />
      {saved ? "Saved" : "Save Test Event"}
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
    width: min(32rem, 94vw);
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
  .field-label {
    font-size: 11.5px;
    color: var(--text-secondary);
  }
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
  .payload-preview {
    max-height: 140px;
    overflow: auto;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11px;
    line-height: 1.4;
    background: var(--bg-element);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    padding: 8px 10px;
    color: var(--text-secondary);
    white-space: pre-wrap;
    word-break: break-all;
  }

  .status-box.ok {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--accent-green, #10b981) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-green, #10b981) 30%, transparent);
    color: var(--accent-green, #10b981);
    font-size: 11.5px;
    font-weight: 500;
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
    color: var(--accent-green, #10b981);
    border-color: color-mix(in srgb, var(--accent-green, #10b981) 40%, transparent);
    background: color-mix(in srgb, var(--accent-green, #10b981) 10%, transparent);
  }
  .btn.ghost {
    border-color: transparent;
  }
  .btn.ghost:hover:not(:disabled) {
    border-color: var(--border-subtle);
  }
</style>
