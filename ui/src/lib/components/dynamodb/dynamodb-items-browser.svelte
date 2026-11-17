<script lang="ts">
  import { onMount } from "svelte";
  import {
    PlayIcon,
    PlusIcon,
    ArrowClockwiseIcon,
    CaretDownIcon,
    CaretRightIcon,
    CopyIcon,
    CheckIcon,
    TrashIcon,
    WarningCircleIcon,
    KeyIcon,
  } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import DynamoItemDrawer from "./dynamodb-item-drawer.svelte";
  import {
    scanDynamoTable,
    queryDynamoTable,
    getDynamoItem,
    deleteDynamoItem,
  } from "#lib/api";
  import {
    attrTypeTag,
    collectColumnNames,
    extractItemKey,
    formatAttrPreview,
    itemKeySignature,
    marshalItem,
    marshalScalar,
    notifyDynamoMutation,
    unmarshalItem,
  } from "#lib/dynamodb";
  import type {
    DynamoItem,
    DynamoKeySchemaItem,
    DynamoReadRequest,
    DynamoTableDetailResponse,
  } from "#lib/types";

  let {
    detail,
    externalMutationTick = 0,
    onMutated = () => {},
  }: {
    detail: DynamoTableDetailResponse;
    externalMutationTick?: number;
    onMutated?: () => void;
  } = $props();

  const table = $derived(detail.table);

  type QueryOp = "=" | "begins_with" | "<" | "<=" | ">" | ">=" | "between";
  type ReadMode = "scan" | "query" | "get";

  interface IndexTarget {
    id: string;
    label: string;
    indexName?: string;
    keySchema: DynamoKeySchemaItem[];
  }

  const indexTargets = $derived.by<IndexTarget[]>(() => {
    const list: IndexTarget[] = [
      {
        id: "__table__",
        label: `Table (${table.keySchema.map((k) => k.attributeName).join(", ")})`,
        keySchema: table.keySchema,
      },
    ];
    for (const lsi of table.localSecondaryIndexes ?? []) {
      list.push({
        id: `lsi:${lsi.indexName}`,
        label: `LSI: ${lsi.indexName} (${lsi.keySchema.map((k) => k.attributeName).join(", ")})`,
        indexName: lsi.indexName,
        keySchema: lsi.keySchema,
      });
    }
    for (const gsi of table.globalSecondaryIndexes ?? []) {
      list.push({
        id: `gsi:${gsi.indexName}`,
        label: `GSI: ${gsi.indexName} (${gsi.keySchema.map((k) => k.attributeName).join(", ")})`,
        indexName: gsi.indexName,
        keySchema: gsi.keySchema,
      });
    }
    return list;
  });

  let mode = $state<ReadMode>("scan");
  let selectedTargetId = $state("__table__");
  let pkValue = $state("");
  let skOp = $state<QueryOp>("=");
  let skValue = $state("");
  let skValue2 = $state("");
  let scanForward = $state(true);
  let getPkValue = $state("");
  let getSkValue = $state("");

  let showAdvanced = $state(false);
  let filterExpression = $state("");
  let projectionExpression = $state("");
  let exprNamesText = $state("");
  let exprValuesText = $state("");
  let pageLimit = $state(50);

  let items = $state<DynamoItem[]>([]);
  let totalScanned = $state(0);
  let lastEvaluatedKey = $state<DynamoItem | undefined>(undefined);
  let pageKeys = $state<(DynamoItem | undefined)[]>([]);
  let currentPageIndex = $state(0);
  let loading = $state(false);
  let error = $state("");
  let hasRun = $state(false);

  let drawerOpen = $state(false);
  let editingItem = $state<DynamoItem | null>(null);
  let copiedRowSig = $state<string | null>(null);
  let armedDeleteSig = $state<string | null>(null);

  const activeTarget = $derived(
    indexTargets.find((t) => t.id === selectedTargetId) ?? indexTargets[0],
  );
  const targetHashKey = $derived(
    activeTarget?.keySchema.find((k) => k.keyType === "HASH")?.attributeName ?? "",
  );
  const targetRangeKey = $derived(
    activeTarget?.keySchema.find((k) => k.keyType === "RANGE")?.attributeName ?? "",
  );
  const tableHashKey = $derived(
    table.keySchema.find((k) => k.keyType === "HASH")?.attributeName ?? "",
  );
  const tableRangeKey = $derived(
    table.keySchema.find((k) => k.keyType === "RANGE")?.attributeName ?? "",
  );

  const columns = $derived(collectColumnNames(items, activeTarget?.keySchema ?? table.keySchema));

  function attrTypeOf(name: string): string {
    return (
      table.attributeDefinitions.find((a) => a.attributeName === name)?.attributeType ?? "S"
    );
  }

  function parseOptionalObject(raw: string, label: string): Record<string, unknown> | undefined {
    const trimmed = raw.trim();
    if (!trimmed) return undefined;
    const parsed = JSON.parse(trimmed);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error(`${label} must be a JSON object`);
    }
    return parsed as Record<string, unknown>;
  }

  function buildExtraExpressions(): {
    filterExpression?: string;
    projectionExpression?: string;
    expressionAttributeNames?: Record<string, string>;
    expressionAttributeValues?: DynamoItem;
  } {
    const namesRaw = parseOptionalObject(exprNamesText, "ExpressionAttributeNames");
    const valuesRaw = parseOptionalObject(exprValuesText, "ExpressionAttributeValues");
    let names: Record<string, string> | undefined;
    if (namesRaw) {
      names = {};
      for (const [k, v] of Object.entries(namesRaw)) names[k] = String(v);
    }
    let values: DynamoItem | undefined;
    if (valuesRaw) {
      // Accept either DynamoDB JSON ({ ":v": { "S": "x" } }) or plain JSON ({ ":v": "x" })
      const looksTyped = Object.values(valuesRaw).every(
        (v) =>
          v &&
          typeof v === "object" &&
          !Array.isArray(v) &&
          ["S", "N", "B", "BOOL", "NULL", "M", "L", "SS", "NS", "BS"].some((k) => k in v),
      );
      values = looksTyped ? (valuesRaw as DynamoItem) : marshalItem(valuesRaw);
    }
    return {
      filterExpression: filterExpression.trim() || undefined,
      projectionExpression: projectionExpression.trim() || undefined,
      expressionAttributeNames: names,
      expressionAttributeValues: values,
    };
  }

  async function runRead(startKey?: DynamoItem, pageIdx = 0) {
    loading = true;
    error = "";
    armedDeleteSig = null;
    try {
      if (mode === "get") {
        if (!tableHashKey || !getPkValue.trim()) {
          throw new Error(`Enter partition key "${tableHashKey}" to run GetItem`);
        }
        const key: DynamoItem = {
          [tableHashKey]: marshalScalar(getPkValue, attrTypeOf(tableHashKey)),
        };
        if (tableRangeKey) {
          if (!getSkValue.trim()) {
            throw new Error(`Enter sort key "${tableRangeKey}" to run GetItem`);
          }
          key[tableRangeKey] = marshalScalar(getSkValue, attrTypeOf(tableRangeKey));
        }
        const res = await getDynamoItem(table.name, key);
        items = res.found && res.item ? [res.item] : [];
        totalScanned = 1;
        lastEvaluatedKey = undefined;
        pageKeys = [];
        currentPageIndex = 0;
        hasRun = true;
        return;
      }

      const extra = buildExtraExpressions();

      if (mode === "scan") {
        const req: DynamoReadRequest = {
          indexName: activeTarget?.indexName,
          limit: pageLimit,
          exclusiveStartKey: startKey,
          ...extra,
        };
        const res = await scanDynamoTable(table.name, req);
        items = res.items ?? [];
        totalScanned = res.scannedCount ?? items.length;
        lastEvaluatedKey = res.lastEvaluatedKey;
        currentPageIndex = pageIdx;
        if (pageIdx === 0) pageKeys = [undefined];
        hasRun = true;
        return;
      }

      // Query mode
      if (!targetHashKey || !pkValue.trim()) {
        throw new Error(`Enter partition key "${targetHashKey}" to run Query`);
      }
      const names: Record<string, string> = {
        ...(extra.expressionAttributeNames ?? {}),
        "#pk": targetHashKey,
      };
      const values: DynamoItem = {
        ...(extra.expressionAttributeValues ?? {}),
        ":pk": marshalScalar(pkValue, attrTypeOf(targetHashKey)),
      };

      let keyCond = "#pk = :pk";
      if (targetRangeKey && skValue.trim()) {
        names["#sk"] = targetRangeKey;
        values[":sk1"] = marshalScalar(skValue, attrTypeOf(targetRangeKey));
        if (skOp === "begins_with") {
          keyCond += " AND begins_with(#sk, :sk1)";
        } else if (skOp === "between") {
          if (!skValue2.trim()) {
            throw new Error("Enter both bounds for BETWEEN sort key condition");
          }
          values[":sk2"] = marshalScalar(skValue2, attrTypeOf(targetRangeKey));
          keyCond += " AND #sk BETWEEN :sk1 AND :sk2";
        } else {
          keyCond += ` AND #sk ${skOp} :sk1`;
        }
      }

      const res = await queryDynamoTable(table.name, {
        indexName: activeTarget?.indexName,
        keyConditionExpression: keyCond,
        filterExpression: extra.filterExpression,
        projectionExpression: extra.projectionExpression,
        expressionAttributeNames: names,
        expressionAttributeValues: values,
        limit: pageLimit,
        exclusiveStartKey: startKey,
        scanIndexForward: scanForward,
      });
      items = res.items ?? [];
      totalScanned = res.scannedCount ?? items.length;
      lastEvaluatedKey = res.lastEvaluatedKey;
      currentPageIndex = pageIdx;
      if (pageIdx === 0) pageKeys = [undefined];
      hasRun = true;
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to read items";
    } finally {
      loading = false;
    }
  }

  function nextPage() {
    if (!lastEvaluatedKey) return;
    const nextIdx = currentPageIndex + 1;
    const updated = pageKeys.slice(0, nextIdx);
    updated[nextIdx] = lastEvaluatedKey;
    pageKeys = updated;
    void runRead(lastEvaluatedKey, nextIdx);
  }

  function prevPage() {
    if (currentPageIndex <= 0) return;
    const prevIdx = currentPageIndex - 1;
    void runRead(pageKeys[prevIdx], prevIdx);
  }

  function openCreate() {
    editingItem = null;
    drawerOpen = true;
  }

  function openEdit(item: DynamoItem) {
    editingItem = item;
    drawerOpen = true;
  }

  async function copyRowJson(e: Event, item: DynamoItem, sig: string) {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(JSON.stringify(unmarshalItem(item), null, 2));
      copiedRowSig = sig;
      setTimeout(() => {
        if (copiedRowSig === sig) copiedRowSig = null;
      }, 1500);
    } catch {}
  }

  function canAutoRerunCurrentRead(): boolean {
    if (mode === "scan") return true;
    if (mode === "query") return pkValue.trim() !== "";
    return getPkValue.trim() !== "" && (!tableRangeKey || getSkValue.trim() !== "");
  }

  let lastObservedMutationTick = 0;
  $effect(() => {
    const tick = externalMutationTick;
    if (tick > lastObservedMutationTick) {
      lastObservedMutationTick = tick;
      if (canAutoRerunCurrentRead()) {
        void runRead(pageKeys[currentPageIndex], currentPageIndex);
      }
    }
  });

  async function quickDeleteRow(e: Event, item: DynamoItem, sig: string) {
    e.stopPropagation();
    if (armedDeleteSig !== sig) {
      armedDeleteSig = sig;
      return;
    }
    try {
      const key = extractItemKey(item, table.keySchema);
      if (Object.keys(key).length < table.keySchema.length) {
        armedDeleteSig = null;
        throw new Error("Cannot delete row because ProjectionExpression excluded primary key attributes");
      }
      await deleteDynamoItem(table.name, key);
      armedDeleteSig = null;
      notifyDynamoMutation(table.name);
      onMutated();
      if (canAutoRerunCurrentRead()) {
        await runRead(pageKeys[currentPageIndex], currentPageIndex);
      }
    } catch (err) {
      error = err instanceof Error ? err.message : "Delete failed";
    }
  }

  onMount(() => {
    void runRead(undefined, 0);
  });
