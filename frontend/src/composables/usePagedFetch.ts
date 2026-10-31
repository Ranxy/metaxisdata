import {
  type ComputedRef,
  computed,
  getCurrentScope,
  onScopeDispose,
  type Ref,
  ref,
} from "vue";
import type { ListPage } from "@/api/list";
import { isAborted } from "@/utils/error";

export interface PagedFetchOptions<T> {
  /**
   * One page of the list; `pageToken` is `""` for the first page and the signal
   * aborts a request the pager has already superseded, or a polling round ended
   * by its caller.
   */
  fetchPage: (pageToken: string, signal: AbortSignal) => Promise<ListPage<T>>;
  /** Called for every failure except a superseded (aborted) request. */
  onError?: (error: unknown) => void;
}

/** A caller-owned abort signal, e.g. the round of a polling loop. */
export interface FetchRequest {
  signal?: AbortSignal;
}

export interface PagedFetch<T> {
  items: Ref<T[]>;
  isLoading: Ref<boolean>;
  hasNext: ComputedRef<boolean>;
  hasPrevious: ComputedRef<boolean>;
  /** Back to the first page, discarding the visited history. */
  reset: (request?: FetchRequest) => Promise<void>;
  /** Reload the page currently shown. */
  refresh: (request?: FetchRequest) => Promise<void>;
  goNext: () => Promise<void>;
  goPrevious: () => Promise<void>;
}

/**
 * Cursor pagination for a list RPC: one page at a time, with a back stack and a
 * guard against out-of-order answers.
 *
 * Clicking Next twice used to race: the older response could land last and leave
 * the table showing a page the user had already left. Every load takes a ticket,
 * only the newest ticket may write state, and the request it replaces is aborted
 * so it does not stay on the wire.
 */
export function usePagedFetch<T>(options: PagedFetchOptions<T>): PagedFetch<T> {
  const items = ref<T[]>([]) as Ref<T[]>;
  const isLoading = ref(false);
  const currentPageToken = ref("");
  const nextPageToken = ref("");
  const previousPageTokens = ref<string[]>([]);

  let ticket = 0;
  let inFlight: AbortController | undefined;

  async function load(pageToken: string, signal?: AbortSignal) {
    // A round cancelled before it started must not reach the wire.
    if (signal?.aborted) {
      return;
    }
    ticket += 1;
    const mine = ticket;
    inFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;
    // A caller's signal (a polling round going away) aborts this request too.
    const forwardAbort = () => controller.abort();
    signal?.addEventListener("abort", forwardAbort, { once: true });

    isLoading.value = true;
    try {
      const page = await options.fetchPage(pageToken, controller.signal);
      // An aborted request's answer must not land, whoever aborted it.
      if (mine !== ticket || controller.signal.aborted) {
        return;
      }
      items.value = page.items;
      currentPageToken.value = pageToken;
      nextPageToken.value = page.nextPageToken;
    } catch (error) {
      if (mine !== ticket || isAborted(error)) {
        return;
      }
      nextPageToken.value = "";
      options.onError?.(error);
    } finally {
      signal?.removeEventListener("abort", forwardAbort);
      if (mine === ticket) {
        isLoading.value = false;
      }
    }
  }

  if (getCurrentScope()) {
    // A page the user left must not keep a request on the wire.
    onScopeDispose(() => {
      inFlight?.abort();
      inFlight = undefined;
    });
  }

  return {
    items,
    isLoading,
    hasNext: computed(() => nextPageToken.value !== ""),
    hasPrevious: computed(() => previousPageTokens.value.length > 0),

    async reset(request?: FetchRequest) {
      previousPageTokens.value = [];
      await load("", request?.signal);
    },

    async refresh(request?: FetchRequest) {
      await load(currentPageToken.value, request?.signal);
    },

    async goNext() {
      if (!nextPageToken.value) {
        return;
      }
      previousPageTokens.value.push(currentPageToken.value);
      await load(nextPageToken.value);
    },

    async goPrevious() {
      const token = previousPageTokens.value.pop();
      if (token === undefined) {
        return;
      }
      await load(token);
    },
  };
}
