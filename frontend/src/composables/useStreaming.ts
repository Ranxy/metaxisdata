import { getCurrentScope, onScopeDispose } from "vue";
import { isAborted } from "@/utils/error";

export interface StreamingOptions<T, Args extends unknown[]> {
  /** Open the stream for one run; the signal aborts when this run is replaced. */
  open: (signal: AbortSignal, ...args: Args) => AsyncIterable<T>;
  /** Deliver one frame of the newest run. */
  onChunk: (chunk: T) => void;
  /** Clear the displayed result before a run starts. */
  onReset?: () => void;
  /** Report a failure that is not an abort. */
  onError: (error: unknown) => void;
  /** Called when the newest run ends without being superseded. */
  onDone?: () => void;
}

export interface Streaming<Args extends unknown[]> {
  /** Start a run, aborting the stream it replaces. */
  run: (...args: Args) => Promise<void>;
  /** Abandon the run in flight; its frames no longer reach the handlers. */
  cancel: () => void;
}

/**
 * The lifecycle of one server-streamed answer: reset the view, consume frames,
 * and let only the newest run write.
 *
 * A streaming answer and the state it fills used to share one lifetime, so a
 * reader switching executions could see two answers interleaved, and leaving
 * the page kept the generation (and its LLM cost) alive. This takes a
 * generation before each run, aborts the run it replaces and on scope disposal,
 * and drops frames of a superseded run even when they arrive after the abort.
 */
export function useStreaming<T, Args extends unknown[] = []>(
  options: StreamingOptions<T, Args>
): Streaming<Args> {
  let generation = 0;
  let inFlight: AbortController | undefined;

  function cancel() {
    generation += 1;
    inFlight?.abort();
    inFlight = undefined;
  }

  async function run(...args: Args) {
    const mine = ++generation;
    inFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;
    options.onReset?.();
    try {
      for await (const chunk of options.open(controller.signal, ...args)) {
        if (mine !== generation) {
          return;
        }
        options.onChunk(chunk);
      }
    } catch (error) {
      if (mine !== generation || isAborted(error)) {
        return;
      }
      options.onError(error);
    } finally {
      if (mine === generation) {
        inFlight = undefined;
        options.onDone?.();
      }
    }
  }

  if (getCurrentScope()) {
    // Leaving the page must stop paying for a generation the server is still
    // producing, whatever the component's last state was.
    onScopeDispose(cancel);
  }

  return { run, cancel };
}
