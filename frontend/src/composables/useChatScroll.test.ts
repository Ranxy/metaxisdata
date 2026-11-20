import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import { defineComponent } from "vue";
import {
  type ChatScroll,
  type ChatScrollOptions,
  useChatScroll,
} from "./useChatScroll";

interface PaneMetrics {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
}

/**
 * Mounts a component that owns the composable, because it registers lifecycle
 * hooks. The pane's geometry is tracked through our own getters: jsdom lays
 * nothing out, so `scrollHeight` and `clientHeight` would stay zero.
 */
function mountChat(
  options: ChatScrollOptions = {},
  { withScroller = true } = {}
) {
  let chat: ChatScroll | undefined;
  const wrapper = mount(
    defineComponent({
      setup() {
        chat = useChatScroll(options);
        return {
          onScroll: chat.onScroll,
          setScroller: (element: Element | null) => {
            if (chat) {
              chat.scrollerRef.value = (element as HTMLElement | null) ?? null;
            }
          },
        };
      },
      template: withScroller
        ? '<div data-test="scroller" :ref="setScroller" @scroll="onScroll"><div data-test="content" /></div>'
        : "<div />",
    })
  );
  return { wrapper, chat: chat as ChatScroll };
}

function pane(wrapper: ReturnType<typeof mountChat>["wrapper"]): HTMLElement {
  return wrapper.get('[data-test="scroller"]').element as HTMLElement;
}

function track(element: HTMLElement): PaneMetrics {
  const metrics: PaneMetrics = {
    scrollTop: 0,
    scrollHeight: 0,
    clientHeight: 0,
  };
  Object.defineProperty(element, "scrollHeight", {
    configurable: true,
    get: () => metrics.scrollHeight,
  });
  Object.defineProperty(element, "clientHeight", {
    configurable: true,
    get: () => metrics.clientHeight,
  });
  Object.defineProperty(element, "scrollTop", {
    configurable: true,
    get: () => metrics.scrollTop,
    set: (value: number) => {
      metrics.scrollTop = value;
    },
  });
  return metrics;
}

