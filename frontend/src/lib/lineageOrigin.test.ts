import { describe, expect, it } from "vitest";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { LineageRelation } from "@/types/proto-es/v1/lineage_service_pb";
import {
  countRelationsByOrigin,
  originColor,
  originLabelKey,
  relationOrigin,
} from "./lineageOrigin";

function rel(metaType?: MetaType): LineageRelation {
  return { metaType } as LineageRelation;
}

describe("relationOrigin", () => {
  it("reads an ingested run's meta type as OpenLineage", () => {
    expect(relationOrigin(rel(MetaType.OPENLINEAGE))).toBe("openlineage");
  });

  it("reads a reported statement as application SQL", () => {
    expect(relationOrigin(rel(MetaType.APPLICATION_SQL))).toBe(
      "applicationsql"
    );
  });

  it("reads every analyzer meta type as SQL", () => {
    expect(relationOrigin(rel(MetaType.VIEW))).toBe("sql");
    expect(relationOrigin(rel(MetaType.TABLE))).toBe("sql");
    expect(relationOrigin(rel(MetaType.MATERIALIZED_VIEW))).toBe("sql");
    expect(relationOrigin(rel())).toBe("sql");
  });
});

describe("originColor", () => {
  it("returns each origin's token, faded on request", () => {
    expect(originColor("sql")).toBe("hsl(var(--lineage-sql))");
    expect(originColor("openlineage", 0.5)).toBe(
      "hsl(var(--lineage-openlineage) / 0.5)"
    );
    expect(originColor("applicationsql")).toBe("hsl(var(--lineage-appsql))");
    expect(originColor("mixed")).toBe("hsl(var(--lineage-mixed))");
  });
});

describe("originLabelKey", () => {
  it("names every origin", () => {
    expect(originLabelKey("sql")).toBe("lineageGraph.originSql");
    expect(originLabelKey("openlineage")).toBe(
      "lineageGraph.originOpenlineage"
    );
    expect(originLabelKey("applicationsql")).toBe(
      "lineageGraph.originApplicationSql"
    );
    expect(originLabelKey("mixed")).toBe("lineageGraph.originMixed");
  });
});

describe("countRelationsByOrigin", () => {
  it("counts the relations of each writer", () => {
    expect(
      countRelationsByOrigin([
        rel(MetaType.VIEW),
        rel(MetaType.VIEW),
        rel(MetaType.OPENLINEAGE),
        rel(MetaType.APPLICATION_SQL),
      ])
    ).toEqual({ sql: 2, openlineage: 1, applicationsql: 1 });
  });

  it("reports zeroes for no relations", () => {
    expect(countRelationsByOrigin([])).toEqual({
      sql: 0,
      openlineage: 0,
      applicationsql: 0,
    });
  });
});
