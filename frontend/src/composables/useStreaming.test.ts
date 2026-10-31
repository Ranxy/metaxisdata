import { Code, ConnectError } from "@connectrpc/connect";
import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { defineComponent, effectScope } from "vue";
import { useStreaming } from "./useStreaming";

/** A stream whose frames are pushed by the test. */
function controlled<T>() {
  let push!: (value: T) => void;
  let finish!: () => void;
  let finished = false;
  const queue: T[] = [];
  const iterable = (async function* () {
    for (;;) {
      if (queue.length > 0) {
        yield queue.shift() as T;
        continue;
      }
      if (finished) {
        return;
      }
      await new Promise<void>((resolve) => {
        push = (value) => {
          queue.push(value);
          resolve();
        };
        finish = () => {
          finished = true;
          resolve();
        };
      });
    }
  })();
  return {
    iterable,
    push(value: T) {
      push(value);
    },
    finish() {
      finish();
    },
  };
}

async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

describe("useStreaming", () => {
  it("resets, delivers every frame and reports done", async () => {
    const state: string[] = [];
    const onReset = vi.fn(() => {
      state.length = 0;
    });
    const onDone = vi.fn();
    const stream = useStreaming<string>({
      open: async function* () {
        yield "a";
        yield "b";
      },
      onReset,
      onChunk: (chunk) => state.push(chunk),
      onError: vi.fn(),
      onDone,
    });

    await stream.run();

    expect(onReset).toHaveBeenCalledOnce();
    expect(state).toEqual(["a", "b"]);
    expect(onDone).toHaveBeenCalledOnce();
  });

  it("passes the run arguments to open", async () => {
    const open = vi.fn(async function* (
      _signal: AbortSignal,
      _force: boolean
    ) {});
    const stream = useStreaming<string, [boolean]>({
      open,
      onChunk: vi.fn(),
      onError: vi.fn(),
    });

    await stream.run(true);

    expect(open.mock.calls[0]?.[1]).toBe(true);
  });

  it("aborts the replaced run and drops its late frame", async () => {
    const first = controlled<string>();
    const second = controlled<string>();
    const state: string[] = [];
    const open = vi
      .fn<(signal: AbortSignal) => AsyncIterable<string>>()
      .mockReturnValueOnce(first.iterable)
      .mockReturnValueOnce(second.iterable);
    const stream = useStreaming<string>({
      open,
      onReset: () => {
        state.length = 0;
      },
      onChunk: (chunk) => state.push(chunk),
      onError: vi.fn(),
    });

    const firstRun = stream.run();
    const signal = open.mock.calls[0]?.[0] as AbortSignal;
    first.push("old");
    await settle();

    const secondRun = stream.run();
    expect(signal.aborted).toBe(true);
    second.push("new");
    first.push("late");
    await settle();
    second.finish();
    await secondRun;
    first.finish();
    await firstRun;

    expect(state).toEqual(["new"]);
  });

  it("stops delivering after cancel", async () => {
    const source = controlled<string>();
    const state: string[] = [];
    const stream = useStreaming<string>({
      open: () => source.iterable,
      onChunk: (chunk) => state.push(chunk),
      onError: vi.fn(),
    });

    const running = stream.run();
    source.push("first");
    await settle();
    stream.cancel();
    source.push("late");
    await settle();

    expect(state).toEqual(["first"]);
    source.finish();
    await running;
  });

  it("reports a real failure and ignores an abort", async () => {
    const onError = vi.fn();
    const stream = useStreaming<string>({
      open: () => {
        throw new ConnectError("boom", Code.Internal);
      },
      onChunk: vi.fn(),
      onError,
    });

    await stream.run();
    expect(onError).toHaveBeenCalledOnce();

    onError.mockClear();
    const canceled = useStreaming<string>({
      open: () => {
        throw new ConnectError("canceled", Code.Canceled);
      },
      onChunk: vi.fn(),
      onError,
    });
    await canceled.run();
    expect(onError).not.toHaveBeenCalled();
  });

  it("does not report done for a superseded run", async () => {
    const first = controlled<string>();
    const second = controlled<string>();
    const onDone = vi.fn();
    const open = vi
      .fn<() => AsyncIterable<string>>()
      .mockReturnValueOnce(first.iterable)
      .mockReturnValueOnce(second.iterable);
    const stream = useStreaming<string>({
      open,
      onChunk: vi.fn(),
      onError: vi.fn(),
      onDone,
    });

    const firstRun = stream.run();
    const secondRun = stream.run();
    second.finish();
    await secondRun;
    first.finish();
    await firstRun;

    expect(onDone).toHaveBeenCalledOnce();
  });

  it("aborts the run in flight when the component is unmounted", async () => {
    const source = controlled<string>();
    let signal: AbortSignal | undefined;
    const Harness = defineComponent({
      setup() {
        const stream = useStreaming<string>({
          open: (runSignal) => {
            signal = runSignal;
            return source.iterable;
          },
          onChunk: vi.fn(),
          onError: vi.fn(),
        });
        (Harness as unknown as { stream?: unknown }).stream = stream;
        return () => null;
      },
    });
    const wrapper = mount(Harness);
    const stream = (
      Harness as unknown as {
        stream: ReturnType<typeof useStreaming<string>>;
      }
    ).stream;
    void stream.run();
    expect(signal?.aborted).toBe(false);

    wrapper.unmount();

    expect(signal?.aborted).toBe(true);
    source.finish();
  });

  it("aborts the run in flight when the owning scope is disposed", async () => {
    const source = controlled<string>();
    let signal: AbortSignal | undefined;
    const scope = effectScope();
    const stream = scope.run(() =>
      useStreaming<string>({
        open: (runSignal) => {
          signal = runSignal;
          return source.iterable;
        },
        onChunk: vi.fn(),
        onError: vi.fn(),
      })
    )!;

    const running = stream.run();
    expect(signal?.aborted).toBe(false);
    scope.stop();
    expect(signal?.aborted).toBe(true);
    source.finish();
    await running;
  });
});
