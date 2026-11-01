import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import QualitySignalBadge from "@/components/data-quality/QualitySignalBadge.vue";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { Engine } from "@/types/proto-es/v1/common_pb";
import {
  QualityStatus,
  QualityTargetSchema,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  MetaType,
  StoredMetadataSchema,
  TableMetadataSchema,
  ViewMetadataSchema,
} from "@/types/proto-es/v1/database_service_pb";
import { InstanceSchema } from "@/types/proto-es/v1/instance_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import MetadataBrowserPage from "./MetadataBrowserPage.vue";

const mocks = vi.hoisted(() => ({
  getMetadata: vi.fn(),
  listMetadata: vi.fn(),
  searchMetadata: vi.fn(),
  listInstances: vi.fn(),
  getInstance: vi.fn(),
  listQualityTargets: vi.fn(),
}));

vi.mock("@/api/database", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/database")>()),
  getMetadata: mocks.getMetadata,
  listMetadata: mocks.listMetadata,
  searchMetadata: mocks.searchMetadata,
}));

vi.mock("@/api/instance", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/instance")>()),
  listInstances: mocks.listInstances,
  getInstance: mocks.getInstance,
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityTargets: mocks.listQualityTargets,
}));

// The detail components print environment labels through the shared store, which
// fetches the catalog on first use; jsdom has no server to answer it.
vi.mock("@/api/environment", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/environment")>()),
  listAllEnvironments: vi.fn().mockResolvedValue([]),
}));

const TABLE_GUID = "instance-1;shop;public;orders";
const VIEW_GUID = "instance-1;shop;public;orders_view";

const Dummy = { template: "<div />" };

function tableMetadata(name: string) {
  return create(StoredMetadataSchema, {
    guid: `instance-1;shop;public;${name}`,
    type: {
      case: "tableMetadata",
      value: create(TableMetadataSchema, {
        name,
        columns: [{ name: "id", type: "int", nullable: false }],
      }),
    },
  });
}

function viewMetadata(name: string) {
  return create(StoredMetadataSchema, {
    guid: `instance-1;shop;public;${name}`,
    type: {
      case: "viewMetadata",
      value: create(ViewMetadataSchema, {
        name,
        columns: [{ name: "id", type: "int", nullable: false }],
      }),
    },
  });
}

/** One target's trust signal as the selection read returns it. */
function signal(options: {
  guid: string;
  metaType: MetaType;
  enabledRuleCount?: number;
  worstStatus?: QualityStatus;
  coveredColumnCount?: number;
  totalColumnCount?: number;
}) {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: options.guid,
      metaType: options.metaType,
    }),
    enabledRuleCount: options.enabledRuleCount ?? 1,
    worstStatus: options.worstStatus ?? QualityStatus.FAIL,
    coveredColumnCount: options.coveredColumnCount ?? 1,
    totalColumnCount: options.totalColumnCount ?? 3,
  });
}

async function mountPage(
  route: Parameters<ReturnType<typeof createRouter>["push"]>[0]
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/metadata", name: "MetadataBrowser", component: Dummy },
      { path: "/metadata/:guid+", name: "MetadataDetail", component: Dummy },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
    ],
  });
  await router.push(route);
  await router.isReady();
  const wrapper = mount(MetadataBrowserPage, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  await flushPromises();
  return { wrapper, router };
}

/** Every selection request the page issued, as `targetGuid#metaType` keys. */
function requestedKeys(): string[] {
  return mocks.listQualityTargets.mock.calls.flatMap(([options]) =>
    (options?.targets ?? []).map(
      (target: { targetGuid: string; metaType: MetaType }) =>
        `${target.targetGuid}#${target.metaType}`
    )
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  setActivePinia(createPinia());
  useAuthStore().user = create(UserSchema, {
    permissions: ["metaxisdata.dataQuality.rules.list"],
  });
  mocks.listInstances.mockResolvedValue({
    instances: [
      create(InstanceSchema, { title: "prod", engine: Engine.MYSQL }),
    ],
    nextPageToken: "",
  });
  mocks.getInstance.mockResolvedValue(
    create(InstanceSchema, { title: "prod", engine: Engine.MYSQL })
  );
  mocks.listQualityTargets.mockResolvedValue({ items: [], nextPageToken: "" });
});

