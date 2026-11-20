import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  LLMProviderType,
  LlmProviderModelSchema,
  LlmProviderProfileSchema,
} from "@/types/proto-es/v1/llm_service_pb";
import LLMProviderManagementPage from "./LLMProviderManagementPage.vue";

const mocks = vi.hoisted(() => ({
  listProfiles: vi.fn(),
  createProfile: vi.fn(),
  updateProfile: vi.fn(),
  deleteProfile: vi.fn(),
  fetchModelsByKey: vi.fn(),
  fetchModelsByProfile: vi.fn(),
}));

vi.mock("@/api/llm", () => ({
  listProfiles: mocks.listProfiles,
  createProfile: mocks.createProfile,
  updateProfile: mocks.updateProfile,
  deleteProfile: mocks.deleteProfile,
  fetchModelsByKey: mocks.fetchModelsByKey,
  fetchModelsByProfile: mocks.fetchModelsByProfile,
}));

const LIMITS = {
  defaultContextWindow: 262_144,
  minContextWindow: 1,
  maxContextWindow: 10_000_000,
  defaultMaxOutputTokens: 32_768,
  minMaxOutputTokens: 256,
  maxMaxOutputTokens: 131_072,
};

const PROFILE = create(LlmProviderProfileSchema, {
  name: "llm-provider-profiles/p1",
  title: "Primary",
  type: LLMProviderType.LLM_PROVIDER_TYPE_OPENAI,
  baseUrl: "https://api.openai.com",
  models: [
    create(LlmProviderModelSchema, {
      name: "gpt-4o",
      enabled: true,
      contextWindow: 131_072,
      maxOutputTokens: 8_192,
    }),
  ],
  maskedApiKey: "sk-…1234",
});

function listProfilesResponse(profiles = [PROFILE]) {
  return { profiles, definitions: [], nextPageToken: "", limits: LIMITS };
}

async function mountPage() {
  const pinia = createPinia();
  setActivePinia(pinia);
  const wrapper = mount(LLMProviderManagementPage, {
    attachTo: document.body,
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

async function openProfile(wrapper: ReturnType<typeof mount>) {
  const entry = wrapper
    .findAll("button")
    .find((button) => button.text().includes("Primary"));
  expect(entry).toBeTruthy();
  await entry?.trigger("click");
  await flushPromises();
}

function saveButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes(i18n.global.t("common.save")));
}

function addModelButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) =>
      button.text().includes(i18n.global.t("llmProvider.addModel"))
    );
}

function fetchModelsButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) =>
      button.text().includes(i18n.global.t("llmProvider.fetchModels"))
    );
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function modelNameInputs(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("input")
    .filter(
      (input) =>
        input.attributes("placeholder") ===
        label("llmProvider.modelNamePlaceholder")
    );
}

function contextInputs(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("input")
    .filter(
      (input) =>
        input.attributes("placeholder") === String(LIMITS.defaultContextWindow)
    );
}

function outputInputs(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("input")
    .filter(
      (input) =>
        input.attributes("placeholder") ===
        String(LIMITS.defaultMaxOutputTokens)
    );
}

async function expandRow(wrapper: ReturnType<typeof mount>, index = 0) {
  const toggles = wrapper
    .findAll("button")
    .filter((button) =>
      button
        .attributes("aria-label")
        ?.startsWith(label("llmProvider.modelAdvanced"))
    );
  await toggles[index]?.trigger("click");
  await flushPromises();
}

function removeButtons(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .filter((button) =>
      button
        .attributes("aria-label")
        ?.startsWith(label("llmProvider.removeModel"))
    );
}

function enabledCheckboxes(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('[role="checkbox"]');
}

