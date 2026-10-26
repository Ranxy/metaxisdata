import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityDimension,
  QualityExecutionSummarySchema,
  type QualityRule,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import type { QualityTargetScope } from "@/utils/dataQuality";
import DataQualityChecksPage from "./DataQualityChecksPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityRules: vi.fn(),
  listAllUsers: vi.fn(),
  runQualityRules: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityRules: mocks.listQualityRules,
  runQualityRules: mocks.runQualityRules,
}));

vi.mock("@/api/user", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/user")>()),
  listAllUsers: mocks.listAllUsers,
}));

const SCOPE: QualityTargetScope = {
  instance: "inst1",
  database: "shop",
  schema: "public",
  table: "orders",
  label: "Orders > shop > public > orders",
};

const USERS = [
  create(UserSchema, { name: "users/101", email: "alice@example.com" }),
];

/** The browser itself is tested separately; this stub reports a picked scope. */
const TargetScopeFilterStub = defineComponent({
  name: "TargetScopeFilter",
  props: { modelValue: Object, id: String },
  emits: ["update:modelValue"],
  setup(_props, { emit }) {
    return () =>
      h(
        "button",
        {
          class: "scope-stub",
          type: "button",
          onClick: () => emit("update:modelValue", SCOPE),
        },
        "scope"
      );
  },
});

/**
 * The picker is tested separately; this stub offers a member and the page's
 * unassigned sentinel, both reported through v-model.
 */
const UserSelectStub = defineComponent({
  name: "UserSelect",
  props: {
    modelValue: String,
    id: String,
    placeholder: String,
    noOwnerValue: String,
    noOwnerLabel: String,
    anyLabel: String,
    clearLabel: String,
    preloadedUsers: Array,
  },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    return () =>
      h("div", { class: "owner-stub" }, [
        h(
          "button",
          {
            class: "owner-user-stub",
            type: "button",
            onClick: () => emit("update:modelValue", "users/101"),
          },
          "member"
        ),
        h(
          "button",
          {
            class: "owner-unassigned-stub",
            type: "button",
            onClick: () => emit("update:modelValue", props.noOwnerValue),
          },
          "unassigned"
        ),
      ]);
  },
});

const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.run",
  "metaxisdata.users.list",
];

const RULE = "workspaces/default/qualityRules/rule-1";
const TARGET_GUID = "inst1;shop;public;orders";

let activeRouter: ReturnType<typeof createRouter> | undefined;

async function mountPage(
  permissions = PERMISSIONS,
  path = "/data-quality/checks"
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/checks",
        name: "DataQualityChecks",
        component: Dummy,
      },
      {
        path: "/data-quality/targets",
        name: "DataQualityTargets",
        component: Dummy,
      },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
    ],
  });
  await router.push(path);
  await router.isReady();
  activeRouter = router;
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualityChecksPage, {
    global: {
      plugins: [router, i18n, pinia],
      stubs: {
        TargetScopeFilter: TargetScopeFilterStub,
        UserSelect: UserSelectStub,
      },
    },
  });
  await flushPromises();
  return wrapper;
}

/** The query of the current URL, i.e. what a reload or a share would carry. */
function currentQuery(): Record<string, unknown> {
  return (activeRouter?.currentRoute.value.query ?? {}) as Record<
    string,
    unknown
  >;
}

function applyButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().trim() === i18n.global.t("common.apply"));
}

beforeEach(() => {
  mocks.listAllUsers.mockResolvedValue(USERS);
  mocks.runQualityRules.mockResolvedValue([]);
});

/** One check with a resolvable target, as the directory lists it. */
function ruleFixture(): QualityRule {
  return create(QualityRuleSchema, {
    name: RULE,
    title: "orders are never null",
    ruleType: QualityRuleType.NULL_PERCENT,
    severity: QualitySeverity.ERROR,
    state: QualityRuleState.ENABLED,
    owner: "users/101",
    lastResult: create(QualityExecutionSummarySchema, {
      status: QualityStatus.PASS,
    }),
    target: create(QualityTargetSchema, {
      metaGuid: TARGET_GUID,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: "orders",
      }),
    }),
  });
}

