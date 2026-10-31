import { getCurrentScope, onScopeDispose, type Ref, ref } from "vue";
import { isAborted } from "@/utils/error";

export interface PollingOptions {
  /** Delay between the end of one round and the start of the next, in ms. */
  intervalMs: number;
  /** Whether another round should run; asked after every completed round. */
  shouldContinue: () => boolean;
  /** Pause while the tab is hidden, resuming when it becomes visible. Defaults to true. */
  pauseWhenHidden?: boolean;
  /** Stop after this many milliseconds, even while `shouldContinue` says yes. */
  timeoutMs?: number;
  /** Keep polling after a failed round instead of stopping. Defaults to false. */
  continueOnError?: boolean;
  /** Called for every failure except a round aborted by `stop`. */
  onError?: (error: unknown) => void;
}

export interface Polling {
  /** Begin polling; a no-op while already active. */
  start: () => void;
  /** Stop scheduling rounds and abort the round in flight. */
  stop: () => void;
  /** Whether polling is on; it may be paused while the tab is hidden. */
  active: Ref<boolean>;
  /** Resolves once polling stops for any reason (condition, deadline, stop). */
  until: () => Promise<void>;
}

/**
 * A recurring async task with a stopping condition and a visibility pause.
 *
 * The six hand-written timers it replaces each decided their own interval,
 * stop rule and failure policy, none of them noticed a background tab, and the
 * interval-based ones could overlap a slow round. This schedules the next round
 * only after the previous one settles, aborts the in-flight one on stop, and
 * pauses while the document is hidden so a background tab stops generating RPC
 * traffic the reader cannot see.
 */
export function usePolling(
  task: (signal: AbortSignal) => Promise<void> | void,
  options: PollingOptions
): Polling {
  const { intervalMs, shouldContinue, pauseWhenHidden = true } = options;
  const active = ref(false);

  let timer: ReturnType<typeof setTimeout> | undefined;
  let deadlineTimer: ReturnType<typeof setTimeout> | undefined;
  let controller: AbortController | undefined;
  let deadline: number | undefined;
  let waiters: Array<() => void> = [];

  function clearTimer() {
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
  }

  function clearDeadlineTimer() {
    if (deadlineTimer !== undefined) {
      clearTimeout(deadlineTimer);
      deadlineTimer = undefined;
    }
  }

  function documentHidden(): boolean {
    return (
      typeof document !== "undefined" && document.visibilityState === "hidden"
    );
  }

  function onVisibilityChange() {
    if (documentHidden()) {
      // The round in flight is allowed to finish; no new one starts until the
      // tab is visible again, when this listener schedules it.
      clearTimer();
      return;
    }
    if (timer === undefined && controller === undefined) {
      schedule();
    }
  }

  function stop() {
    active.value = false;
    clearTimer();
    clearDeadlineTimer();
    controller?.abort();
    controller = undefined;
    deadline = undefined;
    if (typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", onVisibilityChange);
    }
    const pending = waiters;
    waiters = [];
    for (const resolve of pending) {
      resolve();
    }
  }

  function schedule() {
    if (deadline !== undefined && Date.now() >= deadline) {
      stop();
      return;
    }
    if (pauseWhenHidden && documentHidden()) {
      // `onVisibilityChange` resumes the loop.
      return;
    }
    clearTimer();
    timer = setTimeout(() => {
      timer = undefined;
      void run();
    }, intervalMs);
  }

  async function run() {
    const current = new AbortController();
    controller = current;
    try {
      await task(current.signal);
    } catch (e) {
      if (!current.signal.aborted && !isAborted(e)) {
        options.onError?.(e);
        if (!options.continueOnError) {
          stop();
          return;
        }
      }
    } finally {
      if (controller === current) {
        controller = undefined;
      }
    }
    if (!active.value) {
      return;
    }
    if (!shouldContinue()) {
      stop();
      return;
    }
    schedule();
  }

  function start() {
    if (active.value) {
      return;
    }
    active.value = true;
    deadline =
      options.timeoutMs === undefined
        ? undefined
        : Date.now() + options.timeoutMs;
    // A deadline must expire even while a hidden tab pauses the rounds.
    if (options.timeoutMs !== undefined) {
      clearDeadlineTimer();
      deadlineTimer = setTimeout(stop, options.timeoutMs);
    }
    if (typeof document !== "undefined" && pauseWhenHidden) {
      document.addEventListener("visibilitychange", onVisibilityChange);
    }
    schedule();
  }

  function until(): Promise<void> {
    if (!active.value) {
      return Promise.resolve();
    }
    return new Promise((resolve) => {
      waiters.push(resolve);
    });
  }

  if (getCurrentScope()) {
    // A page the reader left must stop producing work the reader cannot see.
    onScopeDispose(stop);
  }

  return { start, stop, active, until };
}
