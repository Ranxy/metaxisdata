import { nextTick, onScopeDispose, type Ref, watch } from "vue";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Keyboard behaviour for a modal drawer: Escape closes it, Tab cycles inside it,
 * and focus returns to whatever the reader was using before it opened.
 *
 * The mobile navigation drawer is an `aside`, not a dialog primitive, so it did
 * not inherit any of that: a keyboard user could Tab into the page behind it and
 * could not dismiss it, and closing left focus nowhere.
 */
export function useDrawerFocus(
  container: Ref<HTMLElement | null>,
  open: Readonly<Ref<boolean>>,
  close: () => void
) {
  let previousFocus: HTMLElement | null = null;

  function focusable(): HTMLElement[] {
    const element = container.value;
    return element ? [...element.querySelectorAll<HTMLElement>(FOCUSABLE)] : [];
  }

  function onKeydown(event: KeyboardEvent) {
    if (!open.value || (event.key !== "Escape" && event.key !== "Tab")) {
      return;
    }
    if (event.key === "Escape") {
      // A menu or dialog the reader has open inside the drawer handles Escape
      // itself; closing the drawer as well would dismiss two layers at once.
      if (event.defaultPrevented) {
        return;
      }
      event.preventDefault();
      close();
      return;
    }
    const items = focusable();
    if (items.length === 0) {
      return;
    }
    const active = document.activeElement;
    // A portalled popover/menu inside the drawer owns its own Tab loop; the
    // drawer must not yank focus back to its first link while that layer is up.
    const layer =
      active instanceof HTMLElement
        ? active.closest('[data-radix-popper-content-wrapper], [role="menu"]')
        : null;
    if (layer && !container.value?.contains(layer)) {
      return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    const inside = active instanceof HTMLElement && items.includes(active);
    if (event.shiftKey && (!inside || active === first)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (!inside || active === last)) {
      event.preventDefault();
      first.focus();
    }
  }

  watch(
    open,
    async (isOpen) => {
      if (isOpen) {
        previousFocus =
          document.activeElement instanceof HTMLElement
            ? document.activeElement
            : null;
        document.addEventListener("keydown", onKeydown);
        await nextTick();
        focusable()[0]?.focus();
        return;
      }
      document.removeEventListener("keydown", onKeydown);
      previousFocus?.focus();
      previousFocus = null;
    },
    { immediate: true }
  );

  onScopeDispose(() => {
    document.removeEventListener("keydown", onKeydown);
  });
}
