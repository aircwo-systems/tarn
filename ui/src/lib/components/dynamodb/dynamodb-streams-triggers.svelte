<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import {
    ArrowClockwiseIcon,
    ArrowRightIcon,
    CaretDownIcon,
    CaretRightIcon,
    CheckIcon,
    CopyIcon,
    PlayIcon,
    PauseIcon,
    ScrollIcon,
    LightningIcon,
    ArrowsClockwiseIcon,
    WarningCircleIcon,
  } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import RcTonePill, { type Tone } from "#lib/components/rack/rc-tone-pill.svelte";
  import { fetchDynamoStreamRecords } from "#lib/api";
  import { dynamoStateTone, unmarshalItem } from "#lib/dynamodb";
  import type { DynamoStreamRecord, DynamoTableDetailResponse } from "#lib/types";

  let {
    detail,
    onNavigate = (_tab: string) => {},
  }: {
    detail: DynamoTableDetailResponse;
    onNavigate?: (tab: string) => void;
  } = $props();

  const table = $derived(detail.table);
  const triggers = $derived(detail.integrations?.triggers ?? []);
  const shards = $derived(detail.stream?.shards ?? []);

  let selectedShardId = $state("");
  let iteratorType = $state<"TRIM_HORIZON" | "LATEST">("TRIM_HORIZON");
  let limit = $state(50);
  let records = $state<DynamoStreamRecord[]>([]);
  let nextShardIterator = $state<string | undefined>(undefined);
  let loading = $state(false);
  let error = $state("");
  let autoPoll = $state(false);
  let rawJson = $state(false);
  let copiedStreamArn = $state(false);
  let expandedEvents = $state<Record<string, boolean>>({});
  let pollTimer: ReturnType<typeof setInterval> | undefined;

  function eventTone(name: string): Tone {
    if (name === "INSERT") return "green";
    if (name === "MODIFY") return "amber";
    if (name === "REMOVE") return "red";
    return "neutral";
  }

  $effect(() => {
    if (shards.length > 0 && (!selectedShardId || !shards.some((s) => s.shardId === selectedShardId))) {
      selectedShardId = shards[0].shardId;
      nextShardIterator = undefined;
    }
  });

  async function copyStreamArn() {
    if (!table.latestStreamArn) return;
    try {
      await navigator.clipboard.writeText(table.latestStreamArn);
      copiedStreamArn = true;
      setTimeout(() => (copiedStreamArn = false), 1500);
    } catch {}
  }

  async function loadRecords(useNextIterator = false) {
    if (!table.streamEnabled) return;
    loading = true;
    error = "";
    try {
      const res = await fetchDynamoStreamRecords(table.name, {
        shardId: selectedShardId || undefined,
        iteratorType: useNextIterator && nextShardIterator ? undefined : iteratorType,
        shardIterator: useNextIterator ? nextShardIterator : undefined,
        limit,
      });
      if (useNextIterator && nextShardIterator) {
        const seen = new Set(records.map((r) => r.eventID));
        const fresh = (res.records ?? []).filter((r) => !seen.has(r.eventID));
        if (fresh.length > 0) {
          records = [...fresh.slice().reverse(), ...records].slice(0, 200);
        }
      } else {
        records = (res.records ?? []).slice().reverse();
      }
      nextShardIterator = res.nextShardIterator;
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load stream records";
    } finally {
      loading = false;
    }
  }

  function toggleAutoPoll() {
    autoPoll = !autoPoll;
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = undefined;
    }
    if (autoPoll) {
      pollTimer = setInterval(() => {
        if (!loading) void loadRecords(Boolean(nextShardIterator));
      }, 2500);
    }
  }

  function formatImage(img: unknown) {
    if (!img || typeof img !== "object") return "{}";
    const obj = rawJson ? img : unmarshalItem(img as Record<string, any>);
    return JSON.stringify(obj, null, 2);
  }

  function formatEpoch(sec: number | undefined): string {
    if (!sec) return "--";
    return new Date(sec * 1000).toLocaleTimeString();
  }

  onMount(() => {
    if (shards.length > 0 && !selectedShardId) {
      selectedShardId = shards[0].shardId;
    }
    if (table.streamEnabled) {
      void loadRecords(false);
    }
  });

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer);
  });
</script>

