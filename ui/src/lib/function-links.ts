import type { FunctionSummary, OverviewResponse, RequestTrace, TraceSpan } from "$lib/types";
import { SUB_SPAN_KINDS, spanKindLabel } from "$lib/trace-utils";

/** A configured or observed thing that invokes a function. */
export interface FunctionCaller {
  key: string;
  kind: "gateway" | "queue" | "stream" | "eventbridge" | "topic" | "stepfunctions" | "observed";
  label: string;
  detail: string;
  /** tab hash to jump to the caller's section */
  href?: string;
  state?: "ok" | "warn" | "error" | "off";
  meta?: string;
}

/** One invocation of the function, reconstructed from a recorded trace. */
export interface FunctionInvocation {
  traceId: string;
  startedAt: string;
  durationMs: number;
  status: "ok" | "error";
  callerKind: string;
  callerName: string;
  title: string;
  downstream: TraceSpan[];
}

export interface FunctionDependency {
  key: string;
  kind: string;
  name: string;
  detail: string;
  calls: number;
  errors: number;
}

/** True when `ref` (a name, ARN, or integration URI) points at the function. */
export function refersToFunction(ref: string | undefined, fn: Pick<FunctionSummary, "name" | "arn">): boolean {
  if (!ref) return false;
  if (ref === fn.name || ref === fn.arn) return true;
  const escaped = fn.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`function:${escaped}(?:[:/"]|$)`).test(ref);
}

function lastSegment(arn: string | undefined): string {
  if (!arn) return "";
  return arn.split(/[:/]/).filter(Boolean).pop() ?? arn;
}

function resultState(result: string | undefined): FunctionCaller["state"] {
  if (!result) return undefined;
  const r = result.toLowerCase();
  if (r.includes("fail") || r.includes("error")) return "error";
  if (r.includes("throttl") || r.includes("disabled")) return "warn";
  return "ok";
}

/** Everything configured to invoke `fn`: API routes, event sources, rules, topics, state machines. */
export function configuredCallers(fn: FunctionSummary, data: OverviewResponse | null): FunctionCaller[] {
  if (!data) return [];
  const callers: FunctionCaller[] = [];

  for (const gw of data.gateways ?? []) {
    for (const route of gw.routeDetails ?? []) {
      if (!refersToFunction(route.integrationTarget, fn)) continue;
      callers.push({
        key: `gw:${gw.apiId}:${route.routeKey}`,
        kind: "gateway",
        label: route.method && route.path ? `${route.method} ${route.path}` : route.routeKey,
        detail: gw.name,
        href: "gateways",
        meta: route.integrationType,
      });
    }
  }

  for (const m of data.eventSourceMappings ?? []) {
    if (!refersToFunction(m.functionName, fn)) continue;
    const isStream = (m.sourceType ?? "").toLowerCase().includes("dynamo") || (m.eventSourceArn ?? "").includes(":stream/");
    const off = m.state.toLowerCase() !== "enabled";
    callers.push({
      key: `esm:${m.uuid}`,
      kind: isStream ? "stream" : "queue",
      label: m.sourceName || m.queueName || lastSegment(m.eventSourceArn),
      detail: `${isStream ? "Stream" : "SQS"} · batch ${m.batchSize}${m.filterCriteria?.Filters?.length ? " · filtered" : ""}`,
      href: isStream ? "dynamodb" : "queues",
      state: off ? "off" : resultState(m.lastResult),
      meta: m.lastResult,
    });
  }

  for (const rule of data.eventBridgeRules ?? []) {
    for (const target of rule.targets ?? []) {
      if (!refersToFunction(target.arn, fn)) continue;
      callers.push({
        key: `eb:${rule.name}:${target.id}`,
        kind: "eventbridge",
        label: rule.name,
        detail: rule.scheduleExpression || "Event pattern",
        href: `eventbridge?rule=${encodeURIComponent(rule.name)}`,
        state: rule.state !== "ENABLED" ? "off" : resultState(target.lastResult ?? rule.lastResult),
        meta: rule.nextRunAt ? `next ${rule.nextRunAt}` : undefined,
      });
    }
  }

  for (const sub of data.subscriptions ?? []) {
    if (sub.protocol !== "lambda" || !refersToFunction(sub.endpoint, fn)) continue;
    callers.push({
      key: `sns:${sub.subscriptionArn}`,
      kind: "topic",
      label: sub.topicName,
      detail: `SNS${sub.filterPolicy ? " · filtered" : ""}${sub.rawMessageDelivery ? " · raw" : ""}`,
      href: "sns",
    });
  }

  for (const sm of data.stateMachines ?? []) {
    if (!refersToFunction(sm.definition, fn) && !(sm.definition ?? "").includes(`"${fn.name}"`)) continue;
    callers.push({
      key: `sfn:${sm.arn}`,
      kind: "stepfunctions",
      label: sm.name,
      detail: `Step Functions · ${sm.type.toLowerCase()}`,
      href: "stepfunctions",
    });
  }

  return callers;
}

