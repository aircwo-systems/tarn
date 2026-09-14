<script lang="ts">
  import { ArrowUpRightIcon } from "phosphor-svelte";
  import type { RequestTrace } from "$lib/types";

  let {
    traces,
    onOpenTrace,
    onViewAll,
  }: {
    traces: RequestTrace[];
    onOpenTrace: (traceId: string) => void;
    onViewAll?: () => void;
  } = $props();

  function traceLabel(t: RequestTrace): string {
    const eb = t.spans.find((s) => s.kind === "eventbridge");
    if (eb) return `EVENTBRIDGE ${eb.name}`;
    return `${t.method ?? ""} ${t.path ?? ""}`.trim() || `trace:${t.id.slice(0, 8)}`;
  }

  function statusTone(status: number): "red" | "amber" | "green" {
    if (status >= 500) return "red";
    if (status >= 400) return "amber";
    return "green";
  }

  function spanColor(kind: string): string {
    const map: Record<string, string> = {
      gateway: "var(--accent-red)",
      lambda: "var(--color-blue)",
      queue: "var(--accent-amber)",
      dlq: "var(--accent-red)",
      topic: "var(--accent-green)",
      eventbridge: "var(--accent-green)",
    };
    return map[kind] ?? "var(--text-tertiary)";
  }

  function chainSegs(t: RequestTrace): { width: number; color: string; title: string }[] {
    if (!t.spans.length) return [];
    const total = Math.max(t.durationMs, t.spans.reduce((s, sp) => s + sp.durationMs, 0), 1);
    return t.spans.map((sp) => ({
      width: Math.max(6, Math.round((sp.durationMs / total) * 80)),
      color: spanColor(sp.kind),
      title: sp.name,
    }));
  }

  function timeAgo(iso: string): string {
    const d = Date.now() - new Date(iso).getTime();
    if (isNaN(d) || d < 0) return "";
    if (d < 60000) return `${Math.round(d / 1000)}s ago`;
    if (d < 3600000) return `${Math.round(d / 60000)}m ago`;
    return `${Math.round(d / 3600000)}h ago`;
  }

  function fmtMs(ms: number): string {
    return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`;
  }

  const errorCount = $derived(traces.filter((t) => t.status >= 500).length);
</script>

<div class="activity">
  <div class="activity-head">
    <span class="activity-title">Activity</span>
    {#if traces.length > 0}
      <span class="activity-count">{traces.length}</span>
      {#if errorCount > 0}
        <span class="activity-errors">{errorCount} err</span>
      {/if}
    {/if}
    {#if onViewAll}
      <button type="button" class="activity-all" onclick={onViewAll}>
        Traces<ArrowUpRightIcon size={10} />
      </button>
    {/if}
  </div>

  <div class="activity-list">
    {#if traces.length === 0}
      <p class="empty">No recent requests</p>
    {:else}
      {#each traces.slice(0, 25) as trace (trace.id)}
        {@const segs = chainSegs(trace)}
        {@const tone = statusTone(trace.status)}
        <button
          type="button"
          class="ev"
          data-tone={tone}
          onclick={() => onOpenTrace(trace.id)}
          aria-label="Open trace {traceLabel(trace)}"
        >
          <span class="ev-tick" aria-hidden="true"></span>
          <span class="ev-status">{trace.status}</span>
          <span class="ev-main">
            <span class="ev-label">{traceLabel(trace)}</span>
            {#if segs.length}
              <span class="ev-segs" aria-hidden="true">
                {#each segs as seg, i (i)}
                  <span style="width:{seg.width}px;background:{seg.color}" title={seg.title}></span>
                {/each}
              </span>
            {/if}
            {#if trace.status >= 500}
              <span class="ev-error">{trace.spans.find((s) => s.status === "error")?.name ?? "error"}</span>
            {/if}
          </span>
          <span class="ev-right">
            <span class="ev-duration">{fmtMs(trace.durationMs)}</span>
            <span class="ev-ago">{timeAgo(trace.startedAt)}</span>
          </span>
          <span class="ev-go"><ArrowUpRightIcon size={11} /></span>
        </button>
      {/each}
    {/if}
  </div>
</div>

<style>
  .activity { display: flex; flex-direction: column; min-height: 0; min-width: 0; }
  .activity-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
  .activity-title {
    font-size: 10.5px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-tertiary);
  }
  .activity-count {
    font: 10.5px var(--font-mono, ui-monospace, monospace); font-variant-numeric: tabular-nums;
    color: var(--text-secondary);
  }
  .activity-errors {
    font: 10px var(--font-mono, ui-monospace, monospace); color: var(--accent-red);
    background: color-mix(in srgb, var(--accent-red) 10%, transparent);
    border-radius: 6px; padding: 1px 6px;
  }
  .activity-all {
    margin-left: auto; display: inline-flex; align-items: center; gap: 3px;
    font-size: 10.5px; color: var(--text-tertiary); transition: color 100ms ease;
  }
  .activity-all:hover { color: var(--text-primary); }

  .activity-list { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; }
  .empty { display: flex; height: 6rem; align-items: center; justify-content: center; font-size: 11px; color: var(--text-tertiary); }

  .ev {
    position: relative; display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 7px 6px 7px 14px; border-radius: 6px; border-bottom: 1px solid var(--border-subtle);
    font-family: var(--font-ui-mono, var(--font-mono, monospace)); text-align: left;
    transition: background 100ms ease; cursor: pointer;
  }
  .ev:hover { background: var(--bg-element-hover); }

  .ev-tick {
    position: absolute; left: 5px; top: 50%; transform: translateY(-50%);
    width: 2px; height: 12px; border-radius: 2px; background: var(--accent-green);
  }
  .ev[data-tone="amber"] .ev-tick { background: var(--accent-amber); }
  .ev[data-tone="red"] .ev-tick { background: var(--accent-red); }

  .ev-status { width: 30px; flex-shrink: 0; font-size: 11px; font-weight: 600; color: var(--accent-green); opacity: 0.85; font-variant-numeric: tabular-nums; }
  .ev[data-tone="amber"] .ev-status { color: var(--accent-amber); opacity: 1; }
  .ev[data-tone="red"] .ev-status { color: var(--accent-red); opacity: 1; }

  .ev-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .ev-label {
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font-size: 11.5px; color: var(--text-primary); opacity: 0.85; transition: opacity 100ms ease;
  }
  .ev:hover .ev-label { opacity: 1; }
  .ev-segs { display: flex; align-items: center; gap: 2px; height: 3px; }
  .ev-segs span { height: 3px; border-radius: 1px; opacity: 0.55; }
  .ev-error { font-size: 10px; color: var(--accent-red); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .ev-right { flex-shrink: 0; display: flex; flex-direction: column; align-items: flex-end; gap: 1px; }
  .ev-duration { font-size: 11px; color: var(--text-secondary); font-variant-numeric: tabular-nums; }
  .ev-ago { font-size: 10px; color: var(--text-tertiary); }

  .ev-go { flex-shrink: 0; display: inline-flex; color: var(--text-tertiary); opacity: 0; transition: opacity 100ms ease; }
  .ev:hover .ev-go { opacity: 1; }
</style>
