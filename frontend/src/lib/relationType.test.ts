import { describe, expect, it } from "vitest";
import enUS from "@/locales/en-US.json";
import zhCN from "@/locales/zh-CN.json";
import { RelationType } from "@/types/proto-es/v1/lineage_service_pb";
import { relationTypeKey } from "./relationType";

const CATALOGS: Record<string, unknown> = { "en-US": enUS, "zh-CN": zhCN };

function label(catalog: unknown, key: string): unknown {
  return key.split(".").reduce<unknown>((node, part) => {
    if (typeof node !== "object" || node === null) {
      return undefined;
    }
    return (node as Record<string, unknown>)[part];
  }, catalog);
}

describe("relationTypeKey", () => {
  it("labels every kind a producer can store", () => {
    expect(relationTypeKey(RelationType.DIRECT)).toBe(
      "metadataBrowser.relationDirect"
    );
    expect(relationTypeKey(RelationType.INDIRECT)).toBe(
      "metadataBrowser.relationIndirect"
    );
    expect(relationTypeKey(RelationType.JOIN)).toBe(
      "metadataBrowser.relationJoin"
    );
    expect(relationTypeKey(RelationType.GROUP)).toBe(
      "metadataBrowser.relationGroup"
    );
    expect(relationTypeKey(RelationType.UNION)).toBe(
      "metadataBrowser.relationUnion"
    );
    expect(relationTypeKey(RelationType.INTERSECT)).toBe(
      "metadataBrowser.relationIntersect"
    );
    expect(relationTypeKey(RelationType.EXCEPT)).toBe(
      "metadataBrowser.relationExcept"
    );
    expect(relationTypeKey(RelationType.UNKNOWN)).toBe(
      "metadataBrowser.relationUnknown"
    );
  });

  it("has no label for a value without a kind", () => {
    expect(
      relationTypeKey(RelationType.RELATION_TYPE_UNSPECIFIED)
    ).toBeUndefined();
    expect(relationTypeKey(99)).toBeUndefined();
  });

  // The static i18n checks cannot follow a key a helper returns, so this is what
  // keeps the labels and the catalogs from drifting apart.
  it("names a key every catalog defines", () => {
    const kinds = [
      RelationType.DIRECT,
      RelationType.INDIRECT,
      RelationType.JOIN,
      RelationType.GROUP,
      RelationType.UNION,
      RelationType.INTERSECT,
      RelationType.EXCEPT,
      RelationType.UNKNOWN,
    ];
    for (const kind of kinds) {
      const key = relationTypeKey(kind);
      expect(key, `relation type ${kind} has no label key`).toBeDefined();
      for (const [locale, catalog] of Object.entries(CATALOGS)) {
        expect(
          label(catalog, key as string),
          `${locale} is missing ${key}`
        ).toEqual(expect.any(String));
      }
    }
  });
});
