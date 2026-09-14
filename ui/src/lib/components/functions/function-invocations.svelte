<script lang="ts">
  import { ArrowUpRightIcon } from "phosphor-svelte";
  import { fly } from "svelte/transition";
  import type { FunctionInvocation } from "$lib/function-links";
  import { formatMs, spanKindLabel } from "$lib/trace-utils";
  import { timeAgo } from "$lib/utils";

  let {
    invocations,
    timeoutSec,
    onopen,
  }: {
    invocations: FunctionInvocation[];
    timeoutSec: number;
    onopen: (traceId: string) => void;
  } = $props();

  const LIMIT = 8;
  const BARS = 32;

  // Oldest → newest so the strip reads left to right.
  const strip = $derived(invocations.slice(0, BARS).reverse());
  const peak = $derived(Math.max(1, ...strip.map((i) => i.durationMs)));
  let hovered = $state<string | null>(null);
</script>

<div class="inv">
  <div class="strip" role="list" aria-label="Recent invocation durations">
    {#each Array(Math.max(0, BARS - strip.length)) as _, i (i)}
      <span class="bar empty"></span>
    {/each}
    {#each strip as inv (inv.traceId)}
      <button
        type="button"
        class="bar"
        class:err={inv.status === "error"}
        class:hot={hovered === inv.traceId}
        style:--h="{Math.max(8, (inv.durationMs / peak) * 100)}%"
        title="{formatMs(inv.durationMs)} · {timeAgo(inv.startedAt)}"
        onmouseenter={() => (hovered = inv.traceId)}
        onmouseleave={() => (hovered = null)}
        onclick={() => onopen(inv.traceId)}
        aria-label="Open trace, {formatMs(inv.durationMs)}"
      ></button>
    {/each}
  </div>
  <div class="scale">
    <span>peak {formatMs(peak)}</span>
    <span>timeout {timeoutSec}s</span>
  </div>

  <ul class="rows">
    {#each invocations.slice(0, LIMIT) as inv, i (inv.traceId)}
      <li in:fly={{ y: 4, duration: 220, delay: i * 25 }}>
        <button
          type="button"
          class="row"
          class:err={inv.status === "error"}
          class:hot={hovered === inv.traceId}
          onmouseenter={() => (hovered = inv.traceId)}
          onmouseleave={() => (hovered = null)}
          onclick={() => onopen(inv.traceId)}
        >
          <span class="when">{timeAgo(inv.startedAt)}</span>
          <span class="caller">
            <span class="kind">{inv.callerKind === "direct" ? "Direct" : spanKindLabel(inv.callerKind)}</span>
            <span class="name">{inv.callerName}</span>
          </span>
          {#if inv.downstream.length > 0}
            <span class="deps">
              {#each [...new Set(inv.downstream.map((d) => spanKindLabel(d.kind)))] as label (label)}
                <span class="dep">{label}</span>
              {/each}
            </span>
          {/if}
          <span class="dur">{formatMs(inv.durationMs)}</span>
          <ArrowUpRightIcon size={11} class="go" />
        </button>
      </li>
    {/each}
  </ul>
</div>

<style>
  .inv { display: flex; flex-direction: column; gap: 6px; }
  .strip { display: flex; align-items: flex-end; gap: 3px; height: 44px; }
  .bar {
    position: relative; flex: 1; height: 100%; min-width: 3px; border-radius: 3px; cursor: pointer;
  }
  .bar::after {
    content: ""; position: absolute; inset: auto 0 0 0; height: var(--h); border-radius: 3px;
    background: var(--text-tertiary); opacity: 0.55;
    transform-origin: bottom; animation: grow 420ms var(--ease-snappy) both;
    transition: opacity 120ms ease, background 120ms ease;
  }
  .bar.empty { cursor: default; }
  .bar.empty::after { height: 3px; opacity: 0.25; animation: none; }
  .bar.err::after { background: var(--accent-red); opacity: 0.75; }
  .bar.hot::after, .bar:hover::after { background: var(--text-primary); opacity: 0.95; }
  .bar.err.hot::after, .bar.err:hover::after { background: var(--accent-red); opacity: 1; }
  @keyframes grow { from { transform: scaleY(0.2); opacity: 0; } }
  .scale { display: flex; justify-content: space-between; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }

  .rows { display: flex; flex-direction: column; gap: 2px; margin-top: 8px; }
  .row {
    display: flex; align-items: center; gap: 12px; width: 100%; height: 32px; padding: 0 10px;
    border-radius: 8px; border: 1px solid transparent; text-align: left;
    transition: background 120ms ease, border-color 120ms ease, padding-left 220ms var(--ease-snappy);
  }
  .row:hover, .row.hot { background: var(--bg-element-hover); }
  .row.err { background: color-mix(in srgb, var(--accent-red) 5%, transparent); }
  .row.err:hover, .row.err.hot { border-color: color-mix(in srgb, var(--accent-red) 35%, transparent); }
  .when { width: 64px; flex-shrink: 0; font-size: 11px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
  .caller { display: flex; align-items: baseline; gap: 8px; flex: 1; min-width: 0; }
  .kind { font-size: 10.5px; color: var(--text-tertiary); flex-shrink: 0; }
  .name { font: 11.5px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .row:hover .name { color: var(--text-primary); }
  .err .name { color: var(--accent-red); }
  .deps { display: flex; gap: 4px; flex-shrink: 0; }
  .dep { font-size: 10px; padding: 1px 6px; border-radius: 6px; color: var(--text-secondary); background: var(--bg-element); }
  .dur { width: 56px; flex-shrink: 0; text-align: right; font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); font-variant-numeric: tabular-nums; }
  .row :global(.go) { color: var(--text-tertiary); opacity: 0; transition: opacity 120ms ease; flex-shrink: 0; }
  .row:hover :global(.go) { opacity: 1; }

  @media (prefers-reduced-motion: reduce) {
    .bar::after { animation: none; }
    .row { transition: none; }
  }
</style>
