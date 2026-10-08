import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";
import { plugin } from "bun";
import { compileModule } from "svelte/compiler";

plugin({
  name: "log-pulse-runes",
  setup(build) {
    build.onLoad({ filter: /log-pulse\.svelte\.ts$/ }, async ({ path }) => ({
      contents: compileModule(
        new Bun.Transpiler({ loader: "ts" }).transformSync(await Bun.file(path).text()),
        { filename: path, generate: "client" },
      ).js.code,
      loader: "js",
    }));
  },
});

const { startLogPulse } = await import("./log-pulse.svelte.ts");
let documentDescriptor;
let page;
let timers;
let requests;
let spies;
let stops;

beforeEach(() => {
  documentDescriptor = Object.getOwnPropertyDescriptor(globalThis, "document");
  page = new EventTarget();
  page.hidden = false;
  Object.defineProperty(globalThis, "document", { configurable: true, value: page });
  timers = new Map();
  requests = [];
  stops = [];
  let timerId = 0;
  spies = [
    spyOn(globalThis, "setInterval").mockImplementation((callback) => {
      timers.set(++timerId, callback);
      return timerId;
    }),
    spyOn(globalThis, "clearInterval").mockImplementation((id) => timers.delete(id)),
    spyOn(globalThis, "fetch").mockImplementation((_url, { signal }) => {
      requests.push(signal);
      return new Promise((_resolve, reject) => {
        signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), {
          once: true,
        });
      });
    }),
  ];
});

afterEach(async () => {
  for (const stop of stops) stop();
  await Bun.sleep(0);
  for (const spy of spies) spy.mockRestore();
  if (documentDescriptor) Object.defineProperty(globalThis, "document", documentDescriptor);
  else delete globalThis.document;
});

function start() {
  const stop = startLogPulse();
  stops.push(stop);
  return stop;
}

function visibility(hidden) {
  page.hidden = hidden;
  page.dispatchEvent(new Event("visibilitychange"));
}

describe("log pulse visibility", () => {
  test("does no polling when started in a hidden tab", () => {
    page.hidden = true;
    start();
    expect(requests).toHaveLength(0);
    expect(timers.size).toBe(0);
    visibility(false);
    expect(requests).toHaveLength(1);
    expect(timers.size).toBe(1);
  });

  test("aborts and stops while hidden, then refreshes once on return", () => {
    start();
    expect(requests).toHaveLength(1);
    visibility(true);
    expect(requests[0].aborted).toBe(true);
    expect(timers.size).toBe(0);
    visibility(false);
    expect(requests).toHaveLength(2);
    expect(timers.size).toBe(1);
    visibility(false);
    expect(requests).toHaveLength(2);
  });

  test("shares one poller and disposes each consumer only once", () => {
    const first = start();
    const second = start();
    expect(requests).toHaveLength(1);
    first();
    first();
    expect(timers.size).toBe(1);
    expect(requests[0].aborted).toBe(false);
    second();
    expect(timers.size).toBe(0);
    expect(requests[0].aborted).toBe(true);
    visibility(true);
    visibility(false);
    expect(requests).toHaveLength(1);
  });
});