describe("DataQualityChecksPage target scope filter", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityRules.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
  });

  it("filters by the target scope only after Apply", async () => {
    const wrapper = await mountPage();
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);
    expect(mocks.listQualityRules.mock.calls[0]?.[0]?.instance).toBeUndefined();

    await wrapper.find(".scope-stub").trigger("click");
    // Picking is pending, like typing in the search box: no request yet.
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);

    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityRules.mock.calls[1]?.[0]).toMatchObject({
      instance: "inst1",
      database: "shop",
      schema: "public",
      table: "orders",
    });
  });
});

describe("DataQualityChecksPage owner filter", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityRules.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
  });

  it("sends the member, then unassigned, only on Apply", async () => {
    const wrapper = await mountPage();
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);
    expect(mocks.listQualityRules.mock.calls[0]?.[0]).toMatchObject({
      owner: "",
      ownerUnassigned: false,
      pageToken: "",
    });

    // Picking is pending, like typing in the search box: no request yet.
    await wrapper.find(".owner-user-stub").trigger("click");
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);

    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityRules.mock.calls[1]?.[0]).toMatchObject({
      owner: "users/101",
      ownerUnassigned: false,
    });

    await wrapper.find(".owner-unassigned-stub").trigger("click");
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);

    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityRules).toHaveBeenCalledTimes(3);
    // The sentinel never reaches the request: an empty owner plus the flag.
    expect(mocks.listQualityRules.mock.calls[2]?.[0]).toMatchObject({
      owner: "",
      ownerUnassigned: true,
    });
  });

  it("restarts the cursor when Apply runs", async () => {
    mocks.listQualityRules.mockResolvedValue({
      items: [
        create(QualityRuleSchema, {
          name: "workspaces/default/qualityRules/rule-1",
          title: "orders are never null",
        }),
      ],
      nextPageToken: "cursor-2",
    });
    const wrapper = await mountPage();

    const next = wrapper
      .findAll("button")
      .find((button) => button.text().trim() === i18n.global.t("common.next"));
    expect(next).toBeTruthy();
    await next?.trigger("click");
    await flushPromises();
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityRules.mock.calls[1]?.[0]?.pageToken).toBe(
      "cursor-2"
    );

    await wrapper.find(".owner-unassigned-stub").trigger("click");
    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    // Apply starts a new walk: the old cursor belongs to the old query.
    expect(
      mocks.listQualityRules.mock.calls[
        mocks.listQualityRules.mock.calls.length - 1
      ]?.[0]
    ).toMatchObject({
      owner: "",
      ownerUnassigned: true,
      pageToken: "",
    });
  });

  it("clears an applied owner filter from the empty state", async () => {
    const wrapper = await mountPage();

    await wrapper.find(".owner-unassigned-stub").trigger("click");
    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);
    const clear = wrapper
      .findAll("button")
      .find(
        (button) =>
          button.text().trim() === i18n.global.t("common.filter.clear")
      );
    expect(clear).toBeTruthy();
    await clear?.trigger("click");
    await flushPromises();

    expect(
      mocks.listQualityRules.mock.calls[
        mocks.listQualityRules.mock.calls.length - 1
      ]?.[0]
    ).toMatchObject({
      owner: "",
      ownerUnassigned: false,
    });
  });

  it("renders the filter but skips the directory without users.list", async () => {
    const wrapper = await mountPage(
      PERMISSIONS.filter((p) => p !== "metaxisdata.users.list")
    );

    expect(wrapper.find(".owner-stub").exists()).toBe(true);
    expect(mocks.listAllUsers).not.toHaveBeenCalled();
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);
  });
});

