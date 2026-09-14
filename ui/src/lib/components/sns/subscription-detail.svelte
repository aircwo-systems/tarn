<script lang="ts">
  import { CheckIcon, CopyIcon } from "phosphor-svelte";
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcTonePill, { type Tone } from "$lib/components/rack/rc-tone-pill.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import type { SubscriptionSummary } from "$lib/types";

  let {
    subscription,
  }: {
    subscription: SubscriptionSummary;
  } = $props();

  const policy = $derived(
    subscription.filterPolicy ? formatJSONForViewer(subscription.filterPolicy) : null,
  );

  let filterView = $state<"formatted" | "raw">("formatted");
  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

  function protocolTone(protocol: string): Tone {
    switch (protocol.toLowerCase()) {
      case "lambda":
        return "green";
      case "sqs":
        return "amber";
      default:
        return "neutral";
    }
  }

  async function copyFilter() {
    if (!subscription.filterPolicy) return;
    const content =
      filterView === "formatted"
        ? (policy?.formatted ?? subscription.filterPolicy)
        : subscription.filterPolicy;
    try {
      await navigator.clipboard.writeText(content);
      copied = true;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1400);
    } catch (error) {
      console.error("Failed to copy SNS filter", error);
    }
  }

  const config = $derived([
    { label: "Topic", value: subscription.topicName, mono: true },
    { label: "Protocol", value: subscription.protocol, mono: true },
    { label: "Endpoint", value: subscription.endpoint, mono: true },
    { label: "Delivery", value: subscription.rawMessageDelivery ? "Raw" : "Envelope", mono: true },
    { label: "Filter scope", value: subscription.filterPolicyScope ?? "--", mono: true, dim: !subscription.filterPolicyScope },
    { label: "Filter", value: subscription.filterPolicy ? "Configured" : "None", mono: true, dim: !subscription.filterPolicy },
    { label: "Topic ARN", value: subscription.topicArn, mono: true, dim: true },
    { label: "Subscription ARN", value: subscription.subscriptionArn, mono: true, dim: true },
  ]);
</script>

<div class="detail">
  <!-- Hero -->
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title={subscription.endpoint}>{subscription.endpoint}</h1>
        <RcTonePill tone={protocolTone(subscription.protocol)}>{subscription.protocol}</RcTonePill>
        <RcTonePill tone="neutral">{subscription.rawMessageDelivery ? "raw" : "envelope"}</RcTonePill>
      </div>
      <p class="subline">
        <span>{subscription.topicName}</span><i></i><span>{subscription.filterPolicy ? "filter configured" : "no filter"}</span>
      </p>
    </div>
  </header>

  <RcPanel
    title="Filter policy"
    description={subscription.filterPolicy
      ? "Inspect the active subscription filter."
      : "No filter policy on this subscription."}
    index={0}
  >
    {#snippet actions()}
      {#if subscription.filterPolicy}
        <div class="view-toggle" role="group" aria-label="Filter view">
          <button
            type="button"
            class:active={filterView === "formatted"}
            onclick={() => (filterView = "formatted")}
          >Formatted</button>
          <button
            type="button"
            class:active={filterView === "raw"}
            onclick={() => (filterView = "raw")}
          >Raw</button>
        </div>
        <button type="button" class="btn ghost" onclick={() => void copyFilter()}>
          {#if copied}<CheckIcon size={11} class="ok" />Copied{:else}<CopyIcon size={11} />Copy{/if}
        </button>
      {/if}
    {/snippet}
    {#if !subscription.filterPolicy}
      <p class="empty">Every message on {subscription.topicName} is delivered to this endpoint.</p>
    {:else if filterView === "formatted"}
      {#if policy}
        <pre class="code">{@html policy.formattedHtml}</pre>
      {:else}
        <pre class="code plain">{subscription.filterPolicy}</pre>
      {/if}
    {:else}
      <pre class="code plain">{subscription.filterPolicy}</pre>
    {/if}
  </RcPanel>

  <RcPanel title="Configuration" index={1}>
    <RcKv items={config} />
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

  .btn {
    display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 9px; border-radius: 8px;
    border: 1px solid transparent; font-size: 11px; color: var(--text-secondary); text-decoration: none;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .btn:hover { color: var(--text-primary); border-color: var(--border-subtle); background: var(--bg-element-hover); }
  .btn:active { transform: scale(0.96); }
  .btn :global(.ok) { color: var(--accent-green); }
  .btn:focus-visible { outline: 1px solid var(--border-focus); outline-offset: 2px; }

  .view-toggle {
    display: inline-flex; padding: 2px; gap: 2px; border-radius: 8px;
    border: 1px solid var(--border-subtle); background: var(--bg-app);
  }
  .view-toggle button {
    height: 20px; padding: 0 8px; border-radius: 6px; font-size: 10.5px; color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease;
  }
  .view-toggle button:hover { color: var(--text-secondary); }
  .view-toggle button.active { color: var(--text-primary); background: var(--bg-element); }

  .empty { font-size: 11.5px; color: var(--text-tertiary); }
  .code {
    max-height: 24rem; overflow-y: auto; padding: 10px 12px; border-radius: 8px; background: var(--bg-app);
    font: 11px/1.7 var(--font-mono, ui-monospace, monospace); color: var(--text-primary);
    white-space: pre-wrap; word-break: break-all;
  }
  .code.plain { color: var(--text-secondary); }
</style>
