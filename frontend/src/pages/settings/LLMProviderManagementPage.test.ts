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

const PROFILE = create(LlmProviderProfileSchema, {
  name: "llmProfiles/p1",
  title: "Primary",
  type: LLMProviderType.LLM_PROVIDER_TYPE_OPENAI,
  baseUrl: "https://api.openai.com",
  models: [create(LlmProviderModelSchema, { name: "gpt-4o", enabled: true })],
  maskedApiKey: "sk-…1234",
  maxOutputTokens: 16384,
});

const INPUT_ID = "#llm-provider-max-output-tokens";

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

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("LLMProviderManagementPage max output tokens", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listProfiles.mockResolvedValue({ profiles: [PROFILE] });
    mocks.createProfile.mockResolvedValue(PROFILE);
    mocks.updateProfile.mockResolvedValue(PROFILE);
    mocks.fetchModelsByProfile.mockResolvedValue({ modelIds: ["gpt-4o"] });
  });

  it("populates the stored budget and saves it in the update mask", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);

    expect((wrapper.find(INPUT_ID).element as HTMLInputElement).value).toBe(
      "16384"
    );

    await wrapper.find(INPUT_ID).setValue("4096");
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.updateProfile).toHaveBeenCalledTimes(1);
    const [profile, mask] = mocks.updateProfile.mock.calls[0] as [
      { maxOutputTokens: number },
      string[],
    ];
    expect(profile.maxOutputTokens).toBe(4096);
    expect(mask).toContain("max_output_tokens");
  });

  it("clears the budget back to the server default when blanked", async () => {
    const wrapper = await mountPage();
    await openProfile(wrapper);

    await wrapper.find(INPUT_ID).setValue("");
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    const [profile, mask] = mocks.updateProfile.mock.calls[0] as [
      { maxOutputTokens: number },
      string[],
    ];
    expect(profile.maxOutputTokens).toBe(0);
    expect(mask).toContain("max_output_tokens");
  });

  it("rejects a budget outside 256-131072 and saves a valid one on create", async () => {
    mocks.listProfiles.mockResolvedValue({ profiles: [] });
    const wrapper = await mountPage();

    const add = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("llmProvider.addProfile"))
      );
    await add?.trigger("click");
    await flushPromises();

    await wrapper.find(INPUT_ID).setValue("100");
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("llmProvider.maxOutputTokensInvalid", {
        min: 256,
        max: 131072,
      })
    );
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(true);

    await wrapper.find(INPUT_ID).setValue("4096");
    await flushPromises();
    expect(
      (saveButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(false);

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.createProfile).toHaveBeenCalledTimes(1);
    const profile = mocks.createProfile.mock.calls[0]?.[0] as {
      maxOutputTokens: number;
    };
    expect(profile.maxOutputTokens).toBe(4096);
  });

  it("sends zero for the server default when creating with a blank budget", async () => {
    mocks.listProfiles.mockResolvedValue({ profiles: [] });
    const wrapper = await mountPage();
    const add = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("llmProvider.addProfile"))
      );
    await add?.trigger("click");
    await flushPromises();

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    const profile = mocks.createProfile.mock.calls[0]?.[0] as {
      maxOutputTokens: number;
    };
    expect(profile.maxOutputTokens).toBe(0);
    wrapper.unmount();
  });
});

const CUSTOM_PROFILE = create(LlmProviderProfileSchema, {
  name: "llm-provider-profiles/p2",
  title: "In-house",
  type: LLMProviderType.LLM_PROVIDER_TYPE_CUSTOM,
  baseUrl: "https://llm.example.com/v1",
  models: [create(LlmProviderModelSchema, { name: "m1", enabled: true })],
  maskedApiKey: "abc…789",
  maxOutputTokens: 0,
});

function customProfileEntry(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes("In-house"));
}

function fetchModelsButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) =>
      button.text().includes(i18n.global.t("llmProvider.fetchModels"))
    );
}

describe("LLMProviderManagementPage fetch models", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listProfiles.mockResolvedValue({ profiles: [CUSTOM_PROFILE] });
    mocks.fetchModelsByKey.mockResolvedValue({ modelIds: ["m1"] });
    mocks.fetchModelsByProfile.mockResolvedValue({ modelIds: ["m1"] });
  });

  it("sends the typed base URL when fetching models for a custom provider", async () => {
    const wrapper = await mountPage();
    const entry = wrapper
      .findAll("button")
      .find((button) => button.text().includes("In-house"));
    expect(entry).toBeTruthy();
    await entry?.trigger("click");
    await flushPromises();

    // Editing a profile keeps its stored base URL; it used to be reset to the
    // provider type's default (empty for custom) on selection.
    const baseUrlInput = wrapper
      .findAll("input")
      .find((input) => input.element.value === "https://llm.example.com/v1");
    expect(baseUrlInput).toBeTruthy();

    await wrapper.find('input[type="password"]').setValue("sk-new");
    const fetchButton = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("llmProvider.fetchModels"))
      );
    expect(fetchButton).toBeTruthy();
    await fetchButton?.trigger("click");
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
    mocks.listProfiles.mockResolvedValue({ profiles: [CUSTOM_PROFILE] });
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

    expect(mocks.updateProfile).toHaveBeenCalledTimes(1);
    const [profile, mask] = mocks.updateProfile.mock.calls[0] as [
      { apiKey: string },
      string[],
    ];
    expect(profile.apiKey).toBe("");
    expect(mask).not.toContain("api_key");
  });
});
