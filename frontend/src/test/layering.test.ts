import { readdirSync, readFileSync } from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

/**
 * The dependency direction `frontend/AGENTS.md` promises: `pages → components →
 * composables → api → lib → utils → proto-es`, plus the store only read by
 * composables/pages. A helper that reaches upward drags the layer above it into
 * every consumer; the reverse edge is what this pins.
 *
 * Relative upward imports are resolved before matching, side-effect imports and
 * re-exports are checked too, and `vue-sonner` is deliberately allowed only in
 * `lib/notify.ts` (the one toast entry point, a message bus rather than a
 * component).
 */
const here = dirname(fileURLToPath(import.meta.url));
const sourceDir = resolve(here, "..");

interface LayerRule {
  /** Directory under src, without a trailing slash. */
  layer: string;
  /** Module specifiers this layer may not import. */
  forbidden: RegExp;
  /** Specifiers this layer may import even though the pattern matches. */
  allow?: RegExp;
}

const RULES: LayerRule[] = [
  {
    layer: "utils",
    forbidden:
      /^(?:vue|vue-router|pinia)$|^@\/(?:api|store|components|pages|lib|composables)\//,
  },
  {
    layer: "lib",
    forbidden:
      /^(?:vue|pinia)$|^@\/(?:api|store|components|pages|composables)\//,
    allow: /^vue-sonner$/,
  },
  {
    layer: "composables",
    forbidden: /^@\/(?:components|pages)\//,
  },
  {
    layer: "api",
    forbidden: /^(?:vue|pinia)$|^@\/(?:store|components|pages|composables)\//,
  },
  {
    layer: "components",
    forbidden: /^@\/pages\//,
  },
  {
    layer: "store",
    forbidden: /^@\/(?:components|pages)\//,
  },
];

const IMPORT_PATTERNS = [
  // import … from "x", import type … from "x", export … from "x"
  /(?<![A-Za-z0-9_$])(?:import|export)\s+(?:type\s+)?[^"'`]*?\bfrom\s+["']([^"']+)["']/g,
  // import("x")
  /\bimport\s*\(\s*["']([^"']+)["']\s*\)/g,
  // side-effect import "x"
  /(?<![A-Za-z0-9_$])import\s+["']([^"']+)["']/g,
];

function sourceFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (entry.name === "proto-es") {
        continue;
      }
      files.push(...sourceFiles(resolve(dir, entry.name)));
      continue;
    }
    if (
      (entry.name.endsWith(".ts") || entry.name.endsWith(".vue")) &&
      !entry.name.endsWith(".test.ts") &&
      entry.name !== "layering.test.ts"
    ) {
      files.push(resolve(dir, entry.name));
    }
  }
  return files;
}

/** The alias-shaped path a specifier points at, resolving relative imports. */
function specifierTarget(file: string, specifier: string): string {
  if (!specifier.startsWith(".")) {
    return specifier;
  }
  const absolute = resolve(dirname(file), specifier);
  const rel = relative(sourceDir, absolute).split(sep).join("/");
  if (rel.startsWith("..")) {
    return rel;
  }
  return `@/${rel}${rel.endsWith("/") ? "" : "/"}`;
}

function importsOf(source: string): string[] {
  const found: string[] = [];
  for (const pattern of IMPORT_PATTERNS) {
    pattern.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(source))) {
      found.push(match[1]);
    }
  }
  return found;
}

describe("module boundaries", () => {
  for (const rule of RULES) {
    it(`${rule.layer}/ does not import the layers above it`, () => {
      const offenders: string[] = [];
      for (const file of sourceFiles(resolve(sourceDir, rule.layer))) {
        for (const specifier of importsOf(readFileSync(file, "utf8"))) {
          const target = specifierTarget(file, specifier);
          if (rule.forbidden.test(target) && !rule.allow?.test(target)) {
            offenders.push(`${relative(sourceDir, file)}: ${specifier}`);
          }
        }
      }
      expect(offenders).toEqual([]);
    });
  }

  it("only the toast entry point and its shadcn wrapper import the toast library", () => {
    const allowed = new Set([
      resolve(sourceDir, "lib/notify.ts"),
      resolve(sourceDir, "components/ui/sonner/index.ts"),
      resolve(sourceDir, "components/ui/sonner/Sonner.vue"),
    ]);
    const offenders = sourceFiles(sourceDir)
      .filter((file) => !allowed.has(file))
      .filter((file) =>
        importsOf(readFileSync(file, "utf8")).includes("vue-sonner")
      )
      .map((file) => relative(sourceDir, file));
    expect(offenders).toEqual([]);
  });
});
