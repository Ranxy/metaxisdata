import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it, vi } from "vitest";
import { effectScope } from "vue";
import type { ListPage } from "@/api/list";
import { type PagedFetch, usePagedFetch } from "./usePagedFetch";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function page(items: string[], nextPageToken = ""): ListPage<string> {
  return { items, nextPageToken };
}

describe("usePagedFetch", () => {
  it("loads the first page and reports both directions", async () => {
    const fetchPage = vi.fn(async () => page(["a"], "p2"));
    const pager = usePagedFetch<string>({ fetchPage });

    await pager.reset();

    expect(fetchPage).toHaveBeenCalledWith("", expect.any(AbortSignal));
    expect(pager.items.value).toEqual(["a"]);
    expect(pager.hasNext.value).toBe(true);
    expect(pager.hasPrevious.value).toBe(false);
    expect(pager.isLoading.value).toBe(false);
  });

  it("walks forward and back through the token stack", async () => {
    const fetchPage = vi.fn(async (token: string) => {
      if (token === "") return page(["a"], "p2");
      if (token === "p2") return page(["b"], "p3");
      return page(["c"]);
    });
    const pager = usePagedFetch<string>({ fetchPage });
    await pager.reset();

    await pager.goNext();
    expect(pager.items.value).toEqual(["b"]);
    expect(pager.hasPrevious.value).toBe(true);

    await pager.goPrevious();
    expect(pager.items.value).toEqual(["a"]);
    expect(pager.hasPrevious.value).toBe(false);
  });

  it("does not follow an absent token", async () => {
    const fetchPage = vi.fn(async () => page(["a"]));
    const pager = usePagedFetch<string>({ fetchPage });
    await pager.reset();

    await pager.goNext();
    await pager.goPrevious();

    expect(fetchPage).toHaveBeenCalledTimes(1);
  });

  it("refreshes the page it is on", async () => {
    const fetchPage = vi.fn(async (token: string) =>
      token === "" ? page(["a"], "p2") : page(["b"], "p3")
    );
    const pager = usePagedFetch<string>({ fetchPage });
    await pager.reset();
    await pager.goNext();

    await pager.refresh();

    expect(fetchPage).toHaveBeenLastCalledWith("p2", expect.any(AbortSignal));
    expect(pager.items.value).toEqual(["b"]);
  });

  it("aborts the superseded request so a slow page cannot win", async () => {
    const first = deferred<ListPage<string>>();
    const signals: AbortSignal[] = [];
    const fetchPage = vi.fn((_token: string, signal: AbortSignal) => {
      signals.push(signal);
      return signals.length === 1
        ? first.promise
        : Promise.resolve(page(["b"]));
    });
    const pager = usePagedFetch<string>({ fetchPage });

    const slow = pager.reset();
    await pager.refresh();

    expect(signals[0].aborted).toBe(true);
    expect(pager.items.value).toEqual(["b"]);

    // The old answer lands last, and must not replace the newer one.
    first.resolve(page(["a"], "p2"));
    await slow;

    expect(pager.items.value).toEqual(["b"]);
    expect(pager.isLoading.value).toBe(false);
  });

  it("reports a failure and drops the forward token", async () => {
    const onError = vi.fn();
    const fetchPage = vi.fn(async () => {
      throw new ConnectError("boom", Code.Internal);
    });
    const pager = usePagedFetch<string>({ fetchPage, onError });

    await pager.reset();

    expect(onError).toHaveBeenCalledTimes(1);
    expect(pager.hasNext.value).toBe(false);
    expect(pager.isLoading.value).toBe(false);
  });

  it("stays silent about a request it cancelled itself", async () => {
    const onError = vi.fn();
    const first = deferred<ListPage<string>>();
    const fetchPage = vi
      .fn<(token: string, signal: AbortSignal) => Promise<ListPage<string>>>()
      .mockImplementationOnce(async () => await first.promise)
      .mockImplementationOnce(async () => {
        first.reject(new ConnectError("cancelled", Code.Canceled));
        return page(["b"]);
      });
    const pager = usePagedFetch<string>({ fetchPage, onError });

    const slow = pager.reset();
    await pager.refresh();
    await slow;

    expect(onError).not.toHaveBeenCalled();
    expect(pager.items.value).toEqual(["b"]);
  });

  it("treats a bare abort signal as a cancellation and any other DOMException as a failure", async () => {
    const onError = vi.fn();

    const aborted = usePagedFetch<string>({
      fetchPage: async () => {
        throw new DOMException("cancelled", "AbortError");
      },
      onError,
    });
    await aborted.reset();
    expect(onError).not.toHaveBeenCalled();

    const failed = usePagedFetch<string>({
      fetchPage: async () => {
        throw new DOMException("boom", "NotSupportedError");
      },
      onError,
    });
    await failed.reset();
    expect(onError).toHaveBeenCalledTimes(1);
  });

  it("aborts the request on the wire when its scope is disposed", async () => {
    const pending = deferred<ListPage<string>>();
    const signals: AbortSignal[] = [];
    const fetchPage = vi.fn((_token: string, signal: AbortSignal) => {
      signals.push(signal);
      return pending.promise;
    });

    const scope = effectScope();
    let pager!: PagedFetch<string>;
    scope.run(() => {
      pager = usePagedFetch<string>({ fetchPage });
    });

    const loading = pager.reset();
    scope.stop();

    expect(signals[0]?.aborted).toBe(true);
    // The abandoned answer must not surface anywhere.
    pending.resolve(page(["late"]));
    await loading;
  });

  it("disposes a pager that never started a request", () => {
    const scope = effectScope();
    scope.run(() => usePagedFetch<string>({ fetchPage: vi.fn() }));

    expect(() => scope.stop()).not.toThrow();
  });
});
