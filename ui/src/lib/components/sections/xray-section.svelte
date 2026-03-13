<script lang="ts">
  import { DetectiveIcon, ArrowUpRightIcon } from "phosphor-svelte";
  import { getDashboard } from "$lib/state.svelte";
  import type { RequestTrace, TraceSpan } from "$lib/types";

  const dashboard = getDashboard();
  const traces = $derived(dashboard.data?.recentTraces ?? []);

  let selectedTraceId = $state<string | null>(null);
  let searchQuery = $state("");

  const filteredTraces = $derived(
    searchQuery.trim()
      ? traces.filter((t) => {
          const q = searchQuery.toLowerCase();
          return (
            (t.path ?? "").toLowerCase().includes(q) ||
            (t.method ?? "").toLowerCase().includes(q) ||
            (t.gatewayName ?? "").toLowerCase().includes(q) ||
            t.id.toLowerCase().includes(q) ||
            t.spans.some(
              (s) =>
                s.name.toLowerCase().includes(q) ||
                s.kind.toLowerCase().includes(q),
            )
          );
        })
      : traces,
  );

  // If the current selection is no longer in filtered results, fall back to the first item
  const effectiveId = $derived(
    filteredTraces.some((t) => t.id === selectedTraceId)
      ? selectedTraceId
      : (filteredTraces[0]?.id ?? null),
  );

  const selectedTrace = $derived(
    filteredTraces.find((t) => t.id === effectiveId) ?? null,
  );

  const errorCount = $derived(traces.filter((t) => t.status >= 500).length);
  const clientErrorCount = $derived(
    traces.filter((t) => t.status >= 400 && t.status < 500).length,
  );
  const avgMs = $derived(
    traces.length > 0
      ? Math.round(traces.reduce((s, t) => s + t.durationMs, 0) / traces.length)
      : 0,
  );
  const p95Ms = $derived(
    traces.length >= 5
      ? [...traces].sort((a, b) => a.durationMs - b.durationMs)[
          Math.floor(traces.length * 0.95)
        ].durationMs
      : 0,
  );

  // ─── Kind styling ───
  function spanColor(kind: string): string {
    switch (kind.toLowerCase()) {
      case "gateway":
        return "var(--color-red)";
      case "lambda":
        return "var(--chart-6)";
      case "queue":
        return "var(--color-amber)";
      case "dlq":
        return "var(--color-red)";
      case "s3":
        return "var(--color-amber)";
      case "secret":
      case "secrets":
        return "var(--chart-2)";
      case "postgres":
      case "postgresql":
      case "mysql":
        return "var(--color-blue)";
      case "redis":
        return "var(--color-amber)";
      default:
        return "var(--color-text-muted)";
    }
  }

  function spanKindLabel(kind: string): string {
    switch (kind.toLowerCase()) {
      case "gateway":
        return "API GW";
      case "lambda":
        return "Lambda";
      case "queue":
        return "SQS";
      case "dlq":
        return "DLQ";
      case "s3":
        return "S3";
      case "secret":
      case "secrets":
        return "Secrets";
      case "postgres":
      case "postgresql":
        return "PostgreSQL";
      case "mysql":
        return "MySQL";
      case "redis":
        return "Redis";
      default:
        return kind.length > 9 ? kind.slice(0, 8) + "…" : kind;
    }
  }

  function formatMs(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  }

  function timeAgo(iso: string): string {
    const diff = Date.now() - new Date(iso).getTime();
    if (diff < 2000) return "just now";
    if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`;
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
    return new Date(iso).toLocaleTimeString();
  }

  function traceTitle(t: RequestTrace): string {
    if (t.method && t.path) return `${t.method} ${t.path}`;
    const s3 = t.spans.find((s) => s.kind === "s3");
    if (s3) return `S3 → ${s3.name}`;
    const q = t.spans.find((s) => s.kind === "queue" || s.kind === "dlq");
    if (q) return `ESM → ${q.name}`;
    const fn = t.spans.find((s) => s.kind === "lambda");
    if (fn) return `Invoke: ${fn.name}`;
    return `trace:${t.id.slice(0, 8)}`;
  }

  function viewInLogs(span: TraceSpan) {
    const group = span.kind === "lambda" ? `/aws/lambda/${span.name}` : null;
    if (group) window.location.hash = `logs?group=${encodeURIComponent(group)}`;
  }

  function statusDotClass(s: number): string {
    return s >= 500 ? "fill-red" : s >= 400 ? "fill-amber" : "fill-accent";
  }

  function statusBadgeClass(s: number): string {
    if (s >= 500) return "text-destructive border-red/40 bg-red/8";
    if (s >= 400) return "text-amber border-amber/40 bg-amber/8";
    return "text-primary border-primary/50 bg-primary/10";
  }

  function spanBadge(span: TraceSpan): { label: string; colorClass: string } {
    if (span.status === "error")
      return { label: "✕ error", colorClass: "text-destructive" };
    if (span.status === "client_error")
      return { label: "⚠ warn", colorClass: "text-amber" };
    return { label: "✓ ok", colorClass: "text-primary" };
  }

  // ─── Flow SVG geometry ───
  const NODE_W = 116;
  const NODE_H = 58;
  const ARROW_LEN = 40;
  const PAD_X = 20;
  const PAD_Y = 26;
  const FLOW_CH = NODE_H + PAD_Y * 2;

  function flowCW(n: number): number {
    return PAD_X * 2 + n * NODE_W + Math.max(0, n - 1) * ARROW_LEN;
  }
  function nodeX(i: number): number {
    return PAD_X + i * (NODE_W + ARROW_LEN) + NODE_W / 2;
  }
  const nodeCY = PAD_Y + NODE_H / 2;

  // ─── Waterfall ───
  // "Sub-spans" are collected from inside the Lambda execution (DB calls, secret
  // fetches, etc.). They are NESTED within the Lambda span, not sequential after
  // it. We right-align them within the enclosing Lambda: since the Lambda cannot
  // return before the sub-span completes, the sub-span's right edge ≈ Lambda's
  // right edge. This is the best approximation without absolute start timestamps.
  const SUB_SPAN_KINDS = new Set([
    "postgres",
    "postgresql",
    "mysql",
    "redis",
    "secret",
    "secrets",
  ]);

  interface WaterfallRow {
    span: TraceSpan;
    offsetPct: number;
    widthPct: number;
    nested: boolean;
  }

  function buildWaterfall(spans: TraceSpan[], total: number): WaterfallRow[] {
    if (total <= 0)
      return spans.map((span) => ({
        span,
        offsetPct: 0,
        widthPct: 2,
        nested: false,
      }));

    let cum = 0;
    let lambdaStartMs = 0;
    let lambdaDurationMs = total;
    const rows: WaterfallRow[] = [];

    for (const span of spans) {
      const kind = span.kind.toLowerCase();
      const nested = SUB_SPAN_KINDS.has(kind);

      let offsetMs: number;
      if (nested) {
        // Right-align within enclosing Lambda's time window.
        const lambdaEnd = lambdaStartMs + lambdaDurationMs;
        offsetMs = Math.max(lambdaStartMs, lambdaEnd - span.durationMs);
      } else {
        offsetMs = cum;
        if (kind === "lambda") {
          lambdaStartMs = cum;
          lambdaDurationMs = span.durationMs;
        }
        cum += span.durationMs;
      }

      rows.push({
        span,
        offsetPct: (offsetMs / total) * 100,
        widthPct: Math.max(0.5, (span.durationMs / total) * 100),
        nested,
      });
    }

    return rows;
  }
</script>

<div class="space-y-4">
  <!-- Header strip -->
  <div
    class="flex items-center justify-between gap-4 flex-wrap rounded-lg border border-border bg-card px-4 py-3"
  >
    <div class="flex items-center gap-3">
      <div
        class="flex items-center justify-center h-8 w-8 rounded-md bg-accent/10"
      >
        <DetectiveIcon size={16} class="text-primary" />
      </div>
      <div>
        <h2 class="text-sm font-semibold text-foreground">X-Ray Traces</h2>
        <p class="text-[10px] font-mono text-muted-foreground/70">
          End-to-end request flow visualiser
        </p>
      </div>
    </div>
    <div class="flex items-center gap-4 text-[11px] font-mono">
      <span class="text-muted-foreground/70"
        >{traces.length} trace{traces.length !== 1 ? "s" : ""}</span
      >
      {#if errorCount > 0}
        <span class="text-destructive">{errorCount} 5xx</span>
      {/if}
      {#if clientErrorCount > 0}
        <span class="text-amber">{clientErrorCount} 4xx</span>
      {/if}
      {#if traces.length > 0}
        <span class="text-muted-foreground/70">avg {formatMs(avgMs)}</span>
      {/if}
      {#if p95Ms > 0}
        <span class="text-muted-foreground/70">p95 {formatMs(p95Ms)}</span>
      {/if}
    </div>
  </div>

  {#if traces.length === 0}
    <!-- Empty state -->
    <div
      class="rounded-lg border border-border bg-card flex flex-col items-center gap-4 px-8 py-16"
    >
      <div
        class="flex items-center justify-center h-12 w-12 rounded-xl border border-border bg-muted"
      >
        <DetectiveIcon size={24} class="text-muted-foreground/70" />
      </div>
      <div class="text-center space-y-1.5">
        <p class="text-sm font-semibold text-muted-foreground">
          No traces recorded yet
        </p>
        <p class="text-xs text-muted-foreground/70 max-w-sm leading-relaxed">
          Make HTTP requests through an API Gateway or trigger SQS event source
          mappings. Traces will appear here showing the full request flow
          through each component.
        </p>
      </div>
      <div
        class="flex items-center gap-5 mt-1 text-[10px] font-mono text-muted-foreground/70"
      >
        <span class="flex items-center gap-1.5">
          <span
            class="h-2 w-2 rounded-sm inline-block"
            style="background:var(--color-red);opacity:0.6"
          ></span>
          API Gateway
        </span>
        <span class="flex items-center gap-1.5">
          <span
            class="h-2 w-2 rounded-sm inline-block"
            style="background:var(--color-accent);opacity:0.6"
          ></span>
          Lambda
        </span>
        <span class="flex items-center gap-1.5">
          <span
            class="h-2 w-2 rounded-sm inline-block"
            style="background:var(--color-amber);opacity:0.6"
          ></span>
          SQS / DLQ
        </span>
        <span class="flex items-center gap-1.5">
          <span
            class="h-2 w-2 rounded-sm inline-block"
            style="background:var(--color-blue);opacity:0.6"
          ></span>
          Secrets / DB
        </span>
      </div>
    </div>
  {:else}
    <div class="flex gap-4 items-start">
      <!-- ─── Trace list panel ─── -->
      <div
        class="w-72 shrink-0 rounded-lg border border-border bg-card overflow-hidden flex flex-col"
      >
        <div class="px-2.5 py-2 border-b border-border">
          <input
            type="text"
            bind:value={searchQuery}
            placeholder="Filter by path, method..."
            class="w-full rounded border border-border bg-muted px-2.5 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/70 outline-none focus:ring-1 focus:ring-primary"
          />
        </div>
        <div
          class="overflow-y-auto max-h-[calc(100vh-18rem)] divide-y divide-border/60"
        >
          {#each filteredTraces as trace (trace.id)}
            {@const lambdaSpan = trace.spans.find((s) => s.kind === "lambda")}
            <div
              class="border-l-2 {effectiveId === trace.id
                ? 'bg-muted border-primary'
                : 'border-transparent'}"
            >
              <button
                type="button"
                class="w-full text-left px-3 py-2.5 transition-colors hover:bg-muted/60"
                onclick={() => (selectedTraceId = trace.id)}
              >
                <div class="flex items-center gap-2 mb-1 min-w-0">
                  <svg width="6" height="6" viewBox="0 0 6 6" class="shrink-0">
                    <circle
                      cx="3"
                      cy="3"
                      r="3"
                      class={statusDotClass(trace.status)}
                    />
                  </svg>
                  <span
                    class="text-[11px] font-mono text-foreground truncate flex-1"
                    >{traceTitle(trace)}</span
                  >
                </div>
                <div
                  class="flex items-center gap-1.5 pl-3.5 text-[10px] font-mono text-muted-foreground/70"
                >
                  <span
                    class={trace.status >= 500
                      ? "text-destructive"
                      : trace.status >= 400
                        ? "text-amber"
                        : "text-muted-foreground/70"}>{trace.status}</span
                  >
                  <span>·</span>
                  <span>{formatMs(trace.durationMs)}</span>
                  <span class="ml-auto">{timeAgo(trace.startedAt)}</span>
                </div>
              </button>
              {#if trace.status >= 500 && lambdaSpan}
                <div class="px-3 pb-2">
                  <button
                    type="button"
                    onclick={() => viewInLogs(lambdaSpan)}
                    class="inline-flex items-center gap-1 text-[10px] font-mono text-destructive/70 hover:text-destructive transition-colors"
                  >
                    <ArrowUpRightIcon size={9} />
                    View in context
                  </button>
                </div>
              {/if}
            </div>
          {:else}
            <div class="px-3 py-8 text-center text-xs text-muted-foreground/70">
              No matching traces
            </div>
          {/each}
        </div>
      </div>

      <!-- ─── Trace detail panel ─── -->
      {#if selectedTrace}
        <div class="flex-1 min-w-0 space-y-3">
          <!-- Trace header card -->
          <div class="rounded-lg border border-border bg-card px-4 py-3">
            <div class="flex items-center gap-3 flex-wrap min-w-0">
              {#if selectedTrace.method}
                <span
                  class="shrink-0 font-mono text-[11px] px-1.5 py-0.5 rounded border border-border text-muted-foreground"
                  >{selectedTrace.method}</span
                >
              {/if}
              <span
                class="font-mono text-sm text-foreground truncate flex-1 min-w-0"
              >
                {selectedTrace.path ??
                  (selectedTrace.gatewayName
                    ? `via ${selectedTrace.gatewayName}`
                    : "Event trigger")}
              </span>
              <span
                class="shrink-0 inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-mono {statusBadgeClass(
                  selectedTrace.status,
                )}">{selectedTrace.status}</span
              >
              <span
                class="shrink-0 text-[11px] font-mono text-muted-foreground/70"
                >{formatMs(selectedTrace.durationMs)} total</span
              >
              <span
                class="shrink-0 text-[11px] font-mono text-muted-foreground/70"
                >{timeAgo(selectedTrace.startedAt)}</span
              >
            </div>
            {#if selectedTrace.spans.length > 0}
              <div
                class="mt-2 text-[10px] font-mono text-muted-foreground/70 flex items-center gap-1 flex-wrap"
              >
                {#each selectedTrace.spans as span, i (i)}
                  {#if i > 0}
                    <span class="opacity-40 select-none">→</span>
                  {/if}
                  <span style="color: {spanColor(span.kind)}"
                    >{spanKindLabel(span.kind)}</span
                  >
                {/each}
              </div>
            {/if}
          </div>

          {#if selectedTrace.spans.length > 0}
            {@const n = selectedTrace.spans.length}
            {@const cw = flowCW(n)}
            {@const rows = buildWaterfall(
              selectedTrace.spans,
              selectedTrace.durationMs,
            )}

            <!-- ─── Flow diagram ─── -->
            <div class="rounded-lg border border-border bg-card px-4 py-3">
              <p
                class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/70 mb-3"
              >
                Request Flow
              </p>
              <div class="overflow-x-auto">
                <svg
                  viewBox="0 0 {cw} {FLOW_CH}"
                  width={cw}
                  height={FLOW_CH}
                  style="display:block; min-height:{FLOW_CH}px"
                >
                  <!-- Dot grid background -->
                  {#each Array(Math.ceil(cw / 32)) as _, ix (ix)}
                    {#each Array(Math.ceil(FLOW_CH / 32)) as _, iy (iy)}
                      <circle
                        cx={16 + ix * 32}
                        cy={16 + iy * 32}
                        r="0.5"
                        class="fill-border"
                        opacity="0.35"
                      />
                    {/each}
                  {/each}

                  <!-- Arrow connectors between spans -->
                  {#each selectedTrace.spans as _span, i (i)}
                    {#if i < n - 1}
                      {@const fx = nodeX(i) + NODE_W / 2}
                      {@const tx = nodeX(i + 1) - NODE_W / 2}
                      <!-- Connector line -->
                      <line
                        x1={fx}
                        y1={nodeCY}
                        x2={tx - 7}
                        y2={nodeCY}
                        stroke="var(--color-text-faint)"
                        stroke-width="1.5"
                        stroke-dasharray="5 3"
                        stroke-linecap="round"
                        opacity="0.5"
                        class="connector-flow"
                      />
                      <!-- Arrowhead -->
                      <polygon
                        points="{tx},{nodeCY} {tx - 8},{nodeCY - 4.5} {tx -
                          8},{nodeCY + 4.5}"
                        fill="var(--color-text-faint)"
                        opacity="0.5"
                      />
                    {/if}
                  {/each}

                  <!-- Span nodes -->
                  {#each selectedTrace.spans as span, i (i)}
                    {@const cx = nodeX(i)}
                    {@const cy = nodeCY}
                    {@const color = spanColor(span.kind)}
                    {@const isDLQ = span.kind.toLowerCase() === "dlq"}
                    {@const hasErr =
                      span.status === "error" || span.status === "client_error"}

                    <!-- Error pulse ring -->
                    {#if hasErr}
                      <rect
                        x={cx - NODE_W / 2 - 4}
                        y={cy - NODE_H / 2 - 4}
                        width={NODE_W + 8}
                        height={NODE_H + 8}
                        rx="12"
                        fill="none"
                        stroke="var(--color-red)"
                        stroke-width="1.5"
                        stroke-dasharray="4 2"
                        opacity="0.55"
                      >
                        <animate
                          attributeName="opacity"
                          values="0.55;0.1;0.55"
                          dur="1.8s"
                          repeatCount="indefinite"
                        />
                      </rect>
                    {/if}

                    <!-- Node background -->
                    <rect
                      x={cx - NODE_W / 2}
                      y={cy - NODE_H / 2}
                      width={NODE_W}
                      height={NODE_H}
                      rx="8"
                      class="fill-bg-overlay"
                      stroke={hasErr ? "var(--color-red)" : color}
                      stroke-width={isDLQ || hasErr ? 1.8 : 1.2}
                      opacity="0.93"
                    />

                    <!-- DLQ danger tint -->
                    {#if isDLQ}
                      <rect
                        x={cx - NODE_W / 2}
                        y={cy - NODE_H / 2}
                        width={NODE_W}
                        height={NODE_H}
                        rx="8"
                        fill="var(--color-red)"
                        opacity="0.06"
                      />
                    {/if}

                    <!-- Top accent line (kind indicator) -->
                    <rect
                      x={cx - NODE_W / 2 + 8}
                      y={cy - NODE_H / 2}
                      width={NODE_W - 16}
                      height="2.5"
                      rx="1"
                      fill={color}
                      opacity="0.7"
                    />

                    <!-- Kind label -->
                    <text
                      x={cx}
                      y={cy - 12}
                      text-anchor="middle"
                      font-size="7.5"
                      font-family="var(--font-mono)"
                      fill={color}
                      >{spanKindLabel(span.kind).toUpperCase()}</text
                    >

                    <!-- Resource name -->
                    <text
                      x={cx}
                      y={cy + 2}
                      text-anchor="middle"
                      font-size="8.5"
                      font-family="var(--font-mono)"
                      class="fill-text"
                      >{span.name.length > 14
                        ? span.name.slice(0, 13) + "…"
                        : span.name}</text
                    >

                    <!-- Duration -->
                    <text
                      x={cx}
                      y={cy + 16}
                      text-anchor="middle"
                      font-size="7"
                      font-family="var(--font-mono)"
                      fill={hasErr
                        ? "var(--color-red)"
                        : "var(--color-text-faint)"}
                      >{formatMs(span.durationMs)}{hasErr ? " ✕" : ""}</text
                    >
                  {/each}
                </svg>
              </div>
            </div>

            <!-- ─── Waterfall timeline ─── -->
            <div class="rounded-lg border border-border bg-card px-4 py-3">
              <p
                class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/70 mb-3"
              >
                Timeline
              </p>
              <div class="space-y-1.5">
                {#each rows as { span, offsetPct, widthPct, nested }, i (i)}
                  <div class="flex items-center gap-2.5 {nested ? 'pl-4' : ''}">
                    <!-- Step / nesting indicator -->
                    {#if nested}
                      <span
                        class="w-4 text-[10px] font-mono text-muted-foreground/70 text-right shrink-0 select-none"
                        >└</span
                      >
                    {:else}
                      <span
                        class="w-4 text-[10px] font-mono text-muted-foreground/70 text-right shrink-0"
                        >{i + 1}</span
                      >
                    {/if}
                    <!-- Kind badge -->
                    <span
                      class="w-[4.5rem] text-right shrink-0 text-[10px] font-mono px-1.5 py-0.5 rounded"
                      style="color:{spanColor(span.kind)};background:{spanColor(
                        span.kind,
                      )}18;border:1px solid {spanColor(span.kind)}30"
                    >
                      {spanKindLabel(span.kind)}
                    </span>
                    <!-- Name -->
                    <span
                      class="w-28 text-[11px] font-mono text-muted-foreground truncate shrink-0"
                      >{span.name}</span
                    >
                    <!-- Bar track -->
                    <div
                      class="flex-1 {nested
                        ? 'h-3'
                        : 'h-4'} rounded bg-muted relative overflow-hidden"
                    >
                      <div
                        class="absolute top-0 h-full rounded"
                        style="left:{offsetPct}%;width:{widthPct}%;background:{spanColor(
                          span.kind,
                        )};opacity:{span.status === 'error'
                          ? 0.85
                          : nested
                            ? 0.45
                            : 0.55};min-width:2px"
                      ></div>
                    </div>
                    <!-- Duration -->
                    <span
                      class="w-12 text-right text-[10px] font-mono text-muted-foreground/70 shrink-0"
                      >{formatMs(span.durationMs)}</span
                    >
                  </div>
                {/each}
              </div>
              <!-- Ruler -->
              <div
                class="flex items-center justify-between mt-2 pt-1.5 border-t border-border text-[10px] font-mono text-muted-foreground/70"
              >
                <span>0ms</span>
                <span>{formatMs(Math.round(selectedTrace.durationMs / 2))}</span
                >
                <span>{formatMs(selectedTrace.durationMs)}</span>
              </div>
            </div>

            <!-- ─── Span detail cards ─── -->
            <div class="rounded-lg border border-border bg-card px-4 py-3">
              <p
                class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/70 mb-3"
              >
                Span Details
              </p>
              <div class="space-y-2">
                {#each selectedTrace.spans as span, i (i)}
                  {@const badge = spanBadge(span)}
                  {@const hasErr =
                    span.status === "error" || span.status === "client_error"}
                  <div
                    class="flex items-start gap-3 rounded-md border px-3 py-2.5 {hasErr
                      ? 'border-red/25 bg-red/5'
                      : 'border-border/60 bg-muted/50'}"
                  >
                    <!-- Step circle -->
                    <div
                      class="mt-0.5 h-5 w-5 rounded-full shrink-0 flex items-center justify-center text-[10px] font-mono"
                      style="color:{spanColor(span.kind)};background:{spanColor(
                        span.kind,
                      )}18;border:1px solid {spanColor(span.kind)}35"
                    >
                      {i + 1}
                    </div>
                    <div class="flex-1 min-w-0">
                      <div class="flex items-center gap-2 flex-wrap mb-0.5">
                        <span
                          class="text-[11px] font-semibold text-foreground truncate"
                          >{span.name}</span
                        >
                        <span
                          class="text-[10px] font-mono"
                          style="color:{spanColor(span.kind)}"
                          >{spanKindLabel(span.kind)}</span
                        >
                        <span class="text-[10px] font-mono {badge.colorClass}"
                          >{badge.label}</span
                        >
                        <span
                          class="ml-auto text-[10px] font-mono text-muted-foreground/70"
                          >{formatMs(span.durationMs)}</span
                        >
                        {#if hasErr && span.kind === "lambda"}
                          <button
                            type="button"
                            onclick={() => viewInLogs(span)}
                            class="inline-flex items-center gap-1 rounded border border-red/30 bg-red/8 px-1.5 py-0.5 text-[10px] font-mono text-destructive hover:bg-red/14 transition-colors shrink-0"
                          >
                            <ArrowUpRightIcon size={10} />
                            View in context
                          </button>
                        {/if}
                      </div>
                      {#if span.meta && Object.keys(span.meta).length > 0}
                        <div class="flex flex-wrap gap-x-3 gap-y-0.5 mt-1">
                          {#each Object.entries(span.meta) as [k, v] (k)}
                            <span
                              class="text-[10px] font-mono text-muted-foreground/70"
                            >
                              <span class="text-muted-foreground">{k}</span>={v}
                            </span>
                          {/each}
                        </div>
                      {/if}
                    </div>
                  </div>
                {/each}
              </div>
            </div>
          {:else}
            <div
              class="rounded-lg border border-border bg-card px-4 py-10 text-center"
            >
              <p class="text-xs text-muted-foreground/70">
                No span data available for this trace
              </p>
            </div>
          {/if}
        </div>
      {:else if filteredTraces.length === 0 && searchQuery}
        <div
          class="flex-1 rounded-lg border border-border bg-card px-4 py-12 text-center"
        >
          <p class="text-xs text-muted-foreground/70">
            No traces match "{searchQuery}"
          </p>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* Subtle flowing animation on connector arrows */
  .connector-flow {
    animation: dash-flow 2s linear infinite;
  }
  @keyframes dash-flow {
    from {
      stroke-dashoffset: 16;
    }
    to {
      stroke-dashoffset: 0;
    }
  }
</style>
