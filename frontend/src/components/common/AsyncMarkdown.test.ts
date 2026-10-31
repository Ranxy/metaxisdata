import { readdirSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ evaluated: false }));

vi.mock("markstream-vue", () => {
  state.evaluated = true;
  return { default: {} };
});

const here = dirname(fileURLToPath(import.meta.url));
const sourceDir = resolve(here, "../..");

function sourceFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (entry.name === "proto-es" || entry.name === "locales") {
        continue;
      }
      files.push(...sourceFiles(resolve(dir, entry.name)));
      continue;
    }
    if (
      (entry.name.endsWith(".ts") || entry.name.endsWith(".vue")) &&
      !entry.name.endsWith(".test.ts")
    ) {
      files.push(resolve(dir, entry.name));
    }
  }
  return files;
}

describe("the markdown bundle stays out of the static graph", () => {
  it("does not evaluate the renderer just by importing the loader", async () => {
    await import("./AsyncMarkdown");
    expect(state.evaluated).toBe(false);
  });

  it("has no static import of the renderer anywhere else", () => {
    const loader = resolve(here, "AsyncMarkdown.ts");
    const offenders = sourceFiles(sourceDir).filter(
      (file) =>
        file !== loader &&
        /from\s+["']markstream-vue["']/.test(readFileSync(file, "utf8"))
    );
    expect(offenders).toEqual([]);
  });

  it("does not pull the renderer stylesheet into the application entry", () => {
    const entry = resolve(sourceDir, "main.ts");
    expect(readFileSync(entry, "utf8")).not.toContain("markstream-vue");
  });
});
