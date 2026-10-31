/**
 * A default ceiling for one RPC, and the exceptions that legitimately outlive it.
 *
 * Without a ceiling a hung request leaves a page spinning until the reader
 * reloads; the transport has no timeout of its own. LLM generation and schema
 * syncs are the known long paths, so they keep no client-side deadline.
 */
export const DEFAULT_REQUEST_TIMEOUT_MS = 60_000;

const LONG_RUNNING_SERVICE = /\/(?:[^/]+\.)?(?:ExplainSQLService|LLMService)\//;

/**
 * Methods that legitimately outlive the default ceiling: provider calls,
 * streaming (the notification push channel), schema syncs, DQ work that runs
 * real database queries, and the exports that walk or render the same rows
 * server-side. The prefix families are exhaustive for those.
 */
const LONG_RUNNING_METHOD =
  /^(?:Batch)?(?:Sync|Run|Suggest|Profile|Preview|Subscribe|Explain|Export)[A-Za-z]*$/;

/** The deadline for the RPC at `path`, or undefined when it has none. */
export function requestTimeoutMs(path: string): number | undefined {
  if (LONG_RUNNING_SERVICE.test(path)) {
    return undefined;
  }
  const method = path.slice(path.lastIndexOf("/") + 1);
  if (LONG_RUNNING_METHOD.test(method)) {
    return undefined;
  }
  return DEFAULT_REQUEST_TIMEOUT_MS;
}

/**
 * One request's abort signal: the caller's own, plus an optional deadline.
 *
 * `AbortSignal.timeout`/`AbortSignal.any` are not available on every browser the
 * SPA targets, and a timeout signal cannot say *why* it aborted, which is how a
 * deadline used to surface as a user cancellation. This arms a timer on a local
 * controller instead, exposes whether the timer fired, and clears both the timer
 * and the caller's listener when the request settles.
 */
export interface RequestDeadline {
  signal: AbortSignal | undefined;
  /** Whether this attempt was ended by the deadline rather than by the caller. */
  timedOut: () => boolean;
  cleanup: () => void;
}

export function createRequestDeadline(
  caller: AbortSignal | null | undefined,
  timeoutMs: number | undefined
): RequestDeadline {
  if (timeoutMs === undefined) {
    return {
      signal: caller ?? undefined,
      timedOut: () => false,
      cleanup: () => {},
    };
  }

  const controller = new AbortController();
  let expired = false;
  const forward = () => controller.abort();
  if (caller) {
    if (caller.aborted) {
      controller.abort();
    } else {
      caller.addEventListener("abort", forward, { once: true });
    }
  }
  const timer = setTimeout(() => {
    expired = true;
    controller.abort();
  }, timeoutMs);

  return {
    signal: controller.signal,
    timedOut: () => expired,
    cleanup: () => {
      clearTimeout(timer);
      caller?.removeEventListener("abort", forward);
    },
  };
}
