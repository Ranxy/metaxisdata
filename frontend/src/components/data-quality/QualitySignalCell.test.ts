import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import type { QualitySignalState } from "@/composables/useQualitySignals";
import { i18n } from "@/locales";
import {
  QualityStatus,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import QualitySignalCell from "./QualitySignalCell.vue";

const GUID = "inst1;shop;public;orders";

function summary(
  fields: {
    worstStatus?: QualityStatus;
    enabledRuleCount?: number;
    coveredColumnCount?: number;
    totalColumnCount?: number;
    lastRunTime?: Date;
  } = {}
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: GUID,
      metaType: MetaType.TABLE,
    }),
    worstStatus: fields.worstStatus ?? QualityStatus.UNSPECIFIED,
    enabledRuleCount: fields.enabledRuleCount ?? 1,
    coveredColumnCount: fields.coveredColumnCount ?? 0,
    totalColumnCount: fields.totalColumnCount ?? 0,
    lastRunTime:
      fields.lastRunTime === undefined
        ? undefined
        : timestampFromDate(fields.lastRunTime),
  });
}

function t(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function mountCell(signal?: QualityTargetSummary, state?: QualitySignalState) {
  return mount(QualitySignalCell, {
    props: { signal, state },
    global: { plugins: [i18n] },
  });
}

describe("QualitySignalCell", () => {
  it("draws nothing while the read that would answer the row is unfinished", () => {
    expect(mountCell(undefined, "idle").text()).toBe("");
    expect(mountCell(undefined, "loading").text()).toBe("");
    expect(mountCell(undefined, "failed").text()).toBe("");
    expect(mountCell(undefined, "denied").text()).toBe("");
  });

  it("states an unmonitored object explicitly, in a neutral tone", () => {
    const wrapper = mountCell(undefined, "ready");

    expect(wrapper.text()).toBe(t("dataQuality.signal.notMonitored"));
    expect(wrapper.find(".bg-muted-foreground\\/50").exists()).toBe(true);
    // There is no coverage or last run to claim, so there is no second line.
    expect(wrapper.findAll(".text-xs")).toHaveLength(0);
    expect(wrapper.attributes("title")).toBe(
      t("dataQuality.signal.notMonitoredHint")
    );
  });

  it("puts the verdict on line one and coverage plus last run on line two", () => {
    const pass = summary({
      worstStatus: QualityStatus.PASS,
      coveredColumnCount: 4,
      totalColumnCount: 9,
      lastRunTime: new Date(Date.now() - 5 * 60_000),
    });
    const wrapper = mountCell(pass, "ready");

    expect(wrapper.text()).toContain(t("dataQuality.status.pass"));
    expect(wrapper.text()).toContain(
      t("dataQuality.signal.coverage", { covered: 4, total: 9 })
    );
    expect(wrapper.find(".bg-success").exists()).toBe(true);
    expect(wrapper.find(".text-xs.text-muted-foreground").exists()).toBe(true);
  });

  it("keeps a known answer on screen while a later batch loads", () => {
    const failing = summary({ worstStatus: QualityStatus.FAIL });
    const wrapper = mountCell(failing, "loading");

    expect(wrapper.text()).toContain(t("dataQuality.status.fail"));
    expect(wrapper.find(".bg-destructive").exists()).toBe(true);
  });

  it("states a fully paused target neutrally, without health facts", () => {
    const paused = summary({
      enabledRuleCount: 0,
      worstStatus: QualityStatus.PASS,
      coveredColumnCount: 9,
      totalColumnCount: 9,
      lastRunTime: new Date(),
    });
    const wrapper = mountCell(paused, "ready");

    expect(wrapper.text()).toBe(t("dataQuality.signal.allPaused"));
    expect(wrapper.find(".bg-success").exists()).toBe(false);
    expect(wrapper.attributes("title")).toBe(
      t("dataQuality.signal.allPausedHint")
    );
  });

  it("marks a target that never ran as neutral, never green", () => {
    const wrapper = mountCell(summary(), "ready");

    expect(wrapper.text()).toContain(t("dataQuality.targets.neverRun"));
    expect(wrapper.find(".bg-success").exists()).toBe(false);
  });
});
