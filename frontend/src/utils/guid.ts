// A metadata GUID is the `;`-joined resource path of an object
// (`instance;database;schema;table`); a MySQL instance has no schema level, so
// the empty string is a meaningful segment. Routes carry the same value as a
// repeated `:guid` param — one path segment per GUID segment — with `~` standing
// in for an empty segment, because a repeated slash does not survive URL
// normalization.
//
// Always push the array form. Each element keeps its own boundary through the
// URL encoding, so a name containing `/` survives and a name containing `;` is
// not mistaken for a separator; one `/`-joined string loses both.

/** The repeated `:guid` param for GUID segments: `[a, b, "", c]` -> `[a, b, ~, c]`. */
export function guidSegmentsToRouteParams(segments: string[]): string[] {
  return segments.map((segment) => (segment === "" ? "~" : segment));
}

/** The repeated `:guid` param for a `;`-joined GUID. */
export function guidToRouteParams(guid: string): string[] {
  return guidSegmentsToRouteParams(guid.split(";"));
}

/**
 * Decodes a vue-router `:guid` param back into a `;`-joined GUID.
 *
 * vue-router has already percent-decoded the param, so decoding it again
 * corrupts any name containing `%`. A lone part that still holds `/` is either a
 * legacy path push or a hand-typed URL, so it is split: with the array form every
 * GUID segment is its own part, and a single-segment GUID cannot contain `/`
 * (OpenLineage GUIDs percent-encode it server-side).
 */
export function routeParamToGuid(
  param: string | string[] | null | undefined
): string {
  const parts = Array.isArray(param) ? param : param ? [param] : [];
  if (parts.length === 0) {
    return "";
  }
  const segments =
    parts.length === 1 && parts[0].includes("/") ? parts[0].split("/") : parts;
  return segments.map((segment) => (segment === "~" ? "" : segment)).join(";");
}
