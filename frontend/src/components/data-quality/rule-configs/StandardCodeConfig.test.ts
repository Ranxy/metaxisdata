import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityCodeTableSchema,
  QualityRuleType,
  QualityStandardCode,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { emptyConfig, type RuleConfigForm } from "@/utils/dataQualityForm";
import StandardCodeConfig from "./StandardCodeConfig.vue";

const mocks = vi.hoisted(() => ({
  listAllQualityCodeTables: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listAllQualityCodeTables: mocks.listAllQualityCodeTables,
}));

const LIST_PERMISSION = "metaxisdata.dataQuality.codeTables.list";
const MUNICIPAL = "workspaces/ws/qualityCodeTables/municipal";

function summary(name: string, title: string, valueCount: number) {
  return create(QualityCodeTableSchema, { name, title, valueCount });
}

function mountConfig(
  config: Partial<RuleConfigForm> = {},
  permissions: string[] = [LIST_PERMISSION]
) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  return mount(StandardCodeConfig, {
    props: {
      ruleType: QualityRuleType.STANDARD_CODE,
      modelValue: {
        ...emptyConfig(),
        standard: QualityStandardCode.CUSTOM_CODE_TABLE,
        ...config,
      },
      fieldErrors: {},
    },
    global: {
      plugins: [i18n, pinia],
      stubs: { RouterLink: { template: "<a><slot /></a>" } },
    },
  });
}

function codeTableSelect(wrapper: ReturnType<typeof mountConfig>) {
  return wrapper
    .findAllComponents(Select)
    .find((candidate) => candidate.find("#dq-field-codeTable").exists());
}

/** Whether the code-table label carries the required-field asterisk. */
function codeTableIsMarkedRequired(
  wrapper: ReturnType<typeof mountConfig>
): boolean {
  const label = wrapper
    .findAll("label")
    .find((candidate) =>
      candidate.text().includes(i18n.global.t("dataQuality.editor.codeTable"))
    );
  return label?.html().includes("text-destructive") ?? false;
}

describe("StandardCodeConfig code table", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    i18n.global.locale.value = "en-US";
    mocks.listAllQualityCodeTables.mockResolvedValue([
      summary(MUNICIPAL, "Municipal codes", 12),
      summary("workspaces/ws/qualityCodeTables/industry", "Industry codes", 3),
    ]);
  });

  it("loads the summaries and shows the selected table's value count", async () => {
    const wrapper = mountConfig({ codeTable: MUNICIPAL });
    await flushPromises();

    expect(mocks.listAllQualityCodeTables).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.codeTable")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.codeTableValueCount", { count: 12 })
    );
    wrapper.unmount();
  });

  it("marks the select required only for a custom standard", async () => {
    const custom = mountConfig();
    await flushPromises();
    expect(codeTableIsMarkedRequired(custom)).toBe(true);
    custom.unmount();

    const builtIn = mountConfig({
      standard: QualityStandardCode.ORGANIZATION_CODE,
    });
    await flushPromises();
    expect(codeTableIsMarkedRequired(builtIn)).toBe(false);
    builtIn.unmount();
  });

  it("writes the chosen resource name into the config", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    useAuthStore().user = create(UserSchema, {
      permissions: [LIST_PERMISSION],
    });
    const model = {
      ...emptyConfig(),
      standard: QualityStandardCode.CUSTOM_CODE_TABLE,
    };
    const wrapper = mount(StandardCodeConfig, {
      props: {
        ruleType: QualityRuleType.STANDARD_CODE,
        modelValue: model,
        fieldErrors: {},
      },
      global: {
        plugins: [i18n, pinia],
        stubs: { RouterLink: { template: "<a><slot /></a>" } },
      },
    });
    await flushPromises();

    codeTableSelect(wrapper)?.vm.$emit("update:modelValue", MUNICIPAL);
    await flushPromises();
    expect(model.codeTable).toBe(MUNICIPAL);
    // The selected table's value count replaces the generic hint.
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.codeTableValueCount", { count: 12 })
    );

    // The "no workspace table" sentinel becomes the empty config value.
    codeTableSelect(wrapper)?.vm.$emit("update:modelValue", "-");
    await flushPromises();
    expect(model.codeTable).toBe("");
    wrapper.unmount();
  });

  it("points at the empty state when the workspace has no tables", async () => {
    mocks.listAllQualityCodeTables.mockResolvedValue([]);
    const wrapper = mountConfig();
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.codeTableEmpty")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.codeTableEmptyAction")
    );
    wrapper.unmount();
  });

  it("offers a retry when the list read fails", async () => {
    mocks.listAllQualityCodeTables.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mountConfig();
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.codeTableLoadFailed")
    );
    const retry = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.retry")));
    expect(retry).toBeDefined();
    await retry?.trigger("click");
    await flushPromises();
    expect(mocks.listAllQualityCodeTables).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("makes no request without the list permission", async () => {
    const wrapper = mountConfig({}, []);
    await flushPromises();

    expect(mocks.listAllQualityCodeTables).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.codeTableNoPermission")
    );
    wrapper.unmount();
  });
});