describe("DataQualityChecksPage owner column", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
  });

  it("shows the owner email, or the unassigned label when there is none", async () => {
    mocks.listQualityRules.mockResolvedValue({
      items: [
        create(QualityRuleSchema, {
          name: "workspaces/default/qualityRules/rule-1",
          title: "owned rule",
          owner: "users/101",
        }),
        create(QualityRuleSchema, {
          name: "workspaces/default/qualityRules/rule-2",
          title: "unowned rule",
        }),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("alice@example.com");
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.unassignedOwner")
    );
    // The cell keeps the resource name in its title, not the email.
    expect(wrapper.find('td[title="users/101"]').exists()).toBe(true);
  });

  it("falls back to the raw owner name when the directory fails", async () => {
    mocks.listAllUsers.mockRejectedValue(new Error("directory unavailable"));
    mocks.listQualityRules.mockResolvedValue({
      items: [
        create(QualityRuleSchema, {
          name: "workspaces/default/qualityRules/rule-1",
          title: "owned rule",
          owner: "users/101",
        }),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("users/101");
    expect(wrapper.text()).toContain("owned rule");
  });
});

describe("DataQualityChecksPage rule type filter", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityRules.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
  });

  it("defaults to every type and sends the picked one only after Apply", async () => {
    const wrapper = await mountPage();
    expect(wrapper.find("#data-quality-rule-type").exists()).toBe(true);
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);
    expect(mocks.listQualityRules.mock.calls[0]?.[0]?.ruleType).toBe(
      QualityRuleType.UNSPECIFIED
    );

    // The enum filters in template order: dimension, rule type, severity, state.
    const selects = wrapper.findAllComponents(Select);
    await selects[1]?.vm.$emit(
      "update:modelValue",
      String(QualityRuleType.NULL_PERCENT)
    );
    await nextTick();
    // Picking is pending, like the other filters: no request yet.
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);

    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityRules.mock.calls[1]?.[0]).toMatchObject({
      ruleType: QualityRuleType.NULL_PERCENT,
    });
  });

  it("clears an applied rule type filter from the empty state", async () => {
    const wrapper = await mountPage();
    const selects = wrapper.findAllComponents(Select);
    await selects[1]?.vm.$emit(
      "update:modelValue",
      String(QualityRuleType.NULL_PERCENT)
    );
    await nextTick();
    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    const clear = wrapper
      .findAll("button")
      .find(
        (button) =>
          button.text().trim() === i18n.global.t("common.filter.clear")
      );
    expect(clear).toBeTruthy();
    await clear?.trigger("click");
    await flushPromises();

    expect(
      mocks.listQualityRules.mock.calls[
        mocks.listQualityRules.mock.calls.length - 1
      ]?.[0]
    ).toMatchObject({ ruleType: QualityRuleType.UNSPECIFIED });
  });
});

describe("DataQualityChecksPage AI drafting shortcut", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityRules.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
  });

  it("offers the suggestion workbench only with the suggest permission", async () => {
    const withSuggest = await mountPage([
      ...PERMISSIONS,
      "metaxisdata.dataQuality.suggest",
    ]);
    const entry = withSuggest
      .findAll("button")
      .find((button) =>
        button
          .text()
          .includes(i18n.global.t("dataQuality.llm.draft.openButton"))
      );
    expect(entry).toBeTruthy();
    withSuggest.unmount();

    const withoutSuggest = await mountPage(PERMISSIONS);
    expect(
      withoutSuggest
        .findAll("button")
        .some((button) =>
          button
            .text()
            .includes(i18n.global.t("dataQuality.llm.draft.openButton"))
        )
    ).toBe(false);
    withoutSuggest.unmount();
  });
});

