import type { RequestTrace, TraceSpan } from "$lib/types";

export const SUB_SPAN_KINDS = new Set([
  "postgres",
  "postgresql",
  "mysql",
  "redis",
  "cache_extension",
  "cache-extension",
  "secret",
  "secrets",
]);

export function spanColor(kind: string): string {
  switch (kind.toLowerCase()) {
    case "external":
      return "var(--color-text-muted)";
    case "gateway":
      return "var(--color-red)";
    case "service":
    case "http":
      return "var(--color-primary)";
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
    case "sql":
      return "var(--color-blue)";
    case "redis":
      return "var(--color-amber)";
    default:
      return "var(--color-text-muted)";
  }
}

export function spanKindLabel(kind: string): string {
  switch (kind.toLowerCase()) {
    case "external":
      return "External";
    case "gateway":
      return "API GW";
    case "service":
      return "Service";
    case "http":
      return "HTTP";
    case "sql":
      return "SQL";
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

export function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export interface WaterfallRow {
  span: TraceSpan;
  offsetPct: number;
  widthPct: number;
  depth: number;
}

/** Sub-millisecond spans are common in distributed traces; keep them readable. */
export function formatSpanDuration(span: TraceSpan): string {
  if (span.durationNs !== undefined && span.durationNs < 1_000_000) {
    return `${Math.round(span.durationNs / 1000)}µs`;
  }
  return formatMs(span.durationMs);
}

export function worstSpanStatus(a: TraceSpan["status"], b: TraceSpan["status"]): TraceSpan["status"] {
  const rank = (status: string) =>
    status === "error" ? 3 : status === "client_error" ? 2 : status === "ok" ? 1 : 0;
  return rank(a) >= rank(b) ? a : b;
}

/** Distributed (imported) spans carry IDs and parents; invocation spans form a chain. */
function isDistributed(spans: TraceSpan[]): boolean {
  return spans.some((span) => span.id);
}

function startTimes(spans: TraceSpan[]): Map<TraceSpan, number> {
  return new Map(spans.map((span) => [span, span.startedAt ? Date.parse(span.startedAt) : 0]));
}

function spansById(spans: TraceSpan[]): Map<string, TraceSpan> {
  return new Map(spans.flatMap((span) => (span.id ? [[span.id, span] as const] : [])));
}

/** Depth-first by start time, so children follow their parent. */
export function orderSpans(spans: TraceSpan[]): TraceSpan[] {
  if (!isDistributed(spans)) return spans;
  const start = startTimes(spans);
  const byStart = (a: TraceSpan, b: TraceSpan) => start.get(a)! - start.get(b)!;
  const byId = spansById(spans);
  const children = new Map<string, TraceSpan[]>();
  const roots: TraceSpan[] = [];
  for (const span of spans) {
    if (span.parentId && span.parentId !== span.id && byId.has(span.parentId)) {
      const siblings = children.get(span.parentId);
      if (siblings) siblings.push(span);
      else children.set(span.parentId, [span]);
    } else {
      roots.push(span);
    }
  }
  const ordered: TraceSpan[] = [];
  const visited = new Set<TraceSpan>();
  const visit = (span: TraceSpan) => {
    if (visited.has(span)) return;
    visited.add(span);
    ordered.push(span);
    for (const child of (children.get(span.id ?? "") ?? []).sort(byStart)) visit(child);
  };
  for (const root of roots.sort(byStart)) visit(root);
  // A parent cycle has no root; keep those spans rather than drop them.
  for (const span of spans) visit(span);
  return ordered;
}

/** Nesting depth per span, memoised so each parent chain is walked once. */
function spanDepths(spans: TraceSpan[]): Map<TraceSpan, number> {
  const byId = spansById(spans);
  const depths = new Map<TraceSpan, number>();
  const depthOf = (span: TraceSpan, seen: Set<TraceSpan>): number => {
    const known = depths.get(span);
    if (known !== undefined) return known;
    const parent = span.parentId ? byId.get(span.parentId) : undefined;
    seen.add(span);
    const depth = parent && !seen.has(parent) ? depthOf(parent, seen) + 1 : 0;
    depths.set(span, depth);
    return depth;
  };
  for (const span of spans) depthOf(span, new Set());
  return depths;
}

export function buildWaterfall(
  spans: TraceSpan[],
  total: number,
  startedAt?: string,
): WaterfallRow[] {
  if (total <= 0) return spans.map((span) => ({ span, offsetPct: 0, widthPct: 2, depth: 0 }));
  const depths = spanDepths(spans);
  const traceStart = startedAt ? Date.parse(startedAt) : 0;

  let cum = 0;
  let containerStartMs = 0;
  let containerDurationMs = total;
  const rows: WaterfallRow[] = [];

  for (const span of spans) {
    if (span.startedAt && startedAt) {
      const duration =
        span.durationNs === undefined ? span.durationMs : span.durationNs / 1_000_000;
      const offset = Math.max(0, Date.parse(span.startedAt) - traceStart);
      rows.push({
        span,
        offsetPct: Math.min(100, (offset / total) * 100),
        widthPct: Math.max(0.5, Math.min(100 - (offset / total) * 100, (duration / total) * 100)),
        depth: depths.get(span)!,
      });
      continue;
    }
    const kind = span.kind.toLowerCase();
    const nested = SUB_SPAN_KINDS.has(kind);

    let offsetMs: number;
    if (nested) {
      const containerEnd = containerStartMs + containerDurationMs;
      offsetMs = Math.max(containerStartMs, containerEnd - span.durationMs);
    } else {
      offsetMs = cum;
      if (kind === "lambda" || kind === "ecs") {
        containerStartMs = cum;
        containerDurationMs = span.durationMs;
      }
      cum += span.durationMs;
    }

    rows.push({
      span,
      offsetPct: (offsetMs / total) * 100,
      widthPct: Math.max(0.5, (span.durationMs / total) * 100),
      depth: nested ? 1 : 0,
    });
  }
  return rows;
}

export interface FlowNode {
  span: TraceSpan;
  calls: number;
}

export interface FlowGraph {
  nodes: FlowNode[];
  edges: [number, number][];
}

/**
 * Invocation traces chain their spans in order. Distributed traces carry
 * dozens of spans per service, so they collapse to one node per service and
 * one edge per service-to-service call; the waterfall keeps every span.
 */
export function buildServiceFlow(spans: TraceSpan[]): FlowGraph {
  if (!isDistributed(spans)) {
    return {
      nodes: spans.map((span) => ({ span, calls: 1 })),
      edges: spans.slice(1).map((_, i) => [i, i + 1]),
    };
  }
  const start = startTimes(spans);
  const keyOf = (span: TraceSpan) => `${span.kind}\n${span.service ?? span.name}`;
  const groups = new Map<string, TraceSpan[]>();
  for (const span of [...spans].sort((a, b) => start.get(a)! - start.get(b)!)) {
    const group = groups.get(keyOf(span));
    if (group) group.push(span);
    else groups.set(keyOf(span), [span]);
  }
  const index = new Map([...groups.keys()].map((key, i) => [key, i]));
  const nodes = [...groups.values()].map((group) => {
    let first = Infinity;
    let last = -Infinity;
    let status: TraceSpan["status"] = "ok";
    for (const span of group) {
      first = Math.min(first, start.get(span)!);
      last = Math.max(last, start.get(span)! + span.durationMs);
      status = worstSpanStatus(status, span.status);
    }
    const head = group[0];
    // The node reports how long the service was active, not summed overlapping spans.
    return {
      span: { ...head, name: head.service ?? head.name, durationMs: last - first, status },
      calls: group.length,
    };
  });
  const byId = spansById(spans);
  const seen = new Set<string>();
  const edges: [number, number][] = [];
  for (const span of spans) {
    const parent = span.parentId ? byId.get(span.parentId) : undefined;
    if (!parent) continue;
    const from = index.get(keyOf(parent))!;
    const to = index.get(keyOf(span))!;
    if (from === to || seen.has(`${from}>${to}`)) continue;
    seen.add(`${from}>${to}`);
    edges.push([from, to]);
  }
  return { nodes, edges };
}

export function traceTitle(trace: RequestTrace): string {
  if (trace.method && trace.path) return `${trace.method} ${trace.path}`;
  const eb = trace.spans.find((s) => s.kind === "eventbridge");
  if (eb) return `EventBridge → ${eb.name}`;
  const s3 = trace.spans.find((s) => s.kind === "s3");
  if (s3) return `S3 → ${s3.name}`;
  const topic = trace.spans.find((s) => s.kind === "topic");
  if (topic) return `SNS → ${topic.name}`;
  const q = trace.spans.find((s) => s.kind === "queue" || s.kind === "dlq");
  if (q) return `ESM → ${q.name}`;
  const ext = trace.spans.find((s) => s.kind === "external");
  if (ext) return `Direct invoke from ${ext.name}`;
  const fn = trace.spans.find((s) => s.kind === "lambda");
  if (fn) return `Invoke: ${fn.name}`;
  const task = trace.spans.find((s) => s.kind === "ecs");
  if (task) return `RunTask: ${task.name}`;
  return `trace:${trace.id.slice(0, 8)}`;
}
