import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
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

const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
];

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
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const wrapper = mount(DataQualityRulesPage, {
    global: {
      plugins: [router, i18n, pinia],
      stubs: { TargetScopeFilter: TargetScopeFilterStub },
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
