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
</script>

<div class="flex items-stretch border-b border-border py-4">
  <!-- Throughput -->
  <div class="flex flex-1 flex-col gap-0.5 border-r border-border px-5 first:pl-0 last:border-r-0 last:pr-0">
    <div
      class="flex items-baseline gap-1 font-light leading-none"
      style="font-size:20px;letter-spacing:-0.03em;color:var(--color-accent)"
    >
      {throughput || "—"}<span class="text-[11px] font-medium tracking-normal text-muted-foreground/50">req/min</span>
    </div>
    <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">Throughput</div>
    <div class="mt-1">
      <SparkBar bars={sparkThru} color="var(--color-accent)" currentOpacity={0.22} scaleWithHeight />
    </div>
  </div>

  <!-- Avg latency -->
  <div class="flex flex-1 flex-col gap-0.5 border-r border-border px-5 first:pl-0 last:border-r-0 last:pr-0">
    <div class="flex items-baseline gap-1 font-light leading-none" style="font-size:20px;letter-spacing:-0.03em">
      {avgLatency}<span class="text-[11px] font-medium tracking-normal text-muted-foreground/50">ms</span>
    </div>
    <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">Avg Latency</div>
    <div class="mt-1">
      <SparkBar bars={sparkLatency} color="var(--color-text-muted)" currentOpacity={0.15} filledOpacity={0.35} />
    </div>
  </div>

  <!-- p95 latency -->
  <div class="flex flex-1 flex-col gap-0.5 border-r border-border px-5 first:pl-0 last:border-r-0 last:pr-0">
    <div class="flex items-baseline gap-1 font-light leading-none" style="font-size:20px;letter-spacing:-0.03em">
      {p95Latency}<span class="text-[11px] font-medium tracking-normal text-muted-foreground/50">ms</span>
    </div>
    <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">p95 Latency</div>
    <div class="mt-1">
      <SparkBar bars={sparkP95} color="var(--color-text-muted)" currentOpacity={0.15} filledOpacity={0.3} />
    </div>
  </div>

  <!-- Errors -->
  <div class="flex flex-1 flex-col gap-0.5 border-r border-border px-5 first:pl-0 last:border-r-0 last:pr-0">
    <div
      class="flex items-baseline gap-1 font-light leading-none"
      style="font-size:20px;letter-spacing:-0.03em;color:{errorCount > 0 ? 'var(--color-red)' : 'inherit'}"
    >
      {errorCount}<span class="text-[11px] font-medium tracking-normal text-muted-foreground/50">errors</span>
    </div>
    <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">Last 5 min</div>
    <div class="mt-1">
      <SparkBar bars={sparkErrors} color="var(--color-red)" currentOpacity={0.2} filledOpacity={0.55} />
    </div>
  </div>

  <!-- Active services -->
  <div class="flex flex-1 flex-col gap-0.5 px-5 first:pl-0 last:border-r-0 last:pr-0">
    <div
      class="flex items-baseline gap-1 font-light leading-none"
      style="font-size:20px;letter-spacing:-0.03em;color:var(--color-accent)"
    >
      {activeServiceCount}<span class="text-[11px] font-medium tracking-normal text-muted-foreground/50">services</span>
    </div>
    <div class="text-[11px] tracking-[0.02em] text-muted-foreground/50">Active</div>
    {#if infraLegend.length > 0}
      <div class="mt-2 flex flex-wrap gap-x-3 gap-y-1">
        {#each infraLegend as item (item.kind)}
          <div class="flex items-center gap-1 text-[10px] text-muted-foreground/60">
            <span class="inline-block h-2.5 w-2.5 rounded-[2px]" style="background:{item.color};"></span>
            <span class="tabular-nums text-foreground/90">{item.count}</span>
            <span>{item.label}</span>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
