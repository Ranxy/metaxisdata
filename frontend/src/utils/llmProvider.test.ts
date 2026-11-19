import { describe, expect, it } from "vitest";
import {
  buildProviderOptions,
  enabledModelsFor,
  isProviderAllowed,
  providerLabel,
} from "./llmProvider";

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

describe("buildProviderOptions", () => {
  const profiles = [
    {
      name: "llm-provider-profiles/main",
      title: "Main",
      models: [
        { name: "fast", enabled: true },
        { name: "old", enabled: false },
      ],
    },
    {
      name: "llm-provider-profiles/disabled",
      title: "",
      models: [{ name: "idle", enabled: false }],
    },
    {
      name: "llm-provider-profiles/other",
      title: "Other",
      models: [{ name: "reasoner", enabled: true }],
    },
  ];

  it("keeps profiles that enable a model and are allowed", () => {
    expect(buildProviderOptions(profiles, [])).toEqual([
      { name: "llm-provider-profiles/main", title: "Main" },
      { name: "llm-provider-profiles/other", title: "Other" },
    ]);
  });

  it("drops a profile without an enabled model", () => {
    const names = buildProviderOptions(profiles, []).map(
      (option) => option.name
    );
    expect(names).not.toContain("llm-provider-profiles/disabled");
  });

  it("applies the workspace allowlist", () => {
    expect(
      buildProviderOptions(profiles, ["llm-provider-profiles/other"])
    ).toEqual([{ name: "llm-provider-profiles/other", title: "Other" }]);
  });

  it("falls back to the resource name when the profile has no title", () => {
    const options = buildProviderOptions(
      [
        {
          name: "llm-provider-profiles/untitled",
          title: "",
          models: [{ name: "m", enabled: true }],
        },
      ],
      []
    );
    expect(options[0].title).toBe("llm-provider-profiles/untitled");
  });
});

describe("enabledModelsFor", () => {
  const profiles = [
    {
      name: "llm-provider-profiles/main",
      title: "Main",
      models: [
        { name: "fast", enabled: true },
        { name: "retired", enabled: false },
        { name: "reasoner", enabled: true },
      ],
    },
  ];

  it("lists only the enabled models in registry order", () => {
    expect(enabledModelsFor(profiles, "llm-provider-profiles/main")).toEqual([
      "fast",
      "reasoner",
    ]);
  });

  it("returns nothing for an unknown profile", () => {
    expect(enabledModelsFor(profiles, "llm-provider-profiles/gone")).toEqual(
      []
    );
  });
});

describe("providerLabel", () => {
  it("uses the profile title and falls back to the reference", () => {
    const options = [{ name: "llm-provider-profiles/main", title: "Main" }];
    expect(providerLabel("llm-provider-profiles/main", options)).toBe("Main");
    expect(providerLabel("llm-provider-profiles/gone", options)).toBe(
      "llm-provider-profiles/gone"
    );
  });
});
