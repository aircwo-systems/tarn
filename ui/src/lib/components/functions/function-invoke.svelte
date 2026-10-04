<script lang="ts">
  import {
    PlayIcon,
    FloppyDiskIcon,
    TrashIcon,
    CodeIcon,
    CheckIcon,
    CopyIcon,
    PathIcon,
    ListBulletsIcon,
    WarningCircleIcon,
  } from "phosphor-svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcTonePill from "$lib/components/rack/rc-tone-pill.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import { invokeFunction } from "$lib/api";
  import { loadSavedEvents, saveTestEvent, deleteSavedEvent } from "$lib/saved-events";
  import { getAccountSettings } from "$lib/state.svelte";
  import type { InvokeFunctionResult, SavedTestEvent } from "$lib/types";

  let {
    functionName,
    examples = [],
    onTraceOpen,
    onLogsOpen,
  }: {
    functionName: string;
    examples?: Array<{ label: string; body: string }>;
    onTraceOpen?: (traceId: string) => void;
    onLogsOpen?: (group: string, stream?: string) => void;
  } = $props();

  const accountSettings = getAccountSettings();

  let payload = $state("{}");
  let invocationType = $state<"RequestResponse" | "Event">("RequestResponse");
  let selectedTemplate = $state<string>("custom");
  let loading = $state(false);
  let error = $state<string | null>(null);
  let result = $state<InvokeFunctionResult | null>(null);

  // Saved events management
  let savedEvents = $state<SavedTestEvent[]>([]);
  let isSavingEvent = $state(false);
  let newEventName = $state("");
  let copiedResponse = $state(false);

  function reloadSavedEvents() {
    savedEvents = loadSavedEvents(accountSettings.activeAccountId, functionName);
  }

  $effect(() => {
    functionName;
    accountSettings.activeAccountId;
    reloadSavedEvents();
    if (examples.length > 0 && selectedTemplate === "custom") {
      selectedTemplate = `ex:0`;
      payload = examples[0].body;
    }
  });

  function onTemplateChange(e: Event) {
    const val = (e.target as HTMLSelectElement).value;
    selectedTemplate = val;
    if (val === "custom") {
      // keep current payload
      return;
    }
    if (val.startsWith("ex:")) {
      const idx = parseInt(val.slice(3), 10);
      if (examples[idx]) {
        payload = examples[idx].body;
      }
    } else if (val.startsWith("saved:")) {
      const id = val.slice(6);
      const ev = savedEvents.find((s) => s.id === id);
      if (ev) {
        payload = ev.payload;
      }
    }
  }

  function formatJSON() {
    try {
      const parsed = JSON.parse(payload);
      payload = JSON.stringify(parsed, null, 2);
      error = null;
    } catch (err: unknown) {
      error = `Invalid JSON: ${(err as Error).message}`;
    }
  }

  function handleSaveEvent() {
    if (!newEventName.trim()) return;
    saveTestEvent(accountSettings.activeAccountId, functionName, {
      name: newEventName.trim(),
      payload,
    });
    newEventName = "";
    isSavingEvent = false;
    reloadSavedEvents();
  }

  function handleDeleteSavedEvent() {
    if (!selectedTemplate.startsWith("saved:")) return;
    const id = selectedTemplate.slice(6);
    deleteSavedEvent(accountSettings.activeAccountId, functionName, id);
    selectedTemplate = "custom";
    reloadSavedEvents();
  }

  async function handleInvoke() {
    loading = true;
    error = null;
    result = null;

    try {
      result = await invokeFunction(functionName, {
        payload,
        invocationType,
      });
    } catch (err: unknown) {
      error = (err as Error).message || "Invocation failed";
    } finally {
      loading = false;
    }
  }

  async function copyResponsePayload() {
    if (!result?.payload) return;
    await navigator.clipboard.writeText(result.payload);
    copiedResponse = true;
    setTimeout(() => (copiedResponse = false), 1500);
  }

  let formattedResult = $derived.by(() => {
    if (!result?.payload) return null;
    try {
      return JSON.stringify(JSON.parse(result.payload), null, 2);
    } catch {
      return result.payload;
    }
  });
</script>

