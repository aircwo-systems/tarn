import { expect, test } from "bun:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const configFile = fileURLToPath(new URL("../../vite.config.ts", import.meta.url));

test.each([
  ["eventbridge/target-settings", ".settings"],
  ["rack/rc-kv", ".rc-kv"],
])("%s stylesheet can load before its component", async (component, selector) => {
  const server = await createServer({
    configFile,
    logLevel: "silent",
    server: { middlewareMode: true, preTransformRequests: false },
  });

  try {
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
  } finally {
    await server.close();
  }
});
