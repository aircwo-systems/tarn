<script lang="ts">
  import { DetectiveIcon, ArrowUpRightIcon, FlaskIcon, CaretRightIcon, SidebarSimpleIcon } from "phosphor-svelte";
  import { fade, slide } from "svelte/transition";
  import { runEventBridgeRace } from "$lib/api";
  import { getDashboard, refresh } from "$lib/state.svelte";
  import type { RequestTrace, TraceSpan } from "$lib/types";
  import {
    SUB_SPAN_KINDS,
    spanColor,
    spanKindLabel,
    formatMs,
    buildWaterfall,
    traceTitle,
    type WaterfallRow,
  } from "$lib/trace-utils";
  import SectionHeader from "./section-header.svelte";

  let {
    initialTraceId = "",
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    initialTraceId?: string;
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
  // Open span in the details list (also highlighted in the timeline).
  let openSpan = $state<number | null>(null);
  $effect(() => {
    selectedTraceId;
    openSpan = null;
  });

  function focusSpan(i: number) {
    openSpan = i;
    requestAnimationFrame(() =>
      document.getElementById(`span-row-${i}`)?.scrollIntoView({ block: "nearest", behavior: "smooth" }),
    );
  }
  let searchQuery = $state("");
  let raceRuleName = $state("");
  let raceRuns = $state(20);
  let raceRunning = $state(false);
  let raceSessionFilter = $state("");
  let raceMessage = $state("");
  let racePanelOpen = $state(false);

  // ─── Trace list column: resizable / collapsible ───
  const LIST_MIN = 220;
  const LIST_MAX = 440;
  const LIST_DEFAULT = 288;
  const LIST_COLLAPSE_THRESHOLD = 140;
  let traceListWidth = $state(LIST_DEFAULT);
  let traceListCollapsed = $state(false);
  let listResizing = $state(false);
  let releaseToCollapseList = $state(false);
  let listHandleY = $state<number | null>(null);
  let listDragMoved = false;
  let listDragStartX = 0;
  let listDragStartWidth = 0;

  function startListResize(e: PointerEvent) {
    if (e.button !== 0) return;
    e.preventDefault();
    listResizing = true;
    releaseToCollapseList = false;
    listDragMoved = false;
    listDragStartX = e.clientX;
    listDragStartWidth = traceListCollapsed ? 0 : traceListWidth;
    document.body.classList.add("is-resizing");
    window.addEventListener("pointermove", onListResizeMove);
    window.addEventListener("pointerup", stopListResize);
  }

  function onListResizeMove(e: PointerEvent) {
    const next = listDragStartWidth + (e.clientX - listDragStartX);
    if (Math.abs(e.clientX - listDragStartX) > 3) listDragMoved = true;
    releaseToCollapseList = next < LIST_COLLAPSE_THRESHOLD;
    if (releaseToCollapseList) return;
    if (traceListCollapsed) traceListCollapsed = false;
    traceListWidth = Math.round(Math.max(LIST_MIN, Math.min(LIST_MAX, next)));
  }

  function stopListResize() {
    listResizing = false;
    document.body.classList.remove("is-resizing");
    window.removeEventListener("pointermove", onListResizeMove);
    window.removeEventListener("pointerup", stopListResize);
    if (releaseToCollapseList) {
      traceListCollapsed = true;
    } else if (!listDragMoved) {
      traceListCollapsed = !traceListCollapsed;
    }
    releaseToCollapseList = false;
  }

  function trackListHandle(e: PointerEvent) {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    listHandleY = Math.max(28, Math.min(rect.height - 28, e.clientY - rect.top));
  }

  function resetListWidth() {
    traceListWidth = LIST_DEFAULT;
    traceListCollapsed = false;
  }

  $effect(() => {
    return () => {
      window.removeEventListener("pointermove", onListResizeMove);
      window.removeEventListener("pointerup", stopListResize);
      document.body.classList.remove("is-resizing");
    };
  });

  const RACE_PRESETS = [10, 20, 50];

  const raceSessions = $derived(
    [...new Set(traces.map((trace) => traceRaceSession(trace)).filter(Boolean))] as string[],
  );

  const traceFlows = $derived(chainTraceFlows(traces));

  interface RaceSessionSummary {
    session: string;
    count: number;
    successCount: number;
    p95Ms: number;
  }

  const raceSessionSummaries = $derived(
    raceSessions
      .map((session): RaceSessionSummary => {
        const sessionFlows = traceFlows.filter((flow) => traceRaceSession(flow) === session);
        const durations = sessionFlows.map((flow) => flow.durationMs).sort((a, b) => a - b);
        const p95 =
          durations.length > 0
            ? durations[Math.min(durations.length - 1, Math.floor(durations.length * 0.95))]
            : 0;
        return {
          session,
          count: sessionFlows.length,
          successCount: sessionFlows.filter((flow) => flow.status < 400).length,
          p95Ms: p95,
        };
      })
      .sort((a, b) => b.count - a.count),
  );

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

  $effect(() => {
    if (!initialTraceId) return;
    const matched =
      traceFlows.find((trace) => trace.id === initialTraceId) ??
      traceFlows.find((trace) =>
        trace.rawTraces.some((rawTrace) => rawTrace.id === initialTraceId),
      );
    if (matched) {
      selectedTraceId = matched.id;
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


  function timeAgo(iso: string): string {
    const diff = Date.now() - new Date(iso).getTime();
    if (diff < 2000) return "just now";
    if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`;
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
    return new Date(iso).toLocaleTimeString();
  }

  function traceRaceSession(trace: RequestTrace): string {
    return (
      trace.spans.find((span) => span.kind === "eventbridge")?.meta?.raceSession ?? ""
    );
  }


  function traceCoreSpans(trace: RequestTrace): TraceSpan[] {
    return trace.spans.filter((span) => !SUB_SPAN_KINDS.has(span.kind.toLowerCase()));
  }

  function traceExternalSpan(trace: RequestTrace): TraceSpan | null {
    return trace.spans.find((span) => span.kind.toLowerCase() === "external") ?? null;
  }

  function traceSourceLabel(trace: RequestTrace): string {
    return traceExternalSpan(trace)?.name?.trim() ?? "";
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

  function queueReceiveCount(trace: RequestTrace): number {
    const queueSpan = trace.spans.find((span) => span.kind.toLowerCase() === "queue");
    const raw = queueSpan?.meta?.receiveCount ?? "";
    const parsed = Number.parseInt(raw, 10);
    return Number.isFinite(parsed) ? parsed : 0;
  }

  function queueRetryAttempt(trace: RequestTrace): boolean {
    return queueReceiveCount(trace) > 1;
  }

  const COMPUTE_SPAN_KINDS = ["lambda", "ecs"];

  // Returns "" when no compute span is present, so the retry-dedup check below can decline
  // to merge traces it has no compute identity to compare (see sameQueueLambdaAttempt).
  // The trace list's *display* name comes from traceTitle (trace-utils.ts), which already
  // has its own kind-priority fallback chain and an "ecs" branch.
  function traceComputeName(trace: RequestTrace): string {
    for (const kind of COMPUTE_SPAN_KINDS) {
      const match = trace.spans.find((span) => span.kind.toLowerCase() === kind);
      if (match) return match.name;
    }
    return "";
  }

  function sameQueueLambdaAttempt(a: RequestTrace, b: RequestTrace): boolean {
    const aQueue = a.spans.find((span) => span.kind.toLowerCase() === "queue")?.name ?? "";
    const bQueue = b.spans.find((span) => span.kind.toLowerCase() === "queue")?.name ?? "";
    if (!aQueue || !bQueue || aQueue !== bQueue) return false;
    const aCompute = traceComputeName(a);
    const bCompute = traceComputeName(b);
    return !!aCompute && aCompute === bCompute;
  }

  function isRepeatedQueueRetry(flow: TraceFlow, candidate: RequestTrace): boolean {
    if (!queueRetryAttempt(candidate)) return false;
    return flow.rawTraces.some((trace) => sameQueueLambdaAttempt(trace, candidate));
  }

  function shouldChainTrace(flow: TraceFlow, candidate: RequestTrace): boolean {
    if (flow.correlationId && candidate.correlationId && flow.correlationId === candidate.correlationId) {
      if (isRepeatedQueueRetry(flow, candidate)) {
        return false;
      }
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

    const flowTerminal = traceCoreSpans(flow).at(-1) ?? flow.spans.at(-1);
    const candidateEntry = traceCoreSpans(candidate)[0] ?? candidate.spans[0];
    if (!flowTerminal || !candidateEntry) {
      return false;
    }

    const normalizeKind = (value: string) => value.trim().toLowerCase();
    const flowKind = normalizeKind(flowTerminal.kind);
    const candidateKind = normalizeKind(candidateEntry.kind);
    const infraKinds = new Set(["queue", "dlq", "topic", "eventbridge", "s3", "dynamodb"]);

    if (
      infraKinds.has(flowKind) &&
      flowKind === candidateKind &&
      flowTerminal.name === candidateEntry.name
    ) {
      return true;
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
      const runs = Math.max(1, Math.min(500, raceRuns));
      const result = await runEventBridgeRace(
        raceRuleName,
        runs,
        Math.max(1, Math.min(100, Math.ceil(runs / 5))),
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

  function logGroupForSpan(span: TraceSpan): string | null {
    switch (span.kind.toLowerCase()) {
      case "lambda":
        return `/aws/lambda/${span.name}`;
      case "ecs":
        // Log group defaults to the task definition family, not the ECS
        // service/target name span.name holds — they only match by
        // coincidence (see resolveLogGroup in internal/ecs/runner.go).
        return `/ecs/${span.meta?.taskDefinitionFamily || span.name}`;
      default:
        return null;
    }
  }

  function viewInLogs(span: TraceSpan, traceStartedAt?: string) {
    const group = logGroupForSpan(span);
    if (group) {
      let hash = `logs?group=${encodeURIComponent(group)}`;
      if (traceStartedAt) {
        hash += `&ts=${encodeURIComponent(traceStartedAt)}`;
      }
      window.location.hash = hash;
    }
  }

  function traceAccentColor(s: number): string {
    return s >= 500 ? "var(--accent-red)" : s >= 400 ? "var(--accent-amber)" : "var(--text-primary)";
  }

  function statusBadgeClass(s: number): string {
    if (s >= 500) return "status-error";
    if (s >= 400) return "status-warn";
    return "status-ok";
  }

  function spanBadge(span: TraceSpan): { label: string; colorClass: string } {
    if (span.status === "error")
      return { label: "✕ error", colorClass: "tx-error" };
    if (span.status === "client_error")
      return { label: "⚠ warn", colorClass: "tx-warn" };
    return { label: "✓ ok", colorClass: "tx-ok" };
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
</script>

<div class="xray space-y-4">
  <!-- Header strip -->
  <div class="space-y-3">
    <SectionHeader
      title="Traces"
      description="End-to-end request flow visualiser."
      {sidebarCollapsed}
      {onToggleSidebar}
    >
      {#snippet actions()}
        <div class="flex flex-wrap items-center gap-4 font-mono text-[11px] tx-tertiary">
        <span class="tx-tertiary"
          >{traceFlows.length} flow{traceFlows.length !== 1 ? "s" : ""}</span
        >
        {#if traceFlows.length !== traces.length}
          <span class="tx-tertiary opacity-70">from {traces.length} traces</span>
        {/if}
        {#if errorCount > 0}
          <span class="tx-error">{errorCount} 5xx</span>
        {/if}
        {#if clientErrorCount > 0}
          <span class="tx-warn">{clientErrorCount} 4xx</span>
        {/if}
        {#if traces.length > 0}
          <span class="tx-tertiary">avg {formatMs(avgMs)}</span>
        {/if}
        {#if p95Ms > 0}
          <span class="tx-tertiary">p95 {formatMs(p95Ms)}</span>
        {/if}
        <button
          type="button"
          class="pill pill-btn race-toggle"
          class:is-active={racePanelOpen}
          onclick={() => (racePanelOpen = !racePanelOpen)}
        >
          <FlaskIcon size={11} />
          Race
        </button>
        </div>
      {/snippet}
    </SectionHeader>

    {#if racePanelOpen}
      <div transition:slide={{ duration: 200 }}>
        <div class="race-bar">
          <select bind:value={raceRuleName} class="field min-w-48">
            <option value="">Select EventBridge rule</option>
            {#each eventBridgeRules as rule (rule.name)}
              <option value={rule.name}>{rule.name}</option>
            {/each}
          </select>
          <div class="segmented" role="group" aria-label="Run count">
            {#each RACE_PRESETS as preset (preset)}
              <button
                type="button"
                class="segmented-opt"
                class:is-active={raceRuns === preset}
                onclick={() => (raceRuns = preset)}
              >
                ×{preset}
              </button>
            {/each}
          </div>
          <button
            type="button"
            disabled={raceRunning || !raceRuleName}
            onclick={launchRace}
            class="pill pill-btn accent"
          >
            {raceRunning ? "Running race..." : "Run race"}
          </button>
        </div>
        {#if raceMessage}
          <p class="text-[11px] tx-tertiary mt-2">{raceMessage}</p>
        {/if}
        {#if raceSessionSummaries.length > 0}
          <div class="race-sessions">
            {#each raceSessionSummaries as summary (summary.session)}
              {@const active = raceSessionFilter === summary.session}
              <button
                type="button"
                class="race-session-item"
                class:is-active={active}
                onclick={() => (raceSessionFilter = active ? "" : summary.session)}
              >
                <span class="mono race-session-id">{summary.session}</span>
                <span class="race-session-stat">{summary.successCount}/{summary.count} ok</span>
                <span class="race-session-stat tx-tertiary">p95 {formatMs(summary.p95Ms)}</span>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>

  {#if traces.length === 0}
    <!-- Empty state -->
    <div class="empty-state panel">
      <div class="empty-icon">
        <DetectiveIcon size={22} weight="regular" />
      </div>
      <div class="text-center space-y-1.5">
        <p class="text-[13px] font-semibold tx-secondary">
          No traces recorded yet
        </p>
        <p class="text-[11.5px] tx-tertiary max-w-sm leading-relaxed">
          Make HTTP requests through an API Gateway or trigger SQS event source
          mappings. Traces will appear here showing the full request flow
          through each component.
        </p>
      </div>
      <div class="empty-legend">
        <span class="legend-item" style="color:var(--color-red)">API Gateway</span>
        <span class="legend-item" style="color:var(--color-accent)">Lambda</span>
        <span class="legend-item" style="color:var(--color-amber)">SQS / DLQ</span>
        <span class="legend-item" style="color:var(--color-primary)">SNS</span>
        <span class="legend-item" style="color:var(--color-blue)">EventBridge</span>
        <span class="legend-item" style="color:var(--color-blue)">Secrets / DB</span>
      </div>
    </div>
  {:else}
    <div class="trace-split">
      <!-- ─── Trace list ─── -->
      <div
        class="trace-list-col"
        class:collapsed={traceListCollapsed}
        class:no-transition={listResizing}
        style:width={traceListCollapsed ? "0px" : `${traceListWidth}px`}
      >
        <div class="trace-search">
          <input
            type="text"
            bind:value={searchQuery}
            placeholder="Filter by path, method..."
            class="field w-full"
          />
        </div>
        <div class="trace-list">
          {#each filteredTraces as trace (trace.id)}
            {@const logSpan = trace.spans.find((s) => logGroupForSpan(s) !== null)}
            {@const isActive = effectiveId === trace.id}
            <div class="trace-item" class:is-active={isActive} style="--accent:{traceAccentColor(trace.status)}">
              <button
                type="button"
                class="trace-row"
                onclick={() => (selectedTraceId = trace.id)}
              >
                <div class="flex items-center gap-2 mb-1 min-w-0">
                  <span
                    class="text-[11px] font-mono tx-primary truncate flex-1"
                    >{traceTitle(trace)}</span
                  >
                </div>
                <div class="flex items-center gap-1.5 text-[10px] font-mono tx-tertiary">
                  <span
                    class={trace.status >= 500
                      ? "tx-error"
                      : trace.status >= 400
                        ? "tx-warn"
                        : "tx-tertiary"}>{trace.status}</span
                  >
                  <span>·</span>
                  <span>{formatMs(trace.durationMs)}</span>
                  {#if trace.traceCount > 1}
                    <span>·</span>
                    <span>{trace.traceCount} hops</span>
                  {/if}
                  {#if traceSourceLabel(trace)}
                    <span>·</span>
                    <span>from {traceSourceLabel(trace)}</span>
                  {/if}
                  <span class="ml-auto">{timeAgo(trace.startedAt)}</span>
                </div>
                {#if traceRaceSession(trace)}
                  <div class="mt-1">
                    <span class="pill pill-blue">race {traceRaceSession(trace)}</span>
                  </div>
                {/if}
                {#if trace.traceCount > 1}
                  <div class="mt-1">
                    <span class="pill pill-accent">chained flow</span>
                  </div>
                {/if}
              </button>
              {#if trace.status >= 500 && logSpan}
                <div class="trace-context">
                  <button
                    type="button"
                    onclick={() => viewInLogs(logSpan, trace.startedAt)}
                    class="context-link"
                  >
                    <ArrowUpRightIcon size={9} />
                    View in context
                  </button>
                </div>
              {/if}
            </div>
          {:else}
            <div class="px-3 py-8 text-center text-[11px] tx-tertiary">
              No matching traces
            </div>
          {/each}
        </div>
      </div>

      <!-- ─── List resizer / collapse handle ─── -->
      {#if !traceListCollapsed}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="list-resizer"
        class:resizing={listResizing}
        style:left="{traceListWidth - 3}px"
        title="Drag to resize trace list (click to collapse, double-click to reset)"
        onpointerdown={startListResize}
        onpointermove={trackListHandle}
        ondblclick={resetListWidth}
      >
        <div class="resizer-line"></div>
        <div class="resizer-pill-handle" style:top={listHandleY === null ? undefined : `${listHandleY}px`}></div>
        {#if listResizing}
          <div
            class="resizer-width-badge"
            class:collapse-hint={releaseToCollapseList}
            style:top={listHandleY === null ? undefined : `${listHandleY}px`}
          >
            {releaseToCollapseList ? "Release to collapse" : `${traceListWidth}px`}
          </div>
        {/if}
      </div>
      {/if}

      <!-- ─── Trace detail panel ─── -->
      {#if selectedTrace}
        <div class="flex-1 min-w-0 space-y-5" class:pl-4={!traceListCollapsed}>
          <!-- Trace header -->
          <div class="pb-3 border-b bd-subtle">
            <div class="flex items-center gap-3 flex-wrap min-w-0">
              {#if traceListCollapsed}
                <button
                  type="button"
                  class="list-open-btn"
                  onclick={() => (traceListCollapsed = false)}
                  title="Show trace list"
                  aria-label="Show trace list"
                  in:fade={{ duration: 160 }}
                >
                  <SidebarSimpleIcon size={14} />
                </button>
              {/if}
              {#if selectedTrace.method}
                <span class="pill mono">{selectedTrace.method}</span>
              {/if}
              <span
                class="font-mono text-[13px] tx-primary truncate flex-1 min-w-0"
              >
                {selectedTrace.path ??
                  (selectedTrace.gatewayName
                    ? `via ${selectedTrace.gatewayName}`
                    : "Event trigger")}
              </span>
              {#if traceRaceSession(selectedTrace)}
                <span class="pill pill-blue shrink-0">race {traceRaceSession(selectedTrace)}</span>
              {/if}
              {#if selectedTrace.traceCount > 1}
                <span class="pill pill-accent shrink-0">chained {selectedTrace.traceCount} traces</span>
              {/if}
              {#if traceSourceLabel(selectedTrace)}
                <span class="pill shrink-0">from {traceSourceLabel(selectedTrace)}</span>
              {/if}
              <span class="pill status-pill {statusBadgeClass(selectedTrace.status)} shrink-0"
                >{selectedTrace.status}</span
              >
              <span class="shrink-0 text-[11px] font-mono tx-tertiary"
                >{formatMs(selectedTrace.durationMs)} total</span
              >
              <span class="shrink-0 text-[11px] font-mono tx-tertiary"
                >{timeAgo(selectedTrace.startedAt)}</span
              >
            </div>
            {#if selectedTrace.spans.length > 0}
              <div
                class="mt-2 text-[10px] font-mono tx-tertiary flex items-center gap-1 flex-wrap"
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
            <div class="panel">
              <p class="section-label">
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
            <div class="flat-block">
              <p class="section-label">Timeline</p>
              <div class="tl-rows">
                {#each rows as { span, offsetPct, widthPct, nested }, i (i)}
                  {@const hasErr = span.status === "error" || span.status === "client_error"}
                  <button
                    type="button"
                    class="tl-row"
                    class:selected={openSpan === i}
                    class:nested
                    style="--accent:{hasErr ? (span.status === 'error' ? 'var(--accent-red)' : 'var(--accent-amber)') : 'var(--text-primary)'}"
                    onclick={() => (openSpan === i ? (openSpan = null) : focusSpan(i))}
                  >
                    <span class="tl-step">{nested ? "└" : i + 1}</span>
                    <span class="tl-kind" title={spanKindLabel(span.kind)} style="color:{spanColor(span.kind)}">
                      {spanKindLabel(span.kind)}
                    </span>
                    <span class="tl-name" class:tx-error={span.status === "error"} title={span.name}>{span.name}</span>
                    <span class="tl-track">
                      <span
                        class="tl-bar"
                        style="left:{offsetPct}%;width:{widthPct}%;background:{spanColor(span.kind)};opacity:{hasErr ? 0.85 : nested ? 0.45 : 0.6}"
                      ></span>
                    </span>
                    <span class="tl-dur">{formatMs(span.durationMs)}</span>
                  </button>
                {/each}
              </div>
              <div class="tl-ruler">
                <span>0ms</span>
                <span>{formatMs(Math.round(selectedTrace.durationMs / 2))}</span>
                <span>{formatMs(selectedTrace.durationMs)}</span>
              </div>
            </div>

            <!-- ─── Span details ─── -->
            <div class="flat-block">
              <p class="section-label">Span Details</p>
              <div class="span-rows">
                {#each selectedTrace.spans as span, i (i)}
                  {@const badge = spanBadge(span)}
                  {@const hasErr = span.status === "error" || span.status === "client_error"}
                  {@const hasMeta = !!span.meta && Object.keys(span.meta).length > 0}
                  <div
                    id="span-row-{i}"
                    class="span-row"
                    class:open={openSpan === i}
                    class:err={hasErr}
                    style="--accent:{hasErr ? (span.status === 'error' ? 'var(--accent-red)' : 'var(--accent-amber)') : 'var(--text-primary)'}"
                  >
                    <div class="span-head">
                      <button
                        type="button"
                        class="span-toggle"
                        aria-expanded={openSpan === i}
                        onclick={() => (openSpan = openSpan === i ? null : i)}
                      >
                        <CaretRightIcon size={9} class="span-caret" />
                        <span class="span-step">{i + 1}</span>
                        <span class="span-name">{span.name}</span>
                        <span class="span-kind" style="color:{spanColor(span.kind)}">{spanKindLabel(span.kind)}</span>
                        <span class="span-badge {badge.colorClass}">{badge.label}</span>
                        <span class="span-dur">{formatMs(span.durationMs)}</span>
                      </button>
                      {#if hasErr && logGroupForSpan(span)}
                        <button
                          type="button"
                          onclick={() => viewInLogs(span, selectedTrace?.startedAt)}
                          class="context-link context-link--badge shrink-0"
                        >
                          <ArrowUpRightIcon size={10} />
                          View in context
                        </button>
                      {/if}
                    </div>
                    {#if openSpan === i}
                      <div class="span-meta" transition:slide={{ duration: 200 }}>
                        {#if hasMeta}
                          {#each Object.entries(span.meta ?? {}) as [k, v] (k)}
                            <div class="meta-line">
                              <span class="meta-k">{k}</span>
                              <span class="meta-v">{v === "" ? "—" : v}</span>
                            </div>
                          {/each}
                        {:else}
                          <div class="meta-line"><span class="meta-v">No attributes</span></div>
                        {/if}
                      </div>
                    {/if}
                  </div>
                {/each}
              </div>
            </div>
          {:else}
            <div class="py-10 text-center">
              <p class="text-[11px] tx-tertiary">
                No span data available for this trace
              </p>
            </div>
          {/if}
        </div>
      {:else if filteredTraces.length === 0 && searchQuery}
        <div class="flex-1 pl-4 py-12 text-center">
          <p class="text-[11px] tx-tertiary">
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

  /* ─── Tokens ─── */
  .xray :global(.tx-primary) { color: var(--text-primary); }
  .xray :global(.tx-secondary) { color: var(--text-secondary); }
  .xray :global(.tx-tertiary) { color: var(--text-tertiary); }
  .xray :global(.tx-error) { color: var(--accent-red); }
  .xray :global(.tx-warn) { color: var(--accent-amber); }
  .xray :global(.tx-ok) { color: var(--accent-green); }
  .xray :global(.bd-subtle) { border-color: var(--border-subtle); }
  .xray :global(.bg-el) { background: var(--bg-element); }
  .xray :global(.mono) { font-family: var(--font-mono, ui-monospace, monospace); }

  /* ─── Panels ─── */
  .panel {
    border: 1px solid var(--border-subtle);
    border-radius: 12px;
    background: var(--bg-stage);
    padding: 14px 16px;
    animation: panelIn 320ms var(--ease-snappy) both;
  }
  @keyframes panelIn {
    from { opacity: 0; transform: translateY(6px); }
  }
  .section-label {
    font-size: 10px;
    font-family: var(--font-mono, ui-monospace, monospace);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-tertiary);
    margin-bottom: 12px;
  }

  /* ─── Race controls ─── */
  .race-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .field {
    appearance: none;
    -webkit-appearance: none;
    height: 30px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-app);
    color: var(--text-primary);
    font-size: 12px;
    outline: none;
    transition: border-color 120ms ease, background 120ms ease;
  }
  select.field {
    appearance: none;
    -webkit-appearance: none;
    padding-right: 26px;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6' viewBox='0 0 10 6' fill='none'%3E%3Cpath d='M1 1L5 5L9 1' stroke='%23888' stroke-width='1.4' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 10px center;
  }
  .field::placeholder { color: var(--text-tertiary); }
  .field:hover { border-color: var(--border-default); }
  .field:focus { border-color: var(--border-focus); }

  /* ─── Segmented control ─── */
  .segmented {
    display: inline-flex;
    height: 30px;
    padding: 2px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-app);
    gap: 2px;
  }
  .segmented-opt {
    cursor: pointer;
    padding: 0 10px;
    border-radius: 6px;
    font-size: 11px;
    font-family: var(--font-mono, ui-monospace, monospace);
    color: var(--text-tertiary);
    background: transparent;
    transition: color 120ms ease, background 120ms ease;
  }
  .segmented-opt:hover { color: var(--text-secondary); background: var(--bg-element-hover); }
  .segmented-opt.is-active { color: var(--text-primary); background: var(--bg-element); }

  /* ─── Race session summary strip ─── */
  .race-sessions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 2px;
  }
  .race-session-item {
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 26px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--border-subtle);
    background: transparent;
    font-size: 11px;
    color: var(--text-secondary);
    transition: border-color 120ms ease, background 120ms ease, color 120ms ease;
  }
  .race-session-item:hover { border-color: var(--border-default); background: var(--bg-element-hover); }
  .race-session-item.is-active {
    border-color: var(--border-default);
    background: var(--bg-element);
    color: var(--text-primary);
  }
  .race-session-id { color: var(--text-primary); }
  .race-session-stat { color: var(--text-tertiary); }
  .race-session-item.is-active .race-session-stat { color: var(--text-secondary); }

  /* ─── Pills / buttons ─── */
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 22px;
    padding: 0 9px;
    border-radius: 8px;
    font-size: 11px;
    color: var(--text-secondary);
    border: 1px solid var(--border-subtle);
    white-space: nowrap;
    background: transparent;
  }
  .pill-btn {
    cursor: pointer;
    height: 26px;
    padding: 0 12px;
    transition: color 120ms ease, background 120ms ease, border-color 120ms ease, transform 120ms ease;
  }
  .pill-btn:hover { color: var(--text-primary); border-color: var(--border-default); background: var(--bg-element-hover); }
  .pill-btn:active { transform: scale(0.96); }
  .pill-btn:disabled { opacity: 0.5; cursor: not-allowed; transform: none; }
  .pill-btn.accent {
    color: var(--accent-green);
    border-color: color-mix(in srgb, var(--accent-green) 45%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }
  .pill-btn.accent:hover { background: color-mix(in srgb, var(--accent-green) 16%, transparent); }
  .race-toggle.is-active {
    color: var(--text-primary);
    border-color: var(--border-default);
    background: var(--bg-element);
  }
  .pill-accent {
    color: var(--accent-green);
    border-color: color-mix(in srgb, var(--accent-green) 35%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }
  .pill-blue {
    color: var(--color-blue, #60a5fa);
    border-color: color-mix(in srgb, var(--color-blue, #60a5fa) 35%, transparent);
    background: color-mix(in srgb, var(--color-blue, #60a5fa) 10%, transparent);
  }
  .status-pill { border-radius: 8px; padding: 0 10px; }
  .status-pill.status-error {
    color: var(--accent-red);
    border-color: color-mix(in srgb, var(--accent-red) 40%, transparent);
    background: color-mix(in srgb, var(--accent-red) 8%, transparent);
  }
  .status-pill.status-warn {
    color: var(--accent-amber);
    border-color: color-mix(in srgb, var(--accent-amber) 40%, transparent);
    background: color-mix(in srgb, var(--accent-amber) 8%, transparent);
  }
  .status-pill.status-ok {
    color: var(--accent-green);
    border-color: color-mix(in srgb, var(--accent-green) 50%, transparent);
    background: color-mix(in srgb, var(--accent-green) 10%, transparent);
  }

  :global(body.is-resizing) {
    cursor: col-resize !important;
    user-select: none !important;
    -webkit-user-select: none !important;
  }

  /* ─── Trace list ─── */
  .trace-split {
    position: relative;
    display: flex;
    align-items: flex-start;
  }
  .trace-list-col {
    flex-shrink: 0;
    border-right: 1px solid var(--border-subtle);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    transition: width 180ms var(--ease-snappy), opacity 180ms var(--ease-snappy);
  }
  .trace-list-col.no-transition { transition: none; }
  .trace-list-col.collapsed {
    border-right: none;
    opacity: 0;
    pointer-events: none;
  }

  /* ─── List resizer / collapse handle ─── */
  .list-resizer {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 22px;
    margin-left: -11px;
    cursor: col-resize;
    z-index: 10;
    touch-action: none;
    transition: left 180ms var(--ease-snappy);
  }
  .list-resizer.resizing { transition: none; }
  .list-resizer .resizer-line {
    position: absolute;
    top: 8px;
    bottom: 8px;
    left: 14px;
    width: 1px;
    background: transparent;
    pointer-events: none;
    transition: background 140ms ease;
  }
  .list-resizer:hover .resizer-line { background: var(--border-default); }
  .list-resizer.resizing .resizer-line { background: var(--text-tertiary); }
  .resizer-pill-handle {
    position: absolute;
    left: 7px;
    top: 50%;
    transform: translateY(-50%) scale(0.95);
    width: 4px;
    height: 44px;
    border-radius: 9999px;
    background: var(--text-tertiary);
    opacity: 0;
    pointer-events: none;
    transition: opacity 140ms ease, transform 140ms ease, background 100ms ease;
  }
  .list-resizer:hover .resizer-pill-handle,
  .list-resizer.resizing .resizer-pill-handle {
    opacity: 1;
    transform: translateY(-50%) scale(1);
  }
  .list-resizer:hover .resizer-pill-handle { background: var(--text-secondary); }
  .list-resizer.resizing .resizer-pill-handle { background: var(--text-primary); }
  .list-open-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    flex-shrink: 0;
    border-radius: 8px;
    color: var(--text-tertiary);
    transition: color 120ms ease, background 120ms ease, transform 120ms ease;
  }
  .list-open-btn:hover { color: var(--text-primary); background: var(--bg-element-hover); }
  .list-open-btn:active { transform: scale(0.96); }
  .resizer-width-badge {
    position: absolute;
    left: 24px;
    top: 50%;
    transform: translateY(-50%);
    background: var(--bg-element);
    border: 1px solid var(--border-default);
    color: var(--text-primary);
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 10.5px;
    padding: 2px 7px;
    border-radius: 6px;
    pointer-events: none;
    white-space: nowrap;
    z-index: 10;
  }
  .resizer-width-badge.collapse-hint { color: var(--accent-red); }
  .trace-search {
    padding: 10px;
  }
  .trace-list {
    overflow-y: auto;
    max-height: calc(100vh - 18rem);
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .trace-item {
    position: relative;
    border-radius: 8px;
    border: 1px solid transparent;
    transition: background 120ms ease, border-color 120ms ease;
  }
  .trace-item:hover { background: var(--bg-element-hover); }
  .trace-item.is-active {
    background: var(--bg-element);
    border-color: var(--border-default);
  }
  .trace-item.is-active::before {
    content: "";
    position: absolute;
    left: 6px;
    top: 8px;
    bottom: 8px;
    width: 2.5px;
    border-radius: 2px;
    background: var(--accent, var(--text-primary));
    opacity: 0.9;
  }
  .trace-row {
    display: block;
    width: 100%;
    text-align: left;
    padding: 8px 10px 8px 12px;
    border-radius: 8px;
    transition: padding-left 200ms var(--ease-snappy);
  }
  .trace-item.is-active .trace-row { padding-left: 20px; }
  .trace-context { padding: 0 12px 8px; }
  .context-link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 10px;
    font-family: var(--font-mono, ui-monospace, monospace);
    color: color-mix(in srgb, var(--accent-red) 70%, var(--text-tertiary));
    transition: color 120ms ease;
  }
  .context-link:hover { color: var(--accent-red); }
  .context-link--badge {
    border: 1px solid color-mix(in srgb, var(--accent-red) 30%, transparent);
    background: color-mix(in srgb, var(--accent-red) 8%, transparent);
    border-radius: 8px;
    padding: 2px 6px;
    color: var(--accent-red);
  }
  .context-link--badge:hover { background: color-mix(in srgb, var(--accent-red) 14%, transparent); }

  /* ─── Flat blocks (log-style, no card) ─── */
  .flat-block { animation: panelIn 320ms var(--ease-snappy) both; }
  .flat-block + .flat-block { animation-delay: 40ms; }

  /* ─── Timeline rows ─── */
  .tl-rows { display: flex; flex-direction: column; gap: 1px; margin: 0 -8px; }
  .tl-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: 28px;
    padding: 0 10px 0 8px;
    border-radius: 8px;
    border: 1px solid transparent;
    text-align: left;
    transition: background 120ms ease, border-color 120ms ease, padding-left 200ms var(--ease-snappy);
  }
  .tl-row:hover { background: var(--bg-element-hover); }
  .tl-row.selected { background: var(--bg-element); border-color: var(--border-default); padding-left: 16px; }
  .tl-row.selected::before,
  .span-row.open::before {
    content: "";
    position: absolute;
    left: 5px;
    top: 6px;
    bottom: 6px;
    width: 2.5px;
    border-radius: 2px;
    background: var(--accent);
  }
  .tl-step { width: 16px; flex-shrink: 0; text-align: right; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .tl-row.nested .tl-step { padding-left: 4px; }
  .tl-kind {
    width: 96px; flex-shrink: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font: 10px var(--font-mono, ui-monospace, monospace);
  }
  .tl-name {
    width: 160px; flex-shrink: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font: 11px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary);
  }
  .tl-track { position: relative; flex: 1; height: 6px; border-radius: 3px; background: var(--bg-element); overflow: hidden; }
  .tl-row.nested .tl-track { height: 4px; }
  .tl-row:hover .tl-track, .tl-row.selected .tl-track { background: var(--bg-element-hover); }
  .tl-bar { position: absolute; top: 0; height: 100%; min-width: 2px; border-radius: 3px; }
  .tl-dur { width: 48px; flex-shrink: 0; text-align: right; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .tl-ruler {
    display: flex; justify-content: space-between;
    margin-top: 6px; padding: 0 2px 0 calc(16px + 96px + 160px + 36px);
    font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary);
  }

  /* ─── Span rows ─── */
  .span-rows { display: flex; flex-direction: column; gap: 2px; margin: 0 -8px; }
  .span-row {
    position: relative;
    border-radius: 8px;
    border: 1px solid transparent;
    transition: background 120ms ease, border-color 120ms ease;
  }
  .span-row:hover { background: var(--bg-element-hover); }
  .span-row.err { background: color-mix(in srgb, var(--accent) 5%, transparent); }
  .span-row.err:hover { background: color-mix(in srgb, var(--accent) 9%, transparent); }
  .span-row.open { background: var(--bg-element); border-color: var(--border-default); }
  .span-row.err.open {
    background: color-mix(in srgb, var(--accent) 7%, var(--bg-element));
    border-color: color-mix(in srgb, var(--accent) 35%, transparent);
  }
  .span-head { display: flex; align-items: center; gap: 8px; padding-right: 8px; }
  .span-toggle {
    display: flex; align-items: center; gap: 8px;
    flex: 1; min-width: 0; height: 32px;
    padding-left: 8px;
    text-align: left;
    transition: padding-left 200ms var(--ease-snappy);
  }
  .span-row.open .span-toggle { padding-left: 16px; }
  .span-toggle :global(.span-caret) {
    flex-shrink: 0; color: var(--text-tertiary);
    transition: transform 200ms var(--ease-snappy);
  }
  .span-row.open .span-toggle :global(.span-caret) { transform: rotate(90deg); }
  .span-step { width: 14px; flex-shrink: 0; text-align: right; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .span-name { font-size: 11px; font-weight: 600; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .span-kind, .span-badge { flex-shrink: 0; font: 10px var(--font-mono, ui-monospace, monospace); white-space: nowrap; }
  .span-dur { margin-left: auto; flex-shrink: 0; font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); }
  .span-meta {
    display: grid;
    grid-template-columns: max-content 1fr;
    column-gap: 16px;
    row-gap: 3px;
    padding: 2px 12px 12px 55px;
  }
  .meta-line { display: contents; }
  .meta-k { font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-secondary); }
  .meta-v { font: 10px var(--font-mono, ui-monospace, monospace); color: var(--text-tertiary); word-break: break-all; }
  .context-link--badge { height: 22px; }

  /* ─── Empty state ─── */
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
    padding: 56px 32px;
  }
  .empty-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 44px;
    width: 44px;
    border-radius: 10px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-element);
    color: var(--text-tertiary);
  }
  .empty-legend {
    display: flex;
    align-items: center;
    gap: 18px;
    margin-top: 2px;
    font-size: 10px;
    font-family: var(--font-mono, ui-monospace, monospace);
    flex-wrap: wrap;
    justify-content: center;
  }
  .legend-item { opacity: 0.75; }

  @media (prefers-reduced-motion: reduce) {
    .panel, .flat-block { animation: none; }
    .tl-row, .span-row, .span-toggle { transition: none; }
    .trace-row, .trace-item { transition: none; }
  }
</style>