describe("MetadataBrowserPage trust signals", () => {
  it("reads the leaf object's signal with the leaf's own meta type", async () => {
    mocks.getMetadata.mockResolvedValue({ metadata: tableMetadata("orders") });
    mocks.listQualityTargets.mockResolvedValue({
      items: [
        signal({
          guid: TABLE_GUID,
          metaType: MetaType.TABLE,
          worstStatus: QualityStatus.WARN,
        }),
      ],
      nextPageToken: "",
    });

    const { wrapper } = await mountPage({
      name: "MetadataDetail",
      params: { guid: ["instance-1", "shop", "public", "orders"] },
      query: { metaType: String(MetaType.TABLE) },
    });

    // The page's one read names the object and its type; a GUID alone does not
    // identify a registry row.
    expect(requestedKeys()).toEqual([`${TABLE_GUID}#${MetaType.TABLE}`]);

    // The detail view is an async component: the header badge carries the same
    // signal the request asked for once it has rendered.
    await vi.waitFor(() => {
      expect(wrapper.findComponent(QualitySignalBadge).exists()).toBe(true);
    });
    const badge = wrapper.findComponent(QualitySignalBadge);
    expect(badge.props("signal")?.target?.metaGuid).toBe(TABLE_GUID);
    expect(badge.text()).toBe(i18n.global.t("dataQuality.status.warn"));
  });

  it("uses the tab's meta type, not the table's, for a view leaf", async () => {
    mocks.getMetadata.mockResolvedValue({
      metadata: viewMetadata("orders_view"),
    });
    mocks.listQualityTargets.mockResolvedValue({
      items: [
        signal({
          guid: VIEW_GUID,
          metaType: MetaType.VIEW,
          worstStatus: QualityStatus.PASS,
        }),
      ],
      nextPageToken: "",
    });

    await mountPage({
      name: "MetadataDetail",
      params: { guid: ["instance-1", "shop", "public", "orders_view"] },
      query: { metaType: String(MetaType.VIEW) },
    });

    expect(requestedKeys()).toEqual([`${VIEW_GUID}#${MetaType.VIEW}`]);
  });

  it("reads one signal per listed row and renders an unmonitored row as such", async () => {
    mocks.listMetadata.mockResolvedValue({
      typesStoredMetadata: [
        {
          metaType: MetaType.TABLE,
          list: [tableMetadata("orders"), tableMetadata("customers")],
        },
      ],
    });
    // Only one of the two rows has a check.
    mocks.listQualityTargets.mockResolvedValue({
      items: [
        signal({
          guid: TABLE_GUID,
          metaType: MetaType.TABLE,
          worstStatus: QualityStatus.FAIL,
        }),
      ],
      nextPageToken: "",
    });

    const { wrapper } = await mountPage({
      name: "MetadataDetail",
      params: { guid: ["instance-1", "shop", "public"] },
    });

    const keys = requestedKeys();
    expect(keys).toHaveLength(2);
    expect(keys).toContain(`${TABLE_GUID}#${MetaType.TABLE}`);
    expect(keys).toContain(
      `instance-1;shop;public;customers#${MetaType.TABLE}`
    );

    // The monitored row shows its verdict; the row nothing watches says so instead
    // of reading as healthy.
    const rows = wrapper.findAll("tbody tr");
    expect(rows.length).toBe(2);
    expect(rows[0]?.text()).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(rows[1]?.text()).toContain(
      i18n.global.t("dataQuality.signal.notMonitored")
    );
  });

  it("asks for nothing without the rule-catalog permission", async () => {
    useAuthStore().user = create(UserSchema, { permissions: [] });
    mocks.getMetadata.mockResolvedValue({ metadata: tableMetadata("orders") });

    const { wrapper } = await mountPage({
      name: "MetadataDetail",
      params: { guid: ["instance-1", "shop", "public", "orders"] },
      query: { metaType: String(MetaType.TABLE) },
    });

    expect(mocks.listQualityTargets).not.toHaveBeenCalled();
    // The page still mounts the badge, but a denied read leaves it empty: it must
    // not claim "not monitored" for an object nobody asked the server about.
    const badge = wrapper.findComponent(QualitySignalBadge);
    expect(badge.exists() ? badge.text() : "").toBe("");
  });
});
