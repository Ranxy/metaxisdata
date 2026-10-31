import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope, nextTick, ref } from "vue";
import { useDrawerFocus } from "./useDrawerFocus";

function drawer() {
  const container = document.createElement("div");
  container.innerHTML = `<button id="first">first</button><button id="second">second</button>`;
  document.body.append(container);
  const outside = document.createElement("button");
  outside.id = "outside";
  outside.textContent = "outside";
  document.body.append(outside);
  return { container, outside };
}

async function flush() {
  await nextTick();
  await nextTick();
}

beforeEach(() => {
  document.body.innerHTML = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useDrawerFocus", () => {
  it("focuses the first control when the drawer opens and restores on close", async () => {
    const { container, outside } = drawer();
    outside.focus();
    const open = ref(false);
    const close = vi.fn();
    const scope = effectScope();
    scope.run(() =>
      useDrawerFocus(ref<HTMLElement | null>(container), open, close)
    );

    open.value = true;
    await flush();
    expect(document.activeElement?.id).toBe("first");

    open.value = false;
    await flush();
    expect(document.activeElement?.id).toBe("outside");
    scope.stop();
  });

  it("closes on Escape", async () => {
    const { container } = drawer();
    const open = ref(true);
    const close = vi.fn();
    const scope = effectScope();
    scope.run(() =>
      useDrawerFocus(ref<HTMLElement | null>(container), open, close)
    );
    await flush();

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));

    expect(close).toHaveBeenCalledOnce();
    scope.stop();
  });

  it("cycles Tab inside the drawer and back out with Shift+Tab", async () => {
    const { container } = drawer();
    const open = ref(true);
    const scope = effectScope();
    scope.run(() =>
      useDrawerFocus(ref<HTMLElement | null>(container), open, vi.fn())
    );
    await flush();

    const second = document.getElementById("second") as HTMLButtonElement;

    second.focus();
    const tab = new KeyboardEvent("keydown", { key: "Tab", cancelable: true });
    document.dispatchEvent(tab);
    expect(tab.defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe("first");

    const shiftTab = new KeyboardEvent("keydown", {
      key: "Tab",
      shiftKey: true,
      cancelable: true,
    });
    document.dispatchEvent(shiftTab);
    expect(shiftTab.defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe("second");
    scope.stop();
  });

  it("pulls focus back in when it has escaped the drawer", async () => {
    const { container, outside } = drawer();
    const open = ref(true);
    const scope = effectScope();
    scope.run(() =>
      useDrawerFocus(ref<HTMLElement | null>(container), open, vi.fn())
    );
    await flush();

    outside.focus();
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Tab", cancelable: true })
    );

    expect(document.activeElement?.id).toBe("first");
    scope.stop();
  });

  it("ignores keys other than Escape and Tab", async () => {
    const { container } = drawer();
    const open = ref(true);
    const close = vi.fn();
    const scope = effectScope();
    scope.run(() =>
      useDrawerFocus(ref<HTMLElement | null>(container), open, close)
    );
    await flush();

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "a" }));

    expect(close).not.toHaveBeenCalled();
    scope.stop();
  });

  it("does not trap Tab in a drawer with no focusable content", async () => {
    const empty = document.createElement("div");
    document.body.append(empty);
    const open = ref(true);
    const scope = effectScope();
    scope.run(() =>
      useDrawerFocus(ref<HTMLElement | null>(empty), open, vi.fn())
    );
    await flush();

    const tab = new KeyboardEvent("keydown", { key: "Tab", cancelable: true });
    expect(() => document.dispatchEvent(tab)).not.toThrow();
    expect(tab.defaultPrevented).toBe(false);
    scope.stop();
  });

  it("handles a drawer whose element is not mounted yet", async () => {
    const open = ref(true);
    const scope = effectScope();
    scope.run(() =>
      useDrawerFocus(ref<HTMLElement | null>(null), open, vi.fn())
    );
    await flush();

    const tab = new KeyboardEvent("keydown", { key: "Tab", cancelable: true });
    document.dispatchEvent(tab);

    expect(tab.defaultPrevented).toBe(false);
    scope.stop();
  });

  it("stops handling keys once closed", async () => {
    const { container } = drawer();
    const open = ref(true);
    const close = vi.fn();
    const scope = effectScope();
    scope.run(() =>
      useDrawerFocus(ref<HTMLElement | null>(container), open, close)
    );
    await flush();

    open.value = false;
    await flush();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));

    expect(close).not.toHaveBeenCalled();
    scope.stop();
  });
});
