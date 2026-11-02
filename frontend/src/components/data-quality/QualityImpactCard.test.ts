import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type GetQualityTargetImpactResponse,
  GetQualityTargetImpactResponseSchema,
  QualityImpactAssetSchema,
  QualityRootCauseCandidateSchema,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import QualityImpactCard from "./QualityImpactCard.vue";

const mocks = vi.hoisted(() => ({
  getQualityTargetImpact: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityTargetImpact: mocks.getQualityTargetImpact,
}));

const TARGET_GUID = "inst1;shop;public;orders";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const START = new Date("2026-10-07T09:30:00Z");
const PERMISSIONS = ["metaxisdata.dataQuality.rules.list"];
const Dummy = { template: "<div />" };

function target(guid: string, object: string) {
  return create(QualityTargetSchema, {
    metaGuid: guid,
    metaType: MetaType.TABLE,
    path: create(QualityTargetPathSchema, {
      instance: "prod",
      database: "shop",
      schema: "public",
      object,
    }),
  });
}

function summary(options: {
  object: string;
  enabledRuleCount?: number;
  worstStatus?: QualityStatus;
  openIssueCount?: number;
  lastRunTime?: Date;
}) {
  return create(QualityTargetSummarySchema, {
    target: target(`inst1;shop;public;${options.object}`, options.object),
    ruleCount: 2,
    enabledRuleCount: options.enabledRuleCount ?? 1,
    worstStatus: options.worstStatus ?? QualityStatus.PASS,
    openIssueCount: options.openIssueCount ?? 0,
    lastRunTime: options.lastRunTime
      ? timestampFromDate(options.lastRunTime)
      : undefined,
  });
}

/** The response fields a test may override; message internals stay fixed. */
type ImpactResponseOverrides = Partial<
  Omit<GetQualityTargetImpactResponse, "$typeName" | "$unknown">
>;

function impactResponse(overrides: ImpactResponseOverrides = {}) {
  return create(GetQualityTargetImpactResponseSchema, {
    rootCauses: [
      create(QualityRootCauseCandidateSchema, {
        target: target("inst1;raw;public;raw_orders", "raw_orders"),
        distance: -1,
        failingRuleCount: 2,
        failingExecutionCount: 5,
        recentFailingExecutions: [EXECUTION],
        latestFailureTime: timestampFromDate(
          new Date(START.getTime() - 3_600_000)
        ),
        structureChangedTime: timestampFromDate(
          new Date(START.getTime() - 7_200_000)
        ),
      }),
    ],
    downstreamAssets: [
      create(QualityImpactAssetSchema, {
        target: target("inst1;shop;public;orders_enriched", "orders_enriched"),
        distance: 1,
        summary: summary({
          object: "orders_enriched",
          worstStatus: QualityStatus.FAIL,
          openIssueCount: 2,
          lastRunTime: new Date(START.getTime() - 1_800_000),
        }),
      }),
      create(QualityImpactAssetSchema, {
        target: target("inst1;shop;public;revenue", "revenue"),
        distance: 2,
        summary: summary({ object: "revenue", enabledRuleCount: 0 }),
      }),
      create(QualityImpactAssetSchema, {
        target: target("inst1;shop;public;dashboard", "dashboard"),
        distance: 2,
      }),
    ],
    depthReached: 2,
    windowStart: timestampFromDate(new Date(START.getTime() - 86_400_000)),
    windowEnd: timestampFromDate(START),
    ...overrides,
  });
}