describe("LLMProviderManagementPage model capacities", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listProfiles.mockResolvedValue(listProfilesResponse());
    mocks.createProfile.mockResolvedValue(PROFILE);
    mocks.updateProfile.mockResolvedValue(PROFILE);
    mocks.fetchModelsByProfile.mockResolvedValue({
      models: [{ name: "gpt-4o", contextWindow: 0, maxOutputTokens: 0 }],
    });
  });

  it("populates stored capacities and saves them on the model array", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);
    await expandRow(wrapper);

    expect(modelNameInputs(wrapper)[0].element.value).toBe("gpt-4o");
    expect(contextInputs(wrapper)[0].element.value).toBe("131072");
    expect(outputInputs(wrapper)[0].element.value).toBe("8192");

    await contextInputs(wrapper)[0].setValue("64K");
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.updateProfile).toHaveBeenCalledTimes(1);
    const [profile, mask] = mocks.updateProfile.mock.calls[0] as [
      { models: { contextWindow: number; maxOutputTokens: number }[] },
      string[],
    ];
    expect(profile.models[0].contextWindow).toBe(64_000);
    expect(profile.models[0].maxOutputTokens).toBe(8_192);
    expect(mask).toContain("models");
    expect(mask).not.toContain("max_output_tokens");
  });

  it("clears a capacity back to the server default when blanked", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);
    await expandRow(wrapper);

    await contextInputs(wrapper)[0].setValue("");
    await outputInputs(wrapper)[0].setValue("");
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    const [profile] = mocks.updateProfile.mock.calls[0] as [
      { models: { contextWindow: number; maxOutputTokens: number }[] },
    ];
    expect(profile.models[0].contextWindow).toBe(0);
    expect(profile.models[0].maxOutputTokens).toBe(0);
  });

  it("rejects an output budget that consumes the context window", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);
    await expandRow(wrapper);

    await contextInputs(wrapper)[0].setValue("16384");
    await outputInputs(wrapper)[0].setValue("");
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("llmProvider.modelCapacityMismatch", { index: 1 })
    );
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(true);
  });

  it("adds, validates, and removes manual model rows", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);
    expect(modelNameInputs(wrapper)).toHaveLength(1);

    await addModelButton(wrapper)?.trigger("click");
    await flushPromises();
    expect(modelNameInputs(wrapper)).toHaveLength(2);
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(true);

    await modelNameInputs(wrapper)[1].setValue("gpt-4o");
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("llmProvider.modelNameDuplicate", { index: 2 })
    );

    await modelNameInputs(wrapper)[1].setValue("manual-model");
    await flushPromises();
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(false);

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();
    const [profile] = mocks.updateProfile.mock.calls[0] as [
      { models: { name: string; enabled: boolean }[] },
    ];
    expect(profile.models).toHaveLength(2);
    expect(profile.models[1]).toMatchObject({
      name: "manual-model",
      enabled: true,
    });
  });

  it("removes a model row from the saved array", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);
    await addModelButton(wrapper)?.trigger("click");
    await flushPromises();
    await modelNameInputs(wrapper)[1].setValue("manual-model");
    await flushPromises();

    await removeButtons(wrapper)[1].trigger("click");
    await flushPromises();
    expect(modelNameInputs(wrapper)).toHaveLength(1);

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();
    const [profile] = mocks.updateProfile.mock.calls[0] as [
      { models: unknown[] },
    ];
    expect(profile.models).toHaveLength(1);
  });

  it("requires a model name before saving", async () => {
    mocks.listProfiles.mockResolvedValue(listProfilesResponse([]));
    const wrapper = await mountPage();
    const addProfile = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("llmProvider.addProfile"))
      );
    await addProfile?.trigger("click");
    await flushPromises();

    await addModelButton(wrapper)?.trigger("click");
    await flushPromises();
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(true);
    expect(wrapper.text()).toContain(
      label("llmProvider.modelNameRequired", { index: 1 })
    );

    await modelNameInputs(wrapper)[0].setValue("m1");
    await flushPromises();
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(false);

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();
    const profile = mocks.createProfile.mock.calls[0]?.[0] as {
      models: {
        name: string;
        contextWindow: number;
        maxOutputTokens: number;
      }[];
    };
    expect(profile.models[0]).toMatchObject({
      name: "m1",
      contextWindow: 0,
      maxOutputTokens: 0,
    });
  });

  it("merges a fetched listing without losing saved capacities or rows", async () => {
    mocks.fetchModelsByProfile.mockResolvedValue({
      models: [
        { name: "gpt-4o", contextWindow: 0, maxOutputTokens: 0 },
        {
          name: "gpt-4o-mini",
          contextWindow: 200_000,
          maxOutputTokens: 16_000,
        },
      ],
    });
    const wrapper = await mountPage();
    await openProfile(wrapper);
    await addModelButton(wrapper)?.trigger("click");
    await flushPromises();
    await modelNameInputs(wrapper)[1].setValue("hand-added");
    await flushPromises();

    await fetchModelsButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(modelNameInputs(wrapper)).toHaveLength(3);
    expect(
      modelNameInputs(wrapper).map((input) => input.element.value)
    ).toEqual(["gpt-4o", "hand-added", "gpt-4o-mini"]);
    await expandRow(wrapper, 0);
    expect(contextInputs(wrapper)[0].element.value).toBe("131072");
    await expandRow(wrapper, 2);
    expect(contextInputs(wrapper)[2].element.value).toBe("200K");
  });
});

