import { getCurrentScope, onScopeDispose, type Ref, ref } from "vue";
import { isAborted } from "@/utils/error";

export interface AsyncOptions<T> {
  /** Load once when the owning scope sets this up. Defaults to true. */
  immediate?: boolean;
  /** The value `data` holds until the first load resolves. */
  initialData?: T;
  /** Called for every failure except a superseded (aborted) request. */
  onError?: (error: unknown) => void;
}

export interface Async<T> {
  data: Ref<T | undefined>;
  loading: Ref<boolean>;
  /** The failure of the newest request; cleared when a newer one starts. */
  error: Ref<unknown>;
  /** Load again, superseding any request already in flight. */
  refresh: () => Promise<void>;
  /** Abandon the request in flight; the loaded data stays. */
  cancel: () => void;
}

/**
 * One resource's request state: loading, the value, its failure and a refresh.
 *
 * A detail page or a settings form used to hand-roll this with `isLoading` and
 * `try/catch`, which left every such page with its own answer to "does a slow
 * answer overwrite a newer one?" — usually none. This takes a ticket before each
 * request, lets only the newest ticket write state, and aborts the request it
 * replaces so it does not stay on the wire. Leaving the scope aborts too.
 */
export function useAsync<T>(
  load: (signal: AbortSignal) => Promise<T>,
  options: AsyncOptions<T> = {}
): Async<T> {
  const data = ref<T | undefined>(options.initialData) as Ref<T | undefined>;
  const loading = ref(false);
  const error = ref<unknown>(undefined);

  let ticket = 0;
  let inFlight: AbortController | undefined;

  async function refresh() {
    ticket += 1;
    const mine = ticket;
    inFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;

    loading.value = true;
    error.value = undefined;
    try {
      const value = await load(controller.signal);
      if (mine !== ticket) {
        return;
      }
      data.value = value;
    } catch (e) {
      if (mine !== ticket || isAborted(e)) {
        return;
      }
      error.value = e;
      options.onError?.(e);
    } finally {
      if (mine === ticket) {
        loading.value = false;
        inFlight = undefined;
      }
    }
  }

  function cancel() {
    ticket += 1;
    inFlight?.abort();
    inFlight = undefined;
    loading.value = false;
  }

  if (getCurrentScope()) {
    // A page the reader left must not keep a request on the wire.
    onScopeDispose(() => {
      inFlight?.abort();
      inFlight = undefined;
    });
  }

  if (options.immediate !== false) {
    void refresh();
  }

  return { data, loading, error, refresh, cancel };
}
