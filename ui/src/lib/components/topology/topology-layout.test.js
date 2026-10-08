import { describe, expect, test } from "bun:test";
import { buildTopologyGraph, framesCollide, nodeBounds, portPos } from "./topology-connection-model.ts";

const fn = (name) => ({ name, runtime: "nodejs20.x", state: "Active", tags: {} });
const queue = (name) => ({ name, approxVisible: 0, approxInFlight: 0, approxDelayed: 0, tags: {} });
const probe = (name, port) => ({
  name,
  kind: "postgresql",
  host: "localhost",
  port,
  status: "refused",
  latencyMs: 0,
  probedAt: "",
});

function input(order = (xs) => xs) {
  const functions = order(
    Array.from({ length: 22 }, (_, i) => fn(`fn-${String(i).padStart(2, "0")}`)),
  );
  const queues = order([queue("orders"), queue("orders-dlq"), queue("emails")]);
  return {
    gateways: [{ apiId: "gw1", name: "shop", routes: 2 }],
    functions,
    queues,
    dynamodbTables: [{ name: "orders-table", itemCount: 0, streamEnabled: false }],
    topics: [],
    buckets: [],
    secrets: [{ name: "db-password", versionId: "abcdef123" }],
    infra: order([probe("Postgres", 5432), probe("Redis", 6379)]),
    infraConnections: [
      {
        sourceFunction: "shop",
        targetKind: "apigw-lambda",
        targetId: "fn-03",
        targetName: "fn-03",
      },
      {
        sourceFunction: "orders",
        targetKind: "queue-dlq",
        targetId: "orders-dlq",
        targetName: "orders-dlq",
      },
      {
        sourceFunction: "fn-10",
        targetKind: "postgresql",
        targetId: "postgresql-localhost-5432",
        targetName: "Postgres",
      },
      {
        sourceFunction: "fn-07",
        targetKind: "dynamodb-table",
        targetId: "orders-table",
        targetName: "orders-table",
      },
    ],
    eventSourceMappings: [
      { queueName: "orders", functionName: "fn-15" },
      { queueName: "emails", functionName: "fn-01" },
    ],
    infraOrderIds: [],
  };
}

const positions = (model) => model.allNodes.map((n) => `${n.kind}:${n.id}@${n.x},${n.y}`).sort();

