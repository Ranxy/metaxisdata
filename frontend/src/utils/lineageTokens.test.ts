import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { LINEAGE_SCOPE_COUNT } from "./lineageAsset";

/**
 * The lineage accents live in `main.css` and are consumed as `hsl(var(--token))`.
 * A renamed or dropped token is not a type error and no other test reads the
 * stylesheet: the invalid `hsl()` would silently fall back to no stroke, leaving
 * every edge on the canvas invisible while the suite stayed green. This is the
 * one place that ties the two halves together.
 */
const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, "../assets/styles/main.css"), "utf8");
const darkBlock = css.slice(css.indexOf(".dark {"));

function scopeTokens(count: number): string[] {
  return Array.from(
    { length: count },
    (_, index) => `--lineage-scope-${index + 1}:`
  );
}

describe("lineage tokens", () => {
  it("defines every scope accent in both themes", () => {
    for (const token of scopeTokens(LINEAGE_SCOPE_COUNT)) {
      expect(css).toContain(token);
      expect(darkBlock).toContain(token);
    }
  });

  it("defines every origin accent in both themes", () => {
    for (const token of [
      "--lineage-sql:",
      "--lineage-openlineage:",
      "--lineage-mixed:",
    ]) {
      expect(css).toContain(token);
      expect(darkBlock).toContain(token);
    }
  });

  it("gives every token an HSL triplet the `hsl(var(--token))` call accepts", () => {
    const declaration = /--lineage-[a-z0-9-]+:\s*([^;]+);/g;
    const found = [...css.matchAll(declaration)];
    // Both themes declare the same names, so every token appears twice.
    expect(found.length).toBeGreaterThanOrEqual((LINEAGE_SCOPE_COUNT + 3) * 2);
    for (const [, value] of found) {
      expect(value.trim()).toMatch(/^\d+(\.\d+)? \d+(\.\d+)?% \d+(\.\d+)?%$/);
    }
  });
});
