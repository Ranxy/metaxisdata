import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityStatus,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  ColumnMetadataSchema,
  MetaType,
  TableMetadataSchema,
} from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import TableMetadataDetail from "./TableMetadataDetail.vue";

const GUID = "inst1;shop;public;orders";
const PERMISSIONS = ["metaxisdata.dataQuality.rules.list"];

function signal(): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: GUID,
      metaType: MetaType.TABLE,
    }),
    worstStatus: QualityStatus.FAIL,
    enabledRuleCount: 1,
    coveredColumnCount: 1,
    totalColumnCount: 3,
  });
}

function table() {
  return create(TableMetadataSchema, {
    name: "orders",
    columns: [
      create(ColumnMetadataSchema, { name: "id", type: "bigint", position: 1 }),
    ],
  });
}

async function mountDetail(
  qualitySignal?: QualityTargetSummary,
  guid?: string
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
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  return mount(TableMetadataDetail, {
    props: {
      table: table(),
      guid,
      qualitySignal,
      qualityState: "ready",
    },
    global: { plugins: [i18n, pinia, router] },
  });
}

function t(key: string): string {
  return i18n.global.t(key);
}

describe("TableMetadataDetail quality signal", () => {
  it("shows the target's trust signal beside the type badge without opening the tab", async () => {
    const wrapper = await mountDetail(signal(), GUID);

    const badge = wrapper.find("a[href*='/data-quality/targets/']");
    expect(badge.exists()).toBe(true);
    expect(badge.text()).toContain(t("dataQuality.status.fail"));
    // The tab is still there and the signal is not buried inside it.
    expect(wrapper.text()).toContain(t("dataQuality.asset.tab"));
  });

  it("states an unmonitored table explicitly and opens the composer", async () => {
    const wrapper = await mountDetail(undefined, GUID);

    const badge = wrapper.find("a[href*='/data-quality/targets/']");
    expect(badge.text()).toContain(t("dataQuality.signal.notMonitored"));
    expect(badge.attributes("href")).toContain("add=1");
  });

  it("shows no signal for a table the page did not load one for", async () => {
    const wrapper = await mountDetail(undefined, undefined);

    expect(wrapper.find("a[href*='/data-quality/targets/']").exists()).toBe(
      false
    );
  });
});