describe("topology flow layout", () => {
  test("is the same whatever order the resources arrive in", () => {
    expect(positions(buildTopologyGraph(input()))).toEqual(
      positions(buildTopologyGraph(input((xs) => [...xs].reverse()))),
    );
  });

  test("keeps nodes apart and on the canvas", () => {
    const model = buildTopologyGraph(input());
    const boxes = model.allNodes.map(nodeBounds);
    for (const b of boxes) {
      expect(b.left).toBeGreaterThanOrEqual(0);
      expect(b.top).toBeGreaterThanOrEqual(0);
      expect(b.right).toBeLessThanOrEqual(model.canvasSize.width);
      expect(b.bottom).toBeLessThanOrEqual(model.canvasSize.height);
    }
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i];
        const b = boxes[j];
        const overlap =
          a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
        expect(overlap).toBe(false);
      }
    }
  });

  test("flows left to right and gives services a column of their own", () => {
    const model = buildTopologyGraph(input());
    const x = (kind) => model.allNodes.find((n) => n.kind === kind).x;
    expect(x("gateway")).toBeLessThan(x("queue"));
    expect(x("queue")).toBeLessThan(x("function"));
    expect(x("function")).toBeLessThan(x("dynamodb"));
    const infra = model.allNodes.filter((n) => n.kind === "infra");
    expect(new Set(infra.map((n) => n.x)).size).toBe(1);
  });

  test("wraps a long list of functions and tucks a DLQ under its queue", () => {
    const model = buildTopologyGraph(input());
    expect(new Set(model.nodes.functions.map((n) => n.x)).size).toBeGreaterThan(1);
    const queues = [...model.nodes.queues].sort((a, b) => a.y - b.y).map((q) => q.id);
    expect(queues.indexOf("orders-dlq")).toBe(queues.indexOf("orders") + 1);
  });

  test("draws one bracket to the secrets cache instead of a line per function", () => {
    const model = buildTopologyGraph(input());
    expect(model.cacheBus?.in).toBeTruthy();
    expect(model.cacheBus?.out).toBeTruthy();
    expect(model.sections.map((s) => s.label)).toContain("Functions");
  });

  test("fills a view's shape without overlapping", () => {
    const wide = buildTopologyGraph({ ...input(), aspect: 0.6 });
    const plain = buildTopologyGraph(input());
    expect(wide.canvasSize.height).toBeGreaterThanOrEqual(wide.canvasSize.width * 0.6 - 1);
    // Fewer side-by-side wraps in a tall view: narrower relative to height.
    expect(wide.canvasSize.height / wide.canvasSize.width).toBeGreaterThanOrEqual(
      plain.canvasSize.height / plain.canvasSize.width,
    );
    const boxes = wide.allNodes.map(nodeBounds);
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i];
        const b = boxes[j];
        expect(a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom).toBe(false);
      }
      expect(boxes[i].bottom).toBeLessThanOrEqual(wide.canvasSize.height);
    }
  });

  test("frames follow their nodes and can't be pushed into each other", () => {
    const model = buildTopologyGraph({ ...input(), aspect: 0.6 });
    const queues = model.sections.find((s) => s.label === "Queues");
    const fns = model.sections.find((s) => s.label === "Functions");
    const key = queues.nodeKeys[0];
    const q = model.nodeByGraphKey.get(key);
    expect(framesCollide(model, { [key]: { x: q.x, y: q.y + 10 } })).toBe(false);
    const f = model.nodeByGraphKey.get(fns.nodeKeys[0]);
    expect(framesCollide(model, { [key]: { x: f.x, y: f.y } })).toBe(true);
    // A whole section moved together into empty space is fine.
    const moved = Object.fromEntries(
      queues.nodeKeys.map((k) => {
        const n = model.nodeByGraphKey.get(k);
        return [k, { x: n.x, y: n.y + model.canvasSize.height }];
      }),
    );
    expect(framesCollide(model, moved)).toBe(false);
  });

  test("keeps the user's card order and section moves, relative to the layout", () => {
    const base = buildTopologyGraph(input());
    const queues = base.sections.find((s) => s.label === "Queues");
    const order = [...queues.nodeKeys].reverse();
    const reordered = buildTopologyGraph({
      ...input(),
      arrangement: { order: { [queues.id]: order } },
    });
    expect(reordered.sections.find((s) => s.id === queues.id).nodeKeys).toEqual(order);

    // Moved into clear space: it goes there, nodes and all.
    const gw = base.sections.find((s) => s.label === "API gateways");
    const node = base.nodeByGraphKey.get(gw.nodeKeys[0]);
    const moved = buildTopologyGraph({
      ...input(),
      arrangement: { offsets: { [gw.id]: { x: 0, y: -60 } } },
    });
    expect(moved.nodeByGraphKey.get(gw.nodeKeys[0]).y).toBe(node.y - 60);
    expect(moved.sections.find((s) => s.id === gw.id).offset).toEqual({ x: 0, y: -60 });

    // Moved onto another section: dropped, back to the layout's spot.
    const fns = base.sections.find((s) => s.label === "Functions");
    const clash = buildTopologyGraph({
      ...input(),
      arrangement: { offsets: { [gw.id]: { x: fns.x - gw.x, y: fns.y - gw.y } } },
    });
    expect(clash.nodeByGraphKey.get(gw.nodeKeys[0]).y).toBe(node.y);
  });

  test("an opened card grows to show its details and the layout makes room", () => {
    const shut = buildTopologyGraph({ ...input(), aspect: 0.6 });
    const open = buildTopologyGraph({
      ...input(),
      aspect: 0.6,
      allNodeOverrides: { "function:fn-03": { expanded: true }, "function:fn-04": { expanded: true } },
    });
    const card = (m) => m.nodeByGraphKey.get("function:fn-03");
    expect(card(open).details.map(([label]) => label)).toContain("runtime");
    const h = (n) => nodeBounds(n).bottom - nodeBounds(n).top;
    expect(h(card(open))).toBeGreaterThan(h(card(shut)));
    // Connectors stay on the header, not the middle of the taller card.
    expect(portPos(card(open), "input").y).toBeLessThan(card(open).y);
    const boxes = open.allNodes.map(nodeBounds);
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i];
        const b = boxes[j];
        expect(a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom).toBe(false);
      }
    }
  });
});