async function mountCard(
  options: {
    permissions?: string[];
    targetGuid?: string;
    metaType?: MetaType;
    asOf?: ReturnType<typeof timestampFromDate>;
    anchor?: "run" | "issue";
  } = {}
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/metadata/:guid+",
        name: "MetadataDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
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
  const wrapper = mount(QualityImpactCard, {
    props: {
      targetGuid: options.targetGuid ?? TARGET_GUID,
      metaType: options.metaType ?? MetaType.TABLE,
      asOf: options.asOf,
      anchor: options.anchor,
    },
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("QualityImpactCard", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.getQualityTargetImpact.mockResolvedValue(impactResponse());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("explains that the lists come from recorded lineage", async () => {
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    expect(text).toContain(label("dataQuality.impact.title"));
    expect(text).toContain(label("dataQuality.impact.description"));
    expect(text).toContain(label("dataQuality.impact.upstreamTitle"));
    expect(text).toContain(label("dataQuality.impact.downstreamTitle"));
    // The window is the response's own: 24 hours before the run.
    expect(text).toContain(label("dataQuality.impact.window", { hours: 24 }));
  });

  it("names the issue's first-seen instant when asked to anchor at an issue", async () => {
    const { wrapper } = await mountCard({ anchor: "issue" });
    const text = wrapper.text();
    expect(text).toContain(
      label("dataQuality.impact.windowIssue", { hours: 24 })
    );
    expect(text).not.toContain(
      label("dataQuality.impact.window", { hours: 24 })
    );
  });

  it("renders the upstream evidence and the newest failing run", async () => {
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    expect(text).toContain("prod · shop · public.raw_orders");
    expect(text).toContain(label("dataQuality.impact.distanceDirectSource"));
    expect(text).toContain(
      label("dataQuality.impact.failingChecks", { count: 2 })
    );
    expect(text).toContain(
      label("dataQuality.impact.failingRuns", { count: 5 })
    );
    expect(text).toContain(
      label("dataQuality.impact.structureChanged", {
        time: formatRelativeTime(
          timestampFromDate(new Date(START.getTime() - 7_200_000)),
          "en-US"
        ),
      })
    );
    expect(wrapper.html()).toContain(
      formatDateTime(
        timestampFromDate(new Date(START.getTime() - 7_200_000)),
        "en-US"
      )
    );

    const executionLink = wrapper
      .findAll("a")
      .find(
        (link) => link.text() === label("dataQuality.impact.viewFailingRun")
      );
    expect(executionLink?.attributes("href")).toBe(
      "/data-quality/executions/workspaces/ws/qualityExecutions/exec-1"
    );
  });

  it("renders each downstream monitoring state honestly", async () => {
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    // A monitored asset shows its verdict, open issues and last run.
    expect(text).toContain("prod · shop · public.orders_enriched");
    expect(text).toContain(label("dataQuality.impact.distanceDirectDependent"));
    expect(text).toContain(label("dataQuality.status.fail"));
    expect(text).toContain(label("dataQuality.signal.issues", { count: 2 }));
    expect(text).toContain(
      label("dataQuality.signal.lastRun", {
        time: formatRelativeTime(
          timestampFromDate(new Date(START.getTime() - 1_800_000)),
          "en-US"
        ),
      })
    );
    // A target with no enabled check is paused, not healthy.
    expect(text).toContain(label("dataQuality.signal.allPaused"));
    // A target with no check at all is unmonitored, never green.
    expect(text).toContain(label("dataQuality.signal.notMonitored"));
    expect(text).toContain(
      label("dataQuality.impact.distanceHops", { count: 2 })
    );

    const enrichedLink = wrapper
      .findAll('a[href^="/metadata/"]')
      .find((link) => link.text() === "prod · shop · public.orders_enriched");
    expect(enrichedLink?.attributes("href")).toBe(
      "/metadata/inst1/shop/public/orders_enriched?metaType=4"
    );
  });

  it("says nothing is recorded instead of implying health", async () => {
    mocks.getQualityTargetImpact.mockResolvedValue(
      impactResponse({ rootCauses: [], downstreamAssets: [] })
    );
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    expect(text).toContain(label("dataQuality.impact.upstreamEmpty"));
    expect(text).toContain(label("dataQuality.impact.downstreamEmpty"));
  });

  it("passes asOf only when it has one", async () => {
    const asOf = timestampFromDate(START);

    await mountCard({ asOf });
    expect(mocks.getQualityTargetImpact).toHaveBeenLastCalledWith(
      expect.objectContaining({
        targetGuid: TARGET_GUID,
        metaType: MetaType.TABLE,
        depth: 2,
        windowHours: 24,
        asOf,
      })
    );
    expect(
      mocks.getQualityTargetImpact.mock.calls[0]?.[0].signal
    ).toBeInstanceOf(AbortSignal);

    mocks.getQualityTargetImpact.mockClear();
    await mountCard();
    expect(mocks.getQualityTargetImpact).toHaveBeenCalledWith(
      expect.objectContaining({ asOf: undefined })
    );
  });

  it("renders nothing and sends no request without the rule permission", async () => {
    const { wrapper } = await mountCard({ permissions: [] });

    expect(wrapper.text()).toBe("");
    expect(mocks.getQualityTargetImpact).not.toHaveBeenCalled();
  });

  it("renders nothing when the target cannot be addressed", async () => {
    const missingGuid = await mountCard({ targetGuid: "" });
    expect(missingGuid.wrapper.text()).toBe("");
    expect(mocks.getQualityTargetImpact).not.toHaveBeenCalled();

    mocks.getQualityTargetImpact.mockClear();
    const missingType = await mountCard({ metaType: MetaType.UNSPECIFIED });
    expect(missingType.wrapper.text()).toBe("");
    expect(mocks.getQualityTargetImpact).not.toHaveBeenCalled();
  });

  it("shows a card-level loading state, then the content", async () => {
    let resolveLoad: ((value: unknown) => void) | undefined;
    mocks.getQualityTargetImpact.mockReturnValue(
      new Promise((resolve) => {
        resolveLoad = resolve;
      })
    );
    const { wrapper } = await mountCard({ permissions: PERMISSIONS });
    // The card keeps its title while loading, so the page does not reflow.
    expect(wrapper.text()).toContain(label("dataQuality.impact.title"));
    expect(wrapper.find('[role="status"]').exists()).toBe(true);

    resolveLoad?.(impactResponse());
    await flushPromises();

    expect(wrapper.find('[role="status"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("prod · shop · public.orders_enriched");
  });

  it("shows an inline error with a working retry", async () => {
    mocks.getQualityTargetImpact.mockRejectedValueOnce(
      new Error("lineage down")
    );
    const { wrapper } = await mountCard();
    expect(wrapper.text()).toContain(label("dataQuality.impact.fetchError"));

    const retry = wrapper
      .findAll("button")
      .find((button) => button.text() === label("common.retry"));
    expect(retry).toBeTruthy();
    await retry?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).not.toContain(
      label("dataQuality.impact.fetchError")
    );
    expect(wrapper.text()).toContain("prod · shop · public.orders_enriched");
  });

  it("footnotes a bounded walk instead of hiding it", async () => {
    mocks.getQualityTargetImpact.mockResolvedValue(
      impactResponse({
        downstreamTruncated: true,
        omittedDownstreamCount: 4,
        upstreamTruncated: true,
        omittedRootCauseCount: 2,
      })
    );
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    expect(text).toContain(
      label("dataQuality.impact.footnoteDownstreamTruncated")
    );
    expect(text).toContain(
      label("dataQuality.impact.footnoteOmittedDownstream", {
        count: 4,
      })
    );
    expect(text).toContain(
      label("dataQuality.impact.footnoteUpstreamTruncated")
    );
    expect(text).toContain(
      label("dataQuality.impact.footnoteOmittedRootCauses", {
        count: 2,
      })
    );
  });
});