<div class="streams-tab">
  <RcPanel
    title="Lambda Stream Triggers"
    description="Event source mappings reading change records from this table's DynamoDB stream."
    index={0}
  >
    {#snippet actions()}
      <RcTonePill tone={triggers.length > 0 ? "green" : "neutral"}>
        {triggers.length} trigger{triggers.length === 1 ? "" : "s"}
      </RcTonePill>
    {/snippet}

    {#if triggers.length === 0}
      <p class="empty">
        No Lambda event source mappings are attached to this table.
        {#if !table.streamEnabled}
          Enable DynamoDB Streams on the table to attach triggers.
        {/if}
      </p>
    {:else}
      <div class="trigger-list">
        {#each triggers as tr (tr.uuid)}
          <div class="trigger-row">
            <div class="trigger-main">
              <div class="trigger-head">
                <button
                  type="button"
                  class="link-btn mono"
                  onclick={() => onNavigate(`functions?fn=${encodeURIComponent(tr.functionName)}`)}
                >
                  <LightningIcon size={12} />
                  {tr.functionName}
                  <ArrowRightIcon size={11} />
                </button>
                <RcTonePill tone={dynamoStateTone(tr.state)}>{tr.state.toLowerCase()}</RcTonePill>
                <span class="meta-chip">batch {tr.batchSize}</span>
                {#if tr.lastResult}
                  <span class="meta-chip">last: {tr.lastResult}</span>
                {/if}
              </div>
              <div class="trigger-sub">
                <span>UUID: <code>{tr.uuid}</code></span>
                {#if tr.filterCriteria?.Filters?.length}
                  <span>{tr.filterCriteria.Filters.length} filter pattern{tr.filterCriteria.Filters.length === 1 ? "" : "s"}</span>
                {/if}
              </div>
            </div>

            <div class="trigger-actions">
              <RcButton
                small
                variant="ghost"
                onclick={() => onNavigate(`triggers?id=${encodeURIComponent(`dynamodb-${tr.uuid}`)}`)}
              >
                <ArrowsClockwiseIcon size={12} /> Trigger
              </RcButton>
              {#if tr.logGroup}
                <RcButton
                  small
                  variant="ghost"
                  onclick={() => onNavigate(`logs?groups=${encodeURIComponent(tr.logGroup!)}`)}
                >
                  <ScrollIcon size={12} /> Logs
                </RcButton>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </RcPanel>

  <RcPanel
    title="Live Stream Records"
    description="Inspect change data capture events directly from DynamoDB Streams shards."
    index={1}
  >
    {#snippet actions()}
      {#if table.latestStreamArn}
        <RcButton small variant="ghost" onclick={copyStreamArn} title={table.latestStreamArn}>
          {#if copiedStreamArn}<CheckIcon size={11} />Copied{:else}<CopyIcon size={11} />Stream ARN{/if}
        </RcButton>
      {/if}
      <RcTonePill tone={table.streamEnabled ? "green" : "neutral"}>
        {table.streamEnabled ? table.streamViewType || "ENABLED" : "DISABLED"}
      </RcTonePill>
    {/snippet}

    {#if !table.streamEnabled}
      <p class="empty">DynamoDB Streams is not enabled on this table.</p>
    {:else}
      <div class="stream-controls">
        {#if shards.length > 0}
          <label class="inline-field">
            <span>Shard</span>
            <select bind:value={selectedShardId} onchange={() => (nextShardIterator = undefined)}>
              {#each shards as shard (shard.shardId)}
                <option value={shard.shardId}>{shard.shardId}</option>
              {/each}
            </select>
          </label>
        {/if}

        <label class="inline-field">
          <span>Start</span>
          <select bind:value={iteratorType} onchange={() => (nextShardIterator = undefined)}>
            <option value="TRIM_HORIZON">TRIM_HORIZON</option>
            <option value="LATEST">LATEST</option>
          </select>
        </label>

        <label class="inline-field">
          <span>Limit</span>
          <select bind:value={limit}>
            <option value={25}>25</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
          </select>
        </label>

        <div class="stream-btns">
          <RcButton small disabled={loading} onclick={() => loadRecords(false)}>
            <ArrowClockwiseIcon size={12} /> Reload
          </RcButton>
          {#if nextShardIterator}
            <RcButton small disabled={loading} onclick={() => loadRecords(true)}>
              Poll new
            </RcButton>
          {/if}
          <RcButton small variant={autoPoll ? "primary" : "ghost"} onclick={toggleAutoPoll}>
            {#if autoPoll}<PauseIcon size={12} /> Live{:else}<PlayIcon size={12} /> Auto-poll{/if}
          </RcButton>
          <RcButton small variant="ghost" onclick={() => (rawJson = !rawJson)}>
            {rawJson ? "DynamoDB JSON" : "Document JSON"}
          </RcButton>
        </div>
      </div>

      {#if error}
        <div class="error-box" role="alert">
          <WarningCircleIcon size={14} />
          <span>{error}</span>
        </div>
      {/if}

      {#if records.length === 0}
        <p class="empty">
          {loading
            ? "Reading stream shard..."
            : "No records at this iterator position yet. Write or update an item and reload or enable Auto-poll."}
        </p>
      {:else}
        <div class="record-list">
          {#each records as rec (rec.eventID)}
            {@const isOpen = !!expandedEvents[rec.eventID]}
            <div class="record-card">
              <button
                type="button"
                class="record-head"
                onclick={() =>
                  (expandedEvents = { ...expandedEvents, [rec.eventID]: !isOpen })}
              >
                <span class="record-left">
                  {#if isOpen}<CaretDownIcon size={12} />{:else}<CaretRightIcon size={12} />{/if}
                  <RcTonePill tone={eventTone(rec.eventName)}>{rec.eventName}</RcTonePill>
                  <span class="seq">#{rec.dynamodb.SequenceNumber}</span>
                  {#if rec.dynamodb.Keys}
                    <span class="key-preview">
                      {JSON.stringify(unmarshalItem(rec.dynamodb.Keys))}
                    </span>
                  {/if}
                </span>
                <span class="record-right">
                  <span>{formatEpoch(rec.dynamodb.ApproximateCreationDateTime)}</span>
                  <span>{rec.dynamodb.SizeBytes} B</span>
                </span>
              </button>

              {#if isOpen}
                <div class="record-body">
                  {#if rec.dynamodb.Keys}
                    <div class="img-block">
                      <span class="img-label">Keys</span>
                      <pre>{formatImage(rec.dynamodb.Keys)}</pre>
                    </div>
                  {/if}
                  <div class="images-grid">
                    {#if rec.dynamodb.OldImage}
                      <div class="img-block">
                        <span class="img-label">OldImage</span>
                        <pre>{formatImage(rec.dynamodb.OldImage)}</pre>
                      </div>
                    {/if}
                    {#if rec.dynamodb.NewImage}
                      <div class="img-block">
                        <span class="img-label">NewImage</span>
                        <pre>{formatImage(rec.dynamodb.NewImage)}</pre>
                      </div>
                    {/if}
                  </div>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </RcPanel>
</div>

<style>
  .streams-tab {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }
  .empty {
    font-size: 12px;
    color: var(--text-tertiary);
    padding: 8px 0;
  }
  .trigger-list {
    display: flex;
    flex-direction: column;
  }
  .trigger-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    padding: 10px 0;
    border-top: 1px solid var(--border-subtle);
  }
  .trigger-row:first-child {
    padding-top: 0;
    border-top: 0;
  }
  .trigger-head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .link-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12.5px;
    font-weight: 500;
    color: var(--text-primary);
  }
  .link-btn.mono {
    font-family: var(--font-mono, ui-monospace, monospace);
  }
  .link-btn:hover {
    text-decoration: underline;
  }
  .meta-chip {
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--bg-element);
    font: 10.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-secondary);
  }
  .trigger-sub {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    margin-top: 4px;
    font-size: 11px;
    color: var(--text-tertiary);
  }
  .trigger-sub code {
    font-family: var(--font-mono, ui-monospace, monospace);
  }
  .trigger-actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .stream-controls {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    margin-bottom: 12px;
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
    font: 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
  }
  .stream-btns {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    margin-left: auto;
  }

  .error-box {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 9px 11px;
    margin-bottom: 10px;
    border-radius: 7px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red);
    font-size: 11.5px;
  }

  .record-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .record-card {
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    overflow: hidden;
  }
  .record-head {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 8px 10px;
    background: var(--bg-element);
    text-align: left;
  }
  .record-head:hover {
    background: var(--bg-element-hover);
  }
  .record-left {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .seq {
    font: 10.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
  }
  .key-preview {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
  }
  .record-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: 0 0 auto;
    font: 10.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
  }
  .record-body {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px 12px;
    border-top: 1px solid var(--border-subtle);
  }
  .images-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: 10px;
  }
  .img-block {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .img-label {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }
  pre {
    margin: 0;
    padding: 8px 10px;
    border-radius: 6px;
    background: var(--bg-element);
    font: 11px/1.55 var(--font-mono, ui-monospace, monospace);
    color: var(--text-secondary);
    overflow-x: auto;
  }
</style>
