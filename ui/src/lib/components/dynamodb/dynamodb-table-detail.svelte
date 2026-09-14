<script lang="ts">
  import { onDestroy } from "svelte";
  import { CheckIcon, CopyIcon } from "phosphor-svelte";
  import RcButton from "$lib/components/rack/rc-button.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import type { DynamoDBStreamSummary, DynamoDBTableSummary, OverviewResponse } from "$lib/types";

  let {
    table,
    streams,
    config,
  }: {
    table: DynamoDBTableSummary;
    streams: DynamoDBStreamSummary[];
    config: OverviewResponse["config"] | null;
  } = $props();

  const numberFormatter = new Intl.NumberFormat("en-GB");

  const indexCount = $derived(table.localIndexes + table.globalIndexes);
  const shardCount = $derived(streams.reduce((total, stream) => total + stream.shardCount, 0));
  const tableTone = $derived(stateTone(table.status));
  const connectionDetails = $derived(parseEndpoint(config?.endpoint));

  const tableDetails = $derived([
    { label: "ARN", value: table.arn, mono: true, dim: true },
    { label: "Created", value: formatDate(table.createdDate) },
    { label: "Billing mode", value: table.billingMode || "not reported", mono: true },
    { label: "Key schema", value: table.keySchema, mono: true },
  ]);

  const indexDetails = $derived([
    { label: "Local indexes", value: numberFormatter.format(table.localIndexes), mono: true },
    { label: "Global indexes", value: numberFormatter.format(table.globalIndexes), mono: true },
  ]);

  const connectionItems = $derived([
    { label: "Endpoint", value: connectionDetails.endpoint, mono: true },
    { label: "Host", value: connectionDetails.host, mono: true },
    { label: "Port", value: connectionDetails.port, mono: true },
    { label: "Region", value: config?.region ?? "--", mono: true },
    { label: "Account", value: config?.accountId ?? "--", mono: true },
    { label: "Access key", value: "test", mono: true },
    { label: "Secret key", value: "test", mono: true },
  ]);

  let copied = $state<"arn" | "stream" | "endpoint" | null>(null);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

  async function copy(value: string, kind: "arn" | "stream" | "endpoint") {
    try {
      await navigator.clipboard.writeText(value);
      copied = kind;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = null), 1600);
    } catch {
      // Clipboard access is optional in local or insecure browser contexts.
    }
  }

  onDestroy(() => {
    if (copyTimer) clearTimeout(copyTimer);
  });

  function stateTone(value: string | undefined): Tone {
    const state = value?.toLowerCase() ?? "";
    if (["active", "enabled", "available"].includes(state)) return "green";
    if (["creating", "updating", "deleting", "archiving"].includes(state)) return "amber";
    if (["failed", "error", "inactive"].includes(state)) return "red";
    return "neutral";
  }

  function formatDate(value?: string | number): string {
    if (!value) return "--";
    if (typeof value === "number") return new Date(value * 1000).toLocaleString();
    const parsed = new Date(value);
    return Number.isNaN(parsed.valueOf()) ? String(value) : parsed.toLocaleString();
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
</script>

<div class="detail">
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={table.name}>{table.name}</h1>
        <RcTonePill tone={tableTone}>{table.status.toLowerCase()}</RcTonePill>
      </div>
      <p class="subline">
        <span>DynamoDB table</span><i></i>
        <span>{numberFormatter.format(table.itemCount)} items</span><i></i>
        <span>{table.keySchema}</span>
      </p>
    </div>
    <div class="hero-actions">
      <RcButton small onclick={() => copy(table.arn, "arn")}>
        {#if copied === "arn"}<CheckIcon size={11} />Copied{:else}<CopyIcon size={11} />ARN{/if}
      </RcButton>
    </div>
  </header>

  <div class="stats">
    <RcStat label="Items" value={numberFormatter.format(table.itemCount)} sub="reported by table" />
    <RcStat label="Indexes" value={numberFormatter.format(indexCount)} sub="{table.localIndexes} LSI · {table.globalIndexes} GSI" />
    <RcStat
      label="Stream"
      value={table.streamEnabled ? "On" : "Off"}
      sub={streams.length ? `${streams.length} channel${streams.length === 1 ? "" : "s"}` : "not configured"}
      tone={table.streamEnabled ? "green" : undefined}
    />
    <RcStat label="Shards" value={streams.length ? numberFormatter.format(shardCount) : "--"} sub="across selected streams" />
  </div>

  <div class="split">
    <RcPanel title="Table shape" description="Identity, capacity mode, and primary key layout." index={0}>
      <RcKv items={tableDetails} labelWidth="7rem" />
    </RcPanel>

    <RcPanel title="Indexes" description="Secondary indexes reported for this table." index={1}>
      <div class="index-summary">
        <span class="index-total">{numberFormatter.format(indexCount)}</span>
        <span><b>configured indexes</b><small>{indexCount ? "query paths beyond the primary key" : "no secondary access paths"}</small></span>
      </div>
      <RcKv items={indexDetails} labelWidth="7rem" />
      {#if indexCount === 0}<p class="note">No local or global secondary indexes are configured.</p>{/if}
    </RcPanel>
  </div>

  <RcPanel title="Streams" description="Change data capture channels attached to this table." index={2}>
    {#snippet actions()}
      <RcTonePill tone={table.streamEnabled ? "green" : "neutral"}>{table.streamEnabled ? "enabled" : "disabled"}</RcTonePill>
    {/snippet}

    {#if streams.length === 0 && table.streamEnabled && table.streamArn}
      <div class="stream-row">
        <div class="stream-main">
          <div class="stream-heading">
            <span class="stream-label">{table.latestStreamLabel || table.streamViewType || "DynamoDB stream"}</span>
            <RcTonePill tone="neutral">summary</RcTonePill>
          </div>
          <button type="button" class="stream-arn" title="Copy stream ARN" onclick={() => copy(table.streamArn!, "stream")}>
            <span>{table.streamArn}</span>
            {#if copied === "stream"}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
          </button>
        </div>
        <div class="stream-facts">
          <span><b>View</b>{table.streamViewType || "--"}</span>
          <span><b>Shards</b>--</span>
        </div>
      </div>
    {:else if streams.length === 0}
      <p class="empty">No stream channels are attached to this table.</p>
    {:else}
      <div class="stream-list">
        {#each streams as stream (stream.streamArn)}
          <div class="stream-row">
            <div class="stream-main">
              <div class="stream-heading">
                <span class="stream-label">{stream.streamLabel || stream.streamViewType || "DynamoDB stream"}</span>
                <RcTonePill tone={stateTone(stream.streamStatus)}>{stream.streamStatus.toLowerCase()}</RcTonePill>
              </div>
              <button type="button" class="stream-arn" title="Copy stream ARN" onclick={() => copy(stream.streamArn, "stream")}>
                <span>{stream.streamArn}</span>
                {#if copied === "stream"}<CheckIcon size={11} />{:else}<CopyIcon size={11} />{/if}
              </button>
            </div>
            <div class="stream-facts">
              <span><b>View</b>{stream.streamViewType || "--"}</span>
              <span><b>Shards</b>{numberFormatter.format(stream.shardCount)}</span>
              <span><b>Created</b>{formatDate(stream.createdDate)}</span>
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
  .hero-actions { display: flex; gap: 6px; }

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .split { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
  @media (max-width: 1100px) { .split { grid-template-columns: minmax(0, 1fr); } }

  .index-summary {
    display: flex; align-items: center; gap: 12px; min-height: 52px; margin-bottom: 14px; padding-bottom: 12px;
    border-bottom: 1px solid var(--border-subtle);
  }
  .index-total { font: 600 24px var(--font-mono, ui-monospace, monospace); letter-spacing: -0.04em; color: var(--text-primary); }
  .index-summary span:last-child { display: flex; flex-direction: column; gap: 2px; }
  .index-summary b { font-size: 11.5px; font-weight: 500; color: var(--text-secondary); }
  .index-summary small { font-size: 10.5px; color: var(--text-tertiary); }

  .stream-list { display: flex; flex-direction: column; }
  .stream-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(13rem, 0.8fr); gap: 18px; padding: 10px 0; border-top: 1px solid var(--border-subtle); }
  .stream-row:first-child { padding-top: 0; border-top: 0; }
  .stream-main { min-width: 0; }
  .stream-heading { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
  .stream-label { font: 12px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); }
  .stream-arn {
    display: flex; align-items: center; gap: 6px; max-width: 100%; margin-top: 6px; padding: 2px 4px; margin-left: -4px;
    border-radius: 6px; text-align: left; font: 10.5px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .stream-arn span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .stream-arn:hover { color: var(--text-primary); background: var(--bg-element-hover); }
  .stream-arn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }
  .stream-facts { display: flex; flex-wrap: wrap; align-content: center; justify-content: flex-end; gap: 6px 14px; font-size: 10.5px; color: var(--text-secondary); }
  .stream-facts span { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .stream-facts b { font-size: 9.5px; font-weight: 500; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary); }

  .empty, .note { font-size: 11.5px; color: var(--text-tertiary); }
  .note { margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--border-subtle); }

  .connection-grid { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(15rem, 0.9fr); gap: 24px; }
  .client-note { align-self: start; padding: 12px; border-left: 2px solid var(--border-default); background: var(--bg-app); }
  .eyebrow { display: block; font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--text-tertiary); }
  .client-note p { margin-top: 6px; font-size: 11.5px; line-height: 1.55; color: var(--text-secondary); }
  pre {
    margin-top: 10px; overflow-x: auto; padding: 9px 10px; background: var(--bg-element);
    font: 10.5px/1.65 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); white-space: pre;
  }

  @media (max-width: 700px) {
    .stream-row, .connection-grid { grid-template-columns: minmax(0, 1fr); gap: 10px; }
    .stream-facts { justify-content: flex-start; }
  }
  @media (prefers-reduced-motion: reduce) { .stats { animation: none; } }
</style>
