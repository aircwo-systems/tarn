<script lang="ts">
  import type { RequestTrace } from "$lib/types";

  let {
    traces = [],
    activeServiceCount = 0,
  }: {
    traces?: RequestTrace[];
    activeServiceCount?: number;
  } = $props();

  const BUCKETS = 12;

  function timeBucketed(
    sorted: RequestTrace[],
    extractor: (t: RequestTrace) => number,
    reduce: "sum" | "avg" = "avg",
  ): Array<{ h: number; v: number; t: number }> {
    const now = Date.now();
    if (!sorted.length) {
      return Array.from({ length: BUCKETS }, (_, i) => ({
        h: 0, v: 0,
        t: now - ((BUCKETS - i - 0.5) / BUCKETS) * 300_000,
      }));
    }
    const minT = new Date(sorted[0].startedAt).getTime();
    const maxT = new Date(sorted[sorted.length - 1].startedAt).getTime();
    const span = maxT - minT || 1;

    const sums = Array(BUCKETS).fill(0);
    const cnts = Array(BUCKETS).fill(0);

    for (const trace of sorted) {
      const ti = new Date(trace.startedAt).getTime();
      const b = Math.min(Math.floor(((ti - minT) / span) * BUCKETS), BUCKETS - 1);
      sums[b] += extractor(trace);
      cnts[b]++;
    }

    const vals =
      reduce === "avg"
        ? sums.map((s, i) => (cnts[i] > 0 ? s / cnts[i] : 0))
        : sums;
    const max = Math.max(...vals, 1);
    return vals.map((v, i) => ({
      h: Math.round((v / max) * 100),
      v: Math.round(v),
      t: minT + ((i + 0.5) / BUCKETS) * span,
    }));
  }

  function bucketAgo(ts: number): string {
    const diff = Date.now() - ts;
    if (diff < 60_000) return `${Math.floor(diff / 1_000)}s ago`;
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
    return `${Math.floor(diff / 3_600_000)}h ago`;
  }

  const sorted = $derived(
    [...traces].sort(
      (a, b) =>
        new Date(a.startedAt).getTime() - new Date(b.startedAt).getTime(),
    ),
  );

  const fiveMinAgo = $derived(Date.now() - 5 * 60 * 1_000);
  const oneMinAgo = $derived(Date.now() - 60 * 1_000);

  const recent5 = $derived(
    sorted.filter((t) => new Date(t.startedAt).getTime() > fiveMinAgo),
  );
  const recent1 = $derived(
    sorted.filter((t) => new Date(t.startedAt).getTime() > oneMinAgo),
  );

  const avgLatency = $derived(
    traces.length
      ? Math.round(
          traces.reduce((s, t) => s + t.durationMs, 0) / traces.length,
        )
      : null,
  );

  function computeP95(ts: RequestTrace[]): number | null {
    if (!ts.length) return null;
    const ds = [...ts].map((t) => t.durationMs).sort((a, b) => a - b);
    return ds[Math.min(Math.ceil(0.95 * ds.length) - 1, ds.length - 1)];
  }

  const p95Latency = $derived(computeP95(traces));

  const throughput = $derived(recent1.length > 0 ? recent1.length : null);
  const errorCount = $derived(
    recent5.length > 0
      ? recent5.filter((t) => t.status >= 500).length
      : null,
  );

  const throughputBars = $derived(
    timeBucketed(sorted, () => 1, "sum"),
  );
  const avgLatBars = $derived(
    timeBucketed(sorted, (t) => t.durationMs, "avg"),
  );
  const p95LatBars = $derived(
    timeBucketed(
      sorted,
      (t) => t.durationMs,
      "avg", // bucket max approximation for small samples
    ),
  );
  const errorBars = $derived(
    timeBucketed(sorted, (t) => (t.status >= 500 ? 1 : 0), "sum"),
  );

  const hasData = $derived(traces.length > 0);
</script>

