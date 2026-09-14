<script lang="ts">
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcStat from "$lib/components/rack/rc-stat.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcTonePill from "$lib/components/rack/rc-tone-pill.svelte";
  import RcListRow from "$lib/components/rack/rc-list-row.svelte";
  import { formatUnixSeconds } from "$lib/utils";
  import type { SubscriptionSummary, TopicSummary } from "$lib/types";

  let {
    topic,
    subscriptions,
    onselectsubscription,
  }: {
    topic: TopicSummary;
    subscriptions: SubscriptionSummary[];
    onselectsubscription: (subscriptionArn: string) => void;
  } = $props();

  const numberFormatter = new Intl.NumberFormat("en-GB");

  const lambdaCount = $derived(subscriptions.filter((s) => s.protocol.toLowerCase() === "lambda").length);
  const sqsCount = $derived(subscriptions.filter((s) => s.protocol.toLowerCase() === "sqs").length);
  const otherCount = $derived(subscriptions.length - lambdaCount - sqsCount);

  const config = $derived([
    { label: "Type", value: topic.fifo ? "FIFO" : "Standard", mono: true },
    { label: "Created", value: formatUnixSeconds(topic.createdTimestamp), mono: true },
    { label: "Tags", value: String(topic.tagCount), mono: true, dim: topic.tagCount === 0 },
    { label: "ARN", value: topic.arn, mono: true, dim: true },
  ]);

  const tags = $derived(Object.entries(topic.tags ?? {}));
</script>

<div class="detail">
  <!-- Hero -->
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={topic.name}>{topic.name}</h1>
        <RcTonePill tone={topic.fifo ? "amber" : "neutral"}>{topic.fifo ? "fifo" : "standard"}</RcTonePill>
      </div>
      <p class="subline">
        <span>{topic.subscriptions} subscription{topic.subscriptions === 1 ? "" : "s"}</span><i></i><span>created {formatUnixSeconds(topic.createdTimestamp)}</span>
      </p>
    </div>
  </header>

  <!-- Numbers -->
  <div class="stats">
    <RcStat
      label="Subscriptions"
      value={numberFormatter.format(subscriptions.length)}
      sub="endpoints"
    />
    <RcStat
      label="Lambda"
      value={numberFormatter.format(lambdaCount)}
      sub="function targets"
    />
    <RcStat label="SQS" value={numberFormatter.format(sqsCount)} sub="queue targets" />
    <RcStat label="Other" value={numberFormatter.format(otherCount)} sub="remaining targets" />
  </div>

  <RcPanel
    title="Subscriptions"
    description="Endpoints fanning out from this topic. Select one to inspect it."
    index={0}
  >
    {#if subscriptions.length === 0}
      <p class="empty">No subscriptions on this topic yet.</p>
    {:else}
      <div class="rows">
        {#each subscriptions as sub (sub.subscriptionArn)}
          <RcListRow
            mono
            title={sub.endpoint}
            sub="{sub.protocol} · {sub.rawMessageDelivery ? 'raw' : 'envelope'}{sub.filterPolicy ? ' · filtered' : ''}"
            onclick={() => onselectsubscription(sub.subscriptionArn)}
          />
        {/each}
      </div>
    {/if}
  </RcPanel>

  <RcPanel title="Configuration" index={1}>
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

  .stats {
    display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; padding: 14px 2px 10px;
    animation: statsIn 320ms var(--ease-snappy) both;
  }
  @keyframes statsIn { from { opacity: 0; transform: translateY(4px); } }
  @media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  .empty { font-size: 11.5px; color: var(--text-tertiary); }
  .rows { display: flex; flex-direction: column; gap: 2px; }

  .tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; }
  .tag {
    display: inline-flex; gap: 6px; height: 22px; align-items: center; padding: 0 8px; border-radius: 8px;
    background: var(--bg-element); font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
  }
  .tag span { color: var(--text-tertiary); }

  @media (prefers-reduced-motion: reduce) { .stats { animation: none; } }
</style>
