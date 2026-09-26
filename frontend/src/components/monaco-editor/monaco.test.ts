import { describe, expect, it, vi } from "vitest";

// Monaco's clipboard contribution probes this at import time and jsdom does not
// implement it; vi.hoisted keeps the stub ahead of the module import below.
vi.hoisted(() => {
  Object.defineProperty(document, "queryCommandSupported", {
    value: () => false,
    configurable: true,
  });
  // jsdom ships no CSS.escape, which monaco's theme service calls on import.
  Object.defineProperty(globalThis, "CSS", {
    value: { escape: (value: string) => value },
    configurable: true,
  });
});

import * as monaco from "./monaco";

describe("trimmed monaco entry", () => {
  it("registers the SQL language and no language services", () => {
    const ids = monaco.languages.getLanguages().map((language) => language.id);
    expect(ids).toContain("sql");
    // "plaintext" is monaco's built-in default language, used by MonacoEditor.
    expect(ids).toContain("plaintext");
    // The full barrel would also register these and pull in their workers.
    expect(ids).not.toContain("typescript");
    expect(ids).not.toContain("javascript");
    expect(ids).not.toContain("json");
    expect(ids).not.toContain("css");
    expect(ids).not.toContain("html");
  });

  it("exposes the editor API the wrapper uses", () => {
    expect(typeof monaco.editor.create).toBe("function");
    expect(typeof monaco.editor.setTheme).toBe("function");
    expect(new monaco.Range(1, 1, 1, 2).endColumn).toBe(2);
  });

  it("tokenizes SQL through the registered language", () => {
    const [line] = monaco.editor.tokenize("SELECT 1", "sql");
    expect(line.length).toBeGreaterThan(0);
  });
});
