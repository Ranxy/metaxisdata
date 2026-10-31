import { useDebounceFn, useTimeoutFn } from "@vueuse/core";
import { getCurrentScope, onScopeDispose } from "vue";

/**
 * A debounced call that is cancelled when its owner goes away.
 *
 * `useDebounceFn` does not register a disposal hook by itself: a keystroke typed
 * just before navigating away used to fire its request from a page that no
 * longer existed. This restores the `cancel()` the removed `onBeforeUnmount`
 * blocks used to perform, in one place.
 */
export function useDebounced<Args extends unknown[]>(
  fn: (...args: Args) => void,
  delay = 300
) {
  const debounced = useDebounceFn(fn, delay);
  if (getCurrentScope()) {
    onScopeDispose(() => debounced.cancel());
  }
  return debounced;
}

/**
 * A one-shot timer that runs only when started, and is cleared on dispose.
 *
 * `useTimeoutFn` defaults to `immediate: true` in the browser, which arms the
 * callback during setup: the login page's "switch to the login form after
 * registering" timer fired 1.5 s after every page load and dismissed the
 * session-expired banner. Starting explicitly is the only safe default.
 */
export function useOneShot(fn: () => void, delay: number) {
  const timer = useTimeoutFn(fn, delay, { immediate: false });
  if (getCurrentScope()) {
    onScopeDispose(() => timer.stop());
  }
  return timer;
}