function stubSmoothScroll(element: HTMLElement, metrics: PaneMetrics) {
  Object.defineProperty(element, "scrollTo", {
    configurable: true,
    value: vi.fn(({ top }: ScrollToOptions) => {
      metrics.scrollTop = top ?? 0;
    }),
  });
}

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("useChatScroll", () => {
  it("pins inside the threshold and unpins once the reader scrolls away", () => {
    const { wrapper, chat } = mountChat({ threshold: 20 });
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 1000;
    metrics.clientHeight = 400;

    metrics.scrollTop = 580;
    chat.onScroll();
    expect(chat.pinned.value).toBe(true);

    metrics.scrollTop = 579;
    chat.onScroll();
    expect(chat.pinned.value).toBe(false);
    wrapper.unmount();
  });

  it("jumps to the newest item and re-pins", async () => {
    const { wrapper, chat } = mountChat();
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 1200;
    metrics.scrollTop = 0;
    chat.onScroll();
    expect(chat.pinned.value).toBe(false);

    await chat.scrollToLatest();

    expect(metrics.scrollTop).toBe(1200);
    expect(chat.pinned.value).toBe(true);
    wrapper.unmount();
  });

  it("uses a smooth scroll for the jump control", async () => {
    const { wrapper, chat } = mountChat();
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 1200;
    stubSmoothScroll(pane(wrapper), metrics);

    await chat.scrollToLatest({ smooth: true });

    expect(pane(wrapper).scrollTo).toHaveBeenCalledWith({
      top: 1200,
      behavior: "smooth",
    });
    wrapper.unmount();
  });

  it("jumps instantly when the reader prefers reduced motion", async () => {
    vi.spyOn(window, "matchMedia").mockReturnValue({
      matches: true,
    } as MediaQueryList);
    const { wrapper, chat } = mountChat();
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 900;
    stubSmoothScroll(pane(wrapper), metrics);

    await chat.scrollToLatest({ smooth: true });

    expect(pane(wrapper).scrollTo).not.toHaveBeenCalled();
    expect(metrics.scrollTop).toBe(900);
    wrapper.unmount();
  });

  it("keeps landing while lazy content makes the pane taller", async () => {
    vi.useFakeTimers();
    const { wrapper, chat } = mountChat({
      settleAttempts: 10,
      settleIntervalMs: 10,
    });
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 500;
    metrics.clientHeight = 100;

    const landing = chat.settleAtLatest();
    await vi.advanceTimersByTimeAsync(10);
    metrics.scrollHeight = 800;
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    await landing;

    expect(metrics.scrollTop).toBe(800);
    expect(vi.getTimerCount()).toBe(0);
    wrapper.unmount();
  });

  it("stops landing once the reader scrolls away", async () => {
    vi.useFakeTimers();
    const { wrapper, chat } = mountChat({
      settleAttempts: 10,
      settleIntervalMs: 10,
    });
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 600;
    metrics.clientHeight = 100;

    const landing = chat.settleAtLatest();
    await vi.advanceTimersByTimeAsync(10);
    metrics.scrollTop = 0;
    chat.onScroll();
    await vi.advanceTimersByTimeAsync(10);
    await landing;

    expect(metrics.scrollTop).toBe(0);
    expect(chat.pinned.value).toBe(false);
    wrapper.unmount();
  });

  it("keeps landing when its own jump landed short", async () => {
    vi.useFakeTimers();
    const { wrapper, chat } = mountChat({
      settleAttempts: 10,
      settleIntervalMs: 10,
    });
    const element = pane(wrapper);
    const metrics = track(element);
    let maxTop = 300;
    Object.defineProperty(element, "scrollTop", {
      configurable: true,
      get: () => metrics.scrollTop,
      set: (value: number) => {
        metrics.scrollTop = Math.min(value, maxTop);
      },
    });
    metrics.scrollHeight = 1000;
    metrics.clientHeight = 400;

    const landing = chat.settleAtLatest();
    // The pane could only reach 300 of the 600 it needs; the browser reports that
    // jump back as a scroll event, which must not look like the reader leaving.
    await vi.advanceTimersByTimeAsync(10);
    chat.onScroll();
    expect(chat.pinned.value).toBe(true);

    // Lazy layout finished: the same loop can now reach the bottom.
    maxTop = 600;
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    await landing;

    expect(metrics.scrollTop).toBe(600);
    expect(chat.pinned.value).toBe(true);
    wrapper.unmount();
  });

  it("keeps following content that lays out after the loop stopped", async () => {
    vi.useFakeTimers();
    let observerCallback: ResizeObserverCallback | undefined;
    class FakeResizeObserver {
      constructor(callback: ResizeObserverCallback) {
        observerCallback = callback;
      }
      observe = vi.fn();
      unobserve = vi.fn();
      disconnect = vi.fn();
    }
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
    const { wrapper, chat } = mountChat({
      settleAttempts: 10,
      settleIntervalMs: 10,
    });
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 500;
    metrics.clientHeight = 100;

    const landing = chat.settleAtLatest();
    for (let step = 0; step < 6; step += 1) {
      await vi.advanceTimersByTimeAsync(10);
    }
    await landing;
    expect(chat.pinned.value).toBe(true);

    // A lazily laid-out block grows after the loop already saw a stable height.
    metrics.scrollHeight = 900;
    observerCallback?.([], {} as ResizeObserver);
    expect(metrics.scrollTop).toBe(900);
    expect(chat.pinned.value).toBe(true);

    // The grace window closes and hands control back.
    await vi.advanceTimersByTimeAsync(100);
    expect(vi.getTimerCount()).toBe(0);
    expect(chat.pinned.value).toBe(true);
    wrapper.unmount();
  });

  it("stops landing at its attempt budget", async () => {
    vi.useFakeTimers();
    const { wrapper, chat } = mountChat({
      settleAttempts: 2,
      settleIntervalMs: 10,
    });
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 100;
    metrics.clientHeight = 100;

    const landing = chat.settleAtLatest();
    // Every probe grows the pane, so stability is never reached.
    await vi.advanceTimersByTimeAsync(10);
    metrics.scrollHeight = 200;
    await vi.advanceTimersByTimeAsync(10);
    await landing;

    expect(vi.getTimerCount()).toBe(0);
    wrapper.unmount();
  });

  it("keeps the reading position when older history is inserted", async () => {
    const { wrapper, chat } = mountChat();
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 1000;
    metrics.scrollTop = 300;

    await chat.preserveAnchor(() => {
      metrics.scrollHeight = 1400;
    });

    expect(metrics.scrollTop).toBe(700);
    wrapper.unmount();
  });

  it("still runs the insertion when there is no pane yet", async () => {
    const { wrapper, chat } = mountChat({}, { withScroller: false });
    const mutate = vi.fn();

    await chat.preserveAnchor(mutate);

    expect(mutate).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("keeps the newest item in view when the viewport grows", () => {
    const { wrapper, chat } = mountChat();
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 1000;
    metrics.clientHeight = 100;
    metrics.scrollTop = 900;

    window.dispatchEvent(new Event("resize"));
    expect(metrics.scrollTop).toBe(1000);

    metrics.scrollTop = 100;
    chat.onScroll();
    expect(chat.pinned.value).toBe(false);
    window.dispatchEvent(new Event("resize"));
    expect(metrics.scrollTop).toBe(100);
    wrapper.unmount();
  });

  it("treats a runtime without matchMedia as normal motion", async () => {
    vi.stubGlobal("matchMedia", undefined);
    const { wrapper, chat } = mountChat();
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 500;
    stubSmoothScroll(pane(wrapper), metrics);

    await chat.scrollToLatest({ smooth: true });

    expect(pane(wrapper).scrollTo).toHaveBeenCalledWith({
      top: 500,
      behavior: "smooth",
    });
    wrapper.unmount();
  });

  it("lets a newer jump supersede a landing in flight", async () => {
    vi.useFakeTimers();
    const { wrapper, chat } = mountChat({
      settleAttempts: 10,
      settleIntervalMs: 10,
    });
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 400;
    metrics.clientHeight = 100;

    const landing = chat.settleAtLatest();
    await vi.advanceTimersByTimeAsync(10);
    await chat.scrollToLatest();
    await vi.advanceTimersByTimeAsync(10);
    await landing;

    expect(metrics.scrollTop).toBe(400);
    expect(vi.getTimerCount()).toBe(0);
    wrapper.unmount();
  });

  it("leaves a newer landing window alone when a stale one wakes", async () => {
    vi.useFakeTimers();
    const observers: Array<{ disconnect: ReturnType<typeof vi.fn> }> = [];
    class FakeResizeObserver {
      disconnect = vi.fn();
      observe = vi.fn();
      unobserve = vi.fn();
      constructor() {
        observers.push(this);
      }
    }
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
    const { wrapper, chat } = mountChat({
      settleAttempts: 10,
      settleIntervalMs: 10,
    });
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 500;
    metrics.clientHeight = 100;

    const first = chat.settleAtLatest();
    await vi.advanceTimersByTimeAsync(10);
    const second = chat.settleAtLatest();
    await vi.advanceTimersByTimeAsync(10);
    await first;

    // The stale landing must not tear down the newer landing's observer.
    expect(observers.length).toBeGreaterThanOrEqual(2);
    expect(observers[observers.length - 1]?.disconnect).not.toHaveBeenCalled();

    for (let step = 0; step < 6; step += 1) {
      await vi.advanceTimersByTimeAsync(10);
    }
    await second;
    await vi.advanceTimersByTimeAsync(200);
    expect(vi.getTimerCount()).toBe(0);
    expect(chat.pinned.value).toBe(true);
    wrapper.unmount();
  });

  it("ignores a resize reported for a superseded landing", async () => {
    vi.useFakeTimers();
    let observerCallback: ResizeObserverCallback | undefined;
    class FakeResizeObserver {
      constructor(callback: ResizeObserverCallback) {
        observerCallback = callback;
      }
      observe = vi.fn();
      unobserve = vi.fn();
      disconnect = vi.fn();
    }
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
    const { wrapper, chat } = mountChat({
      settleAttempts: 10,
      settleIntervalMs: 10,
    });
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 500;
    metrics.clientHeight = 100;

    const landing = chat.settleAtLatest();
    await vi.advanceTimersByTimeAsync(10);
    await chat.scrollToLatest();
    const before = metrics.scrollTop;
    metrics.scrollHeight = 900;
    observerCallback?.([], {} as ResizeObserver);
    await vi.advanceTimersByTimeAsync(10);
    await landing;

    expect(metrics.scrollTop).toBe(before);
    wrapper.unmount();
  });

  it("does not follow without a content box to watch", async () => {
    vi.useFakeTimers();
    class FakeResizeObserver {
      observe = vi.fn();
      unobserve = vi.fn();
      disconnect = vi.fn();
    }
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
    const { wrapper, chat } = mountChat({
      settleAttempts: 10,
      settleIntervalMs: 10,
    });
    const scroller = pane(wrapper);
    scroller.removeChild(scroller.firstElementChild as Element);
    const metrics = track(scroller);
    metrics.scrollHeight = 500;
    metrics.clientHeight = 100;

    const landing = chat.settleAtLatest();
    for (let step = 0; step < 6; step += 1) {
      await vi.advanceTimersByTimeAsync(10);
    }
    await landing;

    expect(metrics.scrollTop).toBe(500);
    expect(vi.getTimerCount()).toBe(0);
    wrapper.unmount();
  });

  it("stops touching the pane when it unmounts during an insertion", async () => {
    const { wrapper, chat } = mountChat();
    const metrics = track(pane(wrapper));
    metrics.scrollHeight = 1000;
    metrics.scrollTop = 300;

    await chat.preserveAnchor(async () => {
      metrics.scrollHeight = 1400;
      wrapper.unmount();
    });

    expect(metrics.scrollTop).toBe(300);
  });

  it("is inert before the pane is attached", async () => {
    const { wrapper, chat } = mountChat({}, { withScroller: false });

    expect(() => chat.onScroll()).not.toThrow();
    await expect(chat.scrollToLatest()).resolves.toBeUndefined();
    await expect(chat.settleAtLatest()).resolves.toBeUndefined();
    wrapper.unmount();
  });
});
