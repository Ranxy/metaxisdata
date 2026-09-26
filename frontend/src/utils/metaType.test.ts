import { describe, expect, it } from "vitest";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { metaTypeLabel, parseMetaType } from "./metaType";

/** Stands in for vue-i18n, which the util only needs as a lookup. */
const t = (key: string) => key;

describe("metaTypeLabel", () => {
  it("names every type the app can display", () => {
    expect(metaTypeLabel(MetaType.INSTANCE, t)).toBe(
      "metadataBrowser.instances"
    );
    expect(metaTypeLabel(MetaType.DATABASE, t)).toBe(
      "metadataBrowser.databases"
    );
    expect(metaTypeLabel(MetaType.SCHEMA, t)).toBe("metadataBrowser.schemas");
    expect(metaTypeLabel(MetaType.TABLE, t)).toBe("metadataBrowser.tables");
    expect(metaTypeLabel(MetaType.COLUMN, t)).toBe("metadataBrowser.columns");
    expect(metaTypeLabel(MetaType.VIEW, t)).toBe("metadataBrowser.views");
    expect(metaTypeLabel(MetaType.MATERIALIZED_VIEW, t)).toBe(
      "metadataBrowser.materializedViews"
    );
    expect(metaTypeLabel(MetaType.MANUAL_SQL, t)).toBe(
      "metadataBrowser.manualSqls"
    );
  });

  it("keeps an ingested dataset distinct from a declared external table", () => {
    expect(metaTypeLabel(MetaType.EXTERNAL_TABLE, t)).toBe(
      "metadataBrowser.externalTables"
    );
    expect(metaTypeLabel(MetaType.EXTERNAL_DATASET, t)).toBe(
      "metadataBrowser.externalDatasets"
    );
  });

  it("falls back to a label instead of an enum name", () => {
    expect(metaTypeLabel(MetaType.UNSPECIFIED, t)).toBe(
      "metadataBrowser.other"
    );
  });
});

describe("parseMetaType", () => {
  it("accepts the numbers and strings a route can carry", () => {
    expect(parseMetaType(4)).toBe(MetaType.TABLE);
    expect(parseMetaType("4")).toBe(MetaType.TABLE);
    expect(parseMetaType(["5", "4"])).toBe(MetaType.VIEW);
    expect(parseMetaType("0")).toBe(MetaType.UNSPECIFIED);
  });

  it("rejects what the enum does not define", () => {
    // 15 is the gap between SCHEMA (3) and EXTERNAL_TABLE (16); a cast used to
    // let it through as a type the app then rendered and re-sent.
    expect(parseMetaType(15)).toBeNull();
    expect(parseMetaType("999")).toBeNull();
    expect(parseMetaType("TABLE")).toBeNull();
    expect(parseMetaType("")).toBeNull();
    expect(parseMetaType(" ")).toBeNull();
    expect(parseMetaType(undefined)).toBeNull();
    expect(parseMetaType(null)).toBeNull();
    expect(parseMetaType([])).toBeNull();
  });
});
