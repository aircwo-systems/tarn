<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import {
    ArrowClockwiseIcon,
    ArrowSquareOutIcon,
    CheckIcon,
    CopyIcon,
    DatabaseIcon,
    FlowArrowIcon,
    LightningIcon,
    RowsIcon,
    WarningCircleIcon,
  } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcKv from "#lib/components/rack/rc-kv.svelte";
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import RcStat from "#lib/components/rack/rc-stat.svelte";
  import RcTonePill from "#lib/components/rack/rc-tone-pill.svelte";
  import DynamoItemsBrowser from "./dynamodb-items-browser.svelte";
  import DynamoStreamsTriggers from "./dynamodb-streams-triggers.svelte";
  import DynamoIntegrations from "./dynamodb-integrations.svelte";
  import { fetchDynamoTableDetail } from "#lib/api";
  import { DYNAMO_VIEWER_CHANNEL, dynamoStateTone } from "#lib/dynamodb";
  import { formatBytes, formatDate } from "#lib/utils";
  import { refresh as refreshDashboard } from "#lib/state.svelte";
  import type {
    DynamoDBStreamSummary,
    DynamoDBTableSummary,
    DynamoTableDetailResponse,
    OverviewResponse,
  } from "#lib/types";

  export type DynamoSubTab = "overview" | "items" | "streams" | "integrations";

  let {
    table,
    streams,
    config,
    activeSubTab = "overview",
    popout = false,
    onSelectSubTab = (_tab: DynamoSubTab) => {},
    onPopout = () => {},
    onNavigate = (_target: string) => {},
  }: {
    table: DynamoDBTableSummary;
    streams: DynamoDBStreamSummary[];
    config: OverviewResponse["config"] | null;
    activeSubTab?: DynamoSubTab;
    popout?: boolean;
    onSelectSubTab?: (tab: DynamoSubTab) => void;
    onPopout?: () => void;
    onNavigate?: (target: string) => void;
  } = $props();

  const numberFormatter = new Intl.NumberFormat("en-GB");

  let detail = $state<DynamoTableDetailResponse | null>(null);
  let detailLoading = $state(false);
  let detailError = $state("");
  let mutationTick = $state(0);

  let copied = $state<"arn" | "endpoint" | null>(null);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let channel: BroadcastChannel | null = null;

  async function loadDetail() {
    const name = table.name;
    detailLoading = true;
    detailError = "";
    try {
      const next = await fetchDynamoTableDetail(name);
      if (table.name === name) {
        detail = next;
      }
    } catch (e) {
      if (table.name === name) {
        detailError = e instanceof Error ? e.message : "Failed to load table detail";
      }
    } finally {
      if (table.name === name) {
        detailLoading = false;
      }
    }
  }

  onMount(() => {
    void loadDetail();
    try {
      channel = new BroadcastChannel(DYNAMO_VIEWER_CHANNEL);
      channel.onmessage = (ev) => {
        if (ev.data?.tableName === table.name) {
          mutationTick += 1;
          void loadDetail();
          void refreshDashboard();
        }
      };
    } catch {
      channel = null;
    }
  });

  onDestroy(() => {
    if (copyTimer) clearTimeout(copyTimer);
    channel?.close();
  });

  const resolvedItemCount = $derived(detail?.table.itemCount ?? table.itemCount);
  const resolvedLSIs = $derived(detail?.table.localSecondaryIndexes ?? []);
  const resolvedGSIs = $derived(detail?.table.globalSecondaryIndexes ?? []);
  const indexCount = $derived(
    detail
      ? resolvedLSIs.length + resolvedGSIs.length
      : table.localIndexes + table.globalIndexes,
  );
  const shardCount = $derived(
    detail?.stream?.shards.length ??
      streams.reduce((total, stream) => total + stream.shardCount, 0),
  );
  const tableTone = $derived(dynamoStateTone(detail?.table.status ?? table.status));
  const connectionDetails = $derived(parseEndpoint(config?.endpoint));

  const triggersCount = $derived(detail?.integrations?.triggers?.length ?? 0);
  const integrationsCount = $derived(
    detail
      ? (detail.integrations.triggers?.length ?? 0) +
          (detail.integrations.lambdas?.length ?? 0) +
          (detail.integrations.ecs?.length ?? 0) +
          (detail.integrations.stepFunctions?.length ?? 0) +
          (detail.integrations.kmsKey ? 1 : 0) +
          (detail.kinesisDestinations?.length ?? 0)
      : 0,
  );

  const tableDetails = $derived([
    { label: "ARN", value: detail?.table.arn ?? table.arn, mono: true, dim: true },
    { label: "Created", value: formatDate(detail?.table.createdDate ?? table.createdDate) },
    {
      label: "Billing mode",
      value: detail?.table.billingMode ?? (table.billingMode || "PAY_PER_REQUEST"),
      mono: true,
    },
    { label: "Key schema", value: table.keySchema, mono: true },
    ...(detail?.table.tableClass
      ? [{ label: "Table class", value: detail.table.tableClass, mono: true }]
      : []),
    {
      label: "Deletion guard",
      value: detail?.table.deletionProtectionEnabled ? "Enabled" : "Disabled",
    },
  ]);

  const governanceItems = $derived(
    detail
      ? [
          {
            label: "TTL",
            value:
              detail.ttl.status === "ENABLED"
                ? `Enabled (${detail.ttl.attributeName || "attribute unset"})`
                : detail.ttl.status || "DISABLED",
          },
          {
            label: "PITR backups",
            value: detail.continuousBackups.pointInTimeRecoveryStatus || "DISABLED",
          },
          {
            label: "Encryption",
            value: detail.table.sseDescription
              ? `${detail.table.sseDescription.sseType || "KMS"} (${detail.table.sseDescription.status})`
              : "Default AWS owned",
          },
        ]
      : [],
  );

  const connectionItems = $derived([
    { label: "Endpoint", value: connectionDetails.endpoint, mono: true },
    { label: "Host", value: connectionDetails.host, mono: true },
    { label: "Port", value: connectionDetails.port, mono: true },
    { label: "Region", value: config?.region ?? "--", mono: true },
    { label: "Account", value: config?.accountId ?? "--", mono: true },
    { label: "Access key", value: "test", mono: true },
    { label: "Secret key", value: "test", mono: true },
  ]);

  async function copy(value: string, kind: "arn" | "endpoint") {
    try {
      await navigator.clipboard.writeText(value);
      copied = kind;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = null), 1600);
    } catch {}
  }

  function parseEndpoint(endpoint?: string) {
    if (!endpoint) return { endpoint: "--", host: "--", port: "--" };
    try {
      const parsed = new URL(endpoint);
      return {
        endpoint,
        host: parsed.hostname || "--",
        port: parsed.port || (parsed.protocol === "https:" ? "443" : "80"),
      };
    } catch {
      return { endpoint, host: "--", port: "--" };
    }
  }

  function handleMutated() {
    void loadDetail();
    void refreshDashboard();
  }
