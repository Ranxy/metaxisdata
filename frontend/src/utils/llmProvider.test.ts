import { describe, expect, it } from "vitest";
import { isProviderAllowed } from "./llmProvider";

describe("isProviderAllowed", () => {
  it("allows every profile when the allowlist is empty", () => {
    expect(isProviderAllowed("llm-provider-profiles/openai", [])).toBe(true);
  });

  it("only allows the listed profiles", () => {
    const allowed = ["llm-provider-profiles/openai"];
    expect(isProviderAllowed("llm-provider-profiles/openai", allowed)).toBe(
      true
    );
    expect(isProviderAllowed("llm-provider-profiles/local", allowed)).toBe(
      false
    );
  });
});
