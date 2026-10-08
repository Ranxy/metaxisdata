import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { LineageRelation } from "@/types/proto-es/v1/lineage_service_pb";

/**
 * Where a lineage relation came from, which is what the canvas colours it by.
 *
 * Two writers fill the stored graph. The analyzers read the SQL definition of an
 * object inside a managed instance (a view, a materialized view, a manual SQL)
 * and replace that object's edges. OpenLineage ingestion replaces the edges of
 * an ingested run. A stored relation carries the meta type of whichever one
 * wrote it, so the two are told apart without a server change.
 *
 * An edge in the canvas bundles the relations of one object pair, so it can be
 * written by both — hence `mixed`.
 */
export type LineageOrigin = "sql" | "openlineage" | "mixed";

/** A relation's own origin: an edge is only `mixed` once its relations are bundled. */
export type RelationOrigin = Exclude<LineageOrigin, "mixed">;

/** The `--lineage-*` triplet each origin is drawn with. */
const ORIGIN_TRIPLET: Record<LineageOrigin, string> = {
  sql: "var(--lineage-sql)",
  openlineage: "var(--lineage-openlineage)",
  mixed: "var(--lineage-mixed)",
};

/** The stroke colour of an origin, optionally faded — the canvas draws a
 * highlighted edge at full strength and every other one at 65%. */
export function originColor(origin: LineageOrigin, alpha = 1): string {
  const triplet = ORIGIN_TRIPLET[origin];
  return alpha >= 1 ? `hsl(${triplet})` : `hsl(${triplet} / ${alpha})`;
}

/**
 * The stroke pattern that backs each colour up, so the origin survives a
 * colour-blind reader or a greyscale print.
 */
export const LINEAGE_ORIGIN_DASH: Record<LineageOrigin, string | undefined> = {
  sql: undefined,
  openlineage: "7 5",
  mixed: "2 3",
};

/** The origins a legend lists, in the order it lists them. */
export const LINEAGE_ORIGINS: RelationOrigin[] = ["sql", "openlineage"];

/** Which writer stored a relation. */
export function relationOrigin(relation: LineageRelation): RelationOrigin {
  return Number(relation.metaType) === MetaType.OPENLINEAGE
    ? "openlineage"
    : "sql";
}

/**
 * The i18n key naming an origin. A switch of literal keys rather than a lookup
 * table: the i18n checks follow lookup calls, but not keys held in a map value.
 */
export function originLabelKey(origin: LineageOrigin): string {
  switch (origin) {
    case "sql":
      return "lineageGraph.originSql";
    case "openlineage":
      return "lineageGraph.originOpenlineage";
    case "mixed":
      return "lineageGraph.originMixed";
  }
}

/** How many of the given relations each writer stored. */
export function countRelationsByOrigin(
  relations: readonly LineageRelation[]
): Record<RelationOrigin, number> {
  const counts: Record<RelationOrigin, number> = { sql: 0, openlineage: 0 };
  for (const relation of relations) {
    counts[relationOrigin(relation)] += 1;
  }
  return counts;
}
