import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import SuggestionJobTargetDebugDialog from "./SuggestionJobTargetDebugDialog.vue";

const mocks = vi.hoisted(() => ({
  getQualitySuggestionJobTargetDebug: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  getQualitySuggestionJobTargetDebug: mocks.getQualitySuggestionJobTargetDebug,
}));

const JOB = "workspaces/ws/qualitySuggestionJobs/job-1";
const CORRELATION = "corr-123";
const REQUEST = '{"model":"gpt-test","max_output_tokens":8192}';
const RESPONSE = '{"choices":[{"finish_reason":"length"}]}';

function response(overrides: Record<string, unknown> = {}) {
  return {
    available: true,
    correlationId: CORRELATION,
    requestBody: REQUEST,
    responseBody: RESPONSE,
    errorCode: "TRUNCATED",
    ...overrides,
  };
}

function mountDialog() {
  const pinia = createPinia();
  setActivePinia(pinia);
  return mount(SuggestionJobTargetDebugDialog, {
    attachTo: document.body,
    props: {
      open: true,
      jobName: JOB,
      targetIndex: 1,
      correlationId: CORRELATION,
    },
    global: { plugins: [i18n, pinia] },
  });
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function buttonByText(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.includes(text));
}

describe("SuggestionJobTargetDebugDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  afterEach(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: undefined,
    });
  });

  it("fetches and renders the captured request and response", async () => {
    mocks.getQualitySuggestionJobTargetDebug.mockResolvedValue(response());
    const wrapper = mountDialog();
    await flushPromises();

    expect(mocks.getQualitySuggestionJobTargetDebug).toHaveBeenCalledWith(
      JOB,
      1
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.diagnostic.payload.request")
    );
    expect(document.body.textContent).toContain(REQUEST);
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.diagnostic.payload.response")
    );
    expect(document.body.textContent).toContain(RESPONSE);
    expect(document.body.textContent).toContain(CORRELATION);

    // Copy one body through the clipboard.
    const copies = [
      ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] pre'),
    ];
    expect(copies).toHaveLength(2);
    const copyButtons = [
      ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
    ].filter((button) => button.textContent?.trim() === label("common.copy"));
    expect(copyButtons.length).toBeGreaterThanOrEqual(2);
    copyButtons[0]?.click();
    await flushPromises();
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(REQUEST);
    wrapper.unmount();
  });

  it("offers download for each body", async () => {
    mocks.getQualitySuggestionJobTargetDebug.mockResolvedValue(response());
    const originalCreate = URL.createObjectURL;
    const originalRevoke = URL.revokeObjectURL;
    const createObjectURL = vi.fn(() => "blob:test");
    const revokeObjectURL = vi.fn();
    const anchorClick = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: createObjectURL,
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: revokeObjectURL,
    });
    const wrapper = mountDialog();
    await flushPromises();

    buttonByText(
      label("dataQuality.suggestions.diagnostic.payload.download")
    )?.click();
    await flushPromises();

    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledTimes(1);
    expect(anchorClick).toHaveBeenCalledTimes(1);
    wrapper.unmount();
    anchorClick.mockRestore();
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: originalCreate,
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: originalRevoke,
    });
  });

  it("explains that the bodies were not captured or are pruned", async () => {
    mocks.getQualitySuggestionJobTargetDebug.mockResolvedValue(
      response({ available: false, requestBody: "", responseBody: "" })
    );
    const wrapper = mountDialog();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.diagnostic.payload.unavailableTitle")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.diagnostic.payload.unavailableHint")
    );
    expect(document.body.textContent).not.toContain(REQUEST);
    wrapper.unmount();
  });

  it("shows a fetch failure and retries on demand", async () => {
    mocks.getQualitySuggestionJobTargetDebug
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValue(response());
    const wrapper = mountDialog();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.diagnostic.payload.fetchFailed")
    );

    buttonByText(label("common.refresh"))?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(REQUEST);
    expect(mocks.getQualitySuggestionJobTargetDebug).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });
});
