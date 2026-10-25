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
  searchMetadata: vi.fn(),
}));

vi.mock("@/api/instance", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/instance")>()),
  listAllInstances: mocks.listAllInstances,
}));

vi.mock("@/api/database", () => ({
  listMetadata: mocks.listMetadata,
  searchMetadata: mocks.searchMetadata,
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

/** One meta-type group of a ListMetadata response, with a page token. */
function groupPage(
  metaType: MetaType,
  rows: Array<{ guid: string; name: string }>,
  nextPageToken = ""
) {
  return {
    typesStoredMetadata: [
      {
        metaType,
        list: rows.map((row) => ({
          guid: row.guid,
          type: {
            case:
              metaType === MetaType.DATABASE
                ? "databaseMetadata"
                : metaType === MetaType.SCHEMA
                  ? "schemaMetadata"
                  : "tableMetadata",
            value: { name: row.name },
          },
        })),
        nextPageToken,
      },
    ],
    nextPageToken: "",
  };
}

/** One page of a SearchMetadata response. */
function searchPage(
  rows: Array<{ guid: string; name: string }>,
  nextPageToken = ""
) {
  return {
    results: rows.map((row) => ({
      guid: row.guid,
      metaType: MetaType.TABLE,
      metadata: {
        guid: row.guid,
        type: { case: "tableMetadata", value: { name: row.name } },
      },
    })),
    nextPageToken,
  };
}

async function mountPicker(
  engine: Engine,
  mode: "target" | "scope" = "target",
  multiple = false,
  instances?: ReturnType<typeof instance>[]
) {
  mocks.listAllInstances.mockResolvedValue(instances ?? [instance(engine)]);
  mocks.listMetadata.mockImplementation(async ({ parentGuid }) =>
    metadataPage(parentGuid)
  );
  mocks.searchMetadata.mockResolvedValue({
    results: [],
    nextPageToken: "",
  });
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

/** The dialog's search box. */
function searchInput(): HTMLInputElement {
  const input = document.body.querySelector<HTMLInputElement>(
    '[role="dialog"] input'
  );
  if (!input) {
    throw new Error("search input not found");
  }
  return input;
}

/** Type a search term and let the debounce and the requests settle. */
async function typeSearch(term: string) {
  const input = searchInput();
  input.value = term;
  input.dispatchEvent(new Event("input"));
  await vi.advanceTimersByTimeAsync(300);
  await flushPromises();
}

/** A dialog button whose visible label is exactly `label`. */
function dialogButton(label: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((candidate) => candidate.textContent?.trim() === label);
}

function loadMoreButton(): HTMLElement | undefined {
  return dialogButton(i18n.global.t("dataQuality.target.loadMore"));
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

  it("retries a failed level load", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES);
    mocks.listMetadata.mockRejectedValueOnce(new Error("level failed"));
    clickRow("Orders");
    await flushPromises();

    expect(dialogButton(i18n.global.t("common.retry"))).toBeDefined();

    dialogButton(i18n.global.t("common.retry"))?.click();
    await flushPromises();

    expect(breadcrumbText()).toContain("shop");
    wrapper.unmount();
  });

  it("keeps the empty-schema table fallback when the schema page is retried", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES);
    mocks.listMetadata.mockImplementation(
      ({ parentGuid }: { parentGuid: string }) => {
        if (parentGuid === "inst1") {
          return Promise.resolve(metadataPage(parentGuid));
        }
        // The first schema page fails; the database really has no schema rows.
        if (parentGuid === "inst1;db1") {
          return Promise.reject(new Error("schema page failed"));
        }
        return Promise.resolve(metadataPage(parentGuid));
      }
    );
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();
    expect(dialogButton(i18n.global.t("common.retry"))).toBeDefined();

    mocks.listMetadata.mockImplementation(
      ({ parentGuid }: { parentGuid: string }) =>
        Promise.resolve(
          parentGuid === "inst1;db1"
            ? { typesStoredMetadata: [], nextPageToken: "" }
            : metadataPage(parentGuid)
        )
    );
    dialogButton(i18n.global.t("common.retry"))?.click();
    await flushPromises();

    expect(breadcrumbText()).toContain("Tables");
    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({ parentGuid: "inst1;db1;" })
    );
    wrapper.unmount();
  });

  it("stays on the schema level when a search supersedes its load", async () => {
    vi.useFakeTimers();
    try {
      const wrapper = await mountPicker(Engine.POSTGRES);
      let releaseSchema: (value: unknown) => void = () => {};
      mocks.listMetadata.mockImplementation(
        ({ parentGuid }: { parentGuid: string }) => {
          if (parentGuid === "inst1") {
            return Promise.resolve(metadataPage(parentGuid));
          }
          return new Promise((resolve) => {
            releaseSchema = resolve;
          });
        }
      );
      clickRow("Orders");
      await flushPromises();
      clickRow("shop");
      await flushPromises();

      // The schema page is still in flight; the search box is visible above the
      // spinner, and a debounced search supersedes that page.
      const input = searchInput();
      input.value = "pub";
      input.dispatchEvent(new Event("input"));
      await vi.advanceTimersByTimeAsync(300);
      await flushPromises();
      releaseSchema(metadataPage("inst1;db1"));
      await flushPromises();

      expect(mocks.searchMetadata).toHaveBeenCalledWith(
        expect.objectContaining({
          searchStr: "pub",
          parentGuidPrefix: "inst1;db1",
          metaType: MetaType.SCHEMA,
        })
      );
      // The superseded answer must not trigger the empty-schema table fallback.
      expect(breadcrumbText()).not.toContain("Tables");
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("clears the chosen instance when walking back to the instance list", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES, "scope");
    clickRow("Orders");
    await flushPromises();
    expect(
      (applyScopeButton() as HTMLButtonElement | undefined)?.disabled
    ).toBe(false);

    backButton()?.click();
    await flushPromises();

    expect(
      (applyScopeButton() as HTMLButtonElement | undefined)?.disabled
    ).toBe(true);
    wrapper.unmount();
  });

  it("renders the cached instance list in batches", async () => {
    const all = Array.from({ length: 60 }, (_, index) =>
      create(InstanceSchema, {
        name: `instances/inst${index}`,
        title: `Instance ${index}`,
        engine: Engine.POSTGRES,
        state: State.ACTIVE,
      })
    );
    const wrapper = await mountPicker(Engine.POSTGRES, "target", false, all);

    expect(breadcrumbText()).toContain("Instance 49");
    expect(breadcrumbText()).not.toContain("Instance 50");
    expect(loadMoreButton()).toBeDefined();

    loadMoreButton()?.click();
    await flushPromises();

    expect(breadcrumbText()).toContain("Instance 59");
    expect(loadMoreButton()).toBeUndefined();
    wrapper.unmount();
  });

  it("does not fall through to tables when the reader backs out mid-load", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES);
    let releaseSchema: (value: unknown) => void = () => {};
    mocks.listMetadata.mockImplementation(
      ({ parentGuid }: { parentGuid: string }) => {
        if (parentGuid === "inst1") {
          return Promise.resolve(metadataPage(parentGuid));
        }
        if (parentGuid === "inst1;db1") {
          // The schema page stays in flight while the reader walks back.
          return new Promise((resolve) => {
            releaseSchema = resolve;
          });
        }
        return Promise.resolve(metadataPage(parentGuid));
      }
    );
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();

    backButton()?.click();
    await flushPromises();
    releaseSchema(metadataPage("inst1;db1"));
    await flushPromises();

    // The empty schema answer belongs to the level the reader left; it must not
    // trigger the "no schemas, show tables" fallback.
    expect(breadcrumbText()).not.toContain("Tables");
    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({ parentGuid: "inst1" })
    );
    wrapper.unmount();
  });

  it("loads further table pages instead of stopping at the first", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES);
    mocks.listMetadata.mockImplementation(
      async ({
        parentGuid,
        pageToken,
      }: {
        parentGuid: string;
        pageToken?: string;
      }) => {
        if (parentGuid === "inst1") {
          return metadataPage(parentGuid);
        }
        if (parentGuid === "inst1;db1") {
          return metadataPage(parentGuid);
        }
        if (pageToken === "tables-page-2") {
          return groupPage(MetaType.TABLE, [
            { guid: "inst1;db1;public;invoices", name: "invoices" },
          ]);
        }
        return groupPage(
          MetaType.TABLE,
          [
            { guid: "inst1;db1;public;orders", name: "orders" },
            { guid: "inst1;db1;public;users", name: "users" },
          ],
          "tables-page-2"
        );
      }
    );
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();
    clickRow("public");
    await flushPromises();

    expect(breadcrumbText()).toContain("orders");
    expect(breadcrumbText()).not.toContain("invoices");
    expect(loadMoreButton()).toBeDefined();

    loadMoreButton()?.click();
    await flushPromises();

    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({
        parentGuid: "inst1;db1;public",
        pageToken: "tables-page-2",
        metaType: MetaType.TABLE,
      })
    );
    expect(breadcrumbText()).toContain("invoices");
    // The level is exhausted, so the footer goes away.
    expect(loadMoreButton()).toBeUndefined();
    wrapper.unmount();
  });

  it("searches a level on the server instead of filtering the loaded page", async () => {
    vi.useFakeTimers();
    try {
      const wrapper = await mountPicker(Engine.POSTGRES);
      clickRow("Orders");
      await flushPromises();
      clickRow("shop");
      await flushPromises();
      clickRow("public");
      await flushPromises();
      mocks.searchMetadata.mockImplementation(
        async ({ metaType }: { metaType?: MetaType }) =>
          metaType === MetaType.TABLE
            ? searchPage([
                { guid: "inst1;db1;public;invoices", name: "invoices" },
              ])
            : searchPage([])
      );

      await typeSearch("inv");

      // One request per target meta type, scoped to the schema being browsed.
      expect(mocks.searchMetadata).toHaveBeenCalledTimes(3);
      expect(mocks.searchMetadata).toHaveBeenCalledWith(
        expect.objectContaining({
          searchStr: "inv",
          parentGuidPrefix: "inst1;db1;public",
          metaType: MetaType.TABLE,
        })
      );
      expect(breadcrumbText()).toContain("invoices");
      // The previous page is replaced, not filtered in place.
      expect(breadcrumbText()).not.toContain("users");
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("restores the full page when the search is cleared", async () => {
    vi.useFakeTimers();
    try {
      const wrapper = await mountPicker(Engine.POSTGRES);
      clickRow("Orders");
      await flushPromises();
      clickRow("shop");
      await flushPromises();
      clickRow("public");
      await flushPromises();
      mocks.searchMetadata.mockResolvedValue(
        searchPage([{ guid: "inst1;db1;public;invoices", name: "invoices" }])
      );

      await typeSearch("inv");
      expect(breadcrumbText()).toContain("invoices");

      await typeSearch("");

      expect(breadcrumbText()).toContain("orders");
      expect(breadcrumbText()).not.toContain("invoices");
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("says when a server search matches nothing", async () => {
    vi.useFakeTimers();
    try {
      const wrapper = await mountPicker(Engine.POSTGRES);
      clickRow("Orders");
      await flushPromises();
      clickRow("shop");
      await flushPromises();
      clickRow("public");
      await flushPromises();

      await typeSearch("zzz");

      expect(breadcrumbText()).toContain(
        i18n.global.t("dataQuality.target.noMatches")
      );
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("filters the cached instance list without hitting the server", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES);
    expect(breadcrumbText()).toContain("Orders");

    // The instance list needs no debounce or request: it is filtered in place.
    const input = searchInput();
    input.value = "zzz";
    input.dispatchEvent(new Event("input"));
    await flushPromises();

    expect(breadcrumbText()).toContain(
      i18n.global.t("dataQuality.target.noMatches")
    );
    expect(mocks.searchMetadata).not.toHaveBeenCalled();
    expect(mocks.listMetadata).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("offers a retry when the next page fails and keeps the loaded rows", async () => {
    const wrapper = await mountPicker(Engine.POSTGRES);
    mocks.listMetadata.mockImplementation(
      async ({
        parentGuid,
        pageToken,
      }: {
        parentGuid: string;
        pageToken?: string;
      }) => {
        if (parentGuid === "inst1") {
          return metadataPage(parentGuid);
        }
        if (parentGuid === "inst1;db1") {
          return metadataPage(parentGuid);
        }
        if (pageToken === "tables-page-2") {
          throw new Error("page failed");
        }
        return groupPage(
          MetaType.TABLE,
          [{ guid: "inst1;db1;public;orders", name: "orders" }],
          "tables-page-2"
        );
      }
    );
    clickRow("Orders");
    await flushPromises();
    clickRow("shop");
    await flushPromises();
    clickRow("public");
    await flushPromises();
    expect(breadcrumbText()).toContain("orders");

    loadMoreButton()?.click();
    await flushPromises();

    expect(dialogButton(i18n.global.t("common.retry"))).toBeDefined();
    expect(breadcrumbText()).toContain("orders");

    // The retry re-runs the failed page instead of dropping the level.
    mocks.listMetadata.mockResolvedValue(
      groupPage(MetaType.TABLE, [
        { guid: "inst1;db1;public;invoices", name: "invoices" },
      ])
    );
    dialogButton(i18n.global.t("common.retry"))?.click();
    await flushPromises();

    expect(breadcrumbText()).toContain("invoices");
    expect(dialogButton(i18n.global.t("common.retry"))).toBeUndefined();
    wrapper.unmount();
  });
});
