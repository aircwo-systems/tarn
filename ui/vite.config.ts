import { sveltekit } from "@sveltejs/kit/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "");
  const proxyTarget = normalizeProxyTarget(
    env.TARN_UI_PROXY_TARGET || "http://127.0.0.1:4566",
  );
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
    plugins: [tailwindcss(), sveltekit()],
    server: {
      proxy,
    },
    preview: {
      proxy,
    },
  };
});

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