function isFunctionSpan(span: TraceSpan, fn: FunctionSummary): boolean {
  return span.kind.toLowerCase() === "lambda" && refersToFunction(span.name, fn);
}

/** Invocations of `fn` found in recorded traces, newest first. */
export function invocationsFromTraces(fn: FunctionSummary, traces: RequestTrace[] | undefined): FunctionInvocation[] {
  const out: FunctionInvocation[] = [];
  for (const trace of traces ?? []) {
    const idx = trace.spans.findIndex((s) => isFunctionSpan(s, fn));
    if (idx < 0) continue;
    const span = trace.spans[idx];

    let caller: TraceSpan | undefined;
    for (let i = idx - 1; i >= 0; i--) {
      if (!SUB_SPAN_KINDS.has(trace.spans[i].kind.toLowerCase())) { caller = trace.spans[i]; break; }
    }

    const downstream: TraceSpan[] = [];
    for (let i = idx + 1; i < trace.spans.length; i++) {
      const s = trace.spans[i];
      if (!SUB_SPAN_KINDS.has(s.kind.toLowerCase())) break;
      downstream.push(s);
    }

    const failed = span.status === "error" || downstream.some((s) => s.status === "error");
    out.push({
      traceId: trace.id,
      startedAt: trace.startedAt,
      durationMs: span.durationMs,
      status: failed ? "error" : "ok",
      callerKind: caller?.kind ?? "direct",
      callerName: caller?.name ?? (trace.method && trace.path ? `${trace.method} ${trace.path}` : "Direct invoke"),
      title: trace.method && trace.path ? `${trace.method} ${trace.path}` : caller ? `${spanKindLabel(caller.kind)} → ${caller.name}` : "Direct invoke",
      downstream,
    });
  }
  return out.sort((a, b) => Date.parse(b.startedAt) - Date.parse(a.startedAt));
}

/** Callers seen in traces, grouped by kind + name. */
export function observedCallers(invocations: FunctionInvocation[]): FunctionCaller[] {
  const counts = new Map<string, { kind: string; name: string; n: number }>();
  for (const inv of invocations) {
    const key = `${inv.callerKind}:${inv.callerName}`;
    const entry = counts.get(key) ?? { kind: inv.callerKind, name: inv.callerName, n: 0 };
    entry.n++;
    counts.set(key, entry);
  }
  return [...counts.values()]
    .sort((a, b) => b.n - a.n)
    .map((c) => ({
      key: `obs:${c.kind}:${c.name}`,
      kind: "observed",
      label: c.name,
      detail: `${c.kind === "direct" ? "Direct" : spanKindLabel(c.kind)} · seen ${c.n}×`,
    }));
}

/** Resources the function reaches: static connections plus sub-spans seen in traces. */
export function dependencies(
  fn: FunctionSummary,
  data: OverviewResponse | null,
  invocations: FunctionInvocation[],
): FunctionDependency[] {
  const deps = new Map<string, FunctionDependency>();

  for (const c of data?.connections ?? []) {
    if (c.sourceFunction !== fn.name || c.targetKind === "apigw-lambda") continue;
    const key = `${c.targetKind}:${c.targetName}`;
    deps.set(key, {
      key,
      kind: c.targetKind,
      name: c.targetName,
      detail: c.targetHost ? `${c.targetHost}:${c.targetPort}` : c.evidence,
      calls: 0,
      errors: 0,
    });
  }

  for (const inv of invocations) {
    for (const s of inv.downstream) {
      const kind = s.kind.toLowerCase();
      const key = [...deps.keys()].find((k) => k.endsWith(`:${s.name}`)) ?? `${kind}:${s.name}`;
      const dep = deps.get(key) ?? { key, kind, name: s.name, detail: "seen in traces", calls: 0, errors: 0 };
      dep.calls++;
      if (s.status === "error") dep.errors++;
      deps.set(key, dep);
    }
  }

  return [...deps.values()].sort((a, b) => b.calls - a.calls || a.name.localeCompare(b.name));
}

/** Example request bodies declared on API routes that target the function. */
export function inputExamples(fn: FunctionSummary, data: OverviewResponse | null): { label: string; body: string }[] {
  const out: { label: string; body: string }[] = [];
  for (const gw of data?.gateways ?? []) {
    for (const route of gw.routeDetails ?? []) {
      if (route.bodyExample === undefined || route.bodyExample === null) continue;
      if (!refersToFunction(route.integrationTarget, fn)) continue;
      const body = typeof route.bodyExample === "string" ? route.bodyExample : JSON.stringify(route.bodyExample, null, 2);
      out.push({ label: route.method && route.path ? `${route.method} ${route.path}` : route.routeKey, body });
    }
  }
  return out;
}

export function percentile(values: number[], p: number): number {
  if (values.length === 0) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length))];
}
