import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope } from "vue";
import { useDebounced, useOneShot } from "./useTimers";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useDebounced", () => {
  it("runs once after the delay with the last arguments", async () => {
    const calls: string[] = [];
    const debounced = useDebounced((value: string) => {
      calls.push(value);
    }, 100);

    debounced("a");
    debounced("b");
    await vi.advanceTimersByTimeAsync(100);

    expect(calls).toEqual(["b"]);
  });

  it("is cancelled when the owning scope is disposed", async () => {
    const calls: string[] = [];
    const scope = effectScope();
    scope.run(() => {
      const debounced = useDebounced((value: string) => {
        calls.push(value);
      }, 100);
      debounced("typed just before leaving");
    });

    scope.stop();
    await vi.advanceTimersByTimeAsync(200);

    expect(calls).toEqual([]);
  });
});

describe("useOneShot", () => {
  it("does not run until started", async () => {
    const run = vi.fn();
    useOneShot(run, 100);

    await vi.advanceTimersByTimeAsync(200);

    expect(run).not.toHaveBeenCalled();
  });

  it("runs once after being started", async () => {
    const run = vi.fn();
    const timer = useOneShot(run, 100);

    timer.start();
    expect(timer.isPending.value).toBe(true);
    await vi.advanceTimersByTimeAsync(100);

    expect(run).toHaveBeenCalledOnce();
    expect(timer.isPending.value).toBe(false);
  });

  it("is cleared by stop() and by scope disposal", async () => {
    const run = vi.fn();
    const scope = effectScope();
    const timer = scope.run(() => useOneShot(run, 100));
    timer?.start();
    timer?.stop();
    await vi.advanceTimersByTimeAsync(200);
    expect(run).not.toHaveBeenCalled();

    timer?.start();
    scope.stop();
    await vi.advanceTimersByTimeAsync(200);
    expect(run).not.toHaveBeenCalled();
  });

  it("restarts its deadline when started again", async () => {
    const run = vi.fn();
    const timer = useOneShot(run, 100);

    timer.start();
    await vi.advanceTimersByTimeAsync(80);
    timer.start();
    await vi.advanceTimersByTimeAsync(80);
    expect(run).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(30);
    expect(run).toHaveBeenCalledOnce();
  });
});
