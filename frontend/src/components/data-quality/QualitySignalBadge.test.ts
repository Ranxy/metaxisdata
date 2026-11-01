import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import type { QualitySignalState } from "@/composables/useQualitySignals";
import { i18n } from "@/locales";
import {
  QualityStatus,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import QualitySignalBadge from "./QualitySignalBadge.vue";

const GUID = "inst1;shop;public;orders";

function summary(
  fields: { worstStatus?: QualityStatus; enabledRuleCount?: number } = {}
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: GUID,
      metaType: MetaType.TABLE,
    }),
    worstStatus: fields.worstStatus ?? QualityStatus.UNSPECIFIED,
    enabledRuleCount: fields.enabledRuleCount ?? 1,
  });
}

function t(key: string): string {
  return i18n.global.t(key);
}

async function mountBadge(
  signal?: QualityTargetSummary,
  state?: QualitySignalState
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div />" } },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/");
  await router.isReady();
  const wrapper = mount(QualitySignalBadge, {
    props: { signal, state, guid: GUID, metaType: MetaType.TABLE },
    global: { plugins: [i18n, router] },
  });
  return { wrapper, router };
}

describe("QualitySignalBadge", () => {
  it("draws nothing until the signal read answers", async () => {
    const idle = await mountBadge(undefined, "idle");
    expect(idle.wrapper.text()).toBe("");
    const failed = await mountBadge(undefined, "failed");
    expect(failed.wrapper.text()).toBe("");
  });

  it("opens the workspace with the composer for an unmonitored object", async () => {
    const { wrapper } = await mountBadge(undefined, "ready");

    const link = wrapper.find("a");
    expect(link.text()).toContain(t("dataQuality.signal.notMonitored"));
    const query = new URLSearchParams(
      (link.attributes("href") ?? "").split("?")[1] ?? ""
    );
    expect(query.get("metaType")).toBe(String(MetaType.TABLE));
    expect(query.get("add")).toBe("1");
    // The neutral badge, not a health colour.
    expect(wrapper.find(".bg-muted").exists()).toBe(true);
  });

  it("opens the workspace without the composer for a monitored object", async () => {
    const { wrapper } = await mountBadge(
      summary({ worstStatus: QualityStatus.FAIL }),
      "ready"
    );

    const link = wrapper.find("a");
    expect(link.text()).toContain(t("dataQuality.status.fail"));
    const query = new URLSearchParams(
      (link.attributes("href") ?? "").split("?")[1] ?? ""
    );
    expect(query.has("add")).toBe(false);
    expect(query.get("metaType")).toBe(String(MetaType.TABLE));
    expect(link.attributes("title")).toContain(t("dataQuality.status.fail"));
  });

  it("shows a fully paused target as a neutral state, not as a verdict", async () => {
    const { wrapper } = await mountBadge(
      summary({ enabledRuleCount: 0, worstStatus: QualityStatus.PASS }),
      "ready"
    );

    const link = wrapper.find("a");
    expect(link.text()).toContain(t("dataQuality.signal.allPaused"));
    expect(link.attributes("title")).toBe(
      t("dataQuality.signal.allPausedHint")
    );
    // Paused still has checks, so the workspace opens without the composer.
    expect(
      new URLSearchParams(
        (link.attributes("href") ?? "").split("?")[1] ?? ""
      ).has("add")
    ).toBe(false);
    expect(wrapper.find(".bg-success").exists()).toBe(false);
  });
});
