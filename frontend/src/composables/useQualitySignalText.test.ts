import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { beforeEach, describe, expect, it } from "vitest";
import { createApp } from "vue";
import { i18n } from "@/locales";
import {
  QualityStatus,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { useQualitySignalText } from "./useQualitySignalText";

const GUID = "inst1;shop;public;orders";
const RUN_TIME = new Date(Date.now() - 5 * 60_000);

function summary(
  fields: {
    worstStatus?: QualityStatus;
    enabledRuleCount?: number;
    qualityScore?: number;
    recentPassRate?: number;
    openIssueCount?: number;
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
    qualityScore: fields.qualityScore,
    recentPassRate: fields.recentPassRate,
    openIssueCount: fields.openIssueCount ?? 0,
    coveredColumnCount: fields.coveredColumnCount ?? 0,
    totalColumnCount: fields.totalColumnCount ?? 0,
    lastRunTime:
      fields.lastRunTime === undefined
        ? undefined
        : timestampFromDate(fields.lastRunTime),
  });
}

/** The composable calls `useI18n()`, so it has to run inside a setup. */
function inSetup<T>(composable: () => T): T {
  let result!: T;
  const app = createApp({
    setup() {
      result = composable();
      return () => null;
    },
  });
  app.use(i18n);
  app.mount(document.createElement("div"));
  return result;
}

function text() {
  return inSetup(useQualitySignalText);
}

function t(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("useQualitySignalText", () => {
  beforeEach(() => {
    i18n.global.locale.value = "en-US";
  });

  it("states absence as 'not monitored', never as a verdict", () => {
    const signal = text();

    expect(signal.statusText(undefined)).toBe(
      t("dataQuality.signal.notMonitored")
    );
    expect(signal.detailText(undefined)).toBe("");
    expect(signal.tooltip(undefined)).toBe(
      t("dataQuality.signal.notMonitoredHint")
    );
    expect(signal.tone(undefined)).toBe("neutral");
    expect(signal.facts(undefined)).toEqual([]);
  });

  it("states a fully paused target as paused, with no health numbers", () => {
    const paused = summary({
      enabledRuleCount: 0,
      worstStatus: QualityStatus.PASS,
      qualityScore: 99,
      recentPassRate: 1,
      coveredColumnCount: 9,
      totalColumnCount: 9,
      lastRunTime: RUN_TIME,
    });
    const signal = text();

    expect(signal.statusText(paused)).toBe(t("dataQuality.signal.allPaused"));
    expect(signal.tone(paused)).toBe("neutral");
    expect(signal.detailText(paused)).toBe("");
    expect(signal.tooltip(paused)).toBe(t("dataQuality.signal.allPausedHint"));
    expect(signal.facts(paused)).toEqual([]);
    expect(signal.allPaused(paused)).toBe(true);
  });

  it("states a never-run target as never run, not as pass", () => {
    const signal = text();
    const neverRun = summary({ enabledRuleCount: 1 });

    expect(signal.statusText(neverRun)).toBe(t("dataQuality.targets.neverRun"));
    expect(signal.tone(neverRun)).toBe("neutral");
  });

  it("renders the verdict, its coverage and its last run", () => {
    const signal = text();
    const pass = summary({
      worstStatus: QualityStatus.PASS,
      coveredColumnCount: 4,
      totalColumnCount: 9,
      lastRunTime: RUN_TIME,
    });

    expect(signal.statusText(pass)).toBe(t("dataQuality.status.pass"));
    expect(signal.tone(pass)).toBe("success");
    expect(signal.detailText(pass)).toBe(
      [
        t("dataQuality.signal.coverage", { covered: 4, total: 9 }),
        formatRelativeTime(pass.lastRunTime, "en-US"),
      ].join(" · ")
    );
  });

  it("puts the judged facts in the tooltip", () => {
    const signal = text();
    const failing = summary({
      worstStatus: QualityStatus.FAIL,
      qualityScore: 40,
      recentPassRate: 0.25,
      openIssueCount: 2,
      coveredColumnCount: 0,
      totalColumnCount: 3,
      lastRunTime: RUN_TIME,
    });

    const tooltip = signal.tooltip(failing);
    expect(tooltip).toContain(t("dataQuality.status.fail"));
    expect(tooltip).toContain(t("dataQuality.signal.score", { score: "40.0" }));
    expect(tooltip).toContain(
      t("dataQuality.signal.passRate", { rate: "25.0%" })
    );
    expect(tooltip).toContain(t("dataQuality.signal.issues", { count: 2 }));
    expect(tooltip).toContain(
      t("dataQuality.signal.coverage", { covered: 0, total: 3 })
    );
    expect(tooltip).toContain(
      t("dataQuality.signal.lastRun", {
        time: formatRelativeTime(failing.lastRunTime, "en-US"),
      })
    );
  });

  it("omits the score and pass rate while nothing has been judged", () => {
    const signal = text();
    const neverRun = summary({
      coveredColumnCount: 1,
      totalColumnCount: 2,
      lastRunTime: RUN_TIME,
    });

    const tooltip = signal.tooltip(neverRun);
    expect(tooltip).not.toContain(t("dataQuality.signal.scoreLabel"));
    expect(tooltip).not.toContain(t("dataQuality.signal.passRateLabel"));
    expect(tooltip).not.toContain(t("dataQuality.signal.issues", { count: 0 }));
  });

  it("lists the judged facts one by one for the summary strip", () => {
    const signal = text();
    const pass = summary({
      worstStatus: QualityStatus.PASS,
      qualityScore: 92.5,
      recentPassRate: 0.9,
      coveredColumnCount: 4,
      totalColumnCount: 9,
      lastRunTime: RUN_TIME,
    });

    expect(signal.facts(pass)).toEqual([
      {
        key: "score",
        label: t("dataQuality.signal.scoreLabel"),
        value: "92.5",
      },
      {
        key: "passRate",
        label: t("dataQuality.signal.passRateLabel"),
        value: "90.0%",
      },
      {
        key: "coverage",
        label: t("dataQuality.signal.coverageLabel"),
        value: t("dataQuality.signal.coverage", { covered: 4, total: 9 }),
      },
      {
        key: "lastRun",
        label: t("dataQuality.signal.lastRunLabel"),
        value: formatRelativeTime(pass.lastRunTime, "en-US"),
        title: formatDateTime(pass.lastRunTime, "en-US"),
      },
    ]);
  });

  it("leaves the coverage fact out when the target has no column list", () => {
    const signal = text();
    const pass = summary({ worstStatus: QualityStatus.PASS });

    expect(signal.facts(pass).map((fact) => fact.key)).toEqual(["lastRun"]);
    expect(signal.detailText(pass)).toBe(t("dataQuality.targets.neverRun"));
  });
});
