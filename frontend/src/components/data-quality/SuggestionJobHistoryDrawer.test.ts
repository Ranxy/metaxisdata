import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import TablePager from "@/components/common/TablePager.vue";
import { Select } from "@/components/ui/select";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import {
  type QualityLLMDiagnostic,
  QualityLLMDiagnosticSchema,
  QualitySuggestionJobSchema,
  QualitySuggestionJobStatus,
  QualitySuggestionJobTargetSchema,
  QualitySuggestionJobTargetState,
  type QualitySuggestionQueueSummary,
  QualitySuggestionQueueSummarySchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import SuggestionJobHistoryDrawer from "./SuggestionJobHistoryDrawer.vue";

const mocks = vi.hoisted(() => ({
  listQualitySuggestionJobs: vi.fn(),
  getQualitySuggestionJobTargetDebug: vi.fn(),
  cancelQualitySuggestionJob: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualitySuggestionJobs: mocks.listQualitySuggestionJobs,
  getQualitySuggestionJobTargetDebug: mocks.getQualitySuggestionJobTargetDebug,
  cancelQualitySuggestionJob: mocks.cancelQualitySuggestionJob,
}));

const JOB = "workspaces/ws/qualitySuggestionJobs/job-1";
const OTHER_JOB = "workspaces/ws/qualitySuggestionJobs/job-2";
const TARGET = "inst1;db1;public;orders";
const OTHER_TARGET = "inst1;db1;public;customers";

function targetFixture(options: {
  guid?: string;
  object?: string;
  state?: QualitySuggestionJobTargetState;
  suggestions?: string[];
  error?: string;
  diagnostic?: QualityLLMDiagnostic;
  summary?: QualitySuggestionQueueSummary;
}) {
  return create(QualitySuggestionJobTargetSchema, {
    target: create(QualityTargetSchema, {
      metaGuid: options.guid ?? TARGET,
      metaType: MetaType.TABLE,
      path: {
        instance: "inst1",
        database: "db1",
        schema: "public",
        object: options.object ?? "orders",
      },
    }),
    state: options.state ?? QualitySuggestionJobTargetState.PENDING,
    suggestions: options.suggestions ?? [],
    error: options.error ?? "",
    diagnostic: options.diagnostic,
    summary: options.summary,
  });
}

function jobFixture(
  options: {
    name?: string;
    status?: QualitySuggestionJobStatus;
    intent?: string;
    targets?: ReturnType<typeof targetFixture>[];
    completed?: number;
    suggestions?: number;
    error?: string;
  } = {}
) {
  const targets = options.targets ?? [
    targetFixture({ state: QualitySuggestionJobTargetState.RUNNING }),
    targetFixture({
      guid: OTHER_TARGET,
      object: "customers",
      state: QualitySuggestionJobTargetState.PENDING,
    }),
  ];
  return create(QualitySuggestionJobSchema, {
    name: options.name ?? JOB,
    status: options.status ?? QualitySuggestionJobStatus.RUNNING,
    intent: options.intent ?? "Keep the orders table healthy",
    language: "en-US",
    targets,
    targetCount: targets.length,
    completedTargetCount: options.completed ?? 0,
    suggestionCount: options.suggestions ?? 0,
    error: options.error ?? "",
    createTime: { seconds: 1_700_000_000n, nanos: 0 },
  });
}

function truncatedDiagnostic(): QualityLLMDiagnostic {
  return create(QualityLLMDiagnosticSchema, {
    errorCode: "TRUNCATED",
    provider: "openai",
    model: "gpt-test",
    promptVersion: "dq-rules-v3",
    maxOutputTokens: 8192,
    finishReason: "length",
    promptTokens: 1200n,
    completionTokens: 8192n,
    totalTokens: 9392n,
    promptChars: 5400n,
    correlationId: "corr-123",
    providerStatus: 200,
    debugPayloadAvailable: true,
  });
}

function page(items: unknown[], nextPageToken = "") {
  return { items, nextPageToken };
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function buttonByText(text: string): HTMLElement | undefined {
  return [...document.body.querySelectorAll<HTMLElement>("button")].find(
    (button) => button.textContent?.includes(text)
  );
}

function expandButton(ariaLabel: string): HTMLElement | undefined {
  return (
    document.body.querySelector<HTMLElement>(`[aria-label="${ariaLabel}"]`) ??
    undefined
  );
}

async function mountDrawer(open = true) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const wrapper = mount(SuggestionJobHistoryDrawer, {
    attachTo: document.body,
    props: { open },
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("SuggestionJobHistoryDrawer", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.getQualitySuggestionJobTargetDebug.mockReset();
    mocks.cancelQualitySuggestionJob.mockReset();
    mocks.listQualitySuggestionJobs.mockResolvedValue(page([]));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("lists a page of jobs with status, progress and the pager", async () => {
    mocks.listQualitySuggestionJobs.mockResolvedValue(
      page(
        [
          jobFixture({
            completed: 1,
            suggestions: 3,
            targets: [
              targetFixture({
                state: QualitySuggestionJobTargetState.DONE,
                suggestions: ["workspaces/ws/qualitySuggestions/s-1"],
              }),
              targetFixture({
                guid: OTHER_TARGET,
                object: "customers",
                state: QualitySuggestionJobTargetState.PENDING,
              }),
            ],
          }),
          jobFixture({
            name: OTHER_JOB,
            status: QualitySuggestionJobStatus.COMPLETED,
            intent: "Cover the older tables",
            targets: [
              targetFixture({
                state: QualitySuggestionJobTargetState.DONE,
                suggestions: ["workspaces/ws/qualitySuggestions/s-2"],
              }),
            ],
          }),
        ],
        "next-page"
      )
    );
    const wrapper = await mountDrawer();

    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledWith(
      expect.objectContaining({ pageSize: 20, pageToken: "" })
    );
    expect(document.body.textContent).toContain(
      "Keep the orders table healthy"
    );
    expect(document.body.textContent).toContain("Cover the older tables");
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.progress", {
        completed: 1,
        total: 2,
        suggestions: 3,
        count: 3,
      })
    );

    buttonByText(label("common.next"))?.click();
    await flushPromises();
    expect(mocks.listQualitySuggestionJobs).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "next-page" })
    );

    buttonByText(label("common.previous"))?.click();
    await flushPromises();
    expect(mocks.listQualitySuggestionJobs).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "" })
    );
    wrapper.unmount();
  });

  it("restarts the walk at page one when the status filter changes", async () => {
    mocks.listQualitySuggestionJobs
      .mockResolvedValueOnce(page([jobFixture()], "next-page"))
      .mockResolvedValue(page([]));
    const wrapper = await mountDrawer();

    wrapper
      .findComponent(Select)
      .vm.$emit(
        "update:modelValue",
        String(QualitySuggestionJobStatus.COMPLETED)
      );
    await flushPromises();

    expect(mocks.listQualitySuggestionJobs).toHaveBeenLastCalledWith(
      expect.objectContaining({
        pageToken: "",
        status: QualitySuggestionJobStatus.COMPLETED,
      })
    );
    // Nothing completed: the filtered empty state offers the way back.
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.emptyFiltered")
    );
    buttonByText(label("dataQuality.suggestions.jobs.clearFilter"))?.click();
    await flushPromises();

    expect(mocks.listQualitySuggestionJobs).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "", status: undefined })
    );
    wrapper.unmount();
  });

  it("restarts the walk at page one when the page size changes", async () => {
    mocks.listQualitySuggestionJobs.mockResolvedValue(
      page([jobFixture()], "next-page")
    );
    const wrapper = await mountDrawer();

    wrapper.findComponent(TablePager).vm.$emit("update:pageSize", 50);
    await flushPromises();

    expect(mocks.listQualitySuggestionJobs).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageSize: 50, pageToken: "" })
    );
    wrapper.unmount();
  });

  it("expands the targets of one job at a time", async () => {
    mocks.listQualitySuggestionJobs.mockResolvedValue(
      page([
        jobFixture({
          completed: 1,
          suggestions: 3,
          targets: [
            targetFixture({
              state: QualitySuggestionJobTargetState.DONE,
              suggestions: ["workspaces/ws/qualitySuggestions/s-1"],
              summary: create(QualitySuggestionQueueSummarySchema, {
                created: 1,
                refreshed: 2,
                skippedDecided: 1,
              }),
            }),
            targetFixture({
              guid: OTHER_TARGET,
              object: "customers",
              state: QualitySuggestionJobTargetState.FAILED,
              error: "the provider rejected the prompt",
            }),
          ],
        }),
        jobFixture({
          name: OTHER_JOB,
          status: QualitySuggestionJobStatus.COMPLETED,
          intent: "Cover the older tables",
          targets: [targetFixture({ object: "invoices" })],
        }),
      ])
    );
    const wrapper = await mountDrawer();

    expandButton(label("dataQuality.suggestions.jobs.expand"))?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      "inst1 · db1 · public · orders"
    );
    expect(document.body.textContent).toContain(
      "inst1 · db1 · public · customers"
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestionJobTargetState.done")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestionJobTargetState.failed")
    );
    expect(document.body.textContent).toContain(
      "the provider rejected the prompt"
    );
    // The job persists each target's generation summary, so the drawer can say
    // what the target's pass created, refreshed and skipped.
    expect(document.body.textContent).toContain(
      [
        label("dataQuality.suggestionSummary.created", { count: 1 }),
        label("dataQuality.suggestionSummary.refreshed", { count: 2 }),
        label("dataQuality.suggestionSummary.skipped", { count: 1 }),
      ].join(" · ")
    );

    // Opening another row closes the first: expansion never accumulates.
    expandButton(label("dataQuality.suggestions.jobs.expand"))?.click();
    await flushPromises();
    expect(document.body.textContent).not.toContain(
      "inst1 · db1 · public · orders"
    );
    expect(
      document.body.querySelectorAll(
        `[aria-label="${label("dataQuality.suggestions.jobs.collapse")}"]`
      )
    ).toHaveLength(1);
    wrapper.unmount();
  });

  it("explains a failed model call and opens its raw payload", async () => {
    mocks.listQualitySuggestionJobs.mockResolvedValue(
      page([
        jobFixture({
          targets: [
            targetFixture({
              state: QualitySuggestionJobTargetState.FAILED,
              error:
                "internal: the LLM analysis failed: LLM response was truncated by the provider (finish_reason=length)",
              diagnostic: truncatedDiagnostic(),
            }),
          ],
        }),
      ])
    );
    mocks.getQualitySuggestionJobTargetDebug.mockResolvedValue({
      available: true,
      correlationId: "corr-123",
      requestBody: '{"model":"gpt-test"}',
      responseBody: '{"finish_reason":"length"}',
      errorCode: "TRUNCATED",
    });
    const wrapper = await mountDrawer();
    expandButton(label("dataQuality.suggestions.jobs.expand"))?.click();
    await flushPromises();

    // The classifier label and the actionable hint replace the raw error line.
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.diagnostic.code.truncated")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.diagnostic.hint.truncated")
    );
    expect(document.body.textContent).toContain("openai · gpt-test");
    expect(document.body.textContent).toContain("length");
    expect(document.body.textContent).toContain("8,192");
    expect(document.body.textContent).toContain("5,400");
    expect(document.body.textContent).toContain("corr-123");
    expect(document.body.textContent).not.toContain(
      "LLM response was truncated by the provider"
    );

    buttonByText(
      label("dataQuality.suggestions.diagnostic.viewPayload")
    )?.click();
    await flushPromises();

    expect(mocks.getQualitySuggestionJobTargetDebug).toHaveBeenCalledWith(
      JOB,
      0
    );
    expect(document.body.textContent).toContain('{"model":"gpt-test"}');
    wrapper.unmount();
  });

  it("cancels an active job after a two-step confirm and asks for a refresh", async () => {
    const success = vi.spyOn(notify, "success");
    mocks.listQualitySuggestionJobs
      .mockResolvedValueOnce(page([jobFixture()]))
      .mockResolvedValue(
        page([
          jobFixture({
            status: QualitySuggestionJobStatus.CANCELED,
            completed: 2,
            targets: [
              targetFixture({
                state: QualitySuggestionJobTargetState.SKIPPED,
              }),
              targetFixture({
                guid: OTHER_TARGET,
                object: "customers",
                state: QualitySuggestionJobTargetState.SKIPPED,
              }),
            ],
          }),
        ])
      );
    mocks.cancelQualitySuggestionJob.mockResolvedValue(
      jobFixture({ status: QualitySuggestionJobStatus.CANCELED })
    );
    const wrapper = await mountDrawer();

    buttonByText(label("dataQuality.suggestions.jobs.cancel"))?.click();
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.cancelConfirm")
    );

    buttonByText(
      label("dataQuality.suggestions.jobs.cancelConfirmAction")
    )?.click();
    await flushPromises();

    expect(mocks.cancelQualitySuggestionJob).toHaveBeenCalledWith(JOB);
    expect(success).toHaveBeenCalledWith(
      label("dataQuality.suggestions.jobs.canceledToast")
    );
    expect(wrapper.emitted("changed")).toHaveLength(1);
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestionJobStatus.canceled")
    );
    success.mockRestore();
    wrapper.unmount();
  });

  it("keeps the job running when the confirmation is dismissed", async () => {
    mocks.listQualitySuggestionJobs.mockResolvedValue(page([jobFixture()]));
    const wrapper = await mountDrawer();

    buttonByText(label("dataQuality.suggestions.jobs.cancel"))?.click();
    await flushPromises();
    buttonByText(label("dataQuality.suggestions.jobs.cancelKeep"))?.click();
    await flushPromises();

    expect(mocks.cancelQualitySuggestionJob).not.toHaveBeenCalled();
    expect(
      buttonByText(label("dataQuality.suggestions.jobs.cancel"))
    ).toBeTruthy();
    wrapper.unmount();
  });

  it("polls while the visible page holds an active job", async () => {
    vi.useFakeTimers();
    mocks.listQualitySuggestionJobs
      .mockResolvedValueOnce(page([jobFixture()]))
      .mockResolvedValue(
        page([
          jobFixture({
            status: QualitySuggestionJobStatus.COMPLETED,
            completed: 2,
            targets: [
              targetFixture({ state: QualitySuggestionJobTargetState.DONE }),
              targetFixture({
                guid: OTHER_TARGET,
                object: "customers",
                state: QualitySuggestionJobTargetState.DONE,
              }),
            ],
          }),
        ])
      );
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(SuggestionJobHistoryDrawer, {
      attachTo: document.body,
      props: { open: true },
      global: { plugins: [i18n, pinia] },
    });
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(2500);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(2);
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestionJobStatus.completed")
    );

    // A completed job stops the loop.
    await vi.advanceTimersByTimeAsync(10_000);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("stops polling when the drawer closes", async () => {
    vi.useFakeTimers();
    mocks.listQualitySuggestionJobs.mockResolvedValue(page([jobFixture()]));
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(SuggestionJobHistoryDrawer, {
      attachTo: document.body,
      props: { open: true },
      global: { plugins: [i18n, pinia] },
    });
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(2500);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(2);

    await wrapper.setProps({ open: false });
    await vi.advanceTimersByTimeAsync(10_000);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("loads only once it is opened", async () => {
    const wrapper = await mountDrawer(false);
    expect(mocks.listQualitySuggestionJobs).not.toHaveBeenCalled();

    await wrapper.setProps({ open: true });
    await flushPromises();
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("shows the empty state when the workspace has no jobs", async () => {
    const wrapper = await mountDrawer();
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.emptyTitle")
    );
    wrapper.unmount();
  });

  it("shows a fetch failure and clears it on refresh", async () => {
    mocks.listQualitySuggestionJobs
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValue(page([]));
    const wrapper = await mountDrawer();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.fetchFailed")
    );

    buttonByText(label("common.refresh"))?.click();
    await flushPromises();

    expect(document.body.textContent).not.toContain(
      label("dataQuality.suggestions.jobs.fetchFailed")
    );
    wrapper.unmount();
  });
});
