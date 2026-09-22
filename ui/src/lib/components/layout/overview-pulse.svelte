<script lang="ts">
  import SparkBar from "$lib/components/common/spark-bar.svelte";
  import type { SparkBar as SparkBarData } from "$lib/components/common/spark-bar.svelte";
  import type { RequestTrace } from "$lib/types";

  let {
    recentTraces,
    activeServiceCount,
    infraLegend,
  }: {
    recentTraces: RequestTrace[];
    activeServiceCount: number;
    infraLegend: { kind: string; label: string; count: number; color: string }[];
  } = $props();

  // ── Pulse metrics ─────────────────────────────────────────────
  const avgLatency = $derived(
    recentTraces.length > 0
      ? Math.round(recentTraces.reduce((s, t) => s + t.durationMs, 0) / recentTraces.length)
      : 0,
  );

  const p95Latency = $derived(
    (() => {
      if (!recentTraces.length) return 0;
      const sorted = [...recentTraces].map((t) => t.durationMs).sort((a, b) => a - b);
      return sorted[Math.min(Math.floor(sorted.length * 0.95), sorted.length - 1)];
    })(),
  );

  const errorCount = $derived(recentTraces.filter((t) => t.status >= 500).length);
  const hasTraceData = $derived(recentTraces.length > 0);

  const throughput = $derived(
    (() => {
      const cutoff = Date.now() - 60_000;
      return recentTraces.filter((t) => {
        const ms = new Date(t.startedAt).getTime();
        return !isNaN(ms) && ms >= cutoff;
      }).length;
    })(),
  );

  // ── Sparklines: 12 × 15-min buckets = 3 h rolling window ─────
  const BUCKET_N = 12;
  const BUCKET_MS = 15 * 60 * 1000;

  function fmtBucketLabel(startMs: number, endMs: number): string {
    const fmt = (ms: number) => {
      const d = new Date(ms);
      return `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
    };
    return `${fmt(startMs)}–${fmt(endMs)}`;
  }

  const traceBuckets = $derived(
    (() => {
      const now = Date.now();
      const windowStart = now - BUCKET_N * BUCKET_MS;
      const buckets: { traces: RequestTrace[]; startMs: number; endMs: number }[] =
        Array.from({ length: BUCKET_N }, (_, i) => ({
          traces: [],
          startMs: windowStart + i * BUCKET_MS,
          endMs: windowStart + (i + 1) * BUCKET_MS,
        }));
      for (const t of recentTraces) {
        const ms = new Date(t.startedAt).getTime();
        if (isNaN(ms) || ms < windowStart) continue;
        const idx = Math.min(Math.floor((ms - windowStart) / BUCKET_MS), BUCKET_N - 1);
        buckets[idx].traces.push(t);
      }
      return buckets;
    })(),
  );

  function normBars(values: number[], labels: string[]): SparkBarData[] {
    const max = Math.max(...values, 1);
    return values.map((v, i) => ({ h: Math.round((v / max) * 100), current: i === BUCKET_N - 1, label: labels[i] }));
  }

  const sparkThru = $derived(
    normBars(
      traceBuckets.map((b) => b.traces.length),
      traceBuckets.map((b) => `${b.traces.length} req · ${fmtBucketLabel(b.startMs, b.endMs)}`),
    ),
  );

  const sparkLatency = $derived(
    normBars(
      traceBuckets.map((b) => {
        if (!b.traces.length) return 0;
        return Math.round(b.traces.reduce((s, t) => s + t.durationMs, 0) / b.traces.length);
      }),
      traceBuckets.map((b) => {
        if (!b.traces.length) return `no requests · ${fmtBucketLabel(b.startMs, b.endMs)}`;
        const avg = Math.round(b.traces.reduce((s, t) => s + t.durationMs, 0) / b.traces.length);
        return `avg ${avg}ms · ${fmtBucketLabel(b.startMs, b.endMs)}`;
      }),
    ),
  );

  const sparkP95 = $derived(
    normBars(
      traceBuckets.map((b) => {
        if (!b.traces.length) return 0;
        const sorted = b.traces.map((t) => t.durationMs).sort((a, z) => a - z);
        return sorted[Math.min(Math.floor(sorted.length * 0.95), sorted.length - 1)];
      }),
      traceBuckets.map((b) => {
        if (!b.traces.length) return `no requests · ${fmtBucketLabel(b.startMs, b.endMs)}`;
        const sorted = b.traces.map((t) => t.durationMs).sort((a, z) => a - z);
        const p95 = sorted[Math.min(Math.floor(sorted.length * 0.95), sorted.length - 1)];
        return `p95 ${p95}ms · ${fmtBucketLabel(b.startMs, b.endMs)}`;
      }),
    ),
  );

  const sparkErrors = $derived(
    normBars(
      traceBuckets.map((b) => b.traces.filter((t) => t.status >= 500).length),
      traceBuckets.map((b) => {
        const n = b.traces.filter((t) => t.status >= 500).length;
        return n > 0
          ? `${n} error${n !== 1 ? "s" : ""} · ${fmtBucketLabel(b.startMs, b.endMs)}`
          : `no errors · ${fmtBucketLabel(b.startMs, b.endMs)}`;
      }),
    ),
  );

  const metricCards = $derived([
    {
      id: "throughput",
      label: "Throughput",
      value: hasTraceData ? throughput : "—",
      unit: hasTraceData ? "req/min" : "",
      helper: "last 60 seconds",
      bars: sparkThru,
      color: "var(--accent-green)",
      tone: "green",
      scaleWithHeight: true,
    },
    {
      id: "avg-latency",
      label: "Avg latency",
      value: hasTraceData ? avgLatency : "—",
      unit: hasTraceData ? "ms" : "",
      helper: "all recent traces",
      bars: sparkLatency,
      color: "var(--text-secondary)",
      tone: "neutral",
      scaleWithHeight: false,
    },
    {
      id: "p95-latency",
      label: "p95 latency",
      value: hasTraceData ? p95Latency : "—",
      unit: hasTraceData ? "ms" : "",
      helper: "all recent traces",
      bars: sparkP95,
      color: "var(--text-secondary)",
      tone: "neutral",
      scaleWithHeight: false,
    },
    {
      id: "errors",
      label: "Errors",
      value: hasTraceData ? errorCount : "—",
      unit: hasTraceData ? "errors" : "",
      helper: !hasTraceData
        ? "waiting for traces"
        : errorCount > 0
          ? "last 5 minutes · attention needed"
          : "last 5 minutes · no failures",
      bars: sparkErrors,
      color: "var(--accent-red)",
      tone: errorCount > 0 ? "red" : "neutral",
      scaleWithHeight: false,
    },
  ]);
</script>

<section class="overview-pulse" aria-label="Overview metrics">
  {#each metricCards as metric (metric.id)}
    <div class="metric" data-tone={metric.tone}>
      <div class="metric-label">{metric.label}</div>
      <div class="metric-value-row">
        <span class="metric-value">{metric.value}</span>
        {#if metric.unit}<span class="metric-unit">{metric.unit}</span>{/if}
      </div>
      <div class="metric-helper">{metric.helper}</div>
      <div class="metric-chart" role="img" aria-label={`${metric.label} trend`}>
        <SparkBar
          bars={metric.bars}
          color={metric.color}
          currentOpacity={metric.tone === "red" ? 0.2 : 0.18}
          filledOpacity={metric.tone === "red" ? 0.55 : 0.34}
          scaleWithHeight={metric.scaleWithHeight}
        />
      </div>
    </div>
  {/each}

  <div class="metric services-metric" data-tone="green">
    <div class="metric-label">Active services</div>
    <div class="metric-value-row">
      <span class="metric-value">{activeServiceCount}</span>
      <span class="metric-unit">services</span>
    </div>
    <div class="metric-helper">reachable now</div>
    <div class="service-legend" aria-label="Active service breakdown">
      {#each infraLegend as item (item.kind)}
        <div class="legend-item">
          <span class="legend-dot" style:background={item.color}></span>
          <span class="legend-count">{item.count}</span>
          <span>{item.label}</span>
        </div>
      {/each}
    </div>
  </div>
</section>

<style>
  .overview-pulse {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    min-width: 0;
    border-top: 1px solid var(--border-subtle);
    border-bottom: 1px solid var(--border-subtle);
  }

  .metric {
    min-width: 0;
    padding: 14px 16px 12px;
    border-right: 1px solid var(--border-subtle);
    transition: background 120ms ease;
  }

  .metric:first-child { padding-left: 0; }
  .metric:last-child { padding-right: 0; border-right: 0; }
  .metric:hover { background: var(--bg-element-hover); }

  .metric-label {
    overflow: hidden;
    color: var(--text-tertiary);
    font-size: 10.5px;
    letter-spacing: 0.06em;
    line-height: 1.2;
    text-overflow: ellipsis;
    text-transform: uppercase;
    white-space: nowrap;
  }

  .metric-value-row {
    display: flex;
    align-items: baseline;
    min-width: 0;
    gap: 5px;
    margin-top: 5px;
  }

  .metric-value {
    overflow: hidden;
    color: var(--text-primary);
    font-family: var(--font-ui-mono);
    font-size: 20px;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .metric[data-tone="green"] .metric-value { color: var(--accent-green); }
  .metric[data-tone="red"] .metric-value { color: var(--accent-red); }

  .metric-unit {
    overflow: hidden;
    color: var(--text-secondary);
    font-family: var(--font-ui-mono);
    font-size: 10px;
    font-variant-numeric: tabular-nums;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .metric-helper {
    overflow: hidden;
    margin-top: 4px;
    color: var(--text-tertiary);
    font-size: 10.5px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .metric-chart {
    min-height: 18px;
    margin-top: 9px;
  }

  .service-legend {
    display: flex;
    min-height: 18px;
    flex-wrap: wrap;
    align-content: flex-start;
    gap: 4px 10px;
    margin-top: 9px;
  }

  .legend-item {
    display: inline-flex;
    min-width: 0;
    align-items: center;
    gap: 4px;
    color: var(--text-tertiary);
    font-size: 10px;
    line-height: 1.2;
    white-space: nowrap;
  }

  .legend-dot {
    width: 7px;
    height: 7px;
    flex: 0 0 auto;
    border-radius: 2px;
  }

  .legend-count {
    color: var(--text-primary);
    font-family: var(--font-ui-mono);
    font-variant-numeric: tabular-nums;
  }

  @media (max-width: 900px) {
    .overview-pulse { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .metric { border-top: 1px solid var(--border-subtle); border-right: 0; }
    .metric:nth-child(-n + 2) { border-top: 0; }
    .metric:nth-child(odd) { border-right: 1px solid var(--border-subtle); }
    .metric:nth-child(5) { grid-column: 1 / -1; border-right: 0; }
    .metric:nth-child(2), .metric:nth-child(4), .metric:nth-child(5) { padding-right: 0; }
    .metric:nth-child(3), .metric:nth-child(5) { padding-left: 0; }
  }

  @media (max-width: 540px) {
    .overview-pulse { grid-template-columns: minmax(0, 1fr); }
    .metric,
    .metric:first-child,
    .metric:last-child { padding-right: 0; padding-left: 0; border-right: 0; }
    .metric:first-child { border-top: 0; }
    .metric:nth-child(5) { grid-column: auto; }
  }

  @media (prefers-reduced-motion: reduce) {
    .metric { transition: none; }
  }
</style>
