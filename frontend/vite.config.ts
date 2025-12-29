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
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          "monaco-editor": ["monaco-editor"],
        },
      },
    },
  },
  worker: {
    format: "es",
  },
  server: {
    port: 3000,
    proxy: {
      "/v1": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/metaxisdata.v1": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
