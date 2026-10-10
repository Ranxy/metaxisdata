import { describe, expect, it, vi } from "vitest";

// `monaco.ts` is the multi-megabyte trimmed editor bundle. It is meant to be
// loaded exactly once, dynamically, by `lazy-editor.ts` when an editor is
// actually created. A value import of `../monaco` anywhere in the barrel's
// graph (`MonacoEditor.vue` → `composables/` → `common.ts`) pulls it into every
// chunk that so much as reads `formatSQL`, which used to put the entire bundle
// on every metadata detail page — a table page downloaded it even though the
// "View Schema" dialog was never opened. This pins the boundary.
const state = vi.hoisted(() => ({ evaluated: false }));

vi.mock("./monaco", () => {
  state.evaluated = true;
  return {
    monaco: {},
    editor: {},
    languages: {},
    Range: class {},
  };
});

describe("monaco stays out of the static graph", () => {
  it("loads the barrel and its composables without evaluating the bundle", async () => {
    await import("./index");
    await import("./composables");
    expect(state.evaluated).toBe(false);
  });
});