<RcPanel title="Test & Invoke" description="Execute this function directly with a custom or saved event payload." index={1}>
  <div class="invoke-container">
    <!-- Toolbar -->
    <div class="toolbar">
      <div class="left-controls">
        <label class="template-label" for="template-select">Event:</label>
        <select id="template-select" class="select-field" value={selectedTemplate} onchange={onTemplateChange}>
          <option value="custom">Custom Payload</option>
          {#if examples.length > 0}
            <optgroup label="Declared route inputs">
              {#each examples as ex, i (ex.label)}
                <option value={`ex:${i}`}>{ex.label}</option>
              {/each}
            </optgroup>
          {/if}
          {#if savedEvents.length > 0}
            <optgroup label="Saved test events">
              {#each savedEvents as ev (ev.id)}
                <option value={`saved:${ev.id}`}>{ev.name}</option>
              {/each}
            </optgroup>
          {/if}
        </select>

        {#if selectedTemplate.startsWith("saved:")}
          <button type="button" class="icon-btn danger" title="Delete saved event" onclick={handleDeleteSavedEvent}>
            <TrashIcon size={13} />
          </button>
        {/if}

        <RcButton small variant="ghost" onclick={formatJSON} title="Format JSON payload">
          <CodeIcon size={12} /> Format
        </RcButton>

        {#if !isSavingEvent}
          <RcButton small variant="ghost" onclick={() => (isSavingEvent = true)} title="Save current payload as reusable test event">
            <FloppyDiskIcon size={12} /> Save as...
          </RcButton>
        {/if}
      </div>

      <div class="right-controls">
        <div class="type-toggle">
          <button
            type="button"
            class:active={invocationType === "RequestResponse"}
            onclick={() => (invocationType = "RequestResponse")}
          >
            Sync (RequestResponse)
          </button>
          <button
            type="button"
            class:active={invocationType === "Event"}
            onclick={() => (invocationType = "Event")}
          >
            Async (Event)
          </button>
        </div>

        <RcButton variant="primary" disabled={loading} onclick={handleInvoke} title="Execute function">
          {#if loading}
            <span class="spinner"></span>
            Invoking...
          {:else}
            <PlayIcon size={12} weight="fill" />
            Invoke
          {/if}
        </RcButton>
      </div>
    </div>

    <!-- Save Event Dialog/Bar -->
    {#if isSavingEvent}
      <div class="save-bar">
        <label for="event-name-input">Event name:</label>
        <input
          id="event-name-input"
          type="text"
          placeholder="e.g. Standard Order, Invalid Auth..."
          bind:value={newEventName}
          onkeydown={(e) => {
            if (e.key === "Enter") handleSaveEvent();
            if (e.key === "Escape") isSavingEvent = false;
          }}
        />
        <RcButton small variant="primary" disabled={!newEventName.trim()} onclick={handleSaveEvent}>Save</RcButton>
        <RcButton small variant="ghost" onclick={() => (isSavingEvent = false)}>Cancel</RcButton>
      </div>
    {/if}

    <!-- Payload Input Area -->
    <div class="editor-wrapper">
      <textarea
        class="payload-editor"
        spellcheck="false"
        rows="6"
        bind:value={payload}
        placeholder="Enter JSON payload..."
      ></textarea>
    </div>

    {#if error}
      <div class="error-banner">
        <WarningCircleIcon size={14} />
        <span>{error}</span>
      </div>
    {/if}

    <!-- Result Panel -->
    {#if result}
      <div class="result-box" class:error-result={result.statusCode >= 400 || !!result.functionError}>
        <div class="result-header">
          <div class="result-badges">
            <RcTonePill tone={result.statusCode >= 400 || result.functionError ? "red" : "green"}>
              {result.statusCode} {result.functionError ? "Error" : result.statusCode === 202 ? "Accepted (Async)" : "OK"}
            </RcTonePill>
            <span class="meta-pill">{result.durationMs}ms</span>
            {#if result.requestId}
              <span class="meta-pill mono" title="Request ID">id: {result.requestId.slice(0, 8)}...</span>
            {/if}
          </div>

          <div class="result-actions">
            {#if result.traceId}
              <RcButton small variant="ghost" onclick={() => onTraceOpen?.(result!.traceId!)} title="Inspect execution trace in Traces tab">
                <PathIcon size={12} /> Trace
              </RcButton>
            {/if}
            {#if result.logGroup}
              <RcButton small variant="ghost" onclick={() => onLogsOpen?.(result!.logGroup!, result!.logStream)} title="Jump to logs for this execution">
                <ListBulletsIcon size={12} /> Logs
              </RcButton>
            {/if}
            {#if result.payload}
              <RcButton small variant="ghost" onclick={copyResponsePayload} title="Copy response body">
                {#if copiedResponse}
                  <CheckIcon size={12} /> Copied
                {:else}
                  <CopyIcon size={12} /> Copy
                {/if}
              </RcButton>
            {/if}
          </div>
        </div>

        {#if result.functionError}
          <div class="function-error-msg">
            <strong>Function Error:</strong> {result.functionError}
          </div>
        {/if}

        {#if result.payload}
          <div class="response-body-viewer">
            <FormattedMessageViewer raw={result.payload} formatted={formattedResult} formattedLabel="Response JSON" />
          </div>
        {/if}

        {#if result.logs}
          <details class="logs-details">
            <summary>Execution log tail</summary>
            <pre class="logs-tail">{result.logs}</pre>
          </details>
        {/if}
      </div>
    {/if}
  </div>
</RcPanel>

<style>
  .invoke-container {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 2px 0;
  }
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }
  .left-controls, .right-controls {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .template-label {
    font-size: 11px;
    color: var(--text-tertiary);
  }
  .select-field {
    height: 26px;
    padding: 0 8px;
    font-size: 11px;
    background: var(--bg-element);
    color: var(--text-primary);
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    outline: none;
    max-width: 200px;
  }
  .select-field:focus {
    border-color: var(--border-focus);
  }
  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: transparent;
    color: var(--text-tertiary);
    cursor: pointer;
  }
  .icon-btn:hover {
    color: var(--text-primary);
    background: var(--bg-element-hover);
  }
  .icon-btn.danger:hover {
    color: var(--accent-red);
    border-color: var(--accent-red);
  }
  .type-toggle {
    display: inline-flex;
    border: 1px solid var(--border-subtle);
    border-radius: 7px;
    padding: 1px;
    background: var(--bg-element);
  }
  .type-toggle button {
    height: 24px;
    padding: 0 8px;
    font-size: 10.5px;
    background: transparent;
    border: none;
    color: var(--text-tertiary);
    border-radius: 5px;
    cursor: pointer;
    transition: all 120ms ease;
  }
  .type-toggle button.active {
    background: var(--bg-element-hover);
    color: var(--text-primary);
    font-weight: 500;
  }
  .save-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    background: var(--bg-element);
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    font-size: 11.5px;
  }
  .save-bar input {
    flex: 1;
    height: 24px;
    padding: 0 8px;
    font-size: 11px;
    background: var(--bg-app);
    color: var(--text-primary);
    border: 1px solid var(--border-subtle);
    border-radius: 4px;
    outline: none;
  }
  .editor-wrapper {
    position: relative;
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    overflow: hidden;
    background: var(--bg-app);
  }
  .editor-wrapper:focus-within {
    border-color: var(--border-focus);
  }
  .payload-editor {
    width: 100%;
    min-height: 110px;
    max-height: 320px;
    padding: 10px;
    font-family: var(--font-mono, monospace);
    font-size: 11.5px;
    line-height: 1.5;
    background: transparent;
    color: var(--text-primary);
    border: none;
    outline: none;
    resize: vertical;
  }
  .error-banner {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px;
    background: color-mix(in srgb, var(--accent-red) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    color: var(--accent-red);
    border-radius: 6px;
    font-size: 11.5px;
  }
  .result-box {
    margin-top: 4px;
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .result-box.error-result {
    border-color: color-mix(in srgb, var(--accent-red) 35%, transparent);
  }
  .result-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px;
  }
  .result-badges {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .meta-pill {
    font-size: 10.5px;
    color: var(--text-secondary);
    padding: 2px 6px;
    background: var(--bg-element-hover);
    border-radius: 4px;
  }
  .meta-pill.mono {
    font-family: var(--font-mono, monospace);
  }
  .result-actions {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .function-error-msg {
    font-size: 11.5px;
    color: var(--accent-red);
    padding: 6px 8px;
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    border-radius: 5px;
    font-family: var(--font-mono, monospace);
  }
  .response-body-viewer {
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    overflow: hidden;
  }
  .logs-details {
    margin-top: 4px;
    font-size: 11px;
    color: var(--text-secondary);
  }
  .logs-details summary {
    cursor: pointer;
    user-select: none;
    padding: 2px 0;
  }
  .logs-details summary:hover {
    color: var(--text-primary);
  }
  .logs-tail {
    margin-top: 6px;
    padding: 8px 10px;
    background: var(--bg-app);
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    font-family: var(--font-mono, monospace);
    font-size: 10.5px;
    line-height: 1.4;
    white-space: pre-wrap;
    max-height: 160px;
    overflow-y: auto;
    color: var(--text-secondary);
  }
  .spinner {
    display: inline-block;
    width: 10px;
    height: 10px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
</style>