const CUSTOM_PROFILE = create(LlmProviderProfileSchema, {
  name: "llm-provider-profiles/p2",
  title: "In-house",
  type: LLMProviderType.LLM_PROVIDER_TYPE_CUSTOM,
  baseUrl: "https://llm.example.com/v1",
  models: [create(LlmProviderModelSchema, { name: "m1", enabled: true })],
  maskedApiKey: "abc…789",
});

function customProfileEntry(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes("In-house"));
}

describe("LLMProviderManagementPage fetch models", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listProfiles.mockResolvedValue(
      listProfilesResponse([CUSTOM_PROFILE])
    );
    mocks.fetchModelsByKey.mockResolvedValue({
      models: [{ name: "m1", contextWindow: 0, maxOutputTokens: 0 }],
    });
    mocks.fetchModelsByProfile.mockResolvedValue({
      models: [{ name: "m1", contextWindow: 0, maxOutputTokens: 0 }],
    });
  });

  it("sends the typed base URL when fetching models for a custom provider", async () => {
    const wrapper = await mountPage();
    await customProfileEntry(wrapper)?.trigger("click");
    await flushPromises();

    // Editing a profile keeps its stored base URL; it used to be reset to the
    // provider type's default (empty for custom) on selection.
    const baseUrlInput = wrapper
      .findAll("input")
      .find((input) => input.element.value === "https://llm.example.com/v1");
    expect(baseUrlInput).toBeTruthy();

    await wrapper.find('input[type="password"]').setValue("sk-new");
    await fetchModelsButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.fetchModelsByKey).toHaveBeenCalledWith(
      LLMProviderType.LLM_PROVIDER_TYPE_CUSTOM,
      "sk-new",
      "https://llm.example.com/v1"
    );
  });

  it("uses the stored key while the stored base URL is unchanged", async () => {
    const wrapper = await mountPage();
    await customProfileEntry(wrapper)?.trigger("click");
    await flushPromises();

    await fetchModelsButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.fetchModelsByProfile).toHaveBeenCalledWith(
      "llm-provider-profiles/p2"
    );
    expect(mocks.fetchModelsByKey).not.toHaveBeenCalled();
    expect(wrapper.text()).not.toContain(
      label("llmProvider.apiKeyRequiredForBaseUrl")
    );
  });

  it("asks for the key instead of forwarding the stored one to a changed base URL", async () => {
    const wrapper = await mountPage();
    await customProfileEntry(wrapper)?.trigger("click");
    await flushPromises();

    const baseUrlInput = wrapper
      .findAll("input")
      .find((input) => input.element.value === "https://llm.example.com/v1");
    expect(baseUrlInput).toBeTruthy();
    await baseUrlInput?.setValue("https://other.example.com");

    await fetchModelsButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.fetchModelsByProfile).not.toHaveBeenCalled();
    expect(mocks.fetchModelsByKey).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      label("llmProvider.apiKeyRequiredForBaseUrl")
    );
  });
});

