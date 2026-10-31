import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope } from "vue";
import { usePolling } from "./usePolling";

let hidden = false;

beforeEach(() => {
  vi.useFakeTimers();
  hidden = false;
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => (hidden ? "hidden" : "visible"),
  });
});

afterEach(() => {
  vi.useRealTimers();
  delete (document as { visibilityState?: string }).visibilityState;
});

function setHidden(value: boolean) {
  hidden = value;
  document.dispatchEvent(new Event("visibilitychange"));
}

describe("usePolling", () => {
  it("is a no-op while already active", async () => {
    const task = vi.fn();
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => true,
    });

    polling.start();
    polling.start();
    await vi.advanceTimersByTimeAsync(100);
    expect(task).toHaveBeenCalledTimes(1);
    polling.stop();
  });

  it("waits one interval before the first round and stops when asked", async () => {
    let rounds = 0;
    const task = vi.fn(() => {
      rounds += 1;
    });
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => rounds < 2,
    });

    polling.start();
    expect(task).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(100);
    expect(task).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(100);
    expect(task).toHaveBeenCalledTimes(2);
    expect(polling.active.value).toBe(false);
    await vi.advanceTimersByTimeAsync(500);
    expect(task).toHaveBeenCalledTimes(2);
  });

  it("does not start a second round while one is still running", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const task = vi.fn(() => gate);
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => true,
    });

    polling.start();
    await vi.advanceTimersByTimeAsync(100);
    expect(task).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(500);
    expect(task).toHaveBeenCalledTimes(1);

    release();
    await vi.advanceTimersByTimeAsync(100);
    expect(task).toHaveBeenCalledTimes(2);
    polling.stop();
  });

  it("stops once the deadline passes, even while the condition says continue", async () => {
    const task = vi.fn();
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => true,
      timeoutMs: 250,
    });

    polling.start();
    // The round after the deadline settles the loop: a wait that has run out of
    // time is allowed one last read rather than being cut off mid-flight.
    await vi.advanceTimersByTimeAsync(300);
    expect(polling.active.value).toBe(false);
    const rounds = task.mock.calls.length;
    await vi.advanceTimersByTimeAsync(500);
    expect(task.mock.calls.length).toBe(rounds);
  });

  it("pauses while the document is hidden and resumes when visible", async () => {
    hidden = true;
    const task = vi.fn();
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => true,
    });

    polling.start();
    expect(polling.active.value).toBe(true);
    await vi.advanceTimersByTimeAsync(300);
    expect(task).not.toHaveBeenCalled();

    setHidden(false);
    await vi.advanceTimersByTimeAsync(100);
    expect(task).toHaveBeenCalledTimes(1);

    setHidden(true);
    await vi.advanceTimersByTimeAsync(300);
    expect(task).toHaveBeenCalledTimes(1);

    setHidden(false);
    await vi.advanceTimersByTimeAsync(100);
    expect(task).toHaveBeenCalledTimes(2);
    polling.stop();
  });

  it("can keep polling through visibility changes", async () => {
    const task = vi.fn();
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => true,
      pauseWhenHidden: false,
    });

    polling.start();
    setHidden(true);
    await vi.advanceTimersByTimeAsync(100);
    expect(task).toHaveBeenCalledTimes(1);
    polling.stop();
  });

  it("stops on a failure and reports it", async () => {
    const onError = vi.fn();
    const task = vi
      .fn()
      .mockRejectedValue(new ConnectError("boom", Code.Internal));
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => true,
      onError,
    });

    polling.start();
    await vi.advanceTimersByTimeAsync(100);
    expect(onError).toHaveBeenCalledOnce();
    expect(polling.active.value).toBe(false);
  });

  it("keeps polling through failures when asked", async () => {
    const onError = vi.fn();
    const task = vi.fn().mockRejectedValue(new Error("boom"));
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => true,
      continueOnError: true,
      onError,
    });

    polling.start();
    await vi.advanceTimersByTimeAsync(200);
    expect(task).toHaveBeenCalledTimes(2);
    expect(onError).toHaveBeenCalledTimes(2);
    polling.stop();
  });

  it("drops the failure of a round it aborted itself", async () => {
    const onError = vi.fn();
    let signal!: AbortSignal;
    const task = vi
      .fn<(signal: AbortSignal) => Promise<void>>()
      .mockImplementation((roundSignal) => {
        signal = roundSignal;
        return new Promise((_resolve, reject) => {
          roundSignal.addEventListener("abort", () =>
            reject(new ConnectError("canceled", Code.Canceled))
          );
        });
      });
    const polling = usePolling(task, {
      intervalMs: 100,
      shouldContinue: () => true,
      onError,
    });

    polling.start();
    await vi.advanceTimersByTimeAsync(100);
    expect(signal.aborted).toBe(false);
    polling.stop();
    expect(signal.aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(100);
    expect(onError).not.toHaveBeenCalled();
  });

  it("resolves until() when polling stops", async () => {
    const polling = usePolling(() => {}, {
      intervalMs: 100,
      shouldContinue: () => false,
    });

    polling.start();
    const done = polling.until();
    await vi.advanceTimersByTimeAsync(100);
    await expect(done).resolves.toBeUndefined();
  });

  it("resolves until() immediately when nothing is running", async () => {
    const polling = usePolling(() => {}, {
      intervalMs: 100,
      shouldContinue: () => true,
    });
    await expect(polling.until()).resolves.toBeUndefined();
  });

  it("stops when the owning scope is disposed", async () => {
    const task = vi.fn();
    const scope = effectScope();
    const polling = scope.run(() =>
      usePolling(task, { intervalMs: 100, shouldContinue: () => true })
    );
    polling?.start();
    expect(polling?.active.value).toBe(true);

    scope.stop();
    expect(polling?.active.value).toBe(false);
    await vi.advanceTimersByTimeAsync(300);
    expect(task).not.toHaveBeenCalled();
  });
});
