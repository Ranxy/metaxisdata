import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

/**
 * The part of the SPA a refactor can quietly break everywhere at once: the
 * pure helpers, the domain modules and the composables every page composes.
 *
 * They are what `coverage.include` measures and what the thresholds below
 * guard. Components and pages are deliberately outside: their tests are worth
 * writing for a real interaction, not for a number, and including 100+ view
 * files would drown the layer this budget is about in a percentage nobody can
 * act on. Because `include` names these files, a new shared module that no test
 * touches is reported at 0% and fails instead of being silently absent.
 */
const SHARED_LAYER = [
  "src/utils/**/*.ts",
  "src/lib/**/*.ts",
  "src/composables/**/*.ts",
];

export default defineConfig({
  plugins: [vue()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    coverage: {
      provider: "v8",
      include: SHARED_LAYER,
      exclude: [
        "**/*.test.ts",
        "**/*.d.ts",
        // Type-only: `Translate` is erased at build time, so there is no
        // runtime code to measure and it would sit at a permanent 0%.
        "src/utils/i18n.ts",
      ],
      reporter: ["text", "html", "json-summary"],
      thresholds: {
        // Per file, not averaged: one well-tested module must not pay for an
        // untested neighbour. The numbers sit just under what the layer
        // actually holds, so ordinary error-path additions have room while a
        // drop below a module's own water line fails the run.
        perFile: true,
        lines: 95,
        functions: 95,
        statements: 95,
        branches: 85,
      },
    },
  },
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
    },
  },
});
