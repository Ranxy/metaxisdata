import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityRunCountsSchema,
  QualityRunSchema,
  QualityRunStatus,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityRunsPage from "./DataQualityRunsPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityRuns: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityRuns: mocks.listQualityRuns,
}));

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.runs.list",
];

const RUN = "workspaces/ws/qualityRuns/run-1";
const Dummy = { template: "<div />" };
const START = new Date("2026-10-07T09:30:00Z");

function runFixture(overrides = {}) {
  return create(QualityRunSchema, {
    name: RUN,
    status: QualityRunStatus.COMPLETED,
    outcome: QualityStatus.PASS,
    instance: "instances/inst-1",
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(new Date(START.getTime() + 3000)),
    counts: create(QualityRunCountsSchema, { total: 2, pass: 2 }),
    ...overrides,
  });
}

function target(
  path: MessageInitShape<typeof QualityTargetPathSchema>,
  metaGuid: string
) {
  return create(QualityTargetSchema, {
    metaGuid,
    path: create(QualityTargetPathSchema, path),
  });
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
      { path: "/data-quality/runs", name: "DataQualityRuns", component: Dummy },
      {
        path: "/data-quality/runs/:runId+",
        name: "DataQualityRunDetail",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/runs");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const wrapper = mount(DataQualityRunsPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("DataQualityRunsPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityRuns.mockResolvedValue({
      items: [runFixture()],
      nextPageToken: "",
    });
  });

  it("shows the target of the rules a run executed", async () => {
    mocks.listQualityRuns.mockResolvedValue({
      items: [
        runFixture({
          targets: [
            target(
              {
                instance: "prod",
                database: "shop",
                schema: "public",
                object: "orders",
              },
              "instance_1;shop;public;orders"
            ),
          ],
        }),
      ],
      nextPageToken: "",
    });

    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("public.orders");
  });

  it("collapses several targets into a count badge and keeps them in the tooltip", async () => {
    mocks.listQualityRuns.mockResolvedValue({
      items: [
        runFixture({
          targets: [
            target(
              {
                instance: "prod",
                database: "shop",
                schema: "public",
                object: "orders",
              },
              "instance_1;shop;public;orders"
            ),
            target(
              {
                instance: "prod",
                database: "shop",
                schema: "public",
                object: "customers",
              },
              "instance_1;shop;public;customers"
            ),
          ],
        }),
      ],
      nextPageToken: "",
    });

    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("public.orders");
    expect(wrapper.text()).toContain("+1");

    const titled = wrapper
      .findAll("[title]")
      .map((node) => node.attributes("title"));
    expect(titled).toContain(
      "prod · shop · public.orders\nprod · shop · public.customers"
    );
  });

  it("renders a dash for a run without results", async () => {
    mocks.listQualityRuns.mockResolvedValue({
      items: [runFixture({ targets: [], counts: undefined })],
      nextPageToken: "",
    });

    const wrapper = await mountPage();

    const targetCell = wrapper.findAll("tbody tr")[0].findAll("td")[3];
    expect(targetCell.text()).toBe("-");
    expect(wrapper.text()).not.toContain("public.orders");
  });
});
