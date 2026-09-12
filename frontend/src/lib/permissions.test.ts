import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  ALL_PERMISSIONS,
  isKnownPermission,
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

  it("recognises catalog entries and rejects unknown strings", () => {
    expect(isKnownPermission("metaxisdata.iam.setPolicy")).toBe(true);
    expect(isKnownPermission("metaxisdata.nope.nope")).toBe(false);
    expect(isKnownPermission("")).toBe(false);
  });

  it("derives the display suffix from the permission string", () => {
    expect(permissionSuffix("metaxisdata.instances.sync")).toBe("sync");
    expect(permissionSuffix("metaxisdata.llm.profiles.fetchModels")).toBe(
      "profiles.fetchModels"
    );
  });
});
