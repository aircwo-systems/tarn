<script lang="ts">
  import { onMount } from "svelte";
  import { XIcon, PaperPlaneTiltIcon, CodeIcon, CheckIcon, WarningCircleIcon } from "phosphor-svelte";
  import { sendQueueMessage } from "$lib/api";
  import type { QueueSummary, SendQueueMessageResult } from "$lib/types";

  let {
    queue,
    initialBody = "",
    onclose,
    onsent,
  }: {
    queue: QueueSummary;
    initialBody?: string;
    onclose: () => void;
    onsent: () => void;
  } = $props();

  let body = $state("");
  let delaySeconds = $state(0);
  let messageGroupId = $state("");
  let messageDeduplicationId = $state("");
  let busy = $state(false);
  let error = $state("");
  let result = $state<SendQueueMessageResult | null>(null);
  let formatError = $state<string | null>(null);

  onMount(() => {
    body =
      initialBody.trim() ||
      JSON.stringify(
        {
          action: "process_task",
          timestamp: new Date().toISOString(),
          data: { id: "item-101", priority: "high" },
        },
        null,
        2,
      );
    messageGroupId = queue.fifo ? "default-group" : "";
  });

  const canSend = $derived(
    !busy &&
      body.trim().length > 0 &&
      (!queue.fifo || messageGroupId.trim().length > 0),
  );

  function formatJSON() {
    formatError = null;
    try {
      const parsed = JSON.parse(body);
      body = JSON.stringify(parsed, null, 2);
    } catch (e) {
      formatError = e instanceof Error ? e.message : "Invalid JSON";
    }
  }

  function setTemplate(type: string) {
    formatError = null;
    if (type === "empty-json") {
      body = "{\n  \n}";
    } else if (type === "task") {
      body = JSON.stringify(
        {
          action: "process_task",
          timestamp: new Date().toISOString(),
          data: { id: "item-101", priority: "high" },
        },
        null,
        2,
      );
    } else if (type === "s3-event") {
      body = JSON.stringify(
        {
          Records: [
            {
              eventVersion: "2.1",
              eventSource: "aws:s3",
              eventName: "ObjectCreated:Put",
              s3: {
                bucket: { name: "uploads" },
                object: { key: "photos/sample.jpg", size: 1024 },
              },
            },
          ],
        },
        null,
        2,
      );
    } else if (type === "plain-text") {
      body = "Hello from Tarn SQS sender!";
    }
  }

  async function handleSend() {
    if (!canSend) return;
    busy = true;
    error = "";
    result = null;

    try {
      const res = await sendQueueMessage(queue.name, {
        body,
        delaySeconds: !queue.fifo && delaySeconds > 0 ? delaySeconds : undefined,
        messageGroupId: queue.fifo ? messageGroupId.trim() : undefined,
        messageDeduplicationId:
          queue.fifo && messageDeduplicationId.trim()
            ? messageDeduplicationId.trim()
            : undefined,
      });
      result = res;
      onsent();
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to send message";
    } finally {
      busy = false;
    }
  }
</script>

<div class="overlay" onclick={onclose} role="presentation"></div>

<div class="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title">
  <div class="dialog-head">
    <div class="head-title">
      <h2 id="dialog-title">Send Message</h2>
      <span class="queue-pill">{queue.name}</span>
      {#if queue.fifo}
        <span class="fifo-pill">FIFO</span>
      {/if}
    </div>
    <button type="button" class="icon-btn" onclick={onclose} aria-label="Close dialog">
      <XIcon size={14} />
    </button>
  </div>

  <div class="dialog-body">
    <!-- Template picker & Format JSON -->
    <div class="toolbar-row">
      <div class="template-btns">
        <span class="tool-label">Templates:</span>
        <button type="button" class="tool-btn" onclick={() => setTemplate("task")}>Task</button>
        <button type="button" class="tool-btn" onclick={() => setTemplate("s3-event")}>S3 Event</button>
        <button type="button" class="tool-btn" onclick={() => setTemplate("empty-json")}>Empty JSON</button>
        <button type="button" class="tool-btn" onclick={() => setTemplate("plain-text")}>Text</button>
      </div>

      <button type="button" class="tool-btn format-btn" onclick={formatJSON}>
        <CodeIcon size={12} /> Format JSON
      </button>
    </div>

    {#if formatError}
      <p class="format-err">{formatError}</p>
    {/if}

    <div class="field">
      <label for="msg-body" class="field-label">Message Payload</label>
      <textarea
        id="msg-body"
        bind:value={body}
        rows={10}
        placeholder="Enter JSON or text payload..."
        spellcheck="false"
      ></textarea>
    </div>

    {#if queue.fifo}
      <div class="row">
        <div class="field">
          <label for="msg-group-id" class="field-label">
            Message Group ID <span class="req">*</span>
          </label>
          <input
            id="msg-group-id"
            type="text"
            bind:value={messageGroupId}
            placeholder="e.g. user-123"
            required
          />
        </div>
        <div class="field">
          <label for="msg-dedup-id" class="field-label">
            Message Deduplication ID <span class="dim">(optional)</span>
          </label>
          <input
            id="msg-dedup-id"
            type="text"
            bind:value={messageDeduplicationId}
            placeholder="e.g. req-abc-123"
          />
        </div>
      </div>
    {:else}
      <div class="field delay-field">
        <label for="msg-delay" class="field-label">
          Delivery Delay <span class="dim">(seconds, 0-900)</span>
        </label>
        <input
          id="msg-delay"
          type="number"
          min="0"
          max="900"
          bind:value={delaySeconds}
        />
      </div>
    {/if}

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
          <p class="status-title">Message sent successfully!</p>
          <p class="status-detail">ID: <code>{result.messageId}</code></p>
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
      onclick={handleSend}
      disabled={!canSend}
    >
      <PaperPlaneTiltIcon size={12} weight="fill" />
      {busy ? "Sending…" : result ? "Send Another" : "Send Message"}
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
    width: min(38rem, 94vw);
    max-height: 88vh;
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
  .fifo-pill {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--accent-amber, #f59e0b);
    background: color-mix(in srgb, var(--accent-amber, #f59e0b) 15%, transparent);
    padding: 1px 6px;
    border-radius: 4px;
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
  .dialog-foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--border-subtle);
  }

  .toolbar-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }
  .template-btns {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }
  .tool-label {
    font-size: 11px;
    color: var(--text-tertiary);
    margin-right: 2px;
  }
  .tool-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 22px;
    padding: 0 7px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    font-size: 11px;
    color: var(--text-secondary);
    transition: all 120ms ease;
  }
  .tool-btn:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element-hover);
  }
  .format-btn {
    font-family: var(--font-mono, ui-monospace, monospace);
  }
  .format-err {
    font-size: 11px;
    color: var(--accent-red, #fb7185);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .row {
    display: flex;
    gap: 12px;
  }
  .row .field {
    flex: 1;
  }
  .delay-field {
    max-width: 200px;
  }
  .field-label {
    font-size: 11.5px;
    color: var(--text-secondary);
  }
  .field-label .req {
    color: var(--accent-red, #fb7185);
  }
  .field-label .dim {
    color: var(--text-tertiary);
  }

  input[type="number"],
  input[type="text"],
  textarea {
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
  textarea {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 11.5px;
    line-height: 1.5;
    resize: vertical;
  }
  input:focus-visible,
  textarea:focus-visible {
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
