<script lang="ts">
  import type { RequestTrace, TraceSpan } from "$lib/types";

  let {
    traces = [],
    selectedTraceId = null,
    onSelect = (_id: string) => {},
  }: {
    traces?: RequestTrace[];
    selectedTraceId?: string | null;
    onSelect?: (id: string) => void;
  } = $props();

  function timeAgo(isoString: string): string {
    const diff = Date.now() - new Date(isoString).getTime();
    if (diff < 60_000) return `${Math.floor(diff / 1_000)}s ago`;
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
    return `${Math.floor(diff / 3_600_000)}h ago`;
  }

  function formatDuration(ms: number): string {
    if (ms >= 1_000) return `${(ms / 1_000).toFixed(2)}s`;
    return `${ms}ms`;
  }

  function traceLabel(trace: RequestTrace): string {
    const eb = trace.spans.find((s) => s.kind === "eventbridge");
    if (eb) return `EVENTBRIDGE ${eb.name}`;
    const method = trace.method ?? "";
    const path = trace.path ?? "";
    return `${method} ${path}`.trim() || `trace:${trace.id.slice(0, 8)}`;
  }

  function spanColor(kind: string): string {
    switch (kind) {
      case "gateway": return "var(--color-chart-1)";
      case "lambda":  return "var(--color-primary)";
      case "queue":   return "var(--color-chart-4)";
      case "dlq":     return "var(--color-destructive)";
      case "topic":   return "var(--color-chart-1)";
      case "eventbridge": return "var(--color-chart-5, var(--color-primary))";
      default:        return "var(--color-muted-foreground)";
    }
  }

  function spanChain(
    trace: RequestTrace,
  ): Array<{ width: number; color: string; title: string }> {
    if (!trace.spans.length) return [];
    const total =
      trace.spans.reduce((s, sp) => s + (sp.durationMs || 0), 0) ||
      trace.durationMs ||
      1;
    const MAX_PX = 80;
    return trace.spans.map((sp) => ({
      width: Math.max(6, Math.round(((sp.durationMs || 0) / total) * MAX_PX)),
      color: spanColor(sp.kind),
      title: sp.name || sp.kind,
    }));
  }

  function dotClass(status: number): string {
    if (status >= 500) return "bg-destructive";
    if (status >= 400) return "bg-amber-400";
    return "bg-primary";
  }

  function statusClass(status: number): string {
    if (status >= 500) return "text-destructive";
    if (status >= 400) return "text-amber-400";
    return "text-muted-foreground";
  }

  function durClass(status: number): string {
    if (status >= 500) return "text-destructive";
    return "text-foreground/70";
  }

  function errorMeta(trace: RequestTrace): string | null {
    const errSpan = trace.spans.find(
      (s) => s.status === "error" || trace.status >= 500,
    );
    if (!errSpan) return null;
    const meta = errSpan.meta;
    if (meta?.["error.message"]) return errSpan.name + " " + meta["error.message"];
    if (errSpan.durationMs > 0 && trace.status >= 500)
      return `${errSpan.name} ${errSpan.durationMs}ms`;
    return null;
  }

  const sorted = $derived(
    [...traces].sort(
      (a, b) =>
        new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime(),
    ),
  );
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex shrink-0 items-center justify-between pb-2">
    <span class="font-mono text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/60">
      Activity
    </span>
    <span class="font-mono text-[10px] text-muted-foreground/40">live</span>
  </div>

  {#if sorted.length === 0}
    <div
      class="flex flex-1 items-center justify-center rounded border border-dashed border-border/60 py-12 text-[11px] text-muted-foreground/40"
    >
      No recent requests
    </div>
  {:else}
    <div class="min-h-0 flex-1 overflow-y-auto">
      {#each sorted as trace (trace.id)}
        {@const chain = spanChain(trace)}
        {@const meta = errorMeta(trace)}
        {@const isSelected = selectedTraceId === trace.id}
        <button
          type="button"
          class={`w-full border-b border-border/50 py-2 text-left transition-colors last:border-0 ${
            isSelected
              ? "bg-primary/5"
              : "hover:bg-muted/30"
          }`}
          onclick={() => onSelect(trace.id)}
          style="display: grid; grid-template-columns: 6px 28px 1fr auto; gap: 8px; align-items: start;"
        >
          <!-- status dot -->
          <span
            class={`mt-0.5 h-1.5 w-1.5 flex-shrink-0 self-start rounded-[1px] ${dotClass(trace.status)}`}
          ></span>

          <!-- HTTP status code -->
          <span class={`font-mono text-[11px] font-medium ${statusClass(trace.status)}`}>
            {trace.status}
          </span>

          <!-- body: path + span chain + optional error -->
          <div class="min-w-0">
            <div class="truncate font-mono text-[11px] text-foreground/80">
              {traceLabel(trace)}
            </div>
            {#if chain.length}
              <div class="mt-1 flex items-center gap-px">
                {#each chain as seg}
                  <div
                    title={seg.title}
                    style={`width: ${seg.width}px; height: 3px; border-radius: 1px; background: ${seg.color}; opacity: 0.5; flex-shrink: 0;`}
                  ></div>
                {/each}
              </div>
            {/if}
            {#if meta}
              <div class="mt-0.5 truncate font-mono text-[10px] text-destructive/80">
                {meta}
              </div>
            {/if}
          </div>

          <!-- right: duration + age -->
          <div class="shrink-0 text-right">
            <div class={`font-mono text-[11px] ${durClass(trace.status)}`}>
              {formatDuration(trace.durationMs)}
            </div>
            <div class="font-mono text-[10px] text-muted-foreground/40">
              {timeAgo(trace.startedAt)}
            </div>
          </div>
        </button>
      {/each}
    </div>
  {/if}
</div>
