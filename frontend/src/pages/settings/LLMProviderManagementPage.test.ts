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
