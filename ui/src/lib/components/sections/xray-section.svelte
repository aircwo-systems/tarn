<script lang="ts">
  import { DetectiveIcon, ArrowUpRightIcon } from "phosphor-svelte";
  import { runEventBridgeRace } from "$lib/api";
  import { getDashboard, refresh } from "$lib/state.svelte";
  import type { RequestTrace, TraceSpan } from "$lib/types";
  import SectionHeader from "./section-header.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const traces = $derived(dashboard.data?.recentTraces ?? []);
  const eventBridgeRules = $derived(dashboard.data?.eventBridgeRules ?? []);

  interface TraceFlow extends RequestTrace {
    rawTraces: RequestTrace[];
    traceCount: number;
  }

  let selectedTraceId = $state<string | null>(null);
  let searchQuery = $state("");
  let raceRuleName = $state("");
  let raceRuns = $state(20);
  let raceConcurrency = $state(4);
  let raceRunning = $state(false);
  let raceSessionFilter = $state("");
  let raceMessage = $state("");

  const raceSessions = $derived(
    [...new Set(traces.map((trace) => traceRaceSession(trace)).filter(Boolean))] as string[],
  );

  const traceFlows = $derived(chainTraceFlows(traces));

  $effect(() => {
    if (!raceRuleName && eventBridgeRules.length > 0) {
      raceRuleName = eventBridgeRules[0].name;
    }
    if (raceRuleName && !eventBridgeRules.some((rule) => rule.name === raceRuleName)) {
      raceRuleName = eventBridgeRules[0]?.name ?? "";
    }
  });

  $effect(() => {
    if (raceSessionFilter && !raceSessions.includes(raceSessionFilter)) {
      raceSessionFilter = "";
    }
  });

  const filteredTraces = $derived(
    traceFlows.filter((trace) => {
      if (raceSessionFilter && traceRaceSession(trace) !== raceSessionFilter) {
        return false;
      }
      const query = searchQuery.trim().toLowerCase();
      if (!query) return true;
      return (
        (trace.path ?? "").toLowerCase().includes(query) ||
        (trace.method ?? "").toLowerCase().includes(query) ||
        (trace.gatewayName ?? "").toLowerCase().includes(query) ||
        trace.id.toLowerCase().includes(query) ||
        traceRaceSession(trace).toLowerCase().includes(query) ||
        trace.spans.some(
          (span) =>
            span.name.toLowerCase().includes(query) ||
            span.kind.toLowerCase().includes(query),
        )
      );
    }),
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

  const errorCount = $derived(traceFlows.filter((t) => t.status >= 500).length);
  const clientErrorCount = $derived(
    traceFlows.filter((t) => t.status >= 400 && t.status < 500).length,
  );
  const avgMs = $derived(
    traceFlows.length > 0
      ? Math.round(traceFlows.reduce((s, t) => s + t.durationMs, 0) / traceFlows.length)
      : 0,
  );
  const p95Ms = $derived(
    traceFlows.length >= 5
      ? [...traceFlows].sort((a, b) => a.durationMs - b.durationMs)[
          Math.floor(traceFlows.length * 0.95)
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
      case "eventbridge":
        return "var(--color-blue)";
      case "queue":
        return "var(--color-amber)";
      case "topic":
        return "var(--color-primary)";
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
      case "eventbridge":
        return "EventBridge";
      case "queue":
        return "SQS";
      case "topic":
        return "SNS";
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
    const eventBridge = t.spans.find((s) => s.kind === "eventbridge");
    if (eventBridge) return `EventBridge → ${eventBridge.name}`;
    if (t.method && t.path) return `${t.method} ${t.path}`;
    const s3 = t.spans.find((s) => s.kind === "s3");
    if (s3) return `S3 → ${s3.name}`;
    const topic = t.spans.find((s) => s.kind === "topic");
    if (topic) return `SNS → ${topic.name}`;
    const q = t.spans.find((s) => s.kind === "queue" || s.kind === "dlq");
    if (q) return `ESM → ${q.name}`;
    const fn = t.spans.find((s) => s.kind === "lambda");
    if (fn) return `Invoke: ${fn.name}`;
    return `trace:${t.id.slice(0, 8)}`;
  }

  function traceRaceSession(trace: RequestTrace): string {
    return (
      trace.spans.find((span) => span.kind === "eventbridge")?.meta?.raceSession ?? ""
    );
  }

  const SUB_SPAN_KINDS = new Set([
    "postgres",
    "postgresql",
    "mysql",
    "redis",
    "cache_extension",
    "cache-extension",
    "secret",
    "secrets",
  ]);

  function normalizeTraceToken(value: string): string {
    return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "");
  }

  function splitTraceTokens(value: string): string[] {
    return value
      .split(/[^a-zA-Z0-9]+/)
      .map((part) => normalizeTraceToken(part))
      .filter((part) => part.length >= 3);
  }

  function traceCoreSpans(trace: RequestTrace): TraceSpan[] {
    return trace.spans.filter((span) => !SUB_SPAN_KINDS.has(span.kind.toLowerCase()));
  }

  function traceEntrySpan(trace: RequestTrace): TraceSpan | undefined {
    return traceCoreSpans(trace)[0] ?? trace.spans[0];
  }

  function traceTerminalSpan(trace: RequestTrace): TraceSpan | undefined {
    const core = traceCoreSpans(trace);
    return core[core.length - 1] ?? trace.spans[trace.spans.length - 1];
  }

  function tracePathTokens(trace: RequestTrace): string[] {
    return splitTraceTokens(trace.path ?? "");
  }

  function traceNameTokens(trace: RequestTrace): Set<string> {
    const tokens = new Set<string>();
    if (trace.gatewayName) {
      for (const token of splitTraceTokens(trace.gatewayName)) tokens.add(token);
    }
    for (const token of tracePathTokens(trace)) tokens.add(token);
    for (const span of traceCoreSpans(trace)) {
      for (const token of splitTraceTokens(span.name)) tokens.add(token);
    }
    return tokens;
  }

  function worstTraceStatus(a: number, b: number): number {
    return Math.max(a, b);
  }

  function worstSpanStatus(a: TraceSpan["status"], b: TraceSpan["status"]): TraceSpan["status"] {
    const rank = (status: string) =>
      status === "error" ? 3 : status === "client_error" ? 2 : status === "ok" ? 1 : 0;
    return rank(a) >= rank(b) ? a : b;
  }

  function compactTraceSpans(spans: TraceSpan[]): TraceSpan[] {
    const compacted: TraceSpan[] = [];
    for (const span of spans) {
      const previous = compacted[compacted.length - 1];
      if (previous && previous.kind === span.kind && previous.name === span.name) {
        previous.durationMs = Math.max(previous.durationMs, span.durationMs);
        previous.status = worstSpanStatus(previous.status, span.status);
        if (span.meta) previous.meta = { ...(previous.meta ?? {}), ...span.meta };
        continue;
      }
      compacted.push({
        ...span,
        meta: span.meta ? { ...span.meta } : undefined,
      });
    }
    return compacted;
  }

  function toTraceFlow(trace: RequestTrace): TraceFlow {
    return {
      ...trace,
      correlationId: trace.correlationId ?? trace.id,
      spans: trace.spans.map((span) => ({
        ...span,
        meta: span.meta ? { ...span.meta } : undefined,
      })),
      rawTraces: [trace],
      traceCount: 1,
    };
  }

  function appendTraceFlow(flow: TraceFlow, next: RequestTrace): TraceFlow {
    const flowStart = new Date(flow.startedAt).getTime();
    const flowEnd = Math.max(
      ...flow.rawTraces.map((trace) => new Date(trace.startedAt).getTime() + trace.durationMs),
    );
    const nextEnd = new Date(next.startedAt).getTime() + next.durationMs;

    return {
      ...flow,
      correlationId: flow.correlationId ?? next.correlationId ?? flow.id,
      durationMs: Math.max(flowEnd, nextEnd) - flowStart,
      status: worstTraceStatus(flow.status, next.status),
      spans: compactTraceSpans([...flow.spans, ...next.spans]),
      rawTraces: [...flow.rawTraces, next],
      traceCount: flow.traceCount + 1,
    };
  }

  function shouldChainTrace(flow: TraceFlow, candidate: RequestTrace): boolean {
    if (flow.correlationId && candidate.correlationId && flow.correlationId === candidate.correlationId) {
      return true;
    }

    const candidateStart = new Date(candidate.startedAt).getTime();
    const flowStart = new Date(flow.startedAt).getTime();
    const flowEnd = Math.max(
      ...flow.rawTraces.map((trace) => new Date(trace.startedAt).getTime() + trace.durationMs),
    );

    if (candidateStart < flowStart - 250 || candidateStart - flowEnd > 5000) {
      return false;
    }

    const flowRace = traceRaceSession(flow);
    if (flowRace && flowRace === traceRaceSession(candidate)) {
      return true;
    }

    const flowTerminal = traceTerminalSpan(flow);
    const candidateEntry = traceEntrySpan(candidate);
    if (!candidateEntry) return false;

    const flowTokens = traceNameTokens(flow);
    const candidateTokens = new Set<string>([
      ...splitTraceTokens(candidateEntry.name),
      ...tracePathTokens(candidate),
    ]);

    if (flowTerminal) {
      for (const token of splitTraceTokens(flowTerminal.name)) {
        flowTokens.add(token);
      }
    }

    const flowLambda = [...traceCoreSpans(flow)].reverse().find((span) => span.kind === "lambda");
    const candidateLambda = traceCoreSpans(candidate).find((span) => span.kind === "lambda");
    if (
      flowLambda &&
      candidateLambda &&
      flowLambda.name === candidateLambda.name &&
      !candidate.method &&
      !candidate.path &&
      !["queue", "dlq", "topic", "eventbridge", "s3"].includes(
        candidateEntry.kind.toLowerCase(),
      )
    ) {
      return true;
    }

    for (const token of candidateTokens) {
      if (flowTokens.has(token)) return true;
    }

    return false;
  }

  function chainTraceFlows(input: RequestTrace[]): TraceFlow[] {
    const sorted = [...input].sort(
      (a, b) => new Date(a.startedAt).getTime() - new Date(b.startedAt).getTime(),
    );
    const used = new Set<string>();
    const flows: TraceFlow[] = [];

    for (const trace of sorted) {
      if (used.has(trace.id)) continue;

      let flow = toTraceFlow(trace);
      used.add(trace.id);

      let appended = true;
      while (appended) {
        appended = false;
        for (const candidate of sorted) {
          if (used.has(candidate.id)) continue;
          if (!shouldChainTrace(flow, candidate)) continue;
          used.add(candidate.id);
          flow = appendTraceFlow(flow, candidate);
          appended = true;
          break;
        }
      }

      flows.push(flow);
    }

    return flows.sort(
      (a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime(),
    );
  }

  async function launchRace() {
    if (!raceRuleName) {
      raceMessage = "Pick an EventBridge rule first.";
      return;
    }
    raceRunning = true;
    raceMessage = "";
    try {
      const result = await runEventBridgeRace(
        raceRuleName,
        Math.max(1, Math.min(500, raceRuns)),
        Math.max(1, Math.min(100, raceConcurrency)),
      );
      raceSessionFilter = result.sessionId;
      raceMessage = `Race ${result.sessionId}: ${result.successful}/${result.runs} successful`;
      await refresh();
    } catch (err) {
      raceMessage = err instanceof Error ? err.message : "Race run failed";
    } finally {
      raceRunning = false;
    }
  }

  function viewInLogs(span: TraceSpan, traceStartedAt?: string) {
    const group = span.kind === "lambda" ? `/aws/lambda/${span.name}` : null;
    if (group) {
      let hash = `logs?group=${encodeURIComponent(group)}`;
      if (traceStartedAt) {
        hash += `&ts=${encodeURIComponent(traceStartedAt)}`;
      }
      window.location.hash = hash;
    }
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
  <div class="space-y-3">
    <SectionHeader
      title="X-Ray traces"
      description="End-to-end request flow visualiser."
      icon={DetectiveIcon}
      {sidebarCollapsed}
      {onToggleSidebar}
    >
      {#snippet stats()}
        <span class="text-muted-foreground/70"
          >{traceFlows.length} flow{traceFlows.length !== 1 ? "s" : ""}</span
        >
        {#if traceFlows.length !== traces.length}
          <span class="text-muted-foreground/55">from {traces.length} traces</span>
        {/if}
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
      {/snippet}
    </SectionHeader>

    <div class="flex flex-wrap items-center gap-2">
      <select
        bind:value={raceRuleName}
        class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary min-w-48"
      >
        <option value="">Select EventBridge rule</option>
        {#each eventBridgeRules as rule (rule.name)}
          <option value={rule.name}>{rule.name}</option>
        {/each}
      </select>
      <input
        type="number"
        min="1"
        max="500"
        bind:value={raceRuns}
        class="h-8 w-24 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
        title="Runs"
      />
      <input
        type="number"
        min="1"
        max="100"
        bind:value={raceConcurrency}
        class="h-8 w-24 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
        title="Concurrency"
      />
      <button
        type="button"
        disabled={raceRunning || !raceRuleName}
        onclick={launchRace}
        class="inline-flex items-center gap-1 rounded border border-primary/50 bg-primary/10 px-2 py-1 text-xs text-primary transition-colors hover:bg-primary/20 disabled:opacity-50"
      >
        {raceRunning ? "Running race..." : "Run race"}
      </button>
      <select
        bind:value={raceSessionFilter}
        class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary min-w-40"
      >
        <option value="">All sessions</option>
        {#each raceSessions as session (session)}
          <option value={session}>{session}</option>
        {/each}
      </select>
    </div>
    {#if raceMessage}
      <p class="text-xs text-muted-foreground">{raceMessage}</p>
    {/if}
  </div>

  {#if traces.length === 0}
    <!-- Empty state -->
    <div
      class="flex flex-col items-center gap-4 px-8 py-16"
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
            style="background:var(--color-primary);opacity:0.6"
          ></span>
          SNS
        </span>
        <span class="flex items-center gap-1.5">
          <span
            class="h-2 w-2 rounded-sm inline-block"
            style="background:var(--color-blue);opacity:0.6"
          ></span>
          EventBridge
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
    <div class="flex items-start">
      <!-- ─── Trace list ─── -->
      <div
        class="w-72 shrink-0 border-r border-border overflow-hidden flex flex-col"
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
                  {#if trace.traceCount > 1}
                    <span>·</span>
                    <span>{trace.traceCount} hops</span>
                  {/if}
                  <span class="ml-auto">{timeAgo(trace.startedAt)}</span>
                </div>
                {#if traceRaceSession(trace)}
                  <div class="pl-3.5 mt-1">
                    <span
                      class="inline-flex items-center rounded border border-blue/30 bg-blue/10 px-1.5 py-0.5 text-[10px] font-mono text-blue"
                    >
                      race {traceRaceSession(trace)}
                    </span>
                  </div>
                {/if}
                {#if trace.traceCount > 1}
                  <div class="pl-3.5 mt-1">
                    <span
                      class="inline-flex items-center rounded border border-primary/20 bg-primary/8 px-1.5 py-0.5 text-[10px] font-mono text-primary/85"
                    >
                      chained flow
                    </span>
                  </div>
                {/if}
              </button>
              {#if trace.status >= 500 && lambdaSpan}
                <div class="px-3 pb-2">
                  <button
                    type="button"
                    onclick={() => viewInLogs(lambdaSpan, trace.startedAt)}
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
        <div class="flex-1 min-w-0 space-y-5 pl-4">
          <!-- Trace header -->
          <div class="pb-3 border-b border-border/40">
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
              {#if traceRaceSession(selectedTrace)}
                <span
                  class="shrink-0 inline-flex items-center rounded border border-blue/30 bg-blue/10 px-2 py-0.5 text-[10px] font-mono text-blue"
                >
                  race {traceRaceSession(selectedTrace)}
                </span>
              {/if}
              {#if selectedTrace.traceCount > 1}
                <span
                  class="shrink-0 inline-flex items-center rounded border border-primary/20 bg-primary/8 px-2 py-0.5 text-[10px] font-mono text-primary/85"
                >
                  chained {selectedTrace.traceCount} traces
                </span>
              {/if}
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
            <div>
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
            <div>
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

            <!-- ─── Span details ─── -->
            <div>
              <p
                class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/70 mb-3"
              >
                Span Details
              </p>
              <div class="divide-y divide-border/50">
                {#each selectedTrace.spans as span, i (i)}
                  {@const badge = spanBadge(span)}
                  {@const hasErr =
                    span.status === "error" || span.status === "client_error"}
                  <div
                    class="flex items-start gap-3 py-2.5 {hasErr
                      ? 'bg-red/5'
                      : ''}"
                  >
                    <!-- Step number -->
                    <span
                      class="mt-0.5 shrink-0 text-[10px] font-mono w-4 text-right"
                      style="color:{spanColor(span.kind)}"
                    >
                      {i + 1}
                    </span>
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
                            onclick={() => viewInLogs(span, selectedTrace?.startedAt)}
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
            <div class="py-10 text-center">
              <p class="text-xs text-muted-foreground/70">
                No span data available for this trace
              </p>
            </div>
          {/if}
        </div>
      {:else if filteredTraces.length === 0 && searchQuery}
        <div class="flex-1 pl-4 py-12 text-center">
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