</script>

<div class="browser">
  <RcPanel
    title="Read Items"
    description="Scan, query by partition/sort key or index, or fetch a single item by primary key."
    index={0}
  >
    {#snippet actions()}
      <div class="header-actions">
        <RcButton small onclick={openCreate}>
          <PlusIcon size={12} /> Create item
        </RcButton>
        <RcButton
          small
          variant="primary"
          disabled={loading}
          onclick={() => runRead(undefined, 0)}
        >
          <PlayIcon size={12} weight="fill" />
          {loading ? "Running..." : mode === "scan" ? "Run Scan" : mode === "query" ? "Run Query" : "Get Item"}
        </RcButton>
      </div>
    {/snippet}

    <div class="controls">
      <div class="mode-row">
        <div class="segmented" role="tablist" aria-label="Read mode">
          <button
            type="button"
            role="tab"
            aria-selected={mode === "scan"}
            class:active={mode === "scan"}
            onclick={() => {
              mode = "scan";
              error = "";
            }}
          >
            Scan
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "query"}
            class:active={mode === "query"}
            onclick={() => {
              mode = "query";
              error = "";
            }}
          >
            Query
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "get"}
            class:active={mode === "get"}
            onclick={() => {
              mode = "get";
              error = "";
            }}
          >
            GetItem
          </button>
        </div>

        {#if mode !== "get"}
          <label class="inline-field">
            <span>Target</span>
            <select bind:value={selectedTargetId}>
              {#each indexTargets as t (t.id)}
                <option value={t.id}>{t.label}</option>
              {/each}
            </select>
          </label>
          <label class="inline-field">
            <span>Limit</span>
            <select bind:value={pageLimit}>
              <option value={25}>25</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
              <option value={250}>250</option>
            </select>
          </label>
        {/if}
      </div>

      {#if mode === "query"}
        <form
          class="query-grid"
          onsubmit={(e) => {
            e.preventDefault();
            void runRead(undefined, 0);
          }}
        >
          <label class="field">
            <span>
              Partition Key: <code>{targetHashKey}</code>
              <small>({attrTypeOf(targetHashKey)})</small>
            </span>
            <input
              type="text"
              bind:value={pkValue}
              placeholder={`Enter ${targetHashKey} value`}
              spellcheck="false"
            />
          </label>

          {#if targetRangeKey}
            <div class="sk-group">
              <label class="field sk-op">
                <span>Sort Key: <code>{targetRangeKey}</code></span>
                <select bind:value={skOp}>
                  <option value="=">=</option>
                  <option value="begins_with">begins_with</option>
                  <option value="<">&lt;</option>
                  <option value="<=">&lt;=</option>
                  <option value=">">&gt;</option>
                  <option value=">=">&gt;=</option>
                  <option value="between">between</option>
                </select>
              </label>
              <label class="field sk-val">
                <span>Value <small>({attrTypeOf(targetRangeKey)})</small></span>
                <input
                  type="text"
                  bind:value={skValue}
                  placeholder={`Optional ${targetRangeKey}`}
                  spellcheck="false"
                />
              </label>
              {#if skOp === "between"}
                <label class="field sk-val">
                  <span>And</span>
                  <input
                    type="text"
                    bind:value={skValue2}
                    placeholder="Upper bound"
                    spellcheck="false"
                  />
                </label>
              {/if}
            </div>
          {/if}

          <label class="field sort-order">
            <span>Order</span>
            <select bind:value={scanForward}>
              <option value={true}>Ascending</option>
              <option value={false}>Descending</option>
            </select>
          </label>
          <button type="submit" class="hidden-submit" aria-hidden="true">Run</button>
        </form>
      {:else if mode === "get"}
        <form
          class="query-grid"
          onsubmit={(e) => {
            e.preventDefault();
            void runRead(undefined, 0);
          }}
        >
          <label class="field">
            <span>
              Partition Key: <code>{tableHashKey}</code>
              <small>({attrTypeOf(tableHashKey)})</small>
            </span>
            <input
              type="text"
              bind:value={getPkValue}
              placeholder={`Enter ${tableHashKey}`}
              spellcheck="false"
            />
          </label>
          {#if tableRangeKey}
            <label class="field">
              <span>
                Sort Key: <code>{tableRangeKey}</code>
                <small>({attrTypeOf(tableRangeKey)})</small>
              </span>
              <input
                type="text"
                bind:value={getSkValue}
                placeholder={`Enter ${tableRangeKey}`}
                spellcheck="false"
              />
            </label>
          {/if}
          <button type="submit" class="hidden-submit" aria-hidden="true">Get</button>
        </form>
      {/if}

      {#if mode !== "get"}
        <div class="advanced-wrap">
          <button
            type="button"
            class="advanced-toggle"
            onclick={() => (showAdvanced = !showAdvanced)}
          >
            {#if showAdvanced}
              <CaretDownIcon size={12} />
            {:else}
              <CaretRightIcon size={12} />
            {/if}
            Filter &amp; Projection Expressions
          </button>

          {#if showAdvanced}
            <div class="advanced-grid">
              <label class="field">
                <span>FilterExpression</span>
                <input
                  type="text"
                  bind:value={filterExpression}
                  placeholder="e.g. #status = :active"
                  spellcheck="false"
                />
              </label>
              <label class="field">
                <span>ProjectionExpression</span>
                <input
                  type="text"
                  bind:value={projectionExpression}
                  placeholder="e.g. pk, sk, #status"
                  spellcheck="false"
                />
              </label>
              <label class="field">
                <span>ExpressionAttributeNames (JSON)</span>
                <textarea
                  bind:value={exprNamesText}
                  rows={2}
                  placeholder='&#123;"#status": "status"&#125;'
                  spellcheck="false"
                ></textarea>
              </label>
              <label class="field">
                <span>ExpressionAttributeValues (JSON)</span>
                <textarea
                  bind:value={exprValuesText}
                  rows={2}
                  placeholder='&#123;":active": "active"&#125;'
                  spellcheck="false"
                ></textarea>
              </label>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </RcPanel>

  {#if error}
    <div class="error-banner" role="alert">
      <WarningCircleIcon size={14} />
      <span>{error}</span>
    </div>
  {/if}

  <RcPanel title="Results" index={1}>
    {#snippet actions()}
      <div class="results-meta">
        {#if hasRun}
          <span class="meta-pill">
            {items.length} item{items.length === 1 ? "" : "s"}
            {#if totalScanned !== items.length}· {totalScanned} scanned{/if}
          </span>
          {#if currentPageIndex > 0 || lastEvaluatedKey}
            <span class="meta-pill">Page {currentPageIndex + 1}</span>
          {/if}
        {/if}
        <RcButton
          small
          variant="ghost"
          disabled={loading}
          onclick={() => runRead(pageKeys[currentPageIndex], currentPageIndex)}
        >
          <ArrowClockwiseIcon size={12} /> Refresh
        </RcButton>
      </div>
    {/snippet}

    {#if items.length === 0}
      <div class="empty-results">
        <p>{loading ? "Loading items..." : "No items matched this query."}</p>
        {#if !loading}
          <RcButton small onclick={openCreate}>
            <PlusIcon size={12} /> Create first item
          </RcButton>
        {/if}
      </div>
    {:else}
      <div class="table-scroll">
        <table class="items-table">
          <thead>
            <tr>
              {#each columns as col (col)}
                {@const keyRole = (activeTarget?.keySchema ?? table.keySchema).find((k) => k.attributeName === col)?.keyType}
                <th>
                  <span class="th-inner">
                    {#if keyRole}
                      <KeyIcon size={10} />
                    {/if}
                    <span>{col}</span>
                    {#if keyRole}
                      <span class="key-tag">{keyRole === "HASH" ? "PK" : "SK"}</span>
                    {/if}
                  </span>
                </th>
              {/each}
              <th class="actions-col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each items as item, idx (itemKeySignature(item, table.keySchema, idx))}
              {@const sig = itemKeySignature(item, table.keySchema, idx)}
              <tr onclick={() => openEdit(item)} class="item-row">
                {#each columns as col (col)}
                  {@const av = item[col]}
                  <td>
                    {#if av !== undefined}
                      <div class="cell-val" title={formatAttrPreview(av)}>
                        <span class="type-badge">{attrTypeTag(av)}</span>
                        <span class="val-text">{formatAttrPreview(av)}</span>
                      </div>
                    {:else}
                      <span class="null-cell">--</span>
                    {/if}
                  </td>
                {/each}
                <td class="actions-col" onclick={(e) => e.stopPropagation()}>
                  <div class="row-actions">
                    <button
                      type="button"
                      class="mini-btn"
                      title="Copy Document JSON"
                      onclick={(e) => copyRowJson(e, item, sig)}
                    >
                      {#if copiedRowSig === sig}
                        <CheckIcon size={12} />
                      {:else}
                        <CopyIcon size={12} />
                      {/if}
                    </button>
                    <button
                      type="button"
                      class="mini-btn danger"
                      class:armed={armedDeleteSig === sig}
                      title={armedDeleteSig === sig ? "Click again to delete" : "Delete item"}
                      onclick={(e) => quickDeleteRow(e, item, sig)}
                    >
                      <TrashIcon size={12} />
                      {#if armedDeleteSig === sig}
                        <span>Delete?</span>
                      {/if}
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      {#if currentPageIndex > 0 || lastEvaluatedKey}
        <div class="pager">
          <RcButton small disabled={loading || currentPageIndex === 0} onclick={prevPage}>
            Previous
          </RcButton>
          <span class="pager-info">Page {currentPageIndex + 1}</span>
          <RcButton small disabled={loading || !lastEvaluatedKey} onclick={nextPage}>
            Next page
          </RcButton>
        </div>
      {/if}
    {/if}
  </RcPanel>
</div>

{#if drawerOpen}
  <DynamoItemDrawer
    tableName={table.name}
    keySchema={table.keySchema}
    attributeDefinitions={table.attributeDefinitions}
    initialItem={editingItem}
    onclose={() => (drawerOpen = false)}
    onmutated={() => {
      onMutated();
      if (canAutoRerunCurrentRead()) {
        void runRead(pageKeys[currentPageIndex], currentPageIndex);
      }
    }}
  />
{/if}

<style>
  .browser {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }
  .header-actions,
  .results-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .meta-pill {
    font: 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
    padding: 2px 7px;
    border-radius: 5px;
    background: var(--bg-element);
  }
  .controls {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .mode-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
  }
  .segmented {
    display: inline-flex;
    padding: 2px;
    border-radius: 7px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
  }
  .segmented button {
    padding: 4px 11px;
    border-radius: 5px;
    font-size: 11.5px;
    font-weight: 500;
    color: var(--text-secondary);
  }
  .segmented button.active {
    background: var(--bg-app);
    color: var(--text-primary);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
  }
  .inline-field {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: var(--text-tertiary);
  }
  .inline-field select {
    height: 28px;
    padding: 0 8px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    font: 11.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
  }

  .query-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    align-items: flex-end;
  }
  .sk-group {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: flex-end;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 170px;
    flex: 1 1 180px;
  }
  .field.sk-op {
    flex: 0 0 130px;
    min-width: 120px;
  }
  .field.sort-order {
    flex: 0 0 120px;
    min-width: 110px;
  }
  .field > span {
    font-size: 11px;
    color: var(--text-secondary);
  }
  .field code {
    font: 500 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
  }
  .field small {
    color: var(--text-tertiary);
  }
  .field input,
  .field select,
  .field textarea {
    padding: 6px 9px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    font: 11.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
    outline: none;
  }
  .field input:focus,
  .field select:focus,
  .field textarea:focus {
    border-color: var(--border-focus);
  }
  .hidden-submit {
    display: none;
  }

  .advanced-wrap {
    border-top: 1px solid var(--border-subtle);
    padding-top: 8px;
  }
  .advanced-toggle {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-secondary);
  }
  .advanced-toggle:hover {
    color: var(--text-primary);
  }
  .advanced-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
    margin-top: 10px;
  }
  @media (max-width: 720px) {
    .advanced-grid {
      grid-template-columns: 1fr;
    }
  }

  .error-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red);
    font-size: 12px;
  }

  .empty-results {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 36px 12px;
    text-align: center;
    color: var(--text-tertiary);
    font-size: 12px;
  }

  .table-scroll {
    width: 100%;
    overflow-x: auto;
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
  }
  .items-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  .items-table th {
    position: sticky;
    top: 0;
    text-align: left;
    padding: 8px 10px;
    background: var(--bg-element);
    border-bottom: 1px solid var(--border-subtle);
    font: 600 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-secondary);
    white-space: nowrap;
  }
  .th-inner {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .key-tag {
    padding: 1px 4px;
    border-radius: 4px;
    font-size: 9.5px;
    background: color-mix(in srgb, var(--topology-dynamodb) 16%, transparent);
    color: var(--topology-dynamodb);
  }
  .items-table td {
    padding: 7px 10px;
    border-bottom: 1px solid var(--border-subtle);
    max-width: 260px;
    vertical-align: middle;
  }
  .item-row {
    cursor: pointer;
    transition: background 100ms ease;
  }
  .item-row:hover {
    background: var(--bg-element-hover);
  }
  .item-row:last-child td {
    border-bottom: 0;
  }
  .cell-val {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .type-badge {
    flex: 0 0 auto;
    padding: 1px 4px;
    border-radius: 3px;
    background: var(--bg-element);
    border: 1px solid var(--border-subtle);
    font: 9px var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
  }
  .val-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: 11.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
  }
  .null-cell {
    font: 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
  }
  .actions-col {
    width: 1%;
    white-space: nowrap;
    text-align: right;
  }
  .row-actions {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .mini-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 6px;
    border-radius: 5px;
    color: var(--text-tertiary);
    font-size: 10.5px;
  }
  .mini-btn:hover {
    background: var(--bg-element);
    color: var(--text-primary);
  }
  .mini-btn.danger:hover {
    color: var(--accent-red);
  }
  .mini-btn.danger.armed {
    background: var(--accent-red);
    color: #fff;
  }

  .pager {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    padding-top: 12px;
  }
  .pager-info {
    font: 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
  }
</style>
