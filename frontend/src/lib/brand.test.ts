import { readdirSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { BRAND_NAME } from "./brand";

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

describe("brand name", () => {
  it("is written the same way everywhere", () => {
    expect(BRAND_NAME).toBe("MetaxisData");
  });

  it("has no hard-coded copy outside this module", () => {
    const owners = sourceFiles(sourceDir).filter(
      (file) =>
        file !== resolve(here, "brand.ts") &&
        // Unquoted template text is the most likely drift, so match the name
        // itself rather than only a quoted literal.
        /MetaxisData/.test(readFileSync(file, "utf8"))
    );
    expect(owners).toEqual([]);
  });

  it("is the name the document title shows", () => {
    const indexHtml = readFileSync(resolve(sourceDir, "index.html"), "utf8");
    expect(indexHtml).toContain(`<title>${BRAND_NAME}</title>`);
  });
});
