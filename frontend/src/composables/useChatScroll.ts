import { nextTick, onBeforeUnmount, onMounted, type Ref, ref } from "vue";

export interface ChatScrollOptions {
  /** Distance from the bottom, in pixels, that still counts as following it. */
  threshold?: number;
  /** How many times the landing loop probes the settled height. */
  settleAttempts?: number;
  /** Delay between landing probes, in milliseconds. */
  settleIntervalMs?: number;
}

export interface ChatScroll {
  /** The element whose content scrolls; the pane the follower owns. */
  scrollerRef: Ref<HTMLElement | null>;
  /** Whether the newest item is in view and incoming items should follow it. */
  pinned: Ref<boolean>;
  /** A template `@scroll` handler; it keeps `pinned` current. */
  onScroll: () => void;
  /** Jump to the newest item; smooth scrolling degrades when motion is reduced. */
  scrollToLatest: (options?: { smooth?: boolean }) => Promise<void>;
  /**
   * Land on the newest item and stay there while the content settles.
   *
   * A transcript renders some of its content lazily (the Markdown renderer is a
   * separate chunk), so a single jump can land short: the content keeps growing
   * after the jump. This probes the height until it stops changing, unless the
   * reader scrolls away first.
   */
  settleAtLatest: () => Promise<void>;
  /**
   * Keep the reader's place across an insertion above the viewport: older history
   * is loaded in front of the transcript, so the scroll offset has to grow by the
   * height the insertion added or the visible messages jump.
   */
  preserveAnchor: (mutate: () => Promise<unknown> | unknown) => Promise<void>;
}

const DEFAULT_THRESHOLD_PX = 48;
const DEFAULT_SETTLE_ATTEMPTS = 40;
const DEFAULT_SETTLE_INTERVAL_MS = 50;
/** Consecutive unchanged heights that count as "the content has settled". */
const SETTLE_STABLE_CHECKS = 3;

function prefersReducedMotion(): boolean {
  if (
    typeof window === "undefined" ||
    typeof window.matchMedia !== "function"
  ) {
    return false;
  }
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function sleep(intervalMs: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, intervalMs);
  });
}

/**
 * The scroll behavior of one chat-like pane: follow the newest item, stop
 * following when the reader scrolls back, and keep the reading position when
 * history is inserted above.
 *
 * The composable owns no message semantics. A page decides *when* to land or to
 * follow (first paint, a new polled message, a send); this only owns *how* the
 * pane keeps up. Following is anchored to `pinned`, and the resize observer only
 * runs for the landing window; a permanent one would drag a reader back to the
 * bottom when they expand a tool call above it.
 */
