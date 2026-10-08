import type { ExternalDatasetInfo } from "@/types/proto-es/v1/lineage_service_pb";

/**
 * How a lineage object is named: the scope it lives in, its own name, and the
 * path in between.
 *
 * Two objects in different instances can share a database, a schema and a table
 * name, and their GUIDs then differ only in the first segment. A label built
 * from the last segments alone renders the same string for both, which is what
 * `fullLabel` here exists to prevent: it always carries the scope.
 *
 * A GUID is `instance;database;schema;object`, positionally — a MySQL instance
 * has no schema level, so its third segment is empty and its object still sits
 * at index three. Splitting by position rather than counting non-empty segments
 * is what keeps PostgreSQL and MySQL objects named the same way.
 */

/** The prefix the server gives a dataset that belongs to no managed instance. */
const EXTERNAL_PREFIX = "external:";

/** How many accents the `--lineage-scope-*` palette defines. */
export const LINEAGE_SCOPE_COUNT = 6;

/** Whether a GUID names a dataset outside every managed instance. */
export function isExternalGuid(guid: string): boolean {
  return guid.startsWith(EXTERNAL_PREFIX);
}

export interface LineageAssetView {
  /**
   * What the object is coloured and grouped by: the instance resource id, or an
   * external dataset's namespace. Two objects share it if and only if they are
   * in the same instance, so it is the identity the canvas colours.
   */
  scopeKey: string;
  /** What the scope is called: the instance title, or the dataset namespace. */
  scopeLabel: string;
  /** The object's own name: the last GUID segment. */
  name: string;
  /** The path between the scope and the object, e.g. `e2e.e2e_dwd`; may be empty. */
  qualifier: string;
  /** `scopeLabel · qualifier.name`, the label that tells two same-named objects apart. */
  fullLabel: string;
  isExternal: boolean;
}

/**
 * The display view of one LINEAGE GUID. `scopeTitles` maps an instance resource id
 * to its title; an unknown instance falls back to the id, so a label is never
 * empty and never guesses.
 */
export function lineageAssetView(
  guid: string,
  scopeTitles: ReadonlyMap<string, string>,
  external?: ExternalDatasetInfo
): LineageAssetView {
  if (isExternalGuid(guid)) {
    return externalAssetView(guid, external);
  }

  // A trailing empty segment carries no name — `instance;db;` is the database
  // `db`, not an unnamed object under it.
  const segments = guid.split(";");
  while (segments.length > 0 && segments[segments.length - 1] === "") {
    segments.pop();
  }

  const scopeKey = segments[0] ?? "";
  const name = segments.length <= 1 ? scopeKey : segments[segments.length - 1];
  const qualifier = segments.slice(1, -1).filter(Boolean).join(".");
  const scopeLabel = scopeTitles.get(scopeKey) || scopeKey;

  return {
    scopeKey,
    scopeLabel,
    name,
    qualifier,
    fullLabel: joinAssetLabel(scopeLabel, qualifier, name),
    isExternal: false,
  };
}

function externalAssetView(
  guid: string,
  external?: ExternalDatasetInfo
): LineageAssetView {
  const namespace = external?.namespace || namespaceFromExternalGuid(guid);
  const datasetName = external?.name || nameFromExternalGuid(guid) || guid;

  const parts = datasetName.split(".").filter(Boolean);
  const name = parts.length > 0 ? parts[parts.length - 1] : datasetName;
  const qualifier = parts.slice(0, -1).join(".");
  // A namespace we could not derive stays empty rather than echoing the name
  // back: the label then carries the object's own name alone.
  const scopeLabel = namespace;

  return {
    // Namespaced so a namespace that reads like an instance id is still a
    // different scope: the two are not the same object even when they match.
    scopeKey: `${EXTERNAL_PREFIX}${namespace || datasetName}`,
    scopeLabel,
    name,
    qualifier,
    fullLabel: joinAssetLabel(scopeLabel, qualifier, name),
    isExternal: true,
  };
}

function joinAssetLabel(
  scopeLabel: string,
  qualifier: string,
  name: string
): string {
  const path = qualifier ? `${qualifier}.${name}` : name;
  // An opaque scope that is already the whole label (a bare `external:` GUID the
  // response did not describe) is not repeated.
  return scopeLabel && scopeLabel !== path ? `${scopeLabel} · ${path}` : path;
}

/**
 * The namespace of an external GUID. A namespace itself contains `:` (e.g.
 * `postgres://host:5432`), so the split is at the last colon; the response
 * usually carries `ExternalDatasetInfo`, and this is the fallback for when it
 * does not.
 */
function namespaceFromExternalGuid(guid: string): string {
  const rest = guid.substring(EXTERNAL_PREFIX.length);
  const lastColon = rest.lastIndexOf(":");
  return lastColon < 0 ? "" : rest.substring(0, lastColon);
}

function nameFromExternalGuid(guid: string): string {
  const rest = guid.substring(EXTERNAL_PREFIX.length);
  const lastColon = rest.lastIndexOf(":");
  return lastColon < 0 ? rest : rest.substring(lastColon + 1);
}

/**
 * The order scope keys are laid out in before the palette is assigned. It is a
 * code-point order rather than a locale-aware one: two browsers in different
 * locales must not disagree about which instance is which colour.
 */
export function compareScopeKeys(left: string, right: string): number {
  if (left === right) {
    return 0;
  }
  return left < right ? -1 : 1;
}

/**
 * One accent per scope, in the order given. The caller decides that order — the
 * graph lists the workspace instances first and the external namespaces after
 * them — so a scope keeps its colour when the graph grows a dataset from outside
 * every instance, and never collides with one already on screen.
 */
export function buildScopeColorMap(
  scopeKeys: readonly string[]
): Map<string, string> {
  return new Map(
    [...new Set(scopeKeys)].map((key, index) => [
      key,
      `hsl(var(--lineage-scope-${(index % LINEAGE_SCOPE_COUNT) + 1}))`,
    ])
  );
}

/** The accent of a scope, and a neutral fallback for one the map has not seen. */
export function scopeColor(
  colorMap: ReadonlyMap<string, string>,
  scopeKey: string
): string {
  return colorMap.get(scopeKey) ?? "hsl(var(--muted-foreground))";
}
