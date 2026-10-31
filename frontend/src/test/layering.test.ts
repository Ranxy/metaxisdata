import { readFileSync, readdirSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

/**
 * The dependency direction `frontend/AGENTS.md` promises: `pages → components →
 * composables → api → lib → utils → proto-es`, plus the store only read by
 * composables/pages. A helper that reaches upward drags the layer above it into
 * every consumer; the reverse edge is what this pins.
 *
 * `vue-sonner` is deliberately allowed in `lib/`: `notify` is the one toast
 * entry point, and the library is a message bus, not a component.
 */
const here = dirname(fileURLToPath(import.meta.url));
const sourceDir = resolve(here, "..");

interface LayerRule {
  /** Directory under src, without a trailing slash. */
  layer: string;
  /** Module specifiers this layer may not import. */
  forbidden: RegExp;
}

const RULES: LayerRule[] = [
  {
    layer: "utils",
    forbidden: /^(?:vue|vue-router|pinia)$|^@\/(?:api|store|components|pages)\//,
  },
  {
    layer: "lib",
    forbidden: /^(?:vue|pinia)$|^@\/(?:api|store|components|pages)\//,
  },
  {
    layer: "composables",
    forbidden: /^@\/(?:components|pages)\//,
  },
  {
    layer: "api",
    forbidden: /^(?:vue|pinia)$|^@\/(?:store|components|pages)\//,
  },
];

const IMPORT_RE = /(?:from\s+|import\()\s*["']([^"']+)["']/g;

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
      !entry.name.endsWith(".test.ts")
    ) {
      files.push(resolve(dir, entry.name));
    }
  }
  return files;
}

describe("module boundaries", () => {
  for (const rule of RULES) {
    it(`${rule.layer}/ does not import the layers above it`, () => {
      const offenders: string[] = [];
      for (const file of sourceFiles(resolve(sourceDir, rule.layer))) {
        const source = readFileSync(file, "utf8");
        IMPORT_RE.lastIndex = 0;
        let match: RegExpExecArray | null;
        while ((match = IMPORT_RE.exec(source))) {
          if (rule.forbidden.test(match[1])) {
            offenders.push(`${relative(sourceDir, file)}: ${match[1]}`);
          }
        }
      }
      expect(offenders).toEqual([]);
    });
  }
});
