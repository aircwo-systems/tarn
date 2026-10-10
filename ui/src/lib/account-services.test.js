import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";
import { plugin } from "bun";
import { compileModule } from "svelte/compiler";

plugin({
  name: "account-service-runes",
  setup(build) {
    build.onLoad({ filter: /state\.svelte\.ts$/ }, async ({ path }) => ({
      contents: compileModule(
        new Bun.Transpiler({ loader: "ts" }).transformSync(await Bun.file(path).text()),
        { filename: path, generate: "client" },
      ).js.code,
      loader: "js",
    }));
  },
});

const { getInfraSettings, setUserServices, switchAccount } = await import("./state.svelte.ts");
let requests;
let fetchSpy;
let windowDescriptor;
const service = (accountId) => ({ name: `API ${accountId}`, url: "http://localhost:8080", scope: "account", accountId });
const settled = () => Bun.sleep(0);
const accountFor = (headers) => new Headers(headers).get("Authorization")?.match(/Credential=(\d+)\//)?.[1] ?? "000000000000";

beforeEach(() => {
  windowDescriptor = Object.getOwnPropertyDescriptor(globalThis, "window");
  Object.defineProperty(globalThis, "window", { configurable: true, value: {} });
  requests = [];
  fetchSpy = spyOn(globalThis, "fetch").mockImplementation((url, init) => {
    if (String(url).endsWith("/overview")) return Promise.resolve(Response.json({}));
    // Deliberately ignore aborts to exercise stale responses from a slow server.
    return new Promise((resolve) => requests.push({ init, account: accountFor(init.headers), resolve }));
  });
});

afterEach(async () => {
  switchAccount("000000000000");
  for (const request of requests) request.resolve(Response.json({ services: [] }));
  await settled();
  fetchSpy.mockRestore();
  if (windowDescriptor) Object.defineProperty(globalThis, "window", windowDescriptor);
  else delete globalThis.window;
});

describe("account-scoped additional services", () => {
  test("clears old services on switch and ignores a late account response", async () => {
    switchAccount("111111111111");
    requests[0].resolve(Response.json({ services: [service("111111111111")] }));
    await settled();
    expect(getInfraSettings().userServices[0].accountId).toBe("111111111111");
    switchAccount("222222222222");
    const slowB = requests.at(-1);
    expect(getInfraSettings().userServices).toHaveLength(0);
    expect(getInfraSettings().userServicesLoaded).toBe(false);
    switchAccount("333333333333");
    requests.at(-1).resolve(Response.json({ services: [service("333333333333")] }));
    await settled();
    slowB.resolve(Response.json({ services: [service("222222222222")] }));
    await settled();
    expect(getInfraSettings().userServices[0].accountId).toBe("333333333333");
    expect(getInfraSettings().userServicesLoaded).toBe(true);
  });

  test("a late save stays addressed to its originating account and cannot replace the new list", async () => {
    switchAccount("444444444444");
    requests.at(-1).resolve(Response.json({ services: [] }));
    await settled();
    const savedService = service("444444444444");
    const save = setUserServices([savedService]);
    const saveRequest = requests.at(-1);
    expect(saveRequest.init.method).toBe("PUT");
    expect(saveRequest.account).toBe("444444444444");
    switchAccount("555555555555");
    requests.at(-1).resolve(Response.json({ services: [service("555555555555")] }));
    await settled();
    saveRequest.resolve(Response.json({ services: [savedService] }));
    await save;
    expect(getInfraSettings().userServices[0].accountId).toBe("555555555555");
  });
});
