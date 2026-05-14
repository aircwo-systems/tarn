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

export function spanKindLabel(kind: string): string {
  switch (kind.toLowerCase()) {
    case "external":
      return "External";
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

export function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export interface WaterfallRow {
  span: TraceSpan;
  offsetPct: number;
  widthPct: number;
  nested: boolean;
}

export function buildWaterfall(spans: TraceSpan[], total: number): WaterfallRow[] {
  if (total <= 0)
    return spans.map((span) => ({ span, offsetPct: 0, widthPct: 2, nested: false }));

  let cum = 0;
  let lambdaStartMs = 0;
  let lambdaDurationMs = total;
  const rows: WaterfallRow[] = [];

  for (const span of spans) {
    const kind = span.kind.toLowerCase();
    const nested = SUB_SPAN_KINDS.has(kind);

    let offsetMs: number;
    if (nested) {
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
  return `trace:${trace.id.slice(0, 8)}`;
}
