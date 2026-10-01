import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";
import { plugin } from "bun";
import { compileModule } from "svelte/compiler";

// Run the production rune module through Svelte's compiler, rather than replacing its state.
plugin({
  name: "svelte-state-tests",
  setup(build) {
    build.onLoad({ filter: /\.svelte\.ts$/ }, async ({ path }) => ({
      contents: compileModule(
        new Bun.Transpiler({ loader: "ts" }).transformSync(await Bun.file(path).text()),
        { filename: path, generate: "client" },
      ).js.code,
      loader: "js",
    }));
  },
});

const state = await import("./state.svelte.ts");
const DEFAULT = "000000000000";
const A = "111111111111";
const B = "222222222222";
const dashboard = state.getDashboard();
const accounts = state.getAccountSettings();
let fetchSpy;
let pending;

function overview(accountId) {
  return {
    status: "ok",
    timestamp: "2026-10-01T00:00:00Z",
    services: [],
    config: { accountId, region: "us-east-1", endpoint: "", dataDir: "", uiEnabled: true },
    counts: {
      gateways: 0,
      functions: 0,
      queues: 0,
      topics: 0,
      subscriptions: 0,
      secrets: 0,
      buckets: 0,
      logGroups: 0,
      eventSourceMappings: 0,
      eventBridgeRules: 0,
    },
    gateways: [],
    functions: [],
    queues: [],
    topics: [],
    subscriptions: [],
    secrets: [],
    buckets: [],
    eventSourceMappings: [],
    infrastructure: [],
  };
}

const settle = () => Bun.sleep(0);
const archived = () =>
  new Response(null, { status: 403, headers: { "x-amzn-ErrorType": "AccountArchived" } });

beforeEach(async () => {
  pending = [];
  fetchSpy = spyOn(globalThis, "fetch").mockResolvedValue(Response.json(overview(DEFAULT)));
  state.switchAccount(DEFAULT);
  await state.refresh();
  fetchSpy.mockImplementation(
    (_url, init) =>
      new Promise((resolve, reject) => {
        const authorization = new Headers(init?.headers).get("Authorization") ?? "";
        pending.push({
          account: authorization.match(/Credential=(\d+)\//)?.[1] ?? DEFAULT,
          signal: init?.signal,
          resolve,
          reject,
        });
      }),
  );
});

afterEach(async () => {
  // Finish every deferred load even when an assertion failed before its response was released.
  fetchSpy.mockImplementation(() =>
    Promise.resolve(Response.json(overview(accounts.activeAccountId))),
  );
  for (const request of pending) request.resolve(Response.json(overview(request.account)));
  await state.refresh();
  fetchSpy.mockRestore();
});

describe("dashboard account switches", () => {
  test("clears the rendered account while the next account loads", () => {
    expect(dashboard.data.config.accountId).toBe(DEFAULT);
    state.switchAccount(B);
    expect(dashboard.data).toBeNull();
    expect(dashboard.lastRefresh).toBe("");
    expect(dashboard.loading).toBe(true);
  });

  test("starts the next load immediately and aborts the previous one", () => {
    state.refresh();
    const previous = pending[0];
    state.switchAccount(B);
    expect(pending.map((request) => request.account)).toEqual([DEFAULT, B]);
    expect(previous.signal?.aborted).toBe(true);
  });

  test("ignores a late overview even if cancellation is ignored", async () => {
    state.refresh();
    const previous = pending[0];
    state.switchAccount(B);
    previous.resolve(Response.json(overview(DEFAULT)));
    await settle();
    expect(dashboard.data).toBeNull();
    expect(dashboard.loading).toBe(true);
    const current = pending.find((request) => request.account === B);
    current.resolve(Response.json(overview(B)));
    await settle();
    expect(dashboard.data.config.accountId).toBe(B);
    expect(dashboard.loading).toBe(false);
  });

  test("rejects an old result after switching away and back to the same account", async () => {
    state.refresh();
    const previous = pending[0];
    state.switchAccount(B);
    state.switchAccount(DEFAULT);
    previous.resolve(Response.json(overview(DEFAULT)));
    await settle();
    expect(dashboard.data).toBeNull();
    expect(dashboard.loading).toBe(true);
  });

  test("a late completion does not stop deduplication of the current refresh", async () => {
    state.refresh();
    const previous = pending[0];
    state.switchAccount(B);
    previous.resolve(Response.json(overview(DEFAULT)));
    await settle();
    state.refresh();
    state.refresh();
    expect(pending.map((request) => request.account)).toEqual([DEFAULT, B]);
    pending[1].resolve(Response.json(overview(B)));
    await settle();
    expect(pending.map((request) => request.account)).toEqual([DEFAULT, B, B]);
    pending[2].resolve(Response.json(overview(B)));
    await settle();
    expect(dashboard.data.config.accountId).toBe(B);
  });

  test("a failed new account load cannot leave the old account visible", async () => {
    state.switchAccount(B);
    pending[0].reject(new Error("new account failed"));
    await settle();
    expect(dashboard.data).toBeNull();
    expect(dashboard.error).toBe("new account failed");
    expect(dashboard.loading).toBe(false);
  });

  test("ignores errors from a superseded account load", async () => {
    state.refresh();
    const previous = pending[0];
    state.switchAccount(B);
    previous.reject(new Error("old account failed"));
    await settle();
    expect(dashboard.error).toBe("");
    expect(dashboard.loading).toBe(true);
  });

  test("an old archived-account error cannot change the current selection", async () => {
    state.switchAccount(A);
    const previous = pending[0];
    state.switchAccount(B);
    previous.resolve(archived());
    await settle();
    expect(accounts.activeAccountId).toBe(B);
    expect(dashboard.error).toBe("");
  });

  test("an archived active account still falls back to the default", async () => {
    state.switchAccount(A);
    pending[0].resolve(archived());
    await settle();
    expect(accounts.activeAccountId).toBe(DEFAULT);
    pending
      .find((request) => request.account === DEFAULT)
      .resolve(Response.json(overview(DEFAULT)));
    await settle();
    expect(dashboard.data.config.accountId).toBe(DEFAULT);
    expect(dashboard.loading).toBe(false);
    expect(dashboard.error).toBe("");
  });

  test("removing the active account uses the same clearing and cancellation", () => {
    state.switchAccount(A);
    const previous = pending[0];
    state.removeKnownAccount(A);
    expect(accounts.activeAccountId).toBe(DEFAULT);
    expect(dashboard.data).toBeNull();
    expect(previous.signal?.aborted).toBe(true);
    expect(pending.map((request) => request.account)).toEqual([A, DEFAULT]);
  });
});
