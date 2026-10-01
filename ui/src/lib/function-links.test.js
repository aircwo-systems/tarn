import { describe, expect, test } from "bun:test";
import { dependencies, invocationsFromTraces, observedCallers } from "./function-links";

const fn = {
  name: "audit-function",
  arn: "arn:aws:lambda:us-east-1:111111111111:function:audit-function",
};
const span = (kind, name, durationMs = 1, status = "ok") => ({ kind, name, durationMs, status });
const trace = (id, spans, startedAt = "2026-10-01T10:00:00Z") => ({
  id,
  startedAt,
  durationMs: 100,
  status: 200,
  method: "POST",
  path: "/audit",
  spans,
});

describe("function invocations reconstructed from traces", () => {
  test("keeps each invocation and its own result and downstream calls", () => {
    const invocations = invocationsFromTraces(fn, [
      trace("repeated", [
        span("gateway", "audit-api"),
        span("lambda", fn.name, 10),
        span("redis", "audit-cache", 2),
        span("queue", "audit-queue"),
        span("lambda", fn.arn, 30, "error"),
        span("postgres", "audit-db", 4, "error"),
      ]),
    ]);

    expect(invocations).toHaveLength(2);
    expect(invocations.map((inv) => [inv.durationMs, inv.status, inv.callerName])).toEqual([
      [10, "ok", "audit-api"],
      [30, "error", "audit-queue"],
    ]);
    expect(invocations.map((inv) => inv.downstream.map((call) => call.name))).toEqual([
      ["audit-cache"],
      ["audit-db"],
    ]);
    expect(new Set(invocations.map((inv) => inv.id)).size).toBe(2);
    expect(invocations.every((inv) => inv.traceId === "repeated")).toBe(true);
    expect(
      dependencies(fn, null, invocations).find((dep) => dep.name === "audit-db"),
    ).toMatchObject({ calls: 1, errors: 1 });
  });

  test("counts repeated invocations from the same observed caller", () => {
    const invocations = invocationsFromTraces(fn, [
      trace("repeated-caller", [
        span("queue", "audit-queue"),
        span("lambda", fn.name),
        span("queue", "audit-queue"),
        span("lambda", fn.name),
      ]),
    ]);
    expect(observedCallers(invocations)).toMatchObject([
      { label: "audit-queue", detail: "SQS · seen 2×" },
    ]);
  });

  test("gives adjacent invocations distinct, stable identities", () => {
    const input = [trace("adjacent", [span("lambda", fn.name), span("lambda", fn.name)])];
    const first = invocationsFromTraces(fn, input);
    expect(first).toHaveLength(2);
    expect(first[0].id).toBeDefined();
    expect(first[0].id).not.toBe(first[1].id);
    expect(invocationsFromTraces(fn, input).map((inv) => inv.id)).toEqual(
      first.map((inv) => inv.id),
    );
  });

  test("orders traces newest first without dropping same-trace invocations", () => {
    const invocations = invocationsFromTraces(fn, [
      trace("older", [span("lambda", fn.name)], "2026-10-01T09:00:00Z"),
      trace("newer", [span("lambda", fn.name), span("lambda", fn.name)]),
    ]);
    expect(invocations.map((inv) => inv.traceId)).toEqual(["newer", "newer", "older"]);
    expect(new Set(invocations.map((inv) => inv.id)).size).toBe(3);
  });

  test("ignores unrelated spans and supports direct invocations", () => {
    expect(invocationsFromTraces(fn, undefined)).toEqual([]);
    expect(
      invocationsFromTraces(fn, [
        trace("other", [span("lambda", "other-function"), span("redis", fn.name)]),
      ]),
    ).toEqual([]);
    const direct = invocationsFromTraces(fn, [
      { ...trace("direct", [span("lambda", fn.name)]), method: undefined, path: undefined },
    ]);
    expect(direct).toMatchObject([
      { callerKind: "direct", callerName: "Direct invoke", status: "ok" },
    ]);
  });
});
