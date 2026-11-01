import { create } from "@bufbuild/protobuf";
import { flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope, nextTick, ref } from "vue";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import {
  QUALITY_SIGNAL_BATCH_SIZE,
  type QualitySignals,
  type QualitySignalTarget,
  useQualitySignals,
} from "./useQualitySignals";

const mocks = vi.hoisted(() => ({ listQualityTargets: vi.fn() }));

vi.mock("@/api/data-quality", () => ({
  listQualityTargets: mocks.listQualityTargets,
}));

const GUID = "inst1;shop;public;orders";
const OTHER = "inst1;shop;public;customers";
const PERMISSIONS = ["metaxisdata.dataQuality.rules.list"];

function summary(
  guid: string,
  metaType = MetaType.TABLE
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, { metaGuid: guid, metaType }),
    enabledRuleCount: 1,
  });
}

function page(items: QualityTargetSummary[]) {
  return { items, nextPageToken: "" };
}

/** A promise the test resolves by hand, for the supersede cases. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

interface Harness {
  targets: ReturnType<typeof ref<QualitySignalTarget[]>>;
  signals: QualitySignals;
  dispose: () => void;
}

function mountSignals(permissions = PERMISSIONS): Harness {
  setActivePinia(createPinia());
  useAuthStore().user = create(UserSchema, { permissions });
  const targets = ref<QualitySignalTarget[]>([]);
  const scope = effectScope();
  const signals = scope.run(() => useQualitySignals(() => targets.value));
  if (!signals) {
    throw new Error("the composable ran outside its scope");
  }
  return { targets, signals, dispose: () => scope.stop() };
}

describe("useQualitySignals", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityTargets.mockResolvedValue(page([]));
  });

  it("reads the shown objects as one exact selection", async () => {
    mocks.listQualityTargets.mockResolvedValue(page([summary(GUID)]));
    const harness = mountSignals();
    harness.targets.value = [
      { guid: GUID, metaType: MetaType.TABLE },
      { guid: OTHER, metaType: MetaType.VIEW },
    ];

    await flushPromises();

    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);
    expect(mocks.listQualityTargets).toHaveBeenCalledWith({
      targets: [
        { targetGuid: GUID, metaType: MetaType.TABLE },
        { targetGuid: OTHER, metaType: MetaType.VIEW },
      ],
      signal: expect.any(AbortSignal),
    });
    expect(harness.signals.state.value).toBe("ready");
    expect(
      harness.signals.signalFor(GUID, MetaType.TABLE)?.target?.metaGuid
    ).toBe(GUID);
    harness.dispose();
  });

  it("chunks a page larger than the server's cap", async () => {
    const many = Array.from(
      { length: QUALITY_SIGNAL_BATCH_SIZE + 1 },
      (_, index) => ({
        guid: `inst1;shop;public;table_${index}`,
        metaType: MetaType.TABLE,
      })
    );
    const harness = mountSignals();
    harness.targets.value = many;

    await flushPromises();

    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityTargets.mock.calls[0]?.[0].targets).toHaveLength(
      QUALITY_SIGNAL_BATCH_SIZE
    );
    expect(mocks.listQualityTargets.mock.calls[1]?.[0].targets).toHaveLength(1);
    harness.dispose();
  });

  it("treats an absent answer as not monitored, not as an error", async () => {
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];

    await flushPromises();

    expect(harness.signals.state.value).toBe("ready");
    expect(harness.signals.signalFor(GUID, MetaType.TABLE)).toBeUndefined();
    harness.dispose();
  });

  it("keeps the answers it already has while a later page loads", async () => {
    mocks.listQualityTargets
      .mockResolvedValueOnce(page([summary(GUID)]))
      .mockResolvedValueOnce(page([summary(OTHER)]));
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];
    await flushPromises();

    harness.targets.value = [
      { guid: GUID, metaType: MetaType.TABLE },
      { guid: OTHER, metaType: MetaType.TABLE },
    ];
    await nextTick();

    // Only the new row is read; the known one is not requested again.
    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityTargets.mock.calls[1]?.[0].targets).toEqual([
      { targetGuid: OTHER, metaType: MetaType.TABLE },
    ]);
    // While that read is in flight the old row keeps rendering instead of
    // flashing "not monitored".
    expect(harness.signals.signalFor(GUID, MetaType.TABLE)).toBeDefined();

    await flushPromises();
    expect(harness.signals.state.value).toBe("ready");
    expect(harness.signals.signalFor(OTHER, MetaType.TABLE)).toBeDefined();
    harness.dispose();
  });

  it("lets a newer read supersede one still on the wire", async () => {
    const first = deferred<{
      items: QualityTargetSummary[];
      nextPageToken: string;
    }>();
    mocks.listQualityTargets
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce(page([summary(OTHER)]));
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];
    await nextTick();

    harness.targets.value = [{ guid: OTHER, metaType: MetaType.TABLE }];
    await flushPromises();
    expect(harness.signals.state.value).toBe("ready");

    // The superseded answer arrives late; it must not overwrite the new screen.
    first.resolve(page([summary(GUID)]));
    await flushPromises();
    expect(harness.signals.signalFor(GUID, MetaType.TABLE)).toBeUndefined();
    expect(harness.signals.signalFor(OTHER, MetaType.TABLE)).toBeDefined();
    const firstSignal = mocks.listQualityTargets.mock.calls[0]?.[0].signal;
    expect(firstSignal.aborted).toBe(true);
    harness.dispose();
  });

  it("reports a failed read instead of claiming the objects are unmonitored", async () => {
    mocks.listQualityTargets.mockRejectedValue(new Error("offline"));
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];

    await flushPromises();

    expect(harness.signals.state.value).toBe("failed");
    expect(harness.signals.signalFor(GUID, MetaType.TABLE)).toBeUndefined();
    harness.dispose();
  });

  it("re-reads missing answers on reload", async () => {
    mocks.listQualityTargets.mockRejectedValueOnce(new Error("offline"));
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];
    await flushPromises();
    expect(harness.signals.state.value).toBe("failed");

    mocks.listQualityTargets.mockResolvedValue(page([summary(GUID)]));
    await harness.signals.reload();
    expect(harness.signals.state.value).toBe("ready");
    expect(harness.signals.signalFor(GUID, MetaType.TABLE)).toBeDefined();
    harness.dispose();
  });

  it("requests nothing without the rule catalog permission", async () => {
    const harness = mountSignals([]);
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];

    await flushPromises();

    expect(mocks.listQualityTargets).not.toHaveBeenCalled();
    expect(harness.signals.canView.value).toBe(false);
    expect(harness.signals.state.value).toBe("denied");
    expect(harness.signals.signals.value.size).toBe(0);
    harness.dispose();
  });

  it("drops object types a check cannot target", async () => {
    mocks.listQualityTargets.mockResolvedValue(page([summary(GUID)]));
    const harness = mountSignals();
    harness.targets.value = [
      { guid: "inst1;shop", metaType: MetaType.DATABASE },
      { guid: "inst1;shop;public;orders;id", metaType: MetaType.COLUMN },
      { guid: GUID, metaType: MetaType.TABLE },
    ];

    await flushPromises();

    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);
    expect(mocks.listQualityTargets.mock.calls[0]?.[0].targets).toEqual([
      { targetGuid: GUID, metaType: MetaType.TABLE },
    ]);
    harness.dispose();
  });

  it("prunes answers the screen no longer shows", async () => {
    mocks.listQualityTargets.mockResolvedValue(page([summary(GUID)]));
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];
    await flushPromises();
    expect(harness.signals.signals.value.size).toBe(1);

    harness.targets.value = [];
    await flushPromises();
    expect(harness.signals.state.value).toBe("ready");
    expect(harness.signals.signals.value.size).toBe(0);
    harness.dispose();
  });

  it("is ready and asks for nothing when the screen shows no target", async () => {
    const harness = mountSignals();

    await flushPromises();

    expect(mocks.listQualityTargets).not.toHaveBeenCalled();
    expect(harness.signals.state.value).toBe("ready");
    harness.dispose();
  });

  it("does not read the same object twice for two identical pairs", async () => {
    const harness = mountSignals();
    harness.targets.value = [
      { guid: GUID, metaType: MetaType.TABLE },
      { guid: GUID, metaType: MetaType.TABLE },
    ];

    await flushPromises();

    expect(mocks.listQualityTargets.mock.calls[0]?.[0].targets).toEqual([
      { targetGuid: GUID, metaType: MetaType.TABLE },
    ]);
    harness.dispose();
  });

  it("asks for nothing when every shown object is already answered", async () => {
    mocks.listQualityTargets.mockResolvedValue(page([summary(GUID)]));
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];
    await flushPromises();

    await harness.signals.reload();

    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);
    expect(harness.signals.state.value).toBe("ready");
    harness.dispose();
  });

  it("ignores a superseded read that fails", async () => {
    let rejectFirst!: (reason: unknown) => void;
    const first = new Promise((_, reject) => {
      rejectFirst = reject;
    });
    mocks.listQualityTargets
      .mockReturnValueOnce(first)
      .mockResolvedValueOnce(page([summary(OTHER)]));
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];
    await nextTick();

    harness.targets.value = [{ guid: OTHER, metaType: MetaType.TABLE }];
    await flushPromises();
    rejectFirst(new Error("late failure"));
    await flushPromises();

    // The failure belonged to a screen the reader already left.
    expect(harness.signals.state.value).toBe("ready");
    expect(harness.signals.signalFor(OTHER, MetaType.TABLE)).toBeDefined();
    harness.dispose();
  });

  it("skips an answer that names no target", async () => {
    mocks.listQualityTargets.mockResolvedValue(
      page([create(QualityTargetSummarySchema, { enabledRuleCount: 1 })])
    );
    const harness = mountSignals();
    harness.targets.value = [{ guid: GUID, metaType: MetaType.TABLE }];

    await flushPromises();

    expect(harness.signals.signals.value.size).toBe(0);
    expect(harness.signals.state.value).toBe("ready");
    harness.dispose();
  });
});
