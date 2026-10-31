import {
  type ComputedRef,
  computed,
  getCurrentScope,
  onScopeDispose,
  type Ref,
  ref,
} from "vue";
import { listMetadata, searchMetadata } from "@/api/database";
import {
  MetaType,
  type SearchMetadataResult,
  type StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

/** One child object at the level being browsed. */
export interface MetadataChild {
  guid: string;
  name: string;
  metaType: MetaType;
  /** The row the child was built from, for a caller that renders a type-specific list. */
  metadata?: StoredMetadata;
}

interface Source {
  metaType: MetaType;
  items: MetadataChild[];
  nextPageToken: string;
}

interface Page {
  items: MetadataChild[];
  nextPageToken: string;
}

export interface MetadataChildrenOptions {
  /** Rows fetched per meta type and page. The backend caps a page at 1000. */
  pageSize?: number;
}

export interface MetadataChildren {
  /** Every loaded child, in the order the requested meta types were given. */
  items: Ref<MetadataChild[]>;
  /** A first page or a new search is in flight. */
  loading: Ref<boolean>;
  /** A follow-up page is in flight; loaded rows stay on screen. */
  loadingMore: Ref<boolean>;
  /** Whether any meta-type source still has a page token. */
  hasNext: ComputedRef<boolean>;
  /** First-page/search failure: nothing meaningful is on screen. */
  error: Ref<unknown>;
  /** Follow-up-page failure: the loaded rows stay usable. */
  moreError: Ref<unknown>;
  /**
   * The search term the loaded rows belong to: `""` for the unfiltered list, a
   * trimmed term for search results. The caller uses it to avoid re-running the
   * query it already shows.
   */
  query: Ref<string>;
  /**
   * Replace the level with the first page of the direct children of
   * `parentGuid`, restricted to `metaTypes` and in that order. An empty list
   * keeps every group the response carried, in the response's order. Returns
   * the loaded children so the caller can decide to fall back to another level,
   * or `null` when a newer request superseded this one — a superseded load says
   * nothing about whether the level is empty.
   */
  load: (
    parentGuid: string,
    metaTypes: MetaType[]
  ) => Promise<MetadataChild[] | null>;
  /**
   * Replace the level with the children of `parentGuid` matching `term`. An
   * empty `metaTypes` is one unscoped search request; a non-empty list searches
   * each type separately so each keeps its own page token.
   */
  search: (
    parentGuid: string,
    metaTypes: MetaType[],
    term: string
  ) => Promise<void>;
  /** Whether one meta type still has a page to load. */
  hasNextFor: (metaType: MetaType) => boolean;
  /**
   * Append the next page of one meta type, or of every meta-type source that has
   * one when no type is named.
   */
  loadMore: (metaType?: MetaType) => Promise<void>;
  /** Forget everything and invalidate requests still on the wire. */
  reset: () => void;
}

/**
 * Cursor pagination plus server-side search for one level of the metadata tree.
 *
 * A registry parent can hold far more children than one page: a schema with a
 * thousand tables, a database full of schemas. Reading one page and filtering it
 * in the browser both truncates the list and searches only the rows already
 * fetched. This owns the paging and the searching instead, so the caller renders
 * what it has and asks for more.
 *
 * The list and the search have different response shapes — `ListMetadata`
 * groups by meta type, `SearchMetadata` returns a flat, guid-ordered result set
 * — so a search runs once per meta type and pages are kept per type. That keeps
 * the display order stable when a level holds tables, views and materialized
 * views together, and it lets "load more" advance every group independently.
 *
 * Requests are ticket-guarded and aborted: a slow answer for a level or term the
 * reader has already left must never overwrite the current one.
 */
export function useMetadataChildren(
  options: MetadataChildrenOptions = {}
): MetadataChildren {
  const pageSize = options.pageSize ?? 50;

  const items = ref<MetadataChild[]>([]) as Ref<MetadataChild[]>;
  const loading = ref(false);
  const loadingMore = ref(false);
  const error = ref<unknown>(undefined);
  const moreError = ref<unknown>(undefined);
  const query = ref("");

  const sources = ref<Source[]>([]);
  const hasNext = computed(() =>
    sources.value.some((source) => source.nextPageToken !== "")
  );

  let generation = 0;
  let controller: AbortController | undefined;
  let parentGuid = "";

  function abortInFlight() {
    controller?.abort();
    controller = undefined;
  }

  /** Claim a ticket and the abort signal for one first-page/search request. */
  function startRequest(): { mine: number; signal: AbortSignal } {
    generation += 1;
    abortInFlight();
    // A follow-up page that was in flight belongs to the superseded level.
    loadingMore.value = false;
    controller = new AbortController();
    return { mine: generation, signal: controller.signal };
  }

  function clear() {
    sources.value = [];
    items.value = [];
  }

  if (getCurrentScope()) {
    onScopeDispose(() => {
      generation += 1;
      abortInFlight();
    });
  }

  async function load(
    parent: string,
    metaTypes: MetaType[]
  ): Promise<MetadataChild[] | null> {
    const { mine, signal } = startRequest();
    parentGuid = parent;
    loading.value = true;
    error.value = undefined;
    moreError.value = undefined;
    query.value = "";
    clear();
    try {
      const response = await listMetadata({
        parentGuid: parent,
        pageSize,
        signal,
      });
      if (mine !== generation || signal.aborted) {
        return null;
      }
      sources.value = toSources(response.typesStoredMetadata, metaTypes);
      items.value = flatten(sources.value);
      return items.value;
    } catch (e: unknown) {
      if (mine !== generation || signal.aborted) {
        return null;
      }
      error.value = e;
      return [];
    } finally {
      if (mine === generation) {
        loading.value = false;
      }
    }
  }

  async function search(
    parent: string,
    metaTypes: MetaType[],
    term: string
  ): Promise<void> {
    const trimmed = term.trim();
    if (trimmed === "") {
      await load(parent, metaTypes);
      return;
    }
    const { mine, signal } = startRequest();
    parentGuid = parent;
    loading.value = true;
    error.value = undefined;
    moreError.value = undefined;
    query.value = trimmed;
    clear();
    try {
      if (metaTypes.length === 0) {
        // An unscoped search is one request: the RPC already merges every type a
        // result can be. Only a typed search fans out, because each type has its
        // own page token.
        const response = await searchMetadata({
          searchStr: trimmed,
          parentGuidPrefix: parent,
          pageSize,
          signal,
        });
        if (mine !== generation || signal.aborted) {
          return;
        }
        sources.value = [
          {
            metaType: MetaType.UNSPECIFIED,
            items: (response.results ?? []).flatMap(searchResultToChild),
            nextPageToken: response.nextPageToken ?? "",
          },
        ];
      } else {
        const responses = await Promise.all(
          metaTypes.map((metaType) =>
            searchMetadata({
              searchStr: trimmed,
              parentGuidPrefix: parent,
              metaType,
              pageSize,
              signal,
            })
          )
        );
        if (mine !== generation || signal.aborted) {
          return;
        }
        sources.value = metaTypes.map((metaType, index) => ({
          metaType,
          items: (responses[index]?.results ?? []).flatMap(searchResultToChild),
          nextPageToken: responses[index]?.nextPageToken ?? "",
        }));
      }
      items.value = flatten(sources.value);
    } catch (e: unknown) {
      if (mine !== generation || signal.aborted) {
        return;
      }
      error.value = e;
    } finally {
      if (mine === generation) {
        loading.value = false;
      }
    }
  }

  async function loadMore(metaType?: MetaType): Promise<void> {
    if (loading.value || loadingMore.value || !hasNext.value) {
      return;
    }
    const pending = sources.value.filter(
      (source) =>
        source.nextPageToken !== "" &&
        (metaType === undefined || source.metaType === metaType)
    );
    if (pending.length === 0) {
      return;
    }
    const mine = generation;
    controller = new AbortController();
    const signal = controller.signal;
    loadingMore.value = true;
    moreError.value = undefined;
    try {
      // One request per source; the answers come back in the same order.
      const pages = await Promise.all(
        pending.map((source) => fetchNextPage(source, signal))
      );
      if (mine !== generation || signal.aborted) {
        return;
      }
      pages.forEach((page, index) => {
        pending[index].items.push(...page.items);
        pending[index].nextPageToken = page.nextPageToken;
      });
      items.value = flatten(sources.value);
    } catch (e: unknown) {
      if (mine !== generation || signal.aborted) {
        return;
      }
      moreError.value = e;
    } finally {
      if (mine === generation) {
        loadingMore.value = false;
      }
    }
  }

  async function fetchNextPage(
    source: Source,
    signal: AbortSignal
  ): Promise<Page> {
    if (query.value === "") {
      const response = await listMetadata({
        parentGuid,
        pageSize,
        pageToken: source.nextPageToken,
        metaType: source.metaType,
        signal,
      });
      const group = response.typesStoredMetadata.find(
        (candidate) => candidate.metaType === source.metaType
      );
      return {
        items: toChildren(group?.list ?? [], source.metaType),
        nextPageToken: group?.nextPageToken ?? "",
      };
    }
    const response = await searchMetadata({
      searchStr: query.value,
      parentGuidPrefix: parentGuid,
      metaType:
        source.metaType === MetaType.UNSPECIFIED ? undefined : source.metaType,
      pageSize,
      pageToken: source.nextPageToken,
      signal,
    });
    return {
      items: response.results.flatMap(searchResultToChild),
      nextPageToken: response.nextPageToken,
    };
  }

  function hasNextFor(metaType: MetaType): boolean {
    return sources.value.some(
      (source) => source.metaType === metaType && source.nextPageToken !== ""
    );
  }

  function reset() {
    generation += 1;
    abortInFlight();
    clear();
    parentGuid = "";
    loading.value = false;
    loadingMore.value = false;
    error.value = undefined;
    moreError.value = undefined;
    query.value = "";
  }

  return {
    items,
    loading,
    loadingMore,
    hasNext,
    error,
    moreError,
    query,
    load,
    search,
    hasNextFor,
    loadMore,
    reset,
  };
}

/** The meta-type groups of a `ListMetadata` response, in the requested order. */
function toSources(
  groups: Array<{
    metaType: MetaType;
    list: StoredMetadata[];
    nextPageToken: string;
  }>,
  metaTypes: MetaType[]
): Source[] {
  if (metaTypes.length === 0) {
    return groups.map((group) => ({
      metaType: group.metaType,
      items: toChildren(group.list, group.metaType),
      nextPageToken: group.nextPageToken,
    }));
  }
  return metaTypes.flatMap((metaType) => {
    const group = groups.find((candidate) => candidate.metaType === metaType);
    if (!group) {
      return [];
    }
    return [
      {
        metaType,
        items: toChildren(group.list, metaType),
        nextPageToken: group.nextPageToken,
      },
    ];
  });
}

function toChildren(
  list: StoredMetadata[],
  metaType: MetaType
): MetadataChild[] {
  return list.flatMap((item) => {
    const child = storedMetadataChild(item, metaType);
    // A row whose name is empty would render as a blank level; drop it so the
    // caller's empty-schema compatibility path can take over.
    return child.name.trim() === "" ? [] : [child];
  });
}

function searchResultToChild(result: SearchMetadataResult): MetadataChild[] {
  const metaType = result.metaType;
  const metadata = result.metadata;
  const name = metadata ? storedMetadataName(metadata) : "";
  return name.trim() === ""
    ? []
    : [{ guid: result.guid, name, metaType, metadata }];
}

function storedMetadataChild(
  item: StoredMetadata,
  metaType: MetaType
): MetadataChild {
  return {
    guid: item.guid,
    name: storedMetadataName(item),
    metaType,
    metadata: item,
  };
}

/** The display name of a listed metadata row, whichever arm carries it. */
function storedMetadataName(item: StoredMetadata): string {
  const value = item.type.value;
  return (value as { name?: string } | undefined)?.name ?? item.guid;
}

function flatten(sources: Source[]): MetadataChild[] {
  return sources.flatMap((source) => source.items);
}
