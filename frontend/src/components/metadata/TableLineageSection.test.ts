import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  type LineageRelation,
  LineageRelationSchema,
  RelationType,
} from "@/types/proto-es/v1/lineage_service_pb";
import TableLineageSection from "./TableLineageSection.vue";

const mocks = vi.hoisted(() => ({
  getLineage: vi.fn(),
  listAllInstances: vi.fn(),
}));

vi.mock("@/api/lineage", () => ({ getLineage: mocks.getLineage }));

// The scope labels come from the shared instance cache; the section renders without it,
// so an empty list is enough and keeps the test off the network.
vi.mock("@/api/instance", () => ({ listAllInstances: mocks.listAllInstances }));

const STATEMENT_GUID =
  "test-pg-1;metaxisdata;public;__application_sql__/mWjmPAyBvd7O1wSy";
const TABLE_GUID = "test-pg-1;metaxisdata;public;explain_sql_cache";

// The edge a constant INSERT produces: written by the reported statement, reaching the
// table it writes.
function statementWritesTable(): LineageRelation {
  return create(LineageRelationSchema, {
    id: 5134n,
    metaGuid: STATEMENT_GUID,
    metaType: MetaType.APPLICATION_SQL,
    sourceGuid: STATEMENT_GUID,
    sourceType: MetaType.APPLICATION_SQL,
    targetGuid: TABLE_GUID,
    targetType: MetaType.TABLE,
    relationType: RelationType.DIRECT,
  });
}

async function mountSection(guid: string, relations: LineageRelation[]) {
  mocks.getLineage.mockResolvedValue({
    relationsSource: [],
    relationsTarget: relations,
    externalDatasets: [],
  });

  const router = createRouter({
    history: createMemoryHistory(),
    // The same two named routes the section links to, with the repeated `:guid` param
    // the app uses.
    routes: [
      { path: "/metadata/:guid+", name: "MetadataDetail", component: {} },
      { path: "/lineage/:guid+", name: "LineageGraph", component: {} },
      { path: "/:pathMatch(.*)*", component: {} },
    ],
  });
  await router.push("/");
  await router.isReady();

  const pinia = createPinia();
  setActivePinia(pinia);

  const wrapper = mount(TableLineageSection, {
    props: { guid, metaType: MetaType.APPLICATION_SQL, title: "Lineage" },
    global: { plugins: [router, i18n, pinia] },
  });
  // One tick for the watcher's request, one for the render it triggers.
  await vi.waitFor(() => expect(wrapper.findAll("tbody tr")).toHaveLength(1));
  return wrapper;
}

describe("TableLineageSection", () => {
  beforeEach(() => {
    mocks.getLineage.mockReset();
    mocks.listAllInstances.mockReset();
    mocks.listAllInstances.mockResolvedValue([]);
  });

  // A relation's meta GUID is the object whose definition the analyzer read, so on that
  // object's own page the writer is the page itself. It used to print the statement's
  // generated identifier there, which is not a name.
  it("says the writer is this object instead of printing its own identifier", async () => {
    const wrapper = await mountSection(STATEMENT_GUID, [
      statementWritesTable(),
    ]);
    const row = wrapper.find("tbody tr").text();

    expect(row).toContain("This object");
    expect(row).not.toContain("__application_sql__");
  });

  it("still names a writer that is a different object", async () => {
    const wrapper = await mountSection(TABLE_GUID, [statementWritesTable()]);
    const row = wrapper.find("tbody tr").text();

    expect(row).not.toContain("This object");
  });

  // The metadata browser routes by type: a link without `?metaType=` is read as a list
  // route, so the table's link carries the target's type.
  it("links the related object with the meta type it resolved", async () => {
    const wrapper = await mountSection(STATEMENT_GUID, [
      statementWritesTable(),
    ]);
    const link = wrapper.find("tbody tr a");

    expect(link.attributes("href")).toContain("explain_sql_cache");
    expect(link.attributes("href")).toContain(`metaType=${MetaType.TABLE}`);
  });
});
