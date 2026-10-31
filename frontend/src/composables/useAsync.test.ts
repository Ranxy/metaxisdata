import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it, vi } from "vitest";
import { effectScope } from "vue";
import { useAsync } from "./useAsync";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/** Let the composable's promise chain run to its next await boundary. */
async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

describe("useAsync", () => {
  it("loads immediately and exposes the value", async () => {
    const load = vi.fn().mockResolvedValue("value");
    const { data, loading, error } = useAsync(load);

    expect(loading.value).toBe(true);
    await flush();
    expect(load).toHaveBeenCalledOnce();
    expect(data.value).toBe("value");
    expect(loading.value).toBe(false);
    expect(error.value).toBeUndefined();
  });

  it("starts with the initial value and skips the load when asked", async () => {
    const load = vi.fn().mockResolvedValue("value");
    const { data } = useAsync(load, { immediate: false, initialData: "seed" });

    expect(data.value).toBe("seed");
    await flush();
    expect(load).not.toHaveBeenCalled();
  });

  it("lets only the newest request write state", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const load = vi
      .fn<(signal: AbortSignal) => Promise<string>>()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const { data, refresh } = useAsync(load);

    const firstSignal = load.mock.calls[0]?.[0];
    const reload = refresh();
    expect(firstSignal?.aborted).toBe(true);

    first.resolve("stale");
    await flush();
    expect(data.value).toBeUndefined();

    second.resolve("fresh");
    await reload;
    expect(data.value).toBe("fresh");
  });

  it("keeps a real failure and reports it", async () => {
    const onError = vi.fn();
    const load = vi
      .fn()
      .mockRejectedValue(new ConnectError("boom", Code.Internal));
    const { error } = useAsync(load, { onError });

    await flush();
    expect(error.value).toBeInstanceOf(ConnectError);
    expect(onError).toHaveBeenCalledOnce();
  });

  it("drops the failure of an aborted request", async () => {
    const onError = vi.fn();
    const load = vi
      .fn()
      .mockRejectedValue(new ConnectError("canceled", Code.Canceled));
    const { error } = useAsync(load, { onError });

    await flush();
    expect(error.value).toBeUndefined();
    expect(onError).not.toHaveBeenCalled();
  });

  it("cancels without clearing the loaded value", async () => {
    const first = deferred<string>();
    const load = vi
      .fn<(signal: AbortSignal) => Promise<string>>()
      .mockReturnValueOnce(first.promise)
      .mockResolvedValue("second");
    const { data, loading, refresh, cancel } = useAsync(load, {
      immediate: false,
    });

    const pending = refresh();
    const signal = load.mock.calls[0]?.[0];
    cancel();
    expect(signal?.aborted).toBe(true);
    expect(loading.value).toBe(false);

    first.resolve("ignored");
    await pending;
    expect(data.value).toBeUndefined();
  });

  it("aborts the request when the owning scope is disposed", async () => {
    const pending = deferred<string>();
    const load = vi
      .fn<(signal: AbortSignal) => Promise<string>>()
      .mockReturnValue(pending.promise);
    const scope = effectScope();
    scope.run(() => useAsync(load));

    const signal = load.mock.calls[0]?.[0];
    scope.stop();
    expect(signal?.aborted).toBe(true);
    pending.resolve("ignored");
    await flush();
  });
});
