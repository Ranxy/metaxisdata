import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type GetQualityConsumptionCoverageResponse,
  GetQualityConsumptionCoverageResponseSchema,
  QualityConsumptionGapSchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import QualityConsumptionGapsDialog from "./QualityConsumptionGapsDialog.vue";

const mocks = vi.hoisted(() => ({
  getQualityConsumptionCoverage: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityConsumptionCoverage: mocks.getQualityConsumptionCoverage,
}));

const PERMISSIONS = ["metaxisdata.dataQuality.rules.list"];
const Dummy = { template: "<div />" };

function gap(object: string, downstreamAssetCount: number) {
  return create(QualityConsumptionGapSchema, {
    target: create(QualityTargetSchema, {
      metaGuid: `inst1;shop;public;${object}`,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object,
      }),
    }),
    downstreamAssetCount,
  });
}

function coverageFixture(
  overrides: Partial<GetQualityConsumptionCoverageResponse> = {}
) {
  return create(GetQualityConsumptionCoverageResponseSchema, {
    consumedAssetCount: 40,
    monitoredAssetCount: 12,
    gapCount: 120,
    gaps: [gap("orders", 9), gap("customers", 3)],
    ...overrides,
  });
}

async function mountDialog(
  options: { open?: boolean; permissions?: string[] } = {}
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
    ],
  });
  await router.push("/");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: options.permissions ?? PERMISSIONS,
  });
  const wrapper = mount(QualityConsumptionGapsDialog, {
    props: { open: options.open ?? true },
    global: { plugins: [router, i18n, pinia] },
    attachTo: document.body,
  });
  await flushPromises();
  return { wrapper, router };
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("QualityConsumptionGapsDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.getQualityConsumptionCoverage.mockResolvedValue(coverageFixture());
  });

  it("reads up to 100 gaps when it opens and lists them", async () => {
    await mountDialog();

    expect(mocks.getQualityConsumptionCoverage).toHaveBeenCalledWith({
      pageSize: 100,
      signal: expect.any(AbortSignal),
    });
    const text = document.body.textContent ?? "";
    expect(text).toContain(label("dataQuality.consumptionGaps.title"));
    expect(text).toContain(label("dataQuality.consumptionGaps.description"));
    expect(text).toContain("prod · shop · public.orders");
    expect(text).toContain(
      label("dataQuality.overview.consumptionDownstream", { count: 9 })
    );
    // Two rows are returned but the server reports 120 gaps, so the dialog
    // says the list is bounded instead of pretending it is complete.
    expect(text).toContain(
      label("dataQuality.consumptionGaps.limitHint", { shown: 2, total: 120 })
    );

    const orderLink = [...document.body.querySelectorAll("a")].find(
      (link) => link.textContent?.trim() === "prod · shop · public.orders"
    );
    expect(orderLink?.getAttribute("href")).toBe(
      "/data-quality/targets/inst1/shop/public/orders?metaType=4"
    );
  });

  it("does not read while closed and reads when opened", async () => {
    const { wrapper } = await mountDialog({ open: false });
    expect(mocks.getQualityConsumptionCoverage).not.toHaveBeenCalled();

    await wrapper.setProps({ open: true });
    await flushPromises();

    expect(mocks.getQualityConsumptionCoverage).toHaveBeenCalledTimes(1);
  });

  it("says every consumed asset is watched when there are no gaps", async () => {
    mocks.getQualityConsumptionCoverage.mockResolvedValue(
      coverageFixture({ gapCount: 0, gaps: [] })
    );
    await mountDialog();

    expect(document.body.textContent).toContain(
      label("dataQuality.consumptionGaps.empty")
    );
    expect(document.body.textContent).not.toContain(
      label("dataQuality.consumptionGaps.limitHint", { shown: 0, total: 0 })
    );
  });

  it("keeps a failure inside the dialog and retries it", async () => {
    mocks.getQualityConsumptionCoverage.mockRejectedValueOnce(
      new Error("lineage down")
    );
    const { wrapper } = await mountDialog();

    expect(document.body.textContent).toContain(
      label("dataQuality.consumptionGaps.fetchError")
    );
    const retry = [
      ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
    ].find((button) => button.textContent?.trim() === label("common.retry"));
    expect(retry).toBeTruthy();

    retry?.click();
    await flushPromises();

    expect(document.body.textContent).not.toContain(
      label("dataQuality.consumptionGaps.fetchError")
    );
    expect(document.body.textContent).toContain("prod · shop · public.orders");
    wrapper.unmount();
  });

  it("asks the parent to close", async () => {
    const { wrapper } = await mountDialog();

    const close = [
      ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
    ].find((button) => button.textContent?.trim() === label("common.close"));
    expect(close).toBeTruthy();
    close?.click();
    await flushPromises();

    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("renders nothing and reads nothing without the rule permission", async () => {
    await mountDialog({ permissions: [] });

    expect(mocks.getQualityConsumptionCoverage).not.toHaveBeenCalled();
  });
});
