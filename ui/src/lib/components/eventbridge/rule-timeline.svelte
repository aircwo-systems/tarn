<script lang="ts">
  import { onMount } from "svelte";
  import { describeSchedule } from "$lib/eventbridge-schedule";
  import { formatDate, timeAgo } from "$lib/utils";
  import type { EventBridgeRuleSummary } from "$lib/types";

  let { rule }: { rule: EventBridgeRuleSummary } = $props();

  let now = $state(Date.now());
  onMount(() => {
    const id = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(id);
  });

  const enabled = $derived(rule.state === "ENABLED");
  const last = $derived(rule.lastRunAt ? new Date(rule.lastRunAt).getTime() : NaN);
  const next = $derived(rule.nextRunAt ? new Date(rule.nextRunAt).getTime() : NaN);
  const interval = $derived(describeSchedule(rule.scheduleExpression).intervalMs);

  // Window runs from the previous fire (or one interval back) to the next one.
  const start = $derived(Number.isFinite(last) ? last : Number.isFinite(next) && interval ? next - interval : NaN);
  const progress = $derived(
    enabled && Number.isFinite(start) && Number.isFinite(next) && next > start
      ? Math.min(1, Math.max(0, (now - start) / (next - start)))
      : 0,
  );

  function countdown(target: number): string {
    const s = Math.round((target - now) / 1000);
    if (s <= 0) return "due now";
    if (s < 60) return `in ${s}s`;
    if (s < 3600) return `in ${Math.floor(s / 60)}m ${String(s % 60).padStart(2, "0")}s`;
    if (s < 86400) return `in ${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
    return `in ${Math.floor(s / 86400)}d`;
  }
</script>

<div class="timeline" class:off={!enabled}>
  <div class="end">
    <span class="label">Last run</span>
    <span class="value">{rule.lastRunAt ? timeAgo(rule.lastRunAt, now) : "never"}</span>
    {#if rule.lastRunAt}<span class="sub">{formatDate(rule.lastRunAt)}</span>{/if}
  </div>

  <div class="track" aria-hidden="true">
    <span class="fill" style:transform="scaleX({progress})"></span>
    {#if enabled && progress > 0}<span class="head" style:left="{progress * 100}%"></span>{/if}
  </div>

  <div class="end right">
    <span class="label">Next run</span>
    <span class="value" class:live={enabled && Number.isFinite(next)}>
      {#if !enabled}paused{:else if Number.isFinite(next)}{countdown(next)}{:else}on event{/if}
    </span>
    {#if enabled && rule.nextRunAt}<span class="sub">{formatDate(rule.nextRunAt)}</span>{/if}
  </div>
</div>

<style>
  .timeline { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 18px; }
  .end { display: flex; flex-direction: column; gap: 2px; min-width: 110px; }
  .right { text-align: right; align-items: flex-end; }
  .label { font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary); }
  .value { font: 600 15px var(--font-mono, ui-monospace, monospace); color: var(--text-primary); font-variant-numeric: tabular-nums; }
  .value.live { color: var(--accent-green); }
  .sub { font-size: 10.5px; color: var(--text-tertiary); }
  .track { position: relative; height: 4px; border-radius: 4px; background: var(--bg-element); }
  .fill {
    position: absolute; inset: 0; border-radius: 4px; transform-origin: left;
    background: color-mix(in srgb, var(--accent-green) 55%, transparent);
    transition: transform 1s linear;
  }
  .head {
    position: absolute; top: 50%; width: 10px; height: 10px; margin: -5px 0 0 -5px; border-radius: 50%;
    background: var(--accent-green); box-shadow: 0 0 0 4px color-mix(in srgb, var(--accent-green) 18%, transparent);
    transition: left 1s linear;
  }
  .off .value { color: var(--text-tertiary); }
  @media (max-width: 700px) {
    .timeline { grid-template-columns: 1fr 1fr; }
    .track { grid-column: 1 / -1; grid-row: 2; }
  }
  @media (prefers-reduced-motion: reduce) { .fill, .head { transition: none; } }
</style>
