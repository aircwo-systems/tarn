import { expect, test } from "bun:test";
import { buildServiceFlow, buildWaterfall, formatSpanDuration, orderSpans } from "./trace-utils";

test("distributed spans use their own timing and retain overlapping children", () => {
  const start = "2026-10-06T00:00:00.000Z";
  const rows = buildWaterfall(
    [
      {
        id: "root",
        kind: "service",
        name: "frontend",
        startedAt: start,
        durationMs: 100,
        status: "ok",
      },
      {
        id: "a",
        parentId: "root",
        kind: "http",
        name: "api-a",
        startedAt: "2026-10-06T00:00:00.020Z",
        durationMs: 50,
        status: "ok",
      },
      {
        id: "b",
        parentId: "root",
        kind: "http",
        name: "api-b",
        startedAt: "2026-10-06T00:00:00.030Z",
        durationMs: 40,
        status: "ok",
      },
    ],
    100,
    start,
  );
  expect(rows.map(({ offsetPct, widthPct, depth }) => ({ offsetPct, widthPct, depth }))).toEqual([
    { offsetPct: 0, widthPct: 100, depth: 0 },
    { offsetPct: 20, widthPct: 50, depth: 1 },
    { offsetPct: 30, widthPct: 40, depth: 1 },
  ]);
});

test("existing invocation traces retain their sequential timing", () => {
  const rows = buildWaterfall(
    [
      { kind: "gateway", name: "gateway", durationMs: 10, status: "ok" },
      { kind: "lambda", name: "lambda", durationMs: 90, status: "ok" },
    ],
    100,
  );
  expect(rows.map((row) => row.offsetPct)).toEqual([0, 10]);
});

test("distributed flows collapse repeated spans into one node per service", () => {
  const at = (ms) => new Date(Date.UTC(2026, 9, 6, 0, 0, 0, ms)).toISOString();
  const flow = buildServiceFlow([
    { id: "root", kind: "service", name: "frontend", startedAt: at(0), durationMs: 100, status: "ok" },
    { id: "mw1", parentId: "root", kind: "service", name: "frontend", startedAt: at(5), durationMs: 10, status: "ok" },
    { id: "r1", parentId: "mw1", kind: "redis", name: "frontend-redis", startedAt: at(6), durationMs: 2, status: "ok" },
    { id: "r2", parentId: "root", kind: "redis", name: "frontend-redis", startedAt: at(50), durationMs: 3, status: "error" },
    { id: "gw", parentId: "root", kind: "service", name: "gateway", startedAt: at(20), durationMs: 30, status: "ok" },
    { id: "api", parentId: "gw", kind: "service", name: "api", startedAt: at(25), durationMs: 20, status: "ok" },
  ]);
  expect(flow.nodes.map(({ span, calls }) => [span.name, calls, span.durationMs, span.status])).toEqual([
    ["frontend", 2, 100, "ok"],
    ["frontend-redis", 2, 47, "error"],
    ["gateway", 1, 30, "ok"],
    ["api", 1, 20, "ok"],
  ]);
  expect(flow.edges).toEqual([[0, 1], [0, 2], [2, 3]]);
});

test("invocation flows keep their sequential chain", () => {
  const flow = buildServiceFlow([
    { kind: "gateway", name: "gateway", durationMs: 10, status: "ok" },
    { kind: "lambda", name: "fn", durationMs: 90, status: "ok" },
    { kind: "queue", name: "q", durationMs: 1, status: "ok" },
  ]);
  expect(flow.nodes.map((node) => node.span.name)).toEqual(["gateway", "fn", "q"]);
  expect(flow.edges).toEqual([[0, 1], [1, 2]]);
});

test("distributed spans order depth-first by start time and report depth", () => {
  const at = (ms) => new Date(Date.UTC(2026, 9, 6, 0, 0, 0, ms)).toISOString();
  const spans = orderSpans([
    { id: "c2", parentId: "root", kind: "service", name: "fe", startedAt: at(30), durationMs: 5, status: "ok" },
    { id: "g1", parentId: "c1", kind: "redis", name: "fe-redis", startedAt: at(12), durationMs: 1, status: "ok" },
    { id: "root", kind: "service", name: "fe", startedAt: at(0), durationMs: 50, status: "ok" },
    { id: "c1", parentId: "root", kind: "service", name: "fe", startedAt: at(10), durationMs: 5, status: "ok" },
    { id: "orphan", parentId: "missing", kind: "service", name: "fe", startedAt: at(40), durationMs: 1, status: "ok" },
  ]);
  expect(spans.map((span) => span.id)).toEqual(["root", "c1", "g1", "c2", "orphan"]);
  const rows = buildWaterfall(spans, 50, at(0));
  expect(rows.map((row) => row.depth)).toEqual([0, 1, 2, 1, 0]);
});

test("flow nodes group by service when spans name their operation", () => {
  const at = (ms) => new Date(Date.UTC(2026, 9, 6, 0, 0, 0, ms)).toISOString();
  const flow = buildServiceFlow([
    { id: "root", kind: "service", name: "GET /home", service: "fe", startedAt: at(0), durationMs: 20, status: "ok" },
    { id: "mw", parentId: "root", kind: "service", name: "session", service: "fe", startedAt: at(1), durationMs: 1, status: "ok" },
  ]);
  expect(flow.nodes.map(({ span, calls }) => [span.name, calls])).toEqual([["fe", 2]]);
  expect(flow.edges).toEqual([]);
});

test("sub-millisecond spans show microseconds", () => {
  expect(formatSpanDuration({ kind: "service", name: "fe", durationMs: 0, durationNs: 123_400, status: "ok" })).toBe("123µs");
  expect(formatSpanDuration({ kind: "service", name: "fe", durationMs: 4, durationNs: 4_200_000, status: "ok" })).toBe("4ms");
  expect(formatSpanDuration({ kind: "lambda", name: "fn", durationMs: 0, status: "ok" })).toBe("0ms");
});