export function useChatScroll(options: ChatScrollOptions = {}): ChatScroll {
  const threshold = options.threshold ?? DEFAULT_THRESHOLD_PX;
  const settleAttempts = options.settleAttempts ?? DEFAULT_SETTLE_ATTEMPTS;
  const settleIntervalMs =
    options.settleIntervalMs ?? DEFAULT_SETTLE_INTERVAL_MS;

  const scrollerRef = ref<HTMLElement | null>(null);
  const pinned = ref(true);

  let disposed = false;
  // Every landing or jump takes a ticket; a newer one makes the loop in flight
  // return without touching the pane again.
  let ticket = 0;
  // The offset the landing loop last wrote. While it is set, a scroll above it
  // is our own short jump into lazily laid-out content, not the reader leaving.
  let landingTarget: number | undefined;
  /** Keeps the pane at the bottom while landing content finishes laying out. */
  let landingObserver: ResizeObserver | undefined;
  let landingStopTimer: ReturnType<typeof setTimeout> | undefined;

  function distanceFromBottom(scroller: HTMLElement): number {
    return scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
  }

  function stopLandingFollow(): void {
    landingObserver?.disconnect();
    landingObserver = undefined;
    if (landingStopTimer !== undefined) {
      clearTimeout(landingStopTimer);
      landingStopTimer = undefined;
    }
  }

  /** Recompute `pinned` from where the pane actually is. */
  function syncPinned(): void {
    const scroller = scrollerRef.value;
    if (!disposed && scroller) {
      pinned.value = distanceFromBottom(scroller) <= threshold;
    }
  }

  function endLandingFollow(): void {
    stopLandingFollow();
    landingTarget = undefined;
    syncPinned();
  }

  /**
   * Keep the bottom while the landing window stays open.
   *
   * The landing loop can conclude the height stopped changing and still be wrong:
   * the Markdown renderer lays off-screen blocks out lazily (`content-visibility:
   * auto`), so content below the last painted row can grow a moment later and
   * leave the pane short again. A resize observer on the content absorbs that,
   * then the timer hands control back. Returns whether it could observe anything.
   */
  function startLandingFollow(
    scroller: HTMLElement,
    currentTicket: number,
    durationMs: number
  ): boolean {
    stopLandingFollow();
    if (typeof ResizeObserver === "undefined") {
      return false;
    }
    const content = scroller.firstElementChild;
    if (!content) {
      return false;
    }
    landingObserver = new ResizeObserver(() => {
      if (disposed || currentTicket !== ticket || !pinned.value) {
        stopLandingFollow();
        return;
      }
      scroller.scrollTop = scroller.scrollHeight;
      landingTarget = scroller.scrollTop;
    });
    landingObserver.observe(content);
    landingStopTimer = setTimeout(() => {
      landingStopTimer = undefined;
      endLandingFollow();
    }, durationMs);
    return true;
  }

  function onScroll(): void {
    const scroller = scrollerRef.value;
    if (!scroller) {
      return;
    }
    // Content above the viewport is laid out lazily (the Markdown renderer keeps
    // off-screen blocks in `content-visibility: auto`), so one jump can land
    // short and the pane has to jump again. That jump is ours: only a position
    // above where the landing loop last wrote counts as the reader scrolling
    // back.
    if (
      landingTarget !== undefined &&
      scroller.scrollTop >= landingTarget - threshold
    ) {
      pinned.value = true;
      return;
    }
    pinned.value = distanceFromBottom(scroller) <= threshold;
    if (!pinned.value) {
      // The reader is driving; a landing in flight must not pull the pane back.
      stopLandingFollow();
      landingTarget = undefined;
    }
  }

  async function scrollToLatest(
    options: { smooth?: boolean } = {}
  ): Promise<void> {
    const scroller = scrollerRef.value;
    if (!scroller) {
      return;
    }
    ticket += 1;
    stopLandingFollow();
    landingTarget = undefined;
    if (
      options.smooth &&
      !prefersReducedMotion() &&
      typeof scroller.scrollTo === "function"
    ) {
      scroller.scrollTo({ top: scroller.scrollHeight, behavior: "smooth" });
    } else {
      scroller.scrollTop = scroller.scrollHeight;
    }
    pinned.value = true;
  }

  async function settleAtLatest(): Promise<void> {
    const scroller = scrollerRef.value;
    if (!scroller) {
      return;
    }
    ticket += 1;
    const currentTicket = ticket;
    const following = startLandingFollow(
      scroller,
      currentTicket,
      settleAttempts * settleIntervalMs
    );
    let previousHeight = -1;
    let stable = 0;
    for (let attempt = 0; attempt < settleAttempts; attempt += 1) {
      scroller.scrollTop = scroller.scrollHeight;
      landingTarget = scroller.scrollTop;
      pinned.value = true;
      await sleep(settleIntervalMs);
      // A reader who scrolled away, or a newer jump, wins over this landing.
      if (disposed) {
        stopLandingFollow();
        return;
      }
      // A newer landing owns the follow window now; leave its observer, timer and
      // landing target alone instead of tearing them down with this loop.
      if (currentTicket !== ticket) {
        return;
      }
      if (!pinned.value) {
        endLandingFollow();
        return;
      }
      const settled =
        scroller.scrollHeight === previousHeight &&
        distanceFromBottom(scroller) <= threshold;
      if (settled) {
        stable += 1;
        if (stable >= SETTLE_STABLE_CHECKS) {
          // The height looks settled, but a lazily laid-out block can still grow:
          // let the resize observer keep the bottom until its window closes.
          if (!following) {
            endLandingFollow();
          }
          return;
        }
      } else {
        stable = 0;
      }
      previousHeight = scroller.scrollHeight;
    }
    endLandingFollow();
  }

  async function preserveAnchor(
    mutate: () => Promise<unknown> | unknown
  ): Promise<void> {
    const scroller = scrollerRef.value;
    if (!scroller) {
      await mutate();
      return;
    }
    const previousTop = scroller.scrollTop;
    const previousHeight = scroller.scrollHeight;
    await mutate();
    await nextTick();
    const current = scrollerRef.value;
    if (disposed || !current) {
      return;
    }
    current.scrollTop = Math.max(
      0,
      previousTop + current.scrollHeight - previousHeight
    );
  }

  function onViewportResize(): void {
    if (!pinned.value) {
      return;
    }
    const scroller = scrollerRef.value;
    if (scroller) {
      scroller.scrollTop = scroller.scrollHeight;
    }
  }

  onMounted(() => {
    window.addEventListener("resize", onViewportResize);
  });

  onBeforeUnmount(() => {
    disposed = true;
    stopLandingFollow();
    window.removeEventListener("resize", onViewportResize);
  });

  return {
    scrollerRef,
    pinned,
    onScroll,
    scrollToLatest,
    settleAtLatest,
    preserveAnchor,
  };
}