describe("DataQualityChecksPage URL filters", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityRules.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
  });

  it("writes the applied filters to the URL and reads them back", async () => {
    const wrapper = await mountPage();
    expect(currentQuery()).toEqual({});

    await wrapper.find("#data-quality-search").setValue("orders");
    await wrapper.find(".scope-stub").trigger("click");
    await wrapper.find(".owner-user-stub").trigger("click");
    const selects = wrapper.findAllComponents(Select);
    await selects[0]?.vm.$emit(
      "update:modelValue",
      String(QualityDimension.COMPLETENESS)
    );
    await selects[2]?.vm.$emit(
      "update:modelValue",
      String(QualitySeverity.ERROR)
    );
    await nextTick();

    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(currentQuery()).toMatchObject({
      q: "orders",
      instance: "inst1",
      database: "shop",
      schema: "public",
      table: "orders",
      owner: "users/101",
      dimension: String(QualityDimension.COMPLETENESS),
      severity: String(QualitySeverity.ERROR),
    });
    // Defaults stay out of the URL.
    expect(currentQuery().ruleType).toBeUndefined();
    expect(currentQuery().state).toBeUndefined();
    expect(mocks.listQualityRules).toHaveBeenLastCalledWith(
      expect.objectContaining({
        search: "orders",
        instance: "inst1",
        owner: "users/101",
        dimension: QualityDimension.COMPLETENESS,
        severity: QualitySeverity.ERROR,
      })
    );
    // Applying writes the URL without triggering a second request.
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);

    // A reload at that URL starts from the same applied filters.
    const fullPath = activeRouter?.currentRoute.value.fullPath ?? "";
    wrapper.unmount();
    const reloaded = await mountPage(PERMISSIONS, fullPath);

    expect(mocks.listQualityRules).toHaveBeenLastCalledWith(
      expect.objectContaining({
        search: "orders",
        instance: "inst1",
        database: "shop",
        schema: "public",
        table: "orders",
        owner: "users/101",
        dimension: QualityDimension.COMPLETENESS,
        severity: QualitySeverity.ERROR,
      })
    );
    expect(
      (reloaded.find("#data-quality-search").element as HTMLInputElement).value
    ).toBe("orders");
    expect(
      reloaded.findComponent(TargetScopeFilterStub).props("modelValue")
    ).toMatchObject({
      instance: "inst1",
      database: "shop",
      schema: "public",
      table: "orders",
    });
    expect(reloaded.findComponent(UserSelectStub).props("modelValue")).toBe(
      "users/101"
    );
    expect(reloaded.findAllComponents(Select)[0]?.props("modelValue")).toBe(
      String(QualityDimension.COMPLETENESS)
    );
    expect(reloaded.findAllComponents(Select)[2]?.props("modelValue")).toBe(
      String(QualitySeverity.ERROR)
    );
  });

  it("writes the unassigned owner as its own flag and reads it back", async () => {
    const wrapper = await mountPage();

    await wrapper.find(".owner-unassigned-stub").trigger("click");
    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(currentQuery()).toEqual({ ownerUnassigned: "1" });
    expect(mocks.listQualityRules).toHaveBeenLastCalledWith(
      expect.objectContaining({ owner: "", ownerUnassigned: true })
    );

    const fullPath = activeRouter?.currentRoute.value.fullPath ?? "";
    wrapper.unmount();
    const reloaded = await mountPage(PERMISSIONS, fullPath);

    expect(reloaded.findComponent(UserSelectStub).props("modelValue")).toBe(
      "__unassigned__"
    );
    expect(mocks.listQualityRules).toHaveBeenLastCalledWith(
      expect.objectContaining({ owner: "", ownerUnassigned: true })
    );
  });

  it("clears the URL with the filters, keeping unrelated parameters", async () => {
    const wrapper = await mountPage(
      PERMISSIONS,
      "/data-quality/checks?q=none&state=1&keep=1"
    );

    const clear = wrapper
      .findAll("button")
      .find(
        (button) =>
          button.text().trim() === i18n.global.t("common.filter.clear")
      );
    expect(clear).toBeTruthy();
    await clear?.trigger("click");
    await flushPromises();

    expect(currentQuery()).toEqual({ keep: "1" });
    expect(mocks.listQualityRules).toHaveBeenLastCalledWith(
      expect.objectContaining({
        search: "",
        state: QualityRuleState.UNSPECIFIED,
      })
    );
  });

  it("adopts a filter change that arrives from outside", async () => {
    const wrapper = await mountPage();
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);

    await activeRouter?.replace({ query: { q: "external" } });
    await flushPromises();

    expect(
      (wrapper.find("#data-quality-search").element as HTMLInputElement).value
    ).toBe("external");
    expect(mocks.listQualityRules).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "external" })
    );
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);
  });
});