</script>

<div class="detail">
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={table.name}>{table.name}</h1>
        <RcTonePill tone={tableTone}>{(detail?.table.status ?? table.status).toLowerCase()}</RcTonePill>
      </div>
      <p class="subline">
        <span>DynamoDB table</span><i></i>
        <span>{numberFormatter.format(resolvedItemCount)} items</span><i></i>
        <span>{table.keySchema}</span>
        {#if detail}
          <i></i>
          <span>{formatBytes(detail.table.tableSizeBytes)}</span>
        {/if}
      </p>
    </div>

    <div class="hero-actions">
      <RcButton small variant="ghost" disabled={detailLoading} onclick={loadDetail}>
        <ArrowClockwiseIcon size={12} />
        {detailLoading ? "Syncing..." : "Refresh"}
      </RcButton>
      <RcButton small onclick={() => copy(table.arn, "arn")}>
        {#if copied === "arn"}<CheckIcon size={11} />Copied{:else}<CopyIcon size={11} />ARN{/if}
      </RcButton>
      {#if !popout}
        <RcButton small onclick={onPopout} title="Open DynamoDB viewer in a standalone window">
          <ArrowSquareOutIcon size={12} />
          Pop out
        </RcButton>
      {/if}
    </div>
  </header>

  <nav class="subtabs" aria-label="Table viewer sections">
    <button
      type="button"
      class="subtab"
      class:active={activeSubTab === "overview"}
      onclick={() => onSelectSubTab("overview")}
    >
      <DatabaseIcon size={13} />
      <span>Overview</span>
    </button>
    <button
      type="button"
      class="subtab"
      class:active={activeSubTab === "items"}
      onclick={() => onSelectSubTab("items")}
    >
      <RowsIcon size={13} />
      <span>Items</span>
      <span class="tab-count">{numberFormatter.format(resolvedItemCount)}</span>
    </button>
    <button
      type="button"
      class="subtab"
      class:active={activeSubTab === "streams"}
      onclick={() => onSelectSubTab("streams")}
    >
      <LightningIcon size={13} />
      <span>Streams &amp; Triggers</span>
      {#if triggersCount > 0}
        <span class="tab-count">{triggersCount}</span>
      {:else if table.streamEnabled}
        <span class="tab-dot"></span>
      {/if}
    </button>
    <button
      type="button"
      class="subtab"
      class:active={activeSubTab === "integrations"}
      onclick={() => onSelectSubTab("integrations")}
    >
      <FlowArrowIcon size={13} />
      <span>Integrations</span>
      {#if integrationsCount > 0}
        <span class="tab-count">{integrationsCount}</span>
      {/if}
    </button>
  </nav>

  {#if detailError}
    <div class="error-banner" role="alert">
      <WarningCircleIcon size={14} />
      <span>{detailError}</span>
    </div>
  {/if}

  {#if activeSubTab === "overview"}
    <div class="stats">
      <RcStat
        label="Items"
        value={numberFormatter.format(resolvedItemCount)}
        sub="stored in table"
      />
      <RcStat
        label="Size"
        value={detail ? formatBytes(detail.table.tableSizeBytes) : "--"}
        sub={detail?.table.billingMode ?? table.billingMode ?? "PAY_PER_REQUEST"}
      />
      <RcStat
        label="Indexes"
        value={numberFormatter.format(indexCount)}
        sub="{detail ? resolvedLSIs.length : table.localIndexes} LSI · {detail ? resolvedGSIs.length : table.globalIndexes} GSI"
      />
      <RcStat
        label="Stream"
        value={table.streamEnabled ? "On" : "Off"}
        sub={table.streamEnabled ? `${shardCount || 1} shard${shardCount === 1 ? "" : "s"}` : "not configured"}
        tone={table.streamEnabled ? "green" : undefined}
      />
    </div>

    <div class="split">
      <RcPanel title="Table shape" description="Identity, capacity mode, and primary key layout." index={0}>
        <RcKv items={tableDetails} labelWidth="7.5rem" />
        {#if detail?.table.attributeDefinitions?.length}
          <div class="attr-defs">
            <span class="eyebrow">Key &amp; Index Attributes</span>
            <div class="attr-chips">
              {#each detail.table.attributeDefinitions as attr (attr.attributeName)}
                <span class="attr-chip">
                  <b>{attr.attributeName}</b>
                  <small>{attr.attributeType}</small>
                </span>
              {/each}
            </div>
          </div>
        {/if}
      </RcPanel>

      <RcPanel title="Governance &amp; Encryption" description="TTL, PITR backups, KMS encryption, and tags." index={1}>
        {#if detail}
          <RcKv items={governanceItems} labelWidth="7.5rem" />
          {#if detail.integrations.kmsKey}
            <div class="kms-row">
              <span>KMS Key</span>
              <button
                type="button"
                class="inline-link"
                onclick={() => onNavigate(`kms?key=${encodeURIComponent(detail!.integrations.kmsKey!.keyId)}`)}
              >
                {detail.integrations.kmsKey.aliases?.[0] || detail.integrations.kmsKey.keyId}
              </button>
            </div>
          {/if}
          {#if Object.keys(detail.tags ?? {}).length > 0}
            <div class="tags-wrap">
              <span class="eyebrow">Tags</span>
              <div class="tag-chips">
                {#each Object.entries(detail.tags) as [k, v] (k)}
                  <span class="tag-chip">{k}={v}</span>
                {/each}
              </div>
            </div>
          {/if}
        {:else}
          <p class="note">Loading table configuration...</p>
        {/if}
      </RcPanel>
    </div>

    <RcPanel title="Secondary Indexes" description="Global and local secondary access paths." index={2}>
      {#if indexCount === 0}
        <p class="empty">No local or global secondary indexes are configured.</p>
      {:else if detail}
        <div class="indexes-list">
          {#each resolvedGSIs as gsi (gsi.indexName)}
            <div class="index-row">
              <div class="index-head">
                <span class="index-kind">GSI</span>
                <span class="index-name">{gsi.indexName}</span>
                {#if gsi.indexStatus}
                  <RcTonePill tone={dynamoStateTone(gsi.indexStatus)}>{gsi.indexStatus.toLowerCase()}</RcTonePill>
                {/if}
              </div>
              <div class="index-meta">
                <span>Keys: <code>{gsi.keySchema.map((k) => `${k.attributeName} (${k.keyType})`).join(", ")}</code></span>
                <span>Projection: <code>{gsi.projection?.projectionType || "ALL"}</code></span>
                <span>{numberFormatter.format(gsi.itemCount)} items · {formatBytes(gsi.indexSizeBytes)}</span>
              </div>
            </div>
          {/each}
          {#each resolvedLSIs as lsi (lsi.indexName)}
            <div class="index-row">
              <div class="index-head">
                <span class="index-kind">LSI</span>
                <span class="index-name">{lsi.indexName}</span>
              </div>
              <div class="index-meta">
                <span>Keys: <code>{lsi.keySchema.map((k) => `${k.attributeName} (${k.keyType})`).join(", ")}</code></span>
                <span>Projection: <code>{lsi.projection?.projectionType || "ALL"}</code></span>
                <span>{numberFormatter.format(lsi.itemCount)} items · {formatBytes(lsi.indexSizeBytes)}</span>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </RcPanel>

    <RcPanel title="Connect externally" description="Use any AWS SDK, the AWS CLI, or a DynamoDB-compatible client." index={3}>
      {#snippet actions()}
        <RcButton small variant="ghost" onclick={() => copy(connectionDetails.endpoint, "endpoint")}>
          {#if copied === "endpoint"}<CheckIcon size={11} />Copied{:else}<CopyIcon size={11} />Endpoint{/if}
        </RcButton>
      {/snippet}

      <div class="connection-grid">
        <RcKv items={connectionItems} labelWidth="7rem" />
        <div class="client-note">
          <span class="eyebrow">Client setup</span>
          <p>Point your client at the endpoint override and use the local credentials above. Tarn speaks the standard DynamoDB JSON API used by AWS SDKs and CLI tooling.</p>
          <pre>endpoint_url = "{connectionDetails.endpoint}"
region = "{config?.region ?? "--"}"
access_key_id = "test"</pre>
        </div>
      </div>
    </RcPanel>
  {:else if activeSubTab === "items"}
    {#if detail}
      <DynamoItemsBrowser {detail} externalMutationTick={mutationTick} onMutated={handleMutated} />
    {:else}
      <p class="note">Loading table schema...</p>
    {/if}
  {:else if activeSubTab === "streams"}
    {#if detail}
      <DynamoStreamsTriggers {detail} {onNavigate} />
    {:else}
      <p class="note">Loading streams &amp; triggers...</p>
    {/if}
  {:else if activeSubTab === "integrations"}
    {#if detail}
      <DynamoIntegrations {detail} {onNavigate} />
    {:else}
      <p class="note">Loading infrastructure integrations...</p>
    {/if}
  {/if}
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

  .hero { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; flex-wrap: wrap; padding: 4px 2px 2px; }
  .identity { min-width: 0; }
  .title-row { display: flex; align-items: center; gap: 10px; min-width: 0; }
  h1 {
    font: 600 19px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.02em; color: var(--text-primary);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subline { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; font-size: 11.5px; color: var(--text-tertiary); }
  .subline i { width: 3px; height: 3px; border-radius: 1px; background: var(--border-default); }
  .hero-actions { display: flex; gap: 6px; flex-wrap: wrap; }

  .subtabs {
    display: flex;
    align-items: center;
    gap: 4px;
    border-bottom: 1px solid var(--border-subtle);
    padding-bottom: 1px;
    overflow-x: auto;
  }
  .subtab {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    white-space: nowrap;
    transition: color 120ms ease, border-color 120ms ease;
  }
  .subtab:hover { color: var(--text-primary); }
  .subtab.active {
    color: var(--text-primary);
    border-bottom-color: var(--text-primary);
  }
  .tab-count {
    padding: 1px 6px;
    border-radius: 99px;
    background: var(--bg-element);
    font: 10.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
  }
  .tab-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-green);
  }

  .error-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 9px 12px;
    border-radius: 8px;
    border: 1px solid color-mix(in srgb, var(--accent-red) 35%, transparent);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    color: var(--accent-red);
    font-size: 12px;
  }

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .split { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
  @media (max-width: 1100px) { .split { grid-template-columns: minmax(0, 1fr); } }

  .attr-defs, .tags-wrap {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-subtle);
  }
  .attr-chips, .tag-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 8px;
  }
  .attr-chip, .tag-chip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 3px 8px;
    border-radius: 6px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    font: 11px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
  }
  .attr-chip small { color: var(--text-tertiary); }
  .kms-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border-subtle);
    font-size: 11.5px;
    color: var(--text-secondary);
  }
  .inline-link {
    font: 11.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-primary);
    text-decoration: underline;
  }

  .indexes-list { display: flex; flex-direction: column; }
  .index-row {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 10px 0;
    border-top: 1px solid var(--border-subtle);
  }
  .index-row:first-child { padding-top: 0; border-top: 0; }
  .index-head { display: flex; align-items: center; gap: 8px; }
  .index-kind {
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--bg-element);
    border: 1px solid var(--border-subtle);
    font: 600 10px var(--font-mono, ui-monospace, monospace);
    color: var(--text-secondary);
  }
  .index-name { font: 600 12.5px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }
  .index-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 16px;
    font-size: 11px;
    color: var(--text-secondary);
  }
  .index-meta code { font-family: var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }

  .empty, .note { font-size: 11.5px; color: var(--text-tertiary); }

  .connection-grid { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(15rem, 0.9fr); gap: 24px; }
  .client-note { align-self: start; padding: 12px; border-left: 2px solid var(--border-default); background: var(--bg-app); }
  .eyebrow { display: block; font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-tertiary); }
  .client-note p { margin-top: 6px; font-size: 11.5px; line-height: 1.55; color: var(--text-secondary); }
  pre {
    margin-top: 10px; overflow-x: auto; padding: 9px 10px; background: var(--bg-element);
    font: 10.5px/1.65 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); white-space: pre;
  }

  @media (max-width: 700px) {
    .connection-grid { grid-template-columns: minmax(0, 1fr); gap: 10px; }
  }
  @media (prefers-reduced-motion: reduce) { .stats { animation: none; } }
</style>
