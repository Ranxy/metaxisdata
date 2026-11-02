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
import QualityConsumptionCard from "./QualityConsumptionCard.vue";

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
    gapCount: 28,
    gaps: [
      gap("orders", 9),
      gap("customers", 3),
      gap("payments", 2),
      gap("shipments", 1),
      gap("refunds", 1),
      gap("discounts", 1),
      gap("coupons", 1),
    ],
    ...overrides,
  });
}

async function mountCard(
  options: { permissions?: string[]; refreshKey?: number } = {}
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
  const wrapper = mount(QualityConsumptionCard, {
    props: { refreshKey: options.refreshKey ?? 0 },
    global: { plugins: [router, i18n, pinia] },
    attachTo: document.body,
  });
  await flushPromises();
  return { wrapper, router };
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("QualityConsumptionCard", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.getQualityConsumptionCoverage.mockResolvedValue(coverageFixture());
  });

  it("shows the consumed/monitored ratio and its unmonitored hint", async () => {
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    expect(text).toContain(label("dataQuality.overview.consumptionTitle"));
    expect(text).toContain(
      label("dataQuality.overview.consumptionDescription")
    );
    expect(text).toContain(
      label("dataQuality.overview.consumptionValue", {
        monitored: 12,
        consumed: 40,
      })
    );
    expect(text).toContain("30.0%");
    expect(text).toContain(
      label("dataQuality.overview.consumptionGapHint", { count: 28 })
    );
    expect(mocks.getQualityConsumptionCoverage).toHaveBeenCalledWith({
      pageSize: 5,
      signal: expect.any(AbortSignal),
    });
  });

  it("previews the five most-read unmonitored assets with links", async () => {
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    expect(text).toContain("prod · shop · public.orders");
    expect(text).toContain(
      label("dataQuality.overview.consumptionDownstream", { count: 9 })
    );
    // The card previews five; the dialog is where the rest live.
    expect(wrapper.findAll("li")).toHaveLength(5);
    expect(text).not.toContain("prod · shop · public.coupons");

    const orderLink = wrapper
      .findAll('a[href^="/data-quality/targets/"]')
      .find((link) => link.text() === "prod · shop · public.orders");
    expect(orderLink?.attributes("href")).toBe(
      "/data-quality/targets/inst1/shop/public/orders?metaType=4"
    );
  });

  it("offers the full gap list through a dialog", async () => {
    const { wrapper } = await mountCard();
    const viewAll = wrapper
      .findAll("button")
      .find(
        (button) =>
          button.text() ===
          label("dataQuality.overview.consumptionViewAll", { count: 28 })
      );
    expect(viewAll).toBeTruthy();

    await viewAll?.trigger("click");
    await flushPromises();

    // The dialog asks for the server's 100-row cap, not the preview size.
    expect(mocks.getQualityConsumptionCoverage).toHaveBeenCalledWith({
      pageSize: 100,
      signal: expect.any(AbortSignal),
    });
    expect(document.body.textContent).toContain(
      label("dataQuality.consumptionGaps.title")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.consumptionGaps.limitHint", { shown: 7, total: 28 })
    );
    wrapper.unmount();
  });

  it("shows '-' and explains an empty lineage instead of 0%", async () => {
    mocks.getQualityConsumptionCoverage.mockResolvedValue(
      coverageFixture({
        consumedAssetCount: 0,
        monitoredAssetCount: 0,
        gapCount: 0,
        gaps: [],
      })
    );
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    expect(text).toContain(label("dataQuality.overview.consumptionEmptyTitle"));
    expect(text).toContain(
      label("dataQuality.overview.consumptionEmptyDescription")
    );
    expect(wrapper.findAll("li")).toHaveLength(0);
    // No denominator means no percentage and no "View all".
    expect(text).not.toContain("%");
    expect(
      wrapper
        .findAll("button")
        .some((button) =>
          button
            .text()
            .startsWith(
              label("dataQuality.overview.consumptionViewAll", { count: 0 })
            )
        )
    ).toBe(false);
  });

  it("renders nothing and reads nothing without the rule permission", async () => {
    const { wrapper } = await mountCard({ permissions: [] });

    expect(wrapper.text()).toBe("");
    expect(mocks.getQualityConsumptionCoverage).not.toHaveBeenCalled();
  });

  it("keeps the card-level failure inside the card and retries", async () => {
    mocks.getQualityConsumptionCoverage.mockRejectedValueOnce(
      new Error("lineage down")
    );
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    expect(text).toContain(label("dataQuality.overview.consumptionFetchError"));
    // The card keeps its own frame while failing.
    expect(text).toContain(label("dataQuality.overview.consumptionTitle"));

    const retry = wrapper
      .findAll("button")
      .find((button) => button.text() === label("common.retry"));
    expect(retry).toBeTruthy();
    await retry?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).not.toContain(
      label("dataQuality.overview.consumptionFetchError")
    );
    expect(wrapper.text()).toContain("30.0%");
  });

  it("shows a loading state before the numbers land", async () => {
    let resolveLoad: ((value: unknown) => void) | undefined;
    mocks.getQualityConsumptionCoverage.mockReturnValue(
      new Promise((resolve) => {
        resolveLoad = resolve;
      })
    );
    const { wrapper } = await mountCard();

    expect(wrapper.text()).toContain(
      label("dataQuality.overview.consumptionTitle")
    );
    expect(wrapper.find('[role="status"]').exists()).toBe(true);

    resolveLoad?.(coverageFixture());
    await flushPromises();

    expect(wrapper.find('[role="status"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("30.0%");
  });

  it("re-reads when the page's refresh counter changes", async () => {
    const { wrapper } = await mountCard();
    expect(mocks.getQualityConsumptionCoverage).toHaveBeenCalledTimes(1);

    await wrapper.setProps({ refreshKey: 1 });
    await flushPromises();

    expect(mocks.getQualityConsumptionCoverage).toHaveBeenCalledTimes(2);
  });
});
