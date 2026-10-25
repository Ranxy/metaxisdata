import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import {
  type QualitySuggestionJob,
  QualitySuggestionJobSchema,
  QualitySuggestionJobStatus,
  QualitySuggestionJobTargetSchema,
  QualitySuggestionJobTargetState,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import SuggestionJobHistoryDrawer from "./SuggestionJobHistoryDrawer.vue";
import SuggestionJobsPanel from "./SuggestionJobsPanel.vue";

const mocks = vi.hoisted(() => ({
  listQualitySuggestionJobs: vi.fn(),
  getQualitySuggestionJob: vi.fn(),
  getQualitySuggestionJobTargetDebug: vi.fn(),
  cancelQualitySuggestionJob: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualitySuggestionJobs: mocks.listQualitySuggestionJobs,
  getQualitySuggestionJob: mocks.getQualitySuggestionJob,
  getQualitySuggestionJobTargetDebug: mocks.getQualitySuggestionJobTargetDebug,
  cancelQualitySuggestionJob: mocks.cancelQualitySuggestionJob,
}));

const JOB = "workspaces/ws/qualitySuggestionJobs/job-1";
const OLD_JOB = "workspaces/ws/qualitySuggestionJobs/job-old";
const TARGET = "inst1;db1;public;orders";
const OTHER_TARGET = "inst1;db1;public;customers";
const NEW_SUGGESTIONS = [
  "workspaces/ws/qualitySuggestions/s-1",
  "workspaces/ws/qualitySuggestions/s-2",
];

function targetFixture(options: {
  guid?: string;
  object?: string;
  state?: QualitySuggestionJobTargetState;
  suggestions?: string[];
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

/** One load snapshot: the recent page plus the independent RUNNING list. */
interface Snapshot {
  recent: QualitySuggestionJob[];
  running?: QualitySuggestionJob[];
}

/**
 * Answer the two list calls of each load from the same snapshot. The panel
 * asks for the recent page first and the RUNNING list second; both belong to
 * one snapshot, so the pair advances together.
 */
function mockSnapshots(...snapshots: Snapshot[]) {
  let pair = 0;
  mocks.listQualitySuggestionJobs.mockImplementation(
    async (options: { status?: QualitySuggestionJobStatus } = {}) => {
      const snapshot = snapshots[Math.min(pair, snapshots.length - 1)];
      if (options.status === QualitySuggestionJobStatus.RUNNING) {
        pair += 1;
        return page(snapshot?.running ?? []);
      }
      return page(snapshot?.recent ?? []);
    }
  );
}

async function mountPanel(refreshKey = 0) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const wrapper = mount(SuggestionJobsPanel, {
    attachTo: document.body,
    props: { refreshKey },
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
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

function completedJobs(count: number): QualitySuggestionJob[] {
  return Array.from({ length: count }, (_, index) =>
    jobFixture({
      name: `workspaces/ws/qualitySuggestionJobs/newer-${index}`,
      status: QualitySuggestionJobStatus.COMPLETED,
      intent: `Filled job ${index}`,
    })
  );
}

describe("SuggestionJobsPanel", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.getQualitySuggestionJob.mockReset();
    mocks.getQualitySuggestionJobTargetDebug.mockReset();
    mocks.cancelQualitySuggestionJob.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("stays a fixed strip: summary, the live job and a history entry", async () => {
    mockSnapshots({
      recent: [
        jobFixture(),
        jobFixture({
          name: "workspaces/ws/qualitySuggestionJobs/old",
          status: QualitySuggestionJobStatus.COMPLETED,
          intent: "Cover the older tables",
        }),
      ],
      // The RUNNING list also carries the job: it must not be listed twice.
      running: [jobFixture()],
    });
    const wrapper = await mountPanel();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.summary", {
        active: 1,
        finished: 1,
      })
    );
    // The live job is the one line of per-job detail the strip carries.
    expect(document.body.textContent).toContain(
      "Keep the orders table healthy"
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.progress", {
        completed: 0,
        total: 2,
        suggestions: 0,
        count: 0,
      })
    );
    // A finished job's intent, its targets and its diagnostics are not rendered
    // inline; the history drawer is where the list lives.
    expect(document.body.textContent).not.toContain("Cover the older tables");
    expect(document.body.textContent).not.toContain(
      "inst1 · db1 · public · orders"
    );
    expect(
      document.body.querySelector(
        `[aria-label="${label("dataQuality.suggestions.jobs.expand")}"]`
      )
    ).toBeNull();
    const history = buttonByText(label("dataQuality.suggestions.jobs.history"));
    expect(history).toBeTruthy();
    // The history entry is a dialog trigger, and the progress bar is named by
    // the text next to it instead of repeating that text.
    expect(history?.getAttribute("aria-haspopup")).toBe("dialog");
    expect(
      document.body
        .querySelector('[role="progressbar"]')
        ?.getAttribute("aria-label")
    ).toBeNull();
    wrapper.unmount();
  });

  it("reports the newest finished job, including a failure", async () => {
    mockSnapshots({
      recent: [
        jobFixture({
          status: QualitySuggestionJobStatus.FAILED,
          intent: "Draft rules for the customers table",
          completed: 1,
          suggestions: 2,
          error: "the provider rejected the prompt",
        }),
      ],
    });
    const wrapper = await mountPanel();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestionJobStatus.failed")
    );
    expect(document.body.textContent).toContain(
      "Draft rules for the customers table"
    );
    expect(document.body.textContent).toContain(
      "the provider rejected the prompt"
    );
    // A failure outside the one-job spotlight still shows up in the strip.
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.failedCount", { count: 1 })
    );
    wrapper.unmount();
  });

  it("opens the paginated history drawer on demand", async () => {
    mockSnapshots({ recent: [jobFixture()] });
    const wrapper = await mountPanel();
    const listCallsBefore = mocks.listQualitySuggestionJobs.mock.calls.length;

    buttonByText(label("dataQuality.suggestions.jobs.history"))?.click();
    await flushPromises();

    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull();
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.historyTitle")
    );
    // The drawer loads its own larger page instead of reusing the strip's snapshot.
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(
      listCallsBefore + 1
    );
    expect(mocks.listQualitySuggestionJobs).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageSize: 20, pageToken: "" })
    );
    wrapper.unmount();
  });

  it("reloads the strip when the history drawer changes a job", async () => {
    mockSnapshots({ recent: [jobFixture()] });
    const wrapper = await mountPanel();
    buttonByText(label("dataQuality.suggestions.jobs.history"))?.click();
    await flushPromises();
    const callsBeforeChange = mocks.listQualitySuggestionJobs.mock.calls.length;

    wrapper.findComponent(SuggestionJobHistoryDrawer).vm.$emit("changed");
    await flushPromises();

    // The strip's summary must not keep showing the pre-cancel state.
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(
      callsBeforeChange + 2
    );
    wrapper.unmount();
  });

  it("cancels the live job after a two-step confirm and refreshes", async () => {
    const success = vi.spyOn(notify, "success");
    mockSnapshots(
      { recent: [jobFixture()] },
      {
        recent: [
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
        ],
      }
    );
    mocks.cancelQualitySuggestionJob.mockResolvedValue(
      jobFixture({ status: QualitySuggestionJobStatus.CANCELED })
    );
    const wrapper = await mountPanel();

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
    // The cancel answer is followed by a list refresh (two calls).
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestionJobStatus.canceled")
    );
    // A canceled job refreshes the queue but selects and toasts nothing.
    expect(wrapper.emitted("settled")).toContainEqual([[]]);
    success.mockRestore();
    wrapper.unmount();
  });

  it("keeps the job running when the cancel confirmation is dismissed", async () => {
    mockSnapshots({ recent: [jobFixture()] });
    const wrapper = await mountPanel();

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

  it("polls an active job and settles it exactly once when it completes", async () => {
    vi.useFakeTimers();
    mockSnapshots(
      { recent: [jobFixture()] },
      {
        recent: [
          jobFixture({
            status: QualitySuggestionJobStatus.COMPLETED,
            completed: 2,
            suggestions: NEW_SUGGESTIONS.length,
            targets: [
              targetFixture({
                state: QualitySuggestionJobTargetState.DONE,
                suggestions: [NEW_SUGGESTIONS[0] as string],
              }),
              targetFixture({
                guid: OTHER_TARGET,
                object: "customers",
                state: QualitySuggestionJobTargetState.DONE,
                suggestions: [NEW_SUGGESTIONS[1] as string],
              }),
            ],
          }),
        ],
      }
    );
    const wrapper = await mountPanel();
    await vi.advanceTimersByTimeAsync(0);
    // Two list calls per load: the recent page and the RUNNING list.
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(2500);
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);
    expect(wrapper.emitted("settled")).toContainEqual([NEW_SUGGESTIONS]);

    // A completed job stops the poll loop.
    await vi.advanceTimersByTimeAsync(10_000);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);

    // A later refresh of the same terminal job must not settle it again.
    await wrapper.setProps({ refreshKey: 1 });
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(6);
    expect(wrapper.emitted("settled")).toHaveLength(1);
    wrapper.unmount();
  });

  it("polls and settles an active job that is older than the recent page", async () => {
    vi.useFakeTimers();
    const olderActive = jobFixture({ name: OLD_JOB });
    const finished = jobFixture({
      name: OLD_JOB,
      status: QualitySuggestionJobStatus.COMPLETED,
      completed: 2,
      suggestions: NEW_SUGGESTIONS.length,
      targets: [
        targetFixture({
          state: QualitySuggestionJobTargetState.DONE,
          suggestions: [NEW_SUGGESTIONS[0] as string],
        }),
        targetFixture({
          guid: OTHER_TARGET,
          object: "customers",
          state: QualitySuggestionJobTargetState.DONE,
          suggestions: [NEW_SUGGESTIONS[1] as string],
        }),
      ],
    });
    mocks.getQualitySuggestionJob.mockResolvedValue(finished);
    mockSnapshots(
      // Ten newer completed jobs fill the recent page; the active job is only
      // visible through the independent RUNNING list.
      { recent: completedJobs(10), running: [olderActive] },
      { recent: completedJobs(10), running: [] }
    );
    const wrapper = await mountPanel();
    await vi.advanceTimersByTimeAsync(0);
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestionJobStatus.running")
    );

    await vi.advanceTimersByTimeAsync(2500);
    await vi.advanceTimersByTimeAsync(0);

    expect(mocks.getQualitySuggestionJob).toHaveBeenCalledWith(OLD_JOB);
    expect(wrapper.emitted("settled")).toContainEqual([NEW_SUGGESTIONS]);
    // The terminal job is now in the panel and polling stops.
    await vi.advanceTimersByTimeAsync(10_000);
    expect(mocks.getQualitySuggestionJob).toHaveBeenCalledTimes(1);
    expect(wrapper.emitted("settled")).toHaveLength(1);
    wrapper.unmount();
  });

  it("retries a failed re-read instead of stalling the poll", async () => {
    vi.useFakeTimers();
    const olderActive = jobFixture({ name: OLD_JOB });
    mocks.getQualitySuggestionJob
      .mockRejectedValueOnce(new Error("blip"))
      .mockResolvedValue(
        jobFixture({
          name: OLD_JOB,
          status: QualitySuggestionJobStatus.COMPLETED,
          completed: 2,
          suggestions: NEW_SUGGESTIONS.length,
          targets: [
            targetFixture({
              state: QualitySuggestionJobTargetState.DONE,
              suggestions: [NEW_SUGGESTIONS[0] as string],
            }),
            targetFixture({
              guid: OTHER_TARGET,
              object: "customers",
              state: QualitySuggestionJobTargetState.DONE,
              suggestions: [NEW_SUGGESTIONS[1] as string],
            }),
          ],
        })
      );
    mockSnapshots(
      { recent: completedJobs(10), running: [olderActive] },
      { recent: completedJobs(10), running: [] },
      { recent: completedJobs(10), running: [] }
    );
    const wrapper = await mountPanel();
    await vi.advanceTimersByTimeAsync(0);

    // First transition poll: the re-read fails, but polling must continue.
    await vi.advanceTimersByTimeAsync(2500);
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.getQualitySuggestionJob).toHaveBeenCalledTimes(1);
    expect(wrapper.emitted("settled")).toBeUndefined();

    // Next poll retries the same name and settles it once.
    await vi.advanceTimersByTimeAsync(2500);
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.getQualitySuggestionJob).toHaveBeenCalledTimes(2);
    expect(wrapper.emitted("settled")).toContainEqual([NEW_SUGGESTIONS]);
    wrapper.unmount();
  });

  it("runs a reload requested while a load is in flight instead of dropping it", async () => {
    vi.useFakeTimers();
    let releaseRecent: (value: ReturnType<typeof page>) => void = () => {};
    let releaseRunning: (value: ReturnType<typeof page>) => void = () => {};
    mocks.listQualitySuggestionJobs
      .mockResolvedValueOnce(page([jobFixture()]))
      .mockResolvedValueOnce(page([]))
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            releaseRecent = resolve;
          })
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            releaseRunning = resolve;
          })
      )
      // Later loads see an active job again, so polling re-arms.
      .mockResolvedValue(page([jobFixture()]));
    const wrapper = await mountPanel();
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(2500);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);
    // A submission arrives while that poll is unresolved.
    await wrapper.setProps({ refreshKey: 1 });
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);

    releaseRecent(page([]));
    releaseRunning(page([]));
    await vi.advanceTimersByTimeAsync(0);

    // The deferred reload ran immediately: a fresh pair of list calls.
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(6);
    // It saw an active job again, so the next poll interval is armed.
    await vi.advanceTimersByTimeAsync(2500);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(8);
    wrapper.unmount();
  });

  it("does not start a second request while a poll is in flight", async () => {
    vi.useFakeTimers();
    let releaseRecent: (value: ReturnType<typeof page>) => void = () => {};
    let releaseRunning: (value: ReturnType<typeof page>) => void = () => {};
    mocks.listQualitySuggestionJobs
      .mockResolvedValueOnce(page([jobFixture()]))
      .mockResolvedValueOnce(page([]))
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            releaseRecent = resolve;
          })
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            releaseRunning = resolve;
          })
      );
    const wrapper = await mountPanel();
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(2500);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);
    // The next tick fires while the previous poll is still unresolved.
    await vi.advanceTimersByTimeAsync(2500);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);

    releaseRecent(page([jobFixture()]));
    releaseRunning(page([]));
    await vi.advanceTimersByTimeAsync(0);
    wrapper.unmount();
  });

  it("clears the poll timer on unmount", async () => {
    vi.useFakeTimers();
    mockSnapshots({ recent: [jobFixture()] });
    const wrapper = await mountPanel();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(2500);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);

    wrapper.unmount();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(4);
  });

  it("shows a fetch failure and clears it on retry", async () => {
    mocks.listQualitySuggestionJobs
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce(page([]))
      .mockResolvedValue(page([]));
    const wrapper = await mountPanel();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.jobs.fetchFailed")
    );

    buttonByText(label("common.retry"))?.click();
    await flushPromises();

    expect(document.body.textContent).not.toContain(
      label("dataQuality.suggestions.jobs.fetchFailed")
    );
    wrapper.unmount();
  });
});
