import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import { Engine, State } from "@/types/proto-es/v1/common_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { InstanceSchema } from "@/types/proto-es/v1/instance_service_pb";
import TargetPicker from "./TargetPicker.vue";

const mocks = vi.hoisted(() => ({
  listAllInstances: vi.fn(),
  listMetadata: vi.fn(),
}));

vi.mock("@/api/instance", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/instance")>()),
  listAllInstances: mocks.listAllInstances,
}));

vi.mock("@/api/database", () => ({
  listMetadata: mocks.listMetadata,
}));

function instance(engine: Engine) {
  return create(InstanceSchema, {
    name: "instances/inst1",
    title: "Orders",
    engine,
    state: State.ACTIVE,
  });
}

function groups(
  parent: string
): Array<{ metaType: MetaType; list: Array<{ guid: string; name: string }> }> {
  if (parent === "inst1") {
    return [
      {
        metaType: MetaType.DATABASE,
        list: [{ guid: "inst1;db1", name: "shop" }],
      },
    ];
  }
  if (parent === "inst1;db1") {
    return [
      {
        metaType: MetaType.SCHEMA,
        list: [{ guid: "inst1;db1;public", name: "public" }],
      },
    ];
  }
  return [
    {
      metaType: MetaType.TABLE,
      list: [
        { guid: "inst1;db1;public;orders", name: "orders" },
        { guid: "inst1;db1;public;users", name: "users" },
      ],
    },
  ];
}

function metadataPage(parent: string) {
  const page = groups(parent).map((group) => ({
    metaType: group.metaType,
    list: group.list.map((item) => ({
      guid: item.guid,
      type: {
        case:
          group.metaType === MetaType.DATABASE
            ? "databaseMetadata"
            : group.metaType === MetaType.SCHEMA
              ? "schemaMetadata"
              : "tableMetadata",
        value: { name: item.name },
      },
    })),
  }));
  return { typesStoredMetadata: page, nextPageToken: "" };
}

