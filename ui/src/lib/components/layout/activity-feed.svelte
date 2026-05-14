<script lang="ts">
  import type { RequestTrace } from "$lib/types";

  let {
    traces,
    onOpenTrace,
  }: {
    traces: RequestTrace[];
    onOpenTrace: (traceId: string) => void;
  } = $props();

  function traceLabel(t: RequestTrace): string {
    const eb = t.spans.find((s) => s.kind === "eventbridge");
    if (eb) return `EVENTBRIDGE ${eb.name}`;
    return `${t.method ?? ""} ${t.path ?? ""}`.trim() || `trace:${t.id.slice(0, 8)}`;
  }

  function dotClass(status: number): string {
    if (status >= 500) return "bg-[var(--color-red)]";
    if (status >= 400) return "bg-[var(--color-amber)]";
    return "bg-[var(--color-accent)]";
  }

  function statusClass(status: number): string {
    if (status >= 500) return "text-[var(--color-red)]";
    if (status >= 400) return "text-[var(--color-amber)]";
    return "text-[var(--color-text-muted)]";
  }

  function spanColor(kind: string): string {
    const map: Record<string, string> = {
      gateway: "var(--color-red)",
      lambda: "var(--color-blue)",
      queue: "var(--color-amber)",
      dlq: "var(--color-red)",
      topic: "var(--color-accent)",
      eventbridge: "var(--color-accent)",
    };
    return map[kind] ?? "var(--color-text-muted)";
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
</script>

<div class="flex min-h-0 flex-col">
  <div class="mb-2 flex items-center justify-between text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground/50">
    <span>Activity</span>
  </div>

  <div class="flex-1 overflow-y-auto">
    {#if traces.length === 0}
      <div class="flex h-24 items-center justify-center text-[11px] text-muted-foreground/30">
        No recent requests
      </div>
    {:else}
      {#each traces.slice(0, 25) as trace (trace.id)}
        {@const segs = chainSegs(trace)}
        <button
          type="button"
          class="grid w-full cursor-pointer items-center gap-2 border-b border-border/50 py-2 text-left transition-colors hover:bg-white/[0.02]"
          style="grid-template-columns:6px 30px 1fr auto"
          onclick={() => onOpenTrace(trace.id)}
        >
          <span class="inline-block h-[6px] w-[6px] rounded-[1px] {dotClass(trace.status)}"></span>

          <span class="text-right text-[11px] font-medium {statusClass(trace.status)}">{trace.status}</span>

          <div class="min-w-0">
            <div class="truncate text-[11px] text-muted-foreground/80">{traceLabel(trace)}</div>
            {#if segs.length}
              <div class="mt-[3px] flex items-center gap-[2px]" style="height:3px">
                {#each segs as seg, i (i)}
                  <div
                    class="rounded-[1px]"
                    style="width:{seg.width}px;height:3px;background:{seg.color};opacity:0.5"
                    title={seg.title}
                  ></div>
                {/each}
              </div>
            {/if}
            {#if trace.status >= 500}
              <div class="mt-[2px] text-[10px]" style="color:var(--color-red)">
                {trace.spans.find((s) => s.status === "error")?.name ?? "error"}
              </div>
            {/if}
          </div>

          <div class="shrink-0 text-right">
            <div class="text-[11px] text-muted-foreground/70">{fmtMs(trace.durationMs)}</div>
            <div class="text-[10px] text-muted-foreground/40">{timeAgo(trace.startedAt)}</div>
          </div>
        </button>
      {/each}
    {/if}
  </div>
</div>
