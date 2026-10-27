import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  NullPercentConfigSchema,
  QualityRuleConfigSchema,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetCopilotPanel from "./TargetCopilotPanel.vue";

const mocks = vi.hoisted(() => ({
  suggestQualityRule: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  suggestQualityRule: mocks.suggestQualityRule,
}));

const TARGET = "inst1;db1;public;orders";
const DRAFT = "workspaces/ws/qualitySuggestions/draft-1";
const Dummy = { template: "<div />" };

function draftSuggestion(name = DRAFT) {
  return create(QualitySuggestionSchema, {
    name,
    kind: QualitySuggestionKind.RULE_DRAFT,
    status: QualitySuggestionStatus.PENDING,
    target: create(QualityTargetSchema, {
      metaGuid: TARGET,
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "rule",
        value: {
          title: "Emails must not be empty",
          rationale: "Email is the required contact field.",
          confidence: 0.8,
          severity: 2,
          config: create(QualityRuleConfigSchema, {
            config: {
              case: "nullPercent",
              value: create(NullPercentConfigSchema, { column: "email" }),
            },
          }),
        },
      },
    },
  });
}

async function mountPanel(
  props: Partial<{
    targetGuid: string;
    metaType: MetaType;
    canSuggest: boolean;
    disabledReason: string;
  }> = {}
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/settings/data-quality",
        name: "DataQualitySettings",
        component: Dummy,
      },
    ],
  });
  await router.push("/settings/data-quality");
  await router.isReady();
  const wrapper = mount(TargetCopilotPanel, {
    attachTo: document.body,
    props: {
      targetGuid: TARGET,
      metaType: MetaType.TABLE,
      canSuggest: true,
      ...props,
    },
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return { wrapper, router };
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function buttonByText(text: string): HTMLElement | undefined {
  return [...document.body.querySelectorAll<HTMLElement>("button")].find(
    (button) => button.textContent?.trim() === text
  );
}

function textarea(): HTMLTextAreaElement {
  const element = document.body.querySelector<HTMLTextAreaElement>("textarea");
  if (!element) {
    throw new Error("the copilot input is missing");
  }
  return element;
}

async function submitIntent(text: string) {
  const input = textarea();
  input.value = text;
  input.dispatchEvent(new Event("input"));
  await flushPromises();
  buttonByText(label("dataQuality.copilot.generate"))?.click();
  await flushPromises();
}

describe("TargetCopilotPanel", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.suggestQualityRule.mockResolvedValue({
      qualitySuggestions: [draftSuggestion()],
      summary: undefined,
    });
  });

  it("fills the intent from an example and drafts it for this target", async () => {
    const { wrapper } = await mountPanel();

    buttonByText(label("dataQuality.copilot.exampleNulls"))?.click();
    await flushPromises();
    expect(textarea().value).toBe(label("dataQuality.copilot.exampleNulls"));

    buttonByText(label("dataQuality.copilot.generate"))?.click();
    await flushPromises();

    expect(mocks.suggestQualityRule).toHaveBeenCalledWith(
      expect.objectContaining({
        tableGuid: TARGET,
        metaType: MetaType.TABLE,
        intent: label("dataQuality.copilot.exampleNulls"),
        language: "en-US",
      })
    );
    const drafted = wrapper.emitted("drafted");
    expect(drafted).toHaveLength(1);
    expect(drafted?.[0]?.[0]).toHaveLength(1);
    expect(drafted?.[0]?.[2]).toEqual([DRAFT]);

    // The pass reports its effect inline instead of a toast.
    expect(wrapper.text()).toContain(label("dataQuality.copilot.newDrafts"));
    wrapper.unmount();
  });

  it("submits on Enter but not on Shift+Enter", async () => {
    const { wrapper } = await mountPanel();

    const input = textarea();
    input.value = "Flag empty emails";
    input.dispatchEvent(new Event("input"));
    await flushPromises();
    input.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Enter", shiftKey: true })
    );
    await flushPromises();
    expect(mocks.suggestQualityRule).not.toHaveBeenCalled();

    input.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter" }));
    await flushPromises();
    expect(mocks.suggestQualityRule).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("explains a pass that queued nothing", async () => {
    mocks.suggestQualityRule.mockResolvedValue({
      qualitySuggestions: [],
      summary: undefined,
    });
    const { wrapper } = await mountPanel();

    await submitIntent("Flag empty emails");

    expect(wrapper.text()).toContain(label("dataQuality.copilot.noDrafts"));
    wrapper.unmount();
  });

  it("points at the settings when model assistance is off", async () => {
    mocks.suggestQualityRule.mockRejectedValue(
      new ConnectError("model assistance is off", Code.FailedPrecondition)
    );
    const { wrapper } = await mountPanel();

    await submitIntent("Flag empty emails");

    expect(wrapper.text()).toContain(label("dataQuality.copilot.needLlmTitle"));
    expect(wrapper.text()).toContain(label("dataQuality.copilot.needLlmHint"));
    expect(wrapper.find("a").attributes("href")).toContain(
      "/settings/data-quality"
    );
    // The failure never costs the reader the sentence they typed.
    expect(textarea().value).toBe("Flag empty emails");
    wrapper.unmount();
  });

  it("names a reached quota and a missing permission distinctly", async () => {
    mocks.suggestQualityRule.mockRejectedValueOnce(
      new ConnectError("quota", Code.ResourceExhausted)
    );
    const quota = await mountPanel();
    await submitIntent("Flag empty emails");
    expect(quota.wrapper.text()).toContain(
      label("dataQuality.copilot.quotaExceeded")
    );
    quota.wrapper.unmount();

    document.body.innerHTML = "";
    mocks.suggestQualityRule.mockRejectedValueOnce(
      new ConnectError("denied", Code.PermissionDenied)
    );
    const denied = await mountPanel();
    await submitIntent("Flag empty emails");
    expect(denied.wrapper.text()).toContain(
      label("dataQuality.copilot.noPermission")
    );
    denied.wrapper.unmount();
  });

  it("falls back to the generic failure copy with the server detail", async () => {
    mocks.suggestQualityRule.mockRejectedValue(
      new ConnectError("provider exploded", Code.Internal)
    );
    const { wrapper } = await mountPanel();

    await submitIntent("Flag empty emails");

    expect(wrapper.text()).toContain(label("dataQuality.copilot.failed"));
    expect(wrapper.text()).toContain(label("error.internal"));
    wrapper.unmount();
  });

  it("requires a non-empty intent before it will spend a pass", async () => {
    const { wrapper } = await mountPanel();

    const generate = buttonByText(label("dataQuality.copilot.generate"));
    expect((generate as HTMLButtonElement | undefined)?.disabled).toBe(true);

    const input = textarea();
    input.value = "   ";
    input.dispatchEvent(new Event("input"));
    await flushPromises();
    expect(
      (
        buttonByText(label("dataQuality.copilot.generate")) as
          | HTMLButtonElement
          | undefined
      )?.disabled
    ).toBe(true);
    wrapper.unmount();
  });

  it("stays closed when the caller may not draft or the target is unavailable", async () => {
    const forbidden = await mountPanel({ canSuggest: false });
    expect(
      (
        buttonByText(label("dataQuality.copilot.generate")) as
          | HTMLButtonElement
          | undefined
      )?.disabled
    ).toBe(true);
    forbidden.wrapper.unmount();

    document.body.innerHTML = "";
    const unavailable = await mountPanel({
      disabledReason: label("dataQuality.copilot.unavailable"),
    });
    expect(unavailable.wrapper.text()).toContain(
      label("dataQuality.copilot.unavailable")
    );
    expect(
      (
        buttonByText(label("dataQuality.copilot.generate")) as
          | HTMLButtonElement
          | undefined
      )?.disabled
    ).toBe(true);
    unavailable.wrapper.unmount();
  });
});