<div class="flex items-stretch border-b border-border pb-4 pt-2">
  <!-- Throughput -->
  <div class="flex min-w-0 flex-1 flex-col gap-0.5 pr-5">
    <div class="flex items-baseline gap-1">
      <span class="font-mono text-xl font-light leading-tight tracking-tight text-primary">
        {hasData ? (throughput ?? 0) : "—"}
      </span>
      {#if hasData}
        <span class="font-mono text-[11px] text-muted-foreground">req/min</span>
      {/if}
    </div>
    <div class="font-mono text-[11px] text-muted-foreground/70">Throughput</div>
    <div class="mt-1 flex h-[18px] items-end gap-px">
      {#each throughputBars as { h, v, t }}
        <div class="group relative min-w-[2px] flex-1 h-full flex items-end">
          <span class="pointer-events-none absolute bottom-full mb-1 left-1/2 -translate-x-1/2 rounded border border-border bg-popover px-1.5 py-0.5 font-mono text-[9px] leading-none text-foreground whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity z-20">
            {v} req · {bucketAgo(t)}
          </span>
          <div class="w-full rounded-t-[1px]" style={`height: ${Math.max(h, 2)}%; background: var(--color-primary); opacity: ${(0.2 + (h / 100) * 0.4).toFixed(2)}`}></div>
        </div>
      {/each}
    </div>
  </div>

  <!-- Avg Latency -->
  <div class="flex min-w-0 flex-1 flex-col gap-0.5 border-l border-border px-5">
    <div class="flex items-baseline gap-1">
      <span class="font-mono text-xl font-light leading-tight tracking-tight text-foreground">
        {hasData && avgLatency !== null ? avgLatency : "—"}
      </span>
      {#if hasData && avgLatency !== null}
        <span class="font-mono text-[11px] text-muted-foreground">ms</span>
      {/if}
    </div>
    <div class="font-mono text-[11px] text-muted-foreground/70">Avg Latency</div>
    <div class="mt-1 flex h-[18px] items-end gap-px">
      {#each avgLatBars as { h, v, t }}
        <div class="group relative min-w-[2px] flex-1 h-full flex items-end">
          <span class="pointer-events-none absolute bottom-full mb-1 left-1/2 -translate-x-1/2 rounded border border-border bg-popover px-1.5 py-0.5 font-mono text-[9px] leading-none text-foreground whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity z-20">
            {v}ms avg · {bucketAgo(t)}
          </span>
          <div class="w-full rounded-t-[1px]" style={`height: ${Math.max(h, 2)}%; background: var(--color-muted-foreground); opacity: ${(0.15 + (h / 100) * 0.3).toFixed(2)}`}></div>
        </div>
      {/each}
    </div>
  </div>

  <!-- p95 Latency -->
  <div class="flex min-w-0 flex-1 flex-col gap-0.5 border-l border-border px-5">
    <div class="flex items-baseline gap-1">
      <span class="font-mono text-xl font-light leading-tight tracking-tight text-foreground">
        {hasData && p95Latency !== null ? p95Latency : "—"}
      </span>
      {#if hasData && p95Latency !== null}
        <span class="font-mono text-[11px] text-muted-foreground">ms</span>
      {/if}
    </div>
    <div class="font-mono text-[11px] text-muted-foreground/70">p95 Latency</div>
    <div class="mt-1 flex h-[18px] items-end gap-px">
      {#each p95LatBars as { h, v, t }}
        <div class="group relative min-w-[2px] flex-1 h-full flex items-end">
          <span class="pointer-events-none absolute bottom-full mb-1 left-1/2 -translate-x-1/2 rounded border border-border bg-popover px-1.5 py-0.5 font-mono text-[9px] leading-none text-foreground whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity z-20">
            {v}ms p95 · {bucketAgo(t)}
          </span>
          <div class="w-full rounded-t-[1px]" style={`height: ${Math.max(h, 2)}%; background: var(--color-muted-foreground); opacity: ${(0.15 + (h / 100) * 0.3).toFixed(2)}`}></div>
        </div>
      {/each}
    </div>
  </div>

  <!-- Errors -->
  <div class="flex min-w-0 flex-1 flex-col gap-0.5 border-l border-border px-5">
    <div class="flex items-baseline gap-1">
      <span
        class={`font-mono text-xl font-light leading-tight tracking-tight ${
          errorCount !== null && errorCount > 0
            ? "text-destructive"
            : "text-foreground"
        }`}
      >
        {hasData && errorCount !== null ? errorCount : "—"}
      </span>
      {#if hasData && errorCount !== null}
        <span class="font-mono text-[11px] text-muted-foreground">errors</span>
      {/if}
    </div>
    <div class="font-mono text-[11px] text-muted-foreground/70">Last 5 min</div>
    <div class="mt-1 flex h-[18px] items-end gap-px">
      {#each errorBars as { h, v, t }}
        <div class="group relative min-w-[2px] flex-1 h-full flex items-end">
          <span class="pointer-events-none absolute bottom-full mb-1 left-1/2 -translate-x-1/2 rounded border border-border bg-popover px-1.5 py-0.5 font-mono text-[9px] leading-none text-foreground whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity z-20">
            {v} errors · {bucketAgo(t)}
          </span>
          <div class="w-full rounded-t-[1px]" style={`height: ${Math.max(h, 2)}%; background: var(--color-destructive); opacity: ${h > 0 ? 0.55 : 0.15}`}></div>
        </div>
      {/each}
    </div>
  </div>

  <!-- Active services -->
  <div class="flex min-w-0 flex-1 flex-col gap-0.5 border-l border-border pl-5">
    <div class="flex items-baseline gap-1">
      <span class="font-mono text-xl font-light leading-tight tracking-tight text-primary">
        {activeServiceCount > 0 ? activeServiceCount : "—"}
      </span>
      {#if activeServiceCount > 0}
        <span class="font-mono text-[11px] text-muted-foreground">services</span>
      {/if}
    </div>
    <div class="font-mono text-[11px] text-muted-foreground/70">Active</div>
  </div>
</div>
