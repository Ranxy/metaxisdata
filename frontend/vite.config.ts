import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
    },
  },
  worker: {
    format: "es",
  },
  server: {
    port: 3000,
    proxy: {
      "/v1": {
        target: "http://localhost:8083",
        changeOrigin: true,
      },
      "/metaxisdata.v1": {
        target: "http://localhost:8083",
        changeOrigin: true,
      },
      // The OAuth flow reaches the browser as redirects, not as RPCs:
      // /oauth/authorize sends it to the consent page, which then navigates to
      // /oauth/authorize/complete, and a client reads its metadata from
      // /.well-known before it starts. Those paths are same-origin in
      // production, so the dev server has to forward them to the backend too.
      // `/oauth/consent` is the exception: it is a page of this SPA, so it falls
      // through to Vite's own SPA fallback instead of a backend that has no
      // route for it.
      "/oauth": {
        target: "http://localhost:8083",
        changeOrigin: true,
        bypass: (req) =>
          req.url?.startsWith("/oauth/consent") ? req.url : undefined,
      },
      "/.well-known": {
        target: "http://localhost:8083",
        changeOrigin: true,
      },
    },
  },
});
