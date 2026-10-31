/**
 * The pure half of URL filters: read one query value, validate an enum-ish
 * filter, and compare two queries. Pages keep the URL itself in
 * `composables/useUrlFilters`; nothing here touches Vue, so the parsing rules
 * can be tested without mounting a component.
 */

/** One query parameter's value, as `useRoute().query` exposes it. */
export type QueryValue = string | null | undefined;

/** A query string map: one value per parameter, or repeated values. */
export type Query = Record<string, QueryValue | QueryValue[]>;

/** The first value of a parameter, or `""` when the URL does not carry it. */
export function queryString(value: QueryValue | QueryValue[]): string {
  if (Array.isArray(value)) {
    return String(value[0] ?? "");
  }
  return value === undefined || value === null ? "" : String(value);
}

/** A boolean flag: the URL writes `1` for true and omits the parameter for false. */
export function readFlag(value: QueryValue | QueryValue[]): boolean {
  return queryString(value) === "1";
}

/**
 * A numeric enum filter from the URL. Anything the enum lacks — a stale link, a
 * hand-edited parameter, the `-` the selects use for "any" — reads as "any".
 */
export function enumFilterValue(
  value: QueryValue | QueryValue[],
  members: readonly number[]
): string {
  const raw = queryString(value).trim();
  if (!raw || raw === "-") {
    return "-";
  }
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed > 0 && members.includes(parsed)
    ? String(parsed)
    : "-";
}

/**
 * A select's value as an enum member. `-` and any value the enum no longer has
 * mean the enum's first member (its `UNSPECIFIED`), so a stale URL cannot break
 * a request.
 */
export function enumFromFilter<T extends number>(
  value: string,
  members: readonly T[]
): T {
  const parsed = Number(value);
  return members.includes(parsed as T) ? (parsed as T) : members[0];
}

/** Whether two query maps carry the same parameters and values. */
export function sameQuery(next: Query, current: Query): boolean {
  const keys = Object.keys(next);
  if (keys.length !== Object.keys(current).length) {
    return false;
  }
  return keys.every(
    (key) => queryString(next[key]) === queryString(current[key])
  );
}
