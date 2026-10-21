import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { QualityRuleSchema } from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import type { QualityTargetScope } from "@/utils/dataQuality";
import DataQualityRulesPage from "./DataQualityRulesPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityRules: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityRules: mocks.listQualityRules,
}));

const SCOPE: QualityTargetScope = {
  instance: "inst1",
  database: "shop",
  schema: "public",
  table: "orders",
  label: "Orders > shop > public > orders",
};

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
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.users.list",
];

async function mountPage(permissions = PERMISSIONS) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/new",
        name: "DataQualityRuleNew",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/rules");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualityRulesPage, {
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

function applyButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().trim() === i18n.global.t("common.apply"));
}

describe("DataQualityRulesPage target scope filter", () => {
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

describe("DataQualityRulesPage owner filter", () => {
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

  it("hides the owner filter without users.list", async () => {
    const wrapper = await mountPage(
      PERMISSIONS.filter((p) => p !== "metaxisdata.users.list")
    );

    expect(wrapper.find(".owner-stub").exists()).toBe(false);
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);
  });
});