describe("DataQualityChecksPage directory rows", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listAllUsers.mockResolvedValue(USERS);
    mocks.runQualityRules.mockResolvedValue([]);
    mocks.listQualityRules.mockResolvedValue({
      items: [ruleFixture()],
      nextPageToken: "",
    });
  });

  it("lists the check with its target, type, health and owner", async () => {
    const wrapper = await mountPage();
    const rows = wrapper.findAll("tbody tr");
    expect(rows).toHaveLength(1);

    const text = wrapper.text();
    expect(text).toContain("orders are never null");
    expect(text).toContain("prod · shop · public.orders");
    expect(text).toContain(i18n.global.t("dataQuality.ruleType.nullPercent"));
    expect(text).toContain(i18n.global.t("dataQuality.severity.error"));
    expect(text).toContain(i18n.global.t("dataQuality.ruleState.enabled"));
    expect(text).toContain(i18n.global.t("dataQuality.status.pass"));
    expect(text).toContain("alice@example.com");
  });

  it("links the check title to its target workspace with the check selected", async () => {
    const wrapper = await mountPage();

    const title = wrapper
      .findAll("a")
      .find((link) => link.text().trim() === "orders are never null");
    expect(title).toBeDefined();
    const href = title?.attributes("href") ?? "";
    expect(href).toContain("/data-quality/targets/inst1/shop/public/orders");
    expect(href).toContain(`check=${RULE}`);
    expect(href).toContain("metaType=4");
  });

  it("links the target path and the open-target action to the workspace itself", async () => {
    const wrapper = await mountPage();

    const targetLinks = wrapper
      .findAll("a")
      .filter((link) => link.text().trim() === "prod · shop · public.orders");
    expect(targetLinks.length).toBeGreaterThan(0);
    for (const link of targetLinks) {
      const href = link.attributes("href") ?? "";
      expect(href).toContain("/data-quality/targets/inst1/shop/public/orders");
      expect(href).not.toContain("check=");
    }

    const openTarget = wrapper.find(
      `a[aria-label="${i18n.global.t("dataQuality.checks.openTarget")}"]`
    );
    expect(openTarget.exists()).toBe(true);
    expect(openTarget.attributes("href")).not.toContain("check=");
  });

  it("runs a check from the row without offering edit, pause or delete", async () => {
    const wrapper = await mountPage();

    const run = wrapper.find(
      `[aria-label="${i18n.global.t("dataQuality.runNow")}"]`
    );
    expect(run.exists()).toBe(true);
    await run.trigger("click");
    await flushPromises();

    expect(mocks.runQualityRules).toHaveBeenLastCalledWith({ rules: [RULE] });

    for (const key of [
      "dataQuality.editRule",
      "dataQuality.pauseRule",
      "dataQuality.resumeRule",
    ]) {
      expect(
        wrapper.find(`[aria-label="${i18n.global.t(key)}"]`).exists()
      ).toBe(false);
    }
    expect(
      wrapper.find(`[aria-label="${i18n.global.t("common.delete")}"]`).exists()
    ).toBe(false);
  });
});

describe("DataQualityChecksPage empty and creation", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listAllUsers.mockResolvedValue(USERS);
    mocks.listQualityRules.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
  });

  it("sends an empty directory to the targets page instead of authoring a check here", async () => {
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.checks.emptyTitle")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.checks.emptyDescription")
    );
    const link = wrapper
      .findAll("a")
      .find(
        (candidate) =>
          candidate.text().trim() === i18n.global.t("dataQuality.targets.title")
      );
    expect(link?.attributes("href")).toBe("/data-quality/targets");
  });

  it("never offers a new-check action on the audit directory", async () => {
    const wrapper = await mountPage();
    expect(wrapper.text()).not.toContain(i18n.global.t("dataQuality.newRule"));
    expect(
      wrapper
        .findAll("a")
        .some((link) => link.attributes("href") === "/data-quality/rules/new")
    ).toBe(false);
    expect(
      wrapper
        .findAll("button")
        .some((button) =>
          button.text().includes(i18n.global.t("dataQuality.newRule"))
        )
    ).toBe(false);
  });
});
