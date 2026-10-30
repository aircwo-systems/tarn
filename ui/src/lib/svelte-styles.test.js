import { afterAll, beforeAll, expect, test } from "bun:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const configFile = fileURLToPath(new URL("../../vite.config.ts", import.meta.url));

// Starting Vite takes several seconds on CI runners, past bun's 5s default,
// so the cases share one server and allow for a slow first transform.
const timeout = 60_000;
let server;

beforeAll(async () => {
  server = await createServer({
    configFile,
    logLevel: "silent",
    server: { middlewareMode: true, preTransformRequests: false },
  });
}, timeout);

afterAll(async () => {
  await server?.close();
});

test.each([
  ["eventbridge/target-settings", ".settings"],
  ["rack/rc-kv", ".rc-kv"],
])(
  "%s stylesheet can load before its component",
  async (component, selector) => {
    const result = await server.transformRequest(
      `/src/lib/components/${component}.svelte?svelte&type=style&lang.css`,
    );
    expect(result).not.toBeNull();
    const match = result.code.match(/const __vite__css = (".*")/);
    expect(match).not.toBeNull();
    const css = JSON.parse(match[1]);
    expect(css).toContain(selector);
    expect(css).not.toContain("<script");
    expect(css).not.toContain("type KvItem");
  },
  timeout,
);
