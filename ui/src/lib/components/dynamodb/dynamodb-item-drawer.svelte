<script lang="ts">
  import { onMount } from "svelte";
  import {
    XIcon,
    CodeIcon,
    TrashIcon,
    FloppyDiskIcon,
    WarningCircleIcon,
    ArrowsLeftRightIcon,
  } from "phosphor-svelte";
  import { putDynamoItem, updateDynamoItem, deleteDynamoItem } from "#lib/api";
  import {
    marshalItem,
    unmarshalItem,
    extractItemKey,
    notifyDynamoMutation,
  } from "#lib/dynamodb";
  import type {
    DynamoAttributeDefinition,
    DynamoItem,
    DynamoKeySchemaItem,
  } from "#lib/types";

  let {
    tableName,
    keySchema,
    attributeDefinitions,
    initialItem = null,
    onclose,
    onmutated,
  }: {
    tableName: string;
    keySchema: DynamoKeySchemaItem[];
    attributeDefinitions: DynamoAttributeDefinition[];
    initialItem?: DynamoItem | null;
    onclose: () => void;
    onmutated: () => void;
  } = $props();

  const isCreate = $derived(initialItem === null);

  type EditorMode = "put" | "update";
  type JsonFormat = "document" | "dynamodb";

  let mode = $state<EditorMode>("put");
  let format = $state<JsonFormat>("document");
  let payloadText = $state("");
  let conditionExpr = $state("");
  let updateExpr = $state("SET ");
  let exprNamesText = $state("{}");
  let exprValuesText = $state("{}");
  let busy = $state(false);
  let error = $state("");
  let confirmDelete = $state(false);

  function defaultValueForKey(attrName: string): unknown {
    const def = attributeDefinitions.find((a) => a.attributeName === attrName);
    if (def?.attributeType === "N") return 0;
    return "";
  }

  onMount(() => {
    if (initialItem) {
      payloadText = JSON.stringify(unmarshalItem(initialItem), null, 2);
    } else {
      const seed: Record<string, unknown> = {};
      for (const ks of keySchema) {
        seed[ks.attributeName] = defaultValueForKey(ks.attributeName);
      }
      payloadText = JSON.stringify(seed, null, 2);
    }
  });

  function convertJsonObjectFormat(raw: string, next: JsonFormat): string {
    if (!raw.trim()) return raw;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("JSON must be an object");
    }
    return JSON.stringify(
      next === "dynamodb"
        ? marshalItem(parsed as Record<string, unknown>)
        : unmarshalItem(parsed as DynamoItem),
      null,
      2,
    );
  }

  function switchFormat(next: JsonFormat) {
    if (next === format) return;
    error = "";
    try {
      payloadText = convertJsonObjectFormat(payloadText, next);
      exprValuesText = convertJsonObjectFormat(exprValuesText, next);
      format = next;
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to convert JSON format";
    }
  }

  function formatCurrentJson() {
    error = "";
    try {
      const parsed = JSON.parse(payloadText);
      payloadText = JSON.stringify(parsed, null, 2);
    } catch (e) {
      error = e instanceof Error ? e.message : "Invalid JSON";
    }
  }

  function parseItemPayload(): DynamoItem {
    const parsed = JSON.parse(payloadText);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("Item payload must be a JSON object");
    }
    return format === "document"
      ? marshalItem(parsed as Record<string, unknown>)
      : (parsed as DynamoItem);
  }

  async function handleSave() {
    busy = true;
    error = "";
    try {
      if (mode === "put") {
        const item = parseItemPayload();
        await putDynamoItem(tableName, {
          item,
          conditionExpression: conditionExpr.trim() || undefined,
        });
      } else {
        if (!initialItem) throw new Error("Cannot run UpdateItem on a new item");
        const expr = updateExpr.trim();
        if (!expr) throw new Error("Enter an UpdateExpression");
        const key = extractItemKey(initialItem, keySchema);
        const namesRaw = exprNamesText.trim() ? JSON.parse(exprNamesText) : undefined;
        const valuesRaw = exprValuesText.trim() ? JSON.parse(exprValuesText) : undefined;
        const exprValues =
          valuesRaw && typeof valuesRaw === "object"
            ? format === "document"
              ? marshalItem(valuesRaw as Record<string, unknown>)
              : (valuesRaw as DynamoItem)
            : undefined;
        await updateDynamoItem(tableName, {
          key,
          updateExpression: expr,
          conditionExpression: conditionExpr.trim() || undefined,
          expressionAttributeNames:
            namesRaw && Object.keys(namesRaw).length > 0 ? namesRaw : undefined,
          expressionAttributeValues:
            exprValues && Object.keys(exprValues).length > 0 ? exprValues : undefined,
          returnValues: "ALL_NEW",
        });
      }
      notifyDynamoMutation(tableName);
      onmutated();
      onclose();
    } catch (e) {
      error = e instanceof Error ? e.message : "Save failed";
    } finally {
      busy = false;
    }
  }

  async function handleDelete() {
    if (!initialItem) return;
    if (!confirmDelete) {
      confirmDelete = true;
      return;
    }
    busy = true;
    error = "";
    try {
      const key = extractItemKey(initialItem, keySchema);
      await deleteDynamoItem(tableName, key, conditionExpr.trim() || undefined);
      notifyDynamoMutation(tableName);
      onmutated();
      onclose();
    } catch (e) {
      error = e instanceof Error ? e.message : "Delete failed";
    } finally {
      busy = false;
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") onclose();
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="overlay" onclick={onclose} role="presentation"></div>

<div class="drawer" role="dialog" aria-modal="true" aria-labelledby="ddb-drawer-title">
  <div class="drawer-head">
    <div class="head-title">
      <h2 id="ddb-drawer-title">{isCreate ? "Create Item" : "Inspect & Edit Item"}</h2>
      <span class="table-pill">{tableName}</span>
    </div>
    <button type="button" class="icon-btn" onclick={onclose} aria-label="Close item editor">
      <XIcon size={14} />
    </button>
  </div>

  <div class="drawer-body">
    <div class="toolbar">
      {#if !isCreate}
        <div class="segmented" role="tablist" aria-label="Save mode">
          <button
            type="button"
            role="tab"
            aria-selected={mode === "put"}
            class:active={mode === "put"}
            onclick={() => {
              mode = "put";
              error = "";
            }}
          >
            Replace (PutItem)
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "update"}
            class:active={mode === "update"}
            onclick={() => {
              mode = "update";
              error = "";
            }}
          >
            UpdateExpression
          </button>
        </div>
      {:else}
        <span class="hint">Primary keys: {keySchema.map((k) => `${k.attributeName} (${k.keyType})`).join(", ")}</span>
      {/if}

      <div class="toolbar-right">
        <div class="segmented" role="group" aria-label="JSON representation">
          <button
            type="button"
            class:active={format === "document"}
            onclick={() => switchFormat("document")}
            title="Plain JSON values"
          >
            Document JSON
          </button>
          <button
            type="button"
            class:active={format === "dynamodb"}
            onclick={() => switchFormat("dynamodb")}
            title="Typed DynamoDB AttributeValues"
          >
            <ArrowsLeftRightIcon size={11} />
            DynamoDB JSON
          </button>
        </div>
        {#if mode === "put"}
          <button type="button" class="tool-btn" onclick={formatCurrentJson}>
            <CodeIcon size={12} /> Format
          </button>
        {/if}
      </div>
    </div>

    {#if mode === "put"}
      <div class="field">
        <label for="ddb-item-json" class="field-label">
          Item ({format === "document" ? "Document JSON" : "DynamoDB AttributeValue JSON"})
        </label>
        <textarea
          id="ddb-item-json"
          bind:value={payloadText}
          rows={15}
          spellcheck="false"
          placeholder="&#123;&#125;"
        ></textarea>
      </div>
    {:else}
      <div class="field">
        <label for="ddb-update-expr" class="field-label">UpdateExpression</label>
        <input
          id="ddb-update-expr"
          type="text"
          bind:value={updateExpr}
          placeholder="SET #status = :status, retries = retries + :one"
          spellcheck="false"
        />
      </div>
      <div class="two-col">
        <div class="field">
          <label for="ddb-expr-names" class="field-label">ExpressionAttributeNames (JSON)</label>
          <textarea
            id="ddb-expr-names"
            bind:value={exprNamesText}
            rows={5}
            spellcheck="false"
            placeholder='&#123;"#status": "status"&#125;'
          ></textarea>
        </div>
        <div class="field">
          <label for="ddb-expr-values" class="field-label">
            ExpressionAttributeValues ({format === "document" ? "Document" : "DynamoDB"} JSON)
          </label>
          <textarea
            id="ddb-expr-values"
            bind:value={exprValuesText}
            rows={5}
            spellcheck="false"
            placeholder='&#123;":status": "processed"&#125;'
          ></textarea>
        </div>
      </div>
    {/if}

    <div class="field">
      <label for="ddb-cond-expr" class="field-label">
        ConditionExpression <span class="dim">(optional)</span>
      </label>
      <input
        id="ddb-cond-expr"
        type="text"
        bind:value={conditionExpr}
        placeholder="e.g. attribute_not_exists(pk)"
        spellcheck="false"
      />
    </div>

    {#if error}
      <div class="error-box" role="alert">
        <WarningCircleIcon size={14} />
        <span>{error}</span>
      </div>
    {/if}
  </div>

  <div class="drawer-foot">
    <div class="foot-left">
      {#if !isCreate}
        <button
          type="button"
          class="btn-danger"
          class:armed={confirmDelete}
          disabled={busy}
          onclick={handleDelete}
        >
          <TrashIcon size={13} />
          {confirmDelete ? "Confirm delete" : "Delete item"}
        </button>
        {#if confirmDelete}
          <button
            type="button"
            class="btn-ghost"
            disabled={busy}
            onclick={() => (confirmDelete = false)}
          >
            Cancel
          </button>
        {/if}
      {/if}
    </div>
    <div class="foot-right">
      <button type="button" class="btn-ghost" disabled={busy} onclick={onclose}>Close</button>
      <button type="button" class="btn-primary" disabled={busy} onclick={handleSave}>
        <FloppyDiskIcon size={13} />
        {busy ? "Saving..." : isCreate ? "Put Item" : mode === "put" ? "Save Item" : "Run UpdateItem"}
      </button>
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    z-index: 80;
    backdrop-filter: blur(2px);
  }
  .drawer {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(760px, calc(100vw - 24px));
    max-height: calc(100vh - 32px);
    display: flex;
    flex-direction: column;
    background: var(--bg-app);
    border: 1px solid var(--border-default);
    border-radius: 10px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.35);
    z-index: 81;
    overflow: hidden;
  }
  .drawer-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-subtle);
    background: var(--bg-surface, var(--bg-app));
  }
  .head-title {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .head-title h2 {
    font-size: 13.5px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .table-pill {
    padding: 2px 7px;
    border-radius: 5px;
    background: var(--bg-element);
    border: 1px solid var(--border-subtle);
    font: 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-secondary);
  }
  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 6px;
    color: var(--text-tertiary);
  }
  .icon-btn:hover {
    background: var(--bg-element-hover);
    color: var(--text-primary);
  }

  .drawer-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px 16px;
    overflow-y: auto;
  }
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px;
  }
  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .hint {
    font: 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
  }
  .segmented {
    display: inline-flex;
    padding: 2px;
    border-radius: 7px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
  }
  .segmented button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 9px;
    border-radius: 5px;
    font-size: 11px;
    color: var(--text-secondary);
  }
  .segmented button.active {
    background: var(--bg-app);
    color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
  }
  .tool-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 9px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    font-size: 11px;
    color: var(--text-secondary);
  }
  .tool-btn:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .field-label {
    font-size: 11px;
    font-weight: 500;
    color: var(--text-secondary);
  }
  .dim {
    font-weight: 400;
    color: var(--text-tertiary);
  }
  textarea,
  input[type="text"] {
    width: 100%;
    padding: 8px 10px;
    border-radius: 7px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    font: 12px/1.55 var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
    outline: none;
  }
  textarea:focus,
  input[type="text"]:focus {
    border-color: var(--border-focus);
  }
  .two-col {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  @media (max-width: 640px) {
    .two-col {
      grid-template-columns: 1fr;
    }
  }

  .error-box {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 9px 11px;
    border-radius: 7px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red);
    font-size: 11.5px;
  }

  .drawer-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 11px 16px;
    border-top: 1px solid var(--border-subtle);
    background: var(--bg-surface, var(--bg-app));
  }
  .foot-left,
  .foot-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .btn-ghost,
  .btn-primary,
  .btn-danger {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 30px;
    padding: 0 12px;
    border-radius: 6px;
    font-size: 11.5px;
    font-weight: 500;
  }
  .btn-ghost {
    border: 1px solid var(--border-subtle);
    color: var(--text-secondary);
  }
  .btn-ghost:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
  }
  .btn-primary {
    background: var(--text-primary);
    color: var(--bg-app);
  }
  .btn-primary:hover:not(:disabled) {
    opacity: 0.9;
  }
  .btn-danger {
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    color: var(--accent-red);
  }
  .btn-danger.armed {
    background: var(--accent-red);
    color: #fff;
    border-color: var(--accent-red);
  }
  button:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }
</style>
