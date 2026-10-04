import { sveltekit } from "@sveltejs/kit/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig, loadEnv, type Plugin, type ViteDevServer } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "");
  const proxyTarget = normalizeProxyTarget(env.TARN_UI_PROXY_TARGET || "http://127.0.0.1:4566");
  const proxy = {
    "/_tarn": {
      target: proxyTarget,
      changeOrigin: false,
    },
    "/_s3": {
      target: proxyTarget,
      changeOrigin: false,
    },
  };

  return {
    plugins: [svelteStyles(), tailwindcss(), sveltekit()],
    server: {
      proxy,
    },
    preview: {
      proxy,
    },
  };
});

function svelteStyles(): Plugin {
  let server: ViteDevServer | undefined;

  return {
    name: "svelte-styles",
    apply: "serve",
    enforce: "pre",
    configureServer(devServer) {
      server = devServer;
    },
    async load(id) {
      const [filename, query] = id.split("?", 2);
      if (!server || !filename.endsWith(".svelte") || !query) return;

      const params = new URLSearchParams(query);
      if (!params.has("svelte") || params.get("type") !== "style" || params.has("raw")) return;

      // Svelte's style loader needs the component compiled before it can return cached CSS.
      // Otherwise Vite loads the whole .svelte file and Tailwind tries to parse it as CSS.
      await server.transformRequest(filename);
    },
  };
}

function normalizeProxyTarget(raw: string): string {
  try {
    const url = new URL(raw);
    if (url.hostname === "0.0.0.0" || url.hostname === "::" || url.hostname === "[::]") {
      url.hostname = "127.0.0.1";
    }
    return url.toString();
  } catch {
    return raw;
  }
}
