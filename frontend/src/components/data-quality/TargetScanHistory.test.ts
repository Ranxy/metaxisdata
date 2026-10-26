import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent } from "vue";
import { i18n } from "@/locales";
import {
  QualityExecutionState,
  QualityExecutionTrigger,
  type QualityRun,
  QualityRunSchema,
  QualityStatus,
  QualityStatusCountsSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetScanHistory from "./TargetScanHistory.vue";

const mocks = vi.hoisted(() => ({
  listQualityRuns: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityRuns: mocks.listQualityRuns,
}));

const TARGET = "inst1;shop;public;orders";
const RUN_ONE = "workspaces/ws/qualityRuns/run-1";
const RUN_TWO = "workspaces/ws/qualityRuns/run-2";

const labels = () => i18n.global.t;

const RunCardStub = defineComponent({
  name: "QualityRunCard",
  props: ["run", "targetGuid", "ruleTitles", "modelValue"],
  emits: ["update:modelValue"],
  template: '<div data-test="run-card" />',
});

function run(name: string): QualityRun {
  const start = new Date("2026-10-07T09:30:00Z");
  return create(QualityRunSchema, {
    name,
    trigger: QualityExecutionTrigger.MANUAL,
    state: QualityExecutionState.COMPLETED,
    outcome: QualityStatus.PASS,
    counts: create(QualityStatusCountsSchema, { total: 1, pass: 1 }),
    startTime: timestampFromDate(start),
    finishTime: timestampFromDate(new Date(start.getTime() + 1_000)),
    target: create(QualityTargetSchema, {
      metaGuid: TARGET,
      metaType: MetaType.TABLE,
    }),
    ruleRevisionMin: 1,
    ruleRevisionMax: 1,
  });
}

function page(items: QualityRun[]) {
  return { items, nextPageToken: "" };
}

async function mountHistory(options: { expandRun?: string } = {}) {
  const wrapper = mount(TargetScanHistory, {
    props: {
      targetGuid: TARGET,
      metaType: MetaType.TABLE,
      ruleTitles: {},
      expandRun: options.expandRun ?? "",
    },
    global: {
      plugins: [i18n],
      stubs: { QualityRunCard: RunCardStub },
    },
  });
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  mocks.listQualityRuns.mockResolvedValue(page([run(RUN_ONE), run(RUN_TWO)]));
});

describe("TargetScanHistory", () => {
  it("loads the target's runs and renders one card per batch", async () => {
    const wrapper = await mountHistory();

    expect(mocks.listQualityRuns).toHaveBeenCalledWith({
      targetGuid: TARGET,
      metaType: MetaType.TABLE,
      pageSize: 20,
    });
    expect(wrapper.find("h3").text()).toBe(
      labels()("dataQuality.executions.historyTitle")
    );
    const cards = wrapper.findAllComponents(RunCardStub);
    expect(cards).toHaveLength(2);
    expect(cards[0]?.props("targetGuid")).toBe(TARGET);
    expect(cards[0]?.props("modelValue")).toBe(false);
  });

  it("opens the run named by the URL and reports a toggle back", async () => {
    const wrapper = await mountHistory({ expandRun: RUN_TWO });

    const second = wrapper.findAllComponents(RunCardStub)[1];
    expect(second?.props("modelValue")).toBe(true);

    second?.vm.$emit("update:modelValue", false);
    await flushPromises();
    expect(wrapper.emitted("update:expandRun")).toContainEqual([""]);

    second?.vm.$emit("update:modelValue", true);
    await flushPromises();
    expect(wrapper.emitted("update:expandRun")).toContainEqual([RUN_TWO]);
  });

  it("explains a target that has never been scanned", async () => {
    mocks.listQualityRuns.mockResolvedValue(page([]));
    const wrapper = await mountHistory();

    expect(wrapper.text()).toContain(
      labels()("dataQuality.executions.historyEmpty")
    );
    expect(wrapper.text()).toContain(
      labels()("dataQuality.executions.historyEmptyHint")
    );
  });

  it("shows the failure with a retry instead of an empty history", async () => {
    mocks.listQualityRuns.mockRejectedValueOnce(new Error("boom"));
    const wrapper = await mountHistory();

    expect(wrapper.text()).toContain(
      labels()("dataQuality.executions.historyLoadFailed")
    );

    const retry = wrapper
      .findAll("button")
      .find((button) => button.text().trim() === labels()("common.refresh"));
    await retry?.trigger("click");
    await flushPromises();

    expect(wrapper.findAllComponents(RunCardStub)).toHaveLength(2);
  });
});
