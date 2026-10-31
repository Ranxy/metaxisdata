import { readdirSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  ALL_PERMISSIONS,
  PERMISSION_GROUPS,
  permissionSuffix,
} from "./permissions";

const here = dirname(fileURLToPath(import.meta.url));

interface Catalog {
  permissions: { name: string; id: string }[];
}

function readCatalog(): Catalog {
  const path = resolve(
    here,
    "../../../backend/common/permission/permission.json"
  );
  return JSON.parse(readFileSync(path, "utf8")) as Catalog;
}

describe("permission catalog mirror", () => {
  it("matches backend/common/permission/permission.json exactly", () => {
    const catalog = readCatalog();
    const byId = (a: string, b: string) => a.localeCompare(b);
    const backendIds = catalog.permissions.map((p) => p.id).sort(byId);
    expect([...ALL_PERMISSIONS].sort(byId)).toEqual(backendIds);
  });

  it("has no duplicate permissions", () => {
    expect(new Set(ALL_PERMISSIONS).size).toBe(ALL_PERMISSIONS.length);
  });

  it("groups every permission under its resource", () => {
    for (const group of PERMISSION_GROUPS) {
      for (const permission of group.permissions) {
        expect(permission.startsWith(`metaxisdata.${group.resource}.`)).toBe(
          true
        );
      }
    }
  });

  it("derives the display suffix from the permission string", () => {
    expect(permissionSuffix("metaxisdata.instances.sync")).toBe("sync");
    expect(permissionSuffix("metaxisdata.llm.profiles.fetchModels")).toBe(
      "profiles.fetchModels"
    );
    // A string that is not a catalog entry is rendered as-is, not mangled.
    expect(permissionSuffix("metaxisdata.instances")).toBe(
      "metaxisdata.instances"
    );
  });
});

/** Every `.ts`/`.vue` file under src that can carry a permission identifier. */
function sourceFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      // Generated proto code and the locale catalogs cannot carry permissions.
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

/**
 * Permission identifiers outside the catalog are invisible: `hasPermission`
 * simply answers false and the UI the string guards never renders, with no
 * error anywhere. The typed signature catches a literal typo at compile time;
 * this scan also catches an identifier parked in a variable or a navigation
 * entry before it reaches a typed field.
 */
describe("permission call sites", () => {
  it("only names catalog permissions", () => {
    const catalog = new Set<string>(ALL_PERMISSIONS);
    const patterns = [
      /hasPermission\(\s*["']([^"']+)["']/g,
      /(?:permission|anyPermission)\s*:\s*["']([^"']+)["']/g,
    ];
    const offenders: string[] = [];
    for (const file of sourceFiles(resolve(here, ".."))) {
      const source = readFileSync(file, "utf8");
      for (const pattern of patterns) {
        pattern.lastIndex = 0;
        let match: RegExpExecArray | null;
        while ((match = pattern.exec(source))) {
          const permission = match[1];
          if (!catalog.has(permission)) {
            offenders.push(
              `${file.slice(resolve(here, "../../..").length + 1)}: ${permission}`
            );
          }
        }
      }
    }
    expect(offenders).toEqual([]);
  });
});
