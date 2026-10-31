/**
 * A default ceiling for one RPC, and the exceptions that legitimately outlive it.
 *
 * Without a ceiling a hung request leaves a page spinning until the reader
 * reloads; the transport has no timeout of its own. LLM generation and schema
 * syncs are the known long paths, so they keep no client-side deadline.
 */
export const DEFAULT_REQUEST_TIMEOUT_MS = 60_000;

const LONG_RUNNING_SERVICE = /\/(?:[^/]+\.)?(?:ExplainSQLService|LLMService)\//;

/** A method that starts or performs a sync, or streams an explanation. */
const LONG_RUNNING_METHOD =
  /(?:Sync[A-Za-z]*|Run[A-Za-z]*|ExplainQualityExecution)$/;

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
 * `signal` plus a deadline, as one abort signal. `AbortSignal.timeout` and
 * `AbortSignal.any` are both part of the platform the SPA targets; combining
 * them keeps the caller's own cancellation working while the ceiling runs.
 */
export function withRequestTimeout(
  signal: AbortSignal | null | undefined,
  timeoutMs: number | undefined
): AbortSignal | undefined {
  if (timeoutMs === undefined) {
    return signal ?? undefined;
  }
  const deadline = AbortSignal.timeout(timeoutMs);
  return signal ? AbortSignal.any([signal, deadline]) : deadline;
}
