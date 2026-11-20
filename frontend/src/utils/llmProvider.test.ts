import { describe, expect, it } from "vitest";
import {
  buildProviderOptions,
  type DiscoveredModelCapacity,
  enabledModelsFor,
  formatTokenCapacity,
  isProviderAllowed,
  type ModelCapacityLimits,
  type ModelCapacityRow,
  mergeDiscoveredModels,
  parseTokenCapacity,
  profileTitle,
  validateModelRows,
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

  it("keeps the profiles that enable a model", () => {
    expect(buildProviderOptions(profiles)).toEqual([
      { name: "llm-provider-profiles/main", title: "Main" },
      { name: "llm-provider-profiles/other", title: "Other" },
    ]);
  });

  it("drops a profile without an enabled model", () => {
    const names = buildProviderOptions(profiles).map((option) => option.name);
    expect(names).not.toContain("llm-provider-profiles/disabled");
  });

  it("falls back to the resource name when the profile has no title", () => {
    const options = buildProviderOptions([
      {
        name: "llm-provider-profiles/untitled",
        title: "",
        models: [{ name: "m", enabled: true }],
      },
    ]);
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

describe("profileTitle", () => {
  it("uses the profile title, including for a profile the options exclude", () => {
    const profiles = [
      { name: "llm-provider-profiles/main", title: "Main" },
      { name: "llm-provider-profiles/gone", title: "Gone but titled" },
    ];
    expect(profileTitle("llm-provider-profiles/main", profiles)).toBe("Main");
    expect(profileTitle("llm-provider-profiles/gone", profiles)).toBe(
      "Gone but titled"
    );
  });

  it("falls back to the reference when there is no title", () => {
    expect(profileTitle("llm-provider-profiles/gone", [])).toBe(
      "llm-provider-profiles/gone"
    );
    expect(
      profileTitle("llm-provider-profiles/untitled", [
        { name: "llm-provider-profiles/untitled", title: "" },
      ])
    ).toBe("llm-provider-profiles/untitled");
  });
});

const LIMITS: ModelCapacityLimits = {
  defaultContextWindow: 262_144,
  minContextWindow: 1,
  maxContextWindow: 10_000_000,
  defaultMaxOutputTokens: 32_768,
  minMaxOutputTokens: 256,
  maxMaxOutputTokens: 131_072,
};

describe("parseTokenCapacity", () => {
  it("reads plain and decimal-suffixed counts", () => {
    expect(parseTokenCapacity("")).toBeUndefined();
    expect(parseTokenCapacity("   ")).toBeUndefined();
    expect(parseTokenCapacity("4096")).toBe(4096);
    expect(parseTokenCapacity("128K")).toBe(128_000);
    expect(parseTokenCapacity(" 1m ")).toBe(1_000_000);
  });

  it("rejects anything that is not a whole count with an optional K/M", () => {
    expect(parseTokenCapacity("1.5K")).toBeNaN();
    expect(parseTokenCapacity("12KB")).toBeNaN();
    expect(parseTokenCapacity("-1")).toBeNaN();
    expect(parseTokenCapacity("abc")).toBeNaN();
  });
});

describe("formatTokenCapacity", () => {
  it("uses the shortest decimal form that round-trips", () => {
    expect(formatTokenCapacity(128_000)).toBe("128K");
    expect(formatTokenCapacity(1_000_000)).toBe("1M");
    expect(formatTokenCapacity(131_072)).toBe("131072");
    expect(formatTokenCapacity(32_768)).toBe("32768");
    expect(formatTokenCapacity(0)).toBe("0");
  });
});

function row(
  name: string,
  contextWindow = "",
  maxOutputTokens = "",
  enabled = true
): ModelCapacityRow {
  return { name, enabled, contextWindow, maxOutputTokens };
}

describe("validateModelRows", () => {
  it("accepts defaults and explicit valid pairs", () => {
    expect(
      validateModelRows([row("a"), row("b", "131072", "8192")], LIMITS)
    ).toEqual([undefined, undefined]);
  });

  it("requires unique non-empty names", () => {
    const errors = validateModelRows(
      [row(""), row("dup"), row(" dup ")],
      LIMITS
    );
    expect(errors[0]?.key).toBe("modelNameRequired");
    expect(errors[1]).toBeUndefined();
    expect(errors[2]?.key).toBe("modelNameDuplicate");
  });

  it("rejects unreadable and out-of-range capacities", () => {
    const errors = validateModelRows(
      [
        row("abc", "abc"),
        row("small", "0", "100"),
        row("big-window", "20000000"),
        row("big-output", "131072", "131073"),
      ],
      LIMITS
    );
    expect(errors[0]?.key).toBe("contextWindowInvalid");
    expect(errors[1]?.key).toBe("modelMaxOutputTokensInvalid");
    expect(errors[2]?.key).toBe("contextWindowInvalid");
    expect(errors[3]?.key).toBe("modelMaxOutputTokensInvalid");
  });

  it("requires the output budget to leave message room", () => {
    const errors = validateModelRows(
      [row("mismatch", "16384"), row("equal", "32768", "32768")],
      LIMITS
    );
    expect(errors[0]?.key).toBe("modelCapacityMismatch");
    expect(errors[1]?.key).toBe("modelCapacityMismatch");
  });
});

describe("mergeDiscoveredModels", () => {
  const limits = LIMITS;

  it("appends discovered models disabled with their disclosed capacities", () => {
    const discovered: DiscoveredModelCapacity[] = [
      { name: "openrouter", contextWindow: 200_000, maxOutputTokens: 16_000 },
      { name: "plain", contextWindow: 0, maxOutputTokens: 0 },
    ];
    const merged = mergeDiscoveredModels([], discovered, limits);
    expect(merged).toEqual([
      {
        name: "openrouter",
        enabled: false,
        contextWindow: "200K",
        maxOutputTokens: "16K",
      },
      {
        name: "plain",
        enabled: false,
        contextWindow: "",
        maxOutputTokens: "",
      },
    ]);
  });

  it("keeps saved and hand-added rows and fills only blank capacities", () => {
    const merged = mergeDiscoveredModels(
      [row("kept", "131072", "8192"), row("hand", "", "")],
      [
        { name: "kept", contextWindow: 200_000, maxOutputTokens: 16_000 },
        { name: "hand", contextWindow: 128_000, maxOutputTokens: 4_000 },
      ],
      limits
    );
    expect(merged[0]).toEqual(row("kept", "131072", "8192"));
    expect(merged[1]).toEqual(row("hand", "128K", "4K"));
    expect(merged).toHaveLength(2);
  });

  it("drops duplicate saved rows and ignores invalid disclosures", () => {
    const merged = mergeDiscoveredModels(
      [row("dup", "", ""), row("dup", "65536", "4096")],
      [
        { name: "bad-range", contextWindow: 99_999_999, maxOutputTokens: 1 },
        {
          name: "bad-relation",
          contextWindow: 16_384,
          maxOutputTokens: 32_768,
        },
        { name: "", contextWindow: 1, maxOutputTokens: 1 },
      ],
      limits
    );
    expect(merged).toHaveLength(3);
    expect(merged[0].name).toBe("dup");
    expect(merged[1]).toMatchObject({ name: "bad-range", contextWindow: "" });
    expect(merged[2]).toMatchObject({
      name: "bad-relation",
      contextWindow: "",
      maxOutputTokens: "",
    });
  });

  it("keeps a row whose blank capacity cannot adopt an invalid disclosure", () => {
    const merged = mergeDiscoveredModels(
      [row("m", "32768", "")],
      [{ name: "m", contextWindow: 65_536, maxOutputTokens: 32_768 }],
      limits
    );
    expect(merged[0]).toEqual(row("m", "32768", ""));
  });
});
