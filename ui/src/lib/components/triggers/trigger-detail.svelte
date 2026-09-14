<script lang="ts">
  import RcPanel from "$lib/components/rack/rc-panel.svelte";
  import RcKv from "$lib/components/rack/rc-kv.svelte";
  import RcTonePill from "$lib/components/rack/rc-tone-pill.svelte";
  import FormattedMessageViewer from "$lib/components/common/formatted-message-viewer.svelte";
  import { formatJSONForViewer } from "$lib/json-format";
  import { triggerStateTone, type TriggerRow } from "./trigger-list.svelte";

  let {
    trigger,
  }: {
    trigger: TriggerRow;
  } = $props();

  const payload = $derived(
    trigger.lastResult ? formatJSONForViewer(trigger.lastResult) : null,
  );

  const summary = $derived([
    { label: "Type", value: trigger.type, mono: true },
    { label: "State", value: trigger.state, mono: true },
    { label: "Source ARN", value: trigger.sourceArn, mono: true, dim: true },
    { label: "Target ARN", value: trigger.targetArn, mono: true, dim: true },
  ]);

  const details = $derived(
    trigger.detailFields.map((field) => ({
      label: field.label,
      value: field.value,
      mono: true,
    })),
  );
</script>

<div class="detail">
  <!-- Hero -->
  <header class="hero">
    <div class="identity">
      <div class="title-row">
        <h1 title="{trigger.sourceName} → {trigger.targetName}">{trigger.sourceName} → {trigger.targetName}</h1>
        <RcTonePill tone="neutral">{trigger.type.toLowerCase()}</RcTonePill>
        <RcTonePill tone={triggerStateTone(trigger.state)}>{trigger.state.toLowerCase()}</RcTonePill>
      </div>
      <p class="subline">
        <span>{trigger.detailLabel}</span><i></i><span>{trigger.detail}</span>
      </p>
    </div>
  </header>

  <RcPanel title="Summary" index={0}>
    <RcKv items={summary} />
  </RcPanel>

  <RcPanel title="Mapping details" index={1}>
    <RcKv items={details} />
  </RcPanel>

  <RcPanel
    title="Payload"
    description={trigger.lastResult
      ? "Last captured payload or result."
      : "No payload or result details were captured for this trigger."}
    index={2}
  >
    {#if trigger.lastResult}
      {#if payload}
        <FormattedMessageViewer
          raw={trigger.lastResult}
          formatted={payload.formatted}
          formattedHtml={payload.formattedHtml}
          formattedLabel="Formatted"
          rawLabel="Raw"
          formattedOpenByDefault={true}
          rawOpenByDefault={false}
          formattedContentClass="text-[11px] text-foreground"
          rawContentClass="text-[11px] text-muted-foreground"
          formattedMaxHeightClass="max-h-[24rem]"
          rawMaxHeightClass="max-h-[20rem]"
        />
      {:else}
        <pre class="code plain">{trigger.lastResult}</pre>
      {/if}
    {:else}
      <p class="empty">Invoke the source or wait for the next delivery to capture a payload.</p>
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

  .empty { font-size: 11.5px; color: var(--text-tertiary); }
  .code {
    max-height: 24rem; overflow-y: auto; padding: 10px 12px; border-radius: 8px; background: var(--bg-app);
    font: 11px/1.7 var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
    white-space: pre-wrap; word-break: break-all;
  }
</style>
