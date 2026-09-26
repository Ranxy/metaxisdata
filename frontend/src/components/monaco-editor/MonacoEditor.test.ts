import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";

// The wrapper still loads the trimmed monaco module through its composables, and
// jsdom lacks the two APIs monaco probes at import time.
vi.hoisted(() => {
  Object.defineProperty(document, "queryCommandSupported", {
    value: () => false,
    configurable: true,
  });
  Object.defineProperty(globalThis, "CSS", {
    value: { escape: (value: string) => value },
    configurable: true,
  });
});

const setTheme = vi.hoisted(() => vi.fn());
const createMonacoEditor = vi.hoisted(() => vi.fn());

vi.mock("./editor", () => ({ createMonacoEditor }));

import { useAppStore } from "@/store/modules/app";
import MonacoEditor from "./MonacoEditor.vue";

function fakeEditor() {
  return {
    dispose: vi.fn(),
    getModel: () => null,
    onDidChangeModel: vi.fn(),
    onDidChangeModelContent: vi.fn(),
    updateOptions: vi.fn(),
  };
}

function mountEditor(pinia: ReturnType<typeof createPinia>) {
  return mount(MonacoEditor, {
    props: { content: "select 1" },
    global: { plugins: [pinia] },
  });
}

describe("MonacoEditor theme", () => {
  beforeEach(() => {
    setTheme.mockClear();
    createMonacoEditor.mockReset();
    createMonacoEditor.mockResolvedValue({
      editor: fakeEditor(),
      monaco: { editor: { setModelLanguage: vi.fn(), setTheme } },
    });
  });

  it("creates the editor with the app theme", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    useAppStore().setTheme("dark");

    mountEditor(pinia);
    await flushPromises();

    expect(createMonacoEditor).toHaveBeenCalledWith(
      expect.objectContaining({
        options: expect.objectContaining({ theme: "vs-dark" }),
      })
    );
  });

  it("switches the global monaco theme when the store theme changes", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.setTheme("light");

    const wrapper = mountEditor(pinia);
    await flushPromises();

    store.setTheme("dark");
    await nextTick();
    expect(setTheme).toHaveBeenLastCalledWith("vs-dark");

    store.setTheme("light");
    await nextTick();
    expect(setTheme).toHaveBeenLastCalledWith("vs");

    // "system" resolves through matchMedia, which the jsdom stub reports as light.
    store.setTheme("system");
    await nextTick();
    expect(setTheme).toHaveBeenLastCalledWith("vs");

    wrapper.unmount();
  });
});