async function mountPicker(
  engine: Engine,
  mode: "target" | "scope" = "target",
  multiple = false
) {
  mocks.listAllInstances.mockResolvedValue([instance(engine)]);
  mocks.listMetadata.mockImplementation(async ({ parentGuid }) =>
    metadataPage(parentGuid)
  );
  const pinia = createPinia();
  setActivePinia(pinia);
  const wrapper = mount(TargetPicker, {
    attachTo: document.body,
    props: { open: true, mode, multiple },
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

/** The footer action that applies the scope the reader has reached. */
function applyScopeButton() {
  const label = i18n.global.t("dataQuality.targetScope.apply");
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((candidate) => candidate.textContent?.trim() === label);
}

function clickRow(name: string) {
  const button = [
    ...document.body.querySelectorAll<HTMLElement>(
      '[role="dialog"] button[type="button"]'
    ),
  ].find((candidate) => candidate.textContent?.trim().startsWith(name));
  button?.click();
}

/** The multi-select footer action; its label carries the marked count. */
function addSelectedButton(): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((candidate) => candidate.textContent?.trim().startsWith("Add "));
}

/** The breadcrumb back arrow that walks one level up. */
function backButton(): HTMLElement | undefined {
  const label = i18n.global.t("common.back");
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((candidate) => candidate.getAttribute("aria-label") === label);
}

function breadcrumbText(): string {
  return document.body.querySelector('[role="dialog"]')?.textContent ?? "";
}

describe("TargetPicker", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
  });

  it("skips the schema level for a MySQL-family engine", async () => {
    const wrapper = await mountPicker(Engine.MYSQL);
    clickRow("Orders");
    await flushPromises();

    expect(mocks.listMetadata).not.toHaveBeenCalledWith(
      expect.objectContaining({ parentGuid: "inst1;db1" }),
      expect.anything()
    );
    expect(breadcrumbText()).not.toContain("public");

    clickRow("shop");
    await flushPromises();
    // Tables are listed directly under the database, with the empty-schema
    // parent GUID (a trailing semicolon).
    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({ parentGuid: "inst1;db1;" })
    );
    expect(breadcrumbText()).not.toContain("public");

    clickRow("orders");
    await flushPromises();
    const selection = wrapper.emitted("select")?.[0]?.[0] as
      | { guid: string; path: { schema: string } }
      | undefined;
    expect(selection?.guid).toBe("inst1;db1;public;orders");
    expect(selection?.path.schema).toBe("");
    wrapper.unmount();
  });

  it("keeps the schema level for PostgreSQL and shows it in the breadcrumb", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES);
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();

    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({ parentGuid: "inst1;db1" })
    );
    clickRow("public");
    await flushPromises();
    expect(breadcrumbText()).toContain("public");

    clickRow("orders");
    await flushPromises();
    const selection = wrapper.emitted("select")?.[0]?.[0] as
      | { guid: string; path: { schema: string } }
      | undefined;
    expect(selection?.guid).toBe("inst1;db1;public;orders");
    expect(selection?.path.schema).toBe("public");
    wrapper.unmount();
  });

  it("applies the reached level as a target scope instead of a table", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES, "scope");
    clickRow("Orders");
    await flushPromises();

    applyScopeButton()?.click();
    await flushPromises();

    expect(wrapper.emitted("selectScope")?.[0]?.[0]).toEqual({
      instance: "inst1",
      database: "",
      schema: "",
      table: "",
      label: "Orders",
    });
    wrapper.unmount();
  });

  it("applies a schema-level scope and a table row as a table scope", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES, "scope");
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();
    clickRow("public");
    await flushPromises();

    applyScopeButton()?.click();
    await flushPromises();
    expect(wrapper.emitted("selectScope")?.[0]?.[0]).toEqual({
      instance: "inst1",
      database: "shop",
      schema: "public",
      table: "",
      label: "Orders > shop > public",
    });

    clickRow("orders");
    await flushPromises();
    expect(wrapper.emitted("selectScope")?.[1]?.[0]).toEqual({
      instance: "inst1",
      database: "shop",
      schema: "public",
      table: "orders",
      label: "Orders > shop > public > orders",
    });
    wrapper.unmount();
  });

  it("keeps the MySQL family's empty schema out of the applied scope", async () => {
    const wrapper = await mountPicker(Engine.MYSQL, "scope");
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();

    applyScopeButton()?.click();
    await flushPromises();
    expect(wrapper.emitted("selectScope")?.[0]?.[0]).toEqual({
      instance: "inst1",
      database: "shop",
      schema: "",
      table: "",
      label: "Orders > shop",
    });

    clickRow("orders");
    await flushPromises();
    expect(wrapper.emitted("selectScope")?.[1]?.[0]).toEqual({
      instance: "inst1",
      database: "shop",
      schema: "",
      table: "orders",
      label: "Orders > shop > orders",
    });
    wrapper.unmount();
  });

  it("falls back to the tables when a schema list is empty", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES);
    mocks.listMetadata.mockImplementation(async ({ parentGuid }) =>
      parentGuid === "inst1;db1"
        ? { typesStoredMetadata: [], nextPageToken: "" }
        : metadataPage(parentGuid)
    );
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();

    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({ parentGuid: "inst1;db1;" })
    );
    wrapper.unmount();
  });

  it("marks several tables and adds them in one go in multi-select mode", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES, "target", true);
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();
    clickRow("public");
    await flushPromises();

    clickRow("orders");
    clickRow("users");
    await flushPromises();

    // Table rows only mark; nothing is chosen until the footer applies them.
    expect(wrapper.emitted("select")).toBeUndefined();
    const add = addSelectedButton();
    expect(add?.textContent).toContain("2");
    add?.click();
    await flushPromises();

    const selections = wrapper.emitted("selectMany")?.[0]?.[0] as
      | Array<{ guid: string }>
      | undefined;
    expect(selections?.map((selection) => selection.guid)).toEqual([
      "inst1;db1;public;orders",
      "inst1;db1;public;users",
    ]);
    expect(wrapper.emitted("update:open")).toContainEqual([false]);
    wrapper.unmount();
  });

  it("keeps the multi-select marks while drilling back through a level", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES, "target", true);
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();
    clickRow("public");
    await flushPromises();
    clickRow("orders");
    await flushPromises();

    // Back out to the schema level and return to the tables: the mark remains.
    backButton()?.click();
    await flushPromises();
    clickRow("public");
    await flushPromises();
    clickRow("users");
    await flushPromises();

    expect(addSelectedButton()?.textContent).toContain("2");
    wrapper.unmount();
  });
});