describe("LLMProviderManagementPage API key save", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listProfiles.mockResolvedValue(
      listProfilesResponse([CUSTOM_PROFILE])
    );
    mocks.updateProfile.mockResolvedValue(CUSTOM_PROFILE);
  });

  it("trims the key before writing it", async () => {
    const wrapper = await mountPage();
    await customProfileEntry(wrapper)?.trigger("click");
    await flushPromises();

    await wrapper.find('input[type="password"]').setValue("  sk-new  ");
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.updateProfile).toHaveBeenCalledTimes(1);
    const [profile, mask] = mocks.updateProfile.mock.calls[0] as [
      { apiKey: string },
      string[],
    ];
    expect(profile.apiKey).toBe("sk-new");
    expect(mask).toContain("api_key");
  });

  it("does not overwrite the stored key with whitespace", async () => {
    const wrapper = await mountPage();
    await customProfileEntry(wrapper)?.trigger("click");
    await flushPromises();

    await wrapper.find('input[type="password"]').setValue("   ");
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    const [profile, mask] = mocks.updateProfile.mock.calls[0] as [
      { apiKey: string },
      string[],
    ];
    expect(profile.apiKey).toBe("");
    expect(mask).not.toContain("api_key");
  });
});

describe("LLMProviderManagementPage capacity limits and row editing", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listProfiles.mockResolvedValue(listProfilesResponse());
    mocks.updateProfile.mockResolvedValue(PROFILE);
  });

  it("uses the server capacity limits for placeholders and validation", async () => {
    const limited = {
      ...LIMITS,
      defaultContextWindow: 128_000,
      maxContextWindow: 200_000,
      defaultMaxOutputTokens: 8_192,
      minMaxOutputTokens: 128,
      maxMaxOutputTokens: 16_384,
    };
    mocks.listProfiles.mockResolvedValue({
      ...listProfilesResponse(),
      limits: limited,
    });
    const wrapper = await mountPage();
    await openProfile(wrapper);
    await expandRow(wrapper);

    const customContextInput = wrapper
      .findAll("input")
      .find((input) => input.attributes("placeholder") === "128000");
    const customOutputInput = wrapper
      .findAll("input")
      .find((input) => input.attributes("placeholder") === "8192");
    expect(customContextInput).toBeTruthy();
    expect(customOutputInput).toBeTruthy();

    await customContextInput?.setValue("300000");
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("llmProvider.contextWindowInvalid", {
        index: 1,
        min: limited.minContextWindow,
        max: limited.maxContextWindow,
      })
    );

    await customContextInput?.setValue("");
    await customOutputInput?.setValue("20000");
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("llmProvider.modelMaxOutputTokensInvalid", {
        index: 1,
        min: limited.minMaxOutputTokens,
        max: limited.maxMaxOutputTokens,
      })
    );
  });

  it("reports missing capacity limits and keeps saving disabled", async () => {
    mocks.listProfiles.mockResolvedValue({
      profiles: [PROFILE],
      definitions: [],
      nextPageToken: "",
    });
    const wrapper = await mountPage();
    await openProfile(wrapper);

    expect(wrapper.text()).toContain(label("llmProvider.limitsUnavailable"));
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(true);
  });

  it("reports a failed fetch and still saves a hand-added row", async () => {
    mocks.fetchModelsByProfile.mockRejectedValue(new Error("boom"));
    const wrapper = await mountPage();
    await openProfile(wrapper);
    await addModelButton(wrapper)?.trigger("click");
    await flushPromises();
    await modelNameInputs(wrapper)[1].setValue("manual-model");
    await flushPromises();

    await fetchModelsButton(wrapper)?.trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("boom");
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(false);

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();
    const [profile] = mocks.updateProfile.mock.calls[0] as [
      { models: { name: string }[] },
    ];
    expect(profile.models[1].name).toBe("manual-model");
  });

  it("saves the enabled flag from the row checkbox", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);

    await enabledCheckboxes(wrapper)[0].trigger("click");
    await flushPromises();
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    const [profile] = mocks.updateProfile.mock.calls[0] as [
      { models: { enabled: boolean }[] },
    ];
    expect(profile.models[0].enabled).toBe(false);
  });

  it("renders field-specific validation next to the edited capacity", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);
    await expandRow(wrapper);

    await contextInputs(wrapper)[0].setValue("abc");
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("llmProvider.contextWindowInvalid", {
        index: 1,
        min: LIMITS.minContextWindow,
        max: LIMITS.maxContextWindow,
      })
    );

    await contextInputs(wrapper)[0].setValue("");
    await outputInputs(wrapper)[0].setValue("100");
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("llmProvider.modelMaxOutputTokensInvalid", {
        index: 1,
        min: LIMITS.minMaxOutputTokens,
        max: LIMITS.maxMaxOutputTokens,
      })
    );
  });
});
