import { watch } from "vue";
import {
  type LocationQuery,
  type LocationQueryRaw,
  useRoute,
  useRouter,
} from "vue-router";
import { type Query, sameQuery } from "@/utils/queryFilters";

export interface UrlFiltersOptions<T> {
  /** The query parameters this page owns; every other parameter is preserved. */
  keys: readonly string[];
  /** The filters a query map carries. */
  parse: (query: LocationQuery) => T;
  /** The parameters one filter set travels under; only non-defaults are named. */
  serialize: (filters: T) => Record<string, string>;
}

export interface UrlFilters<T> {
  /** The filters the URL carries right now. */
  read: () => T;
  /** Whether `filters` is already what this page's parameters say. */
  isCurrent: (filters: T) => boolean;
  /**
   * Replace this page's parameters with `filters`, preserving any parameter the
   * page does not own. An equal query is not written, so a reload or a repeated
   * Apply costs no navigation.
   */
  write: (filters: T) => void;
  /**
   * Call `adopt` when the query changed from outside this composable — a
   * reload, a pasted link, back/forward — but not for the writes it performed
   * itself, so an Apply cannot loop back into a second request.
   */
  watchExternal: (adopt: (filters: T) => void) => void;
}

function mergeQuery(
  keys: readonly string[],
  routeQuery: LocationQuery,
  owned: Record<string, string>
): LocationQueryRaw {
  const next: LocationQueryRaw = { ...routeQuery };
  for (const key of keys) {
    delete next[key];
  }
  Object.assign(next, owned);
  return next;
}

/**
 * One page's filters as URL state.
 *
 * The data-quality directories each carried their own copy of "read the query,
 * write only non-defaults, preserve foreign parameters, ignore our own replace,
 * adopt a pasted link" — about a hundred lines per page, none of it testable
 * because it lived in the view. This owns that state machine; the page keeps
 * only its form model and what to re-fetch.
 */
export function useUrlFilters<T>(options: UrlFiltersOptions<T>): UrlFilters<T> {
  const route = useRoute();
  const router = useRouter();
  // The query a write just installed, so its own navigation is not adopted as an
  // external change. Cleared as soon as that navigation is observed.
  let written: LocationQueryRaw | undefined;

  function read(): T {
    return options.parse(route.query);
  }

  function isCurrent(filters: T): boolean {
    const next = mergeQuery(
      options.keys,
      route.query,
      options.serialize(filters)
    );
    return sameQuery(next as Query, route.query as Query);
  }

  function write(filters: T): void {
    const next = mergeQuery(
      options.keys,
      route.query,
      options.serialize(filters)
    );
    if (sameQuery(next as Query, route.query as Query)) {
      return;
    }
    written = next;
    void router.replace({ query: next });
  }

  function watchExternal(adopt: (filters: T) => void): void {
    watch(
      () => route.query,
      (query) => {
        if (written && sameQuery(written as Query, query as Query)) {
          written = undefined;
          return;
        }
        adopt(options.parse(query));
      }
    );
  }

  return { read, isCurrent, write, watchExternal };
}
