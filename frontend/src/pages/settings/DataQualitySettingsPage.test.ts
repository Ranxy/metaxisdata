import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityCodeTableSchema,
  QualityLLMUsageSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  DataQualitySettingSchema,
  DayOfWeek,
  QualityDigestSettingSchema,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualitySettingsPage from "./DataQualitySettingsPage.vue";

const mocks = vi.hoisted(() => ({
  getWorkspaceProfileSetting: vi.fn(),
  updateDataQualitySetting: vi.fn(),
  listProfiles: vi.fn(),
  getQualityLLMUsage: vi.fn(),
  listAllQualityCodeTables: vi.fn(),
  getQualityCodeTable: vi.fn(),
  createQualityCodeTable: vi.fn(),
  updateQualityCodeTable: vi.fn(),
  deleteQualityCodeTable: vi.fn(),
  listQualityAgents: vi.fn(),
  listAllInstances: vi.fn(),
  listDatabases: vi.fn(),
}));

vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
  updateDataQualitySetting: mocks.updateDataQualitySetting,
}));

vi.mock("@/api/llm", () => ({
  listProfiles: mocks.listProfiles,
}));

vi.mock("@/api/instance", () => ({
  listAllInstances: mocks.listAllInstances,
}));

vi.mock("@/api/database", () => ({
  listDatabases: mocks.listDatabases,
}));

vi.mock("@/api/data-quality", () => ({
  getQualityLLMUsage: mocks.getQualityLLMUsage,
  listAllQualityCodeTables: mocks.listAllQualityCodeTables,
  getQualityCodeTable: mocks.getQualityCodeTable,
  createQualityCodeTable: mocks.createQualityCodeTable,
  updateQualityCodeTable: mocks.updateQualityCodeTable,
  deleteQualityCodeTable: mocks.deleteQualityCodeTable,
  listQualityAgents: mocks.listQualityAgents,
}));

const PERMISSIONS = [
  "metaxisdata.settings.get",
  "metaxisdata.settings.update",
  "metaxisdata.dataQuality.suggest",
  "metaxisdata.dataQuality.codeTables.list",
  "metaxisdata.dataQuality.codeTables.create",
  "metaxisdata.dataQuality.codeTables.update",
  "metaxisdata.dataQuality.codeTables.delete",
];

const MUNICIPAL = "workspaces/ws/qualityCodeTables/municipal";

function codeTableSummary() {
  return create(QualityCodeTableSchema, {
    name: MUNICIPAL,
    title: "Municipal codes",
    description: "Divisions of the municipality.",
    valueCount: 12,
  });
}

function setInput(selector: string, value: string) {
  const element = document.querySelector(selector) as HTMLInputElement;
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

function setTextarea(selector: string, value: string) {
  const element = document.querySelector(selector) as HTMLTextAreaElement;
  Object.getOwnPropertyDescriptor(
    HTMLTextAreaElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

/** A button inside one of the page's (teleported) dialogs, by its text. */
function dialogButton(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.trim() === text);
}

async function mountPage(permissions = PERMISSIONS) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualitySettingsPage, {
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

function fieldInput(
  wrapper: ReturnType<typeof mount>,
  label: string
): ReturnType<ReturnType<typeof mount>["find"]> | undefined {
  const field = wrapper
    .findAllComponents(FormField)
    .find((candidate) => candidate.props("label") === label);
  return field?.find("input");
}

/** The header's save button. */
function saveButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes(i18n.global.t("common.save")));
}

/** The digest's weekday select, found by the id its trigger carries. */
function digestDaySelect(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAllComponents(Select)
    .find((candidate) => candidate.find("#dq-setting-digest-day").exists());
}

describe("DataQualitySettingsPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityAgents.mockResolvedValue([]);
    mocks.listAllInstances.mockResolvedValue([]);
    mocks.listDatabases.mockResolvedValue({ databases: [] });
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        allowedLlmProviderProfiles: ["llmProfiles/p1"],
        dataQuality: create(DataQualitySettingSchema, {
          enableLlmAnalysis: true,
          llmProviderProfile: "llmProfiles/p1",
          storeFailureSamples: true,
          failureSampleLimit: 20,
          defaultScheduleInterval: create(DurationSchema, {
            seconds: 3_600n,
            nanos: 0,
          }),
          dailyExecutionLimit: 10000,
          dailyLlmAnalysisLimit: 5000,
          llmConcurrencyLimit: 12,
          maxColumnsPerAnalysis: 50,
          sendSampleValuesToLlm: false,
        }),
      })
    );
    mocks.listProfiles.mockResolvedValue({
      profiles: [
        {
          name: "llmProfiles/p1",
          title: "Primary",
          models: [{ enabled: true }],
        },
      ],
    });
    mocks.updateDataQualitySetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {})
    );
    mocks.listAllQualityCodeTables.mockResolvedValue([]);
    mocks.getQualityCodeTable.mockResolvedValue(codeTableSummary());
    mocks.createQualityCodeTable.mockImplementation(async (table) => table);
    mocks.updateQualityCodeTable.mockImplementation(async (table) => table);
    mocks.deleteQualityCodeTable.mockResolvedValue(undefined);
    mocks.getQualityLLMUsage.mockResolvedValue(
      create(QualityLLMUsageSchema, {
        dailyAnalysisUsed: 1200,
        dailyAnalysisLimit: 10000,
        dailyColumnAnalysisUsed: 30,
        dailyColumnAnalysisLimit: 50,
        inFlight: 2,
        concurrencyLimit: 12,
        resetsAt: { seconds: 1_700_035_200n, nanos: 0 },
      })
    );
  });

  it("shows the effective values the server returned", async () => {
    const wrapper = await mountPage();

    expect(
      (
        fieldInput(
          wrapper,
          i18n.global.t("dataQualitySettings.failureSampleLimit")
        )?.element as HTMLInputElement
      ).value
    ).toBe("20");
    expect(
      (wrapper.find("#dq-setting-schedule").element as HTMLInputElement).value
    ).toBe("1");
    expect(
      (
        fieldInput(
          wrapper,
          i18n.global.t("dataQualitySettings.maxColumnsPerAnalysis")
        )?.element as HTMLInputElement
      ).value
    ).toBe("50");
    expect(
      (
        fieldInput(
          wrapper,
          i18n.global.t("dataQualitySettings.dailyLlmAnalysisLimit")
        )?.element as HTMLInputElement
      ).value
    ).toBe("5000");
    expect(
      (
        fieldInput(
          wrapper,
          i18n.global.t("dataQualitySettings.llmConcurrencyLimit")
        )?.element as HTMLInputElement
      ).value
    ).toBe("12");
    // The LLM profile select lists the allowed profile.
    expect(wrapper.text()).toContain("Primary");
  });

  it("mounts the agent management card for an administrator", async () => {
    mocks.listQualityAgents.mockResolvedValue([]);
    mocks.listAllInstances.mockResolvedValue([]);
    mocks.listDatabases.mockResolvedValue({ databases: [] });
    const wrapper = await mountPage();
    await flushPromises();

    expect(mocks.listQualityAgents).toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.agentsSection")
    );
    await wrapper.find("#dq-setting-agent-autonomy").trigger("click");
    await flushPromises();
  });

  it("configures the agent runner and its database scope", async () => {
    mocks.listAllInstances.mockResolvedValue([
      { name: "instances/i1", title: "Orders" },
    ]);
    mocks.listDatabases.mockResolvedValue({
      databases: [{ name: "instances/i1/databases/warehouse" }],
    });
    // The saved allowlist must render as checked, and unchecking it must remove the
    // scope on save rather than being silently re-saved.
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        dataQuality: create(DataQualitySettingSchema, {
          agentRunnerEnabled: true,
          agentRunnerDatabases: ["instances/i1/databases/warehouse"],
        }),
      })
    );
    const wrapper = await mountPage();
    await flushPromises();

    const database = wrapper.find(
      '[data-testid="dq-runner-db-instances/i1/databases/warehouse"]'
    );
    expect((database.element as HTMLInputElement).checked).toBe(true);
    await database.setValue(false);
    await wrapper.find("#dq-setting-runner-tokens").setValue("250000000");
    await wrapper.find("#dq-setting-runner-goals").setValue("40");
    await wrapper.find("#dq-setting-runner-concurrency").setValue("3");
    await flushPromises();

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.updateDataQualitySetting).toHaveBeenCalledTimes(1);
    const setting = mocks.updateDataQualitySetting.mock.calls[0]?.[0];
    expect(setting).toMatchObject({
      agentRunnerEnabled: true,
      agentRunnerDatabases: [],
      agentRunnerGoalsPerHour: 40,
      agentRunnerMaxConcurrentGoals: 3,
    });
    expect(setting.agentRunnerTokensPerHour).toBe(250_000_000n);
  });

  it("saves the whole section with the expected setting", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.failureSampleLimit")
    )?.setValue("25");
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.dailyLlmAnalysisLimit")
    )?.setValue("25000");
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.llmConcurrencyLimit")
    )?.setValue("6");

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    expect(save).toBeTruthy();
    await save?.trigger("click");
    await flushPromises();

    expect(mocks.updateDataQualitySetting).toHaveBeenCalledTimes(1);
    const setting = mocks.updateDataQualitySetting.mock.calls[0]?.[0];
    expect(setting).toMatchObject({
      enableLlmAnalysis: true,
      llmProviderProfile: "llmProfiles/p1",
      storeFailureSamples: true,
      failureSampleLimit: 25,
      dailyExecutionLimit: 10000,
      dailyLlmAnalysisLimit: 25000,
      llmConcurrencyLimit: 6,
      maxColumnsPerAnalysis: 50,
    });
    expect(setting.defaultScheduleInterval?.seconds).toBe(3_600n);
  });

  it("saves the agent autonomy kill switch", async () => {
    const wrapper = await mountPage();
    await wrapper.find("#dq-setting-agent-autonomy").trigger("click");
    await flushPromises();

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.updateDataQualitySetting).toHaveBeenCalledTimes(1);
    const setting = mocks.updateDataQualitySetting.mock.calls[0]?.[0];
    expect(setting).toMatchObject({ agentAutonomyEnabled: true });
  });

  it("reports a bad number inline and does not save", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.failureSampleLimit")
    )?.setValue("1000");

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    await save?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t(
        "dataQualitySettings.validation.failureSampleLimitInvalid",
        {
          max: 100,
        }
      )
    );
    expect(mocks.updateDataQualitySetting).not.toHaveBeenCalled();
  });

  it("shows the process-wide AI usage counters", async () => {
    const wrapper = await mountPage();

    expect(mocks.getQualityLLMUsage).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageRatio", {
        used: "1,200",
        limit: "10,000",
      })
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageRatio", {
        used: "30",
        limit: "50",
      })
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageRatio", {
        used: "2",
        limit: "12",
      })
    );
  });

  it("shows a usage error with a retry that recovers", async () => {
    mocks.getQualityLLMUsage.mockRejectedValueOnce(new Error("usage down"));
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageFetchFailed")
    );

    mocks.getQualityLLMUsage.mockResolvedValue(
      create(QualityLLMUsageSchema, {
        dailyAnalysisUsed: 5,
        dailyAnalysisLimit: 100,
      })
    );
    const refresh = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("common.refresh"))
      );
    await refresh?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageRatio", {
        used: "5",
        limit: "100",
      })
    );
  });

  it("explains the usage counter is unavailable without the suggest permission", async () => {
    const wrapper = await mountPage([
      "metaxisdata.settings.get",
      "metaxisdata.settings.update",
    ]);

    expect(mocks.getQualityLLMUsage).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageNoPermission")
    );
  });

  it("refetches the usage counters after a save", async () => {
    const wrapper = await mountPage();
    expect(mocks.getQualityLLMUsage).toHaveBeenCalledTimes(1);

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    await save?.trigger("click");
    await flushPromises();

    expect(mocks.getQualityLLMUsage).toHaveBeenCalledTimes(2);
  });

  it("validates the daily column-analysis ceiling inline", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.dailyColumnAnalysisLimit")
    )?.setValue("1000001");

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    await save?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t(
        "dataQualitySettings.validation.dailyColumnAnalysisLimitInvalid",
        { max: 1_000_000 }
      )
    );
    expect(mocks.updateDataQualitySetting).not.toHaveBeenCalled();
  });

  it("validates the new LLM limits before saving", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.dailyLlmAnalysisLimit")
    )?.setValue("1000001");
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.llmConcurrencyLimit")
    )?.setValue("65");

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    await save?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t(
        "dataQualitySettings.validation.dailyLlmAnalysisLimitInvalid",
        {
          max: 1_000_000,
        }
      )
    );
    expect(wrapper.text()).toContain(
      i18n.global.t(
        "dataQualitySettings.validation.llmConcurrencyLimitInvalid",
        {
          max: 64,
        }
      )
    );
    expect(mocks.updateDataQualitySetting).not.toHaveBeenCalled();
  });

  it("disables the LLM profile when LLM analysis is off", async () => {
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        dataQuality: create(DataQualitySettingSchema, {
          enableLlmAnalysis: false,
          failureSampleLimit: 20,
          defaultScheduleInterval: create(DurationSchema, {
            seconds: 3_600n,
            nanos: 0,
          }),
        }),
      })
    );
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.llmDisabledHint")
    );
  });

  it("lists code tables with their value count and updated time", async () => {
    mocks.listAllQualityCodeTables.mockResolvedValue([codeTableSummary()]);
    const wrapper = await mountPage();

    expect(mocks.listAllQualityCodeTables).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.codeTablesSection")
    );
    expect(wrapper.text()).toContain("Municipal codes");
    expect(wrapper.text()).toContain("Divisions of the municipality.");
    expect(wrapper.text()).toContain("12");
  });

  it("creates a code table from trimmed, de-duplicated values", async () => {
    const wrapper = await mountPage();

    await wrapper.find("[data-add-code-table]").trigger("click");
    await flushPromises();
    setInput("#dq-code-table-title", "Municipal codes");
    setTextarea("#dq-code-table-description", "Divisions.");
    setTextarea(
      "#dq-code-table-values",
      " 110000 \n# comment\n110000\n310000\n"
    );
    await flushPromises();
    document.body.querySelector<HTMLElement>("[data-save-code-table]")?.click();
    await flushPromises();

    expect(mocks.createQualityCodeTable).toHaveBeenCalledTimes(1);
    const [table] = mocks.createQualityCodeTable.mock.calls[0] as [
      ReturnType<typeof codeTableSummary>,
    ];
    expect(table.title).toBe("Municipal codes");
    expect(table.description).toBe("Divisions.");
    expect(table.values).toEqual(["110000", "310000"]);
    // The list refreshes so the new table is visible.
    expect(mocks.listAllQualityCodeTables).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQualitySettings.codeTableValuesRequired")
    );
    wrapper.unmount();
  });

  it("loads a table's values before editing and replaces them on save", async () => {
    mocks.listAllQualityCodeTables.mockResolvedValue([codeTableSummary()]);
    mocks.getQualityCodeTable.mockResolvedValue(
      create(QualityCodeTableSchema, {
        name: MUNICIPAL,
        title: "Municipal codes",
        description: "Divisions of the municipality.",
        values: ["110000", "310000"],
        valueCount: 2,
      })
    );
    const wrapper = await mountPage();
    await flushPromises();

    await wrapper
      .find(`button[title="${i18n.global.t("common.edit")}"]`)
      .trigger("click");
    await flushPromises();

    expect(mocks.getQualityCodeTable).toHaveBeenCalledWith(MUNICIPAL);
    expect(
      document.querySelector<HTMLTextAreaElement>("#dq-code-table-values")
        ?.value
    ).toBe("110000\n310000");

    setInput("#dq-code-table-title", "Renamed codes");
    setTextarea("#dq-code-table-values", "110000\n310000\n440000");
    await flushPromises();
    document.body.querySelector<HTMLElement>("[data-save-code-table]")?.click();
    await flushPromises();

    expect(mocks.updateQualityCodeTable).toHaveBeenCalledTimes(1);
    const [table, mask] = mocks.updateQualityCodeTable.mock.calls[0] as [
      ReturnType<typeof codeTableSummary>,
      string[],
    ];
    expect(table.name).toBe(MUNICIPAL);
    expect(table.title).toBe("Renamed codes");
    expect(table.values).toEqual(["110000", "310000", "440000"]);
    expect(mask).toEqual(["title", "description", "values"]);
    wrapper.unmount();
  });

  it("appends CSV codes to the typed values, parsed in the browser", async () => {
    const wrapper = await mountPage();

    await wrapper.find("[data-add-code-table]").trigger("click");
    await flushPromises();
    setTextarea("#dq-code-table-values", "110000");
    const input = document.querySelector(
      "#dq-code-table-csv"
    ) as HTMLInputElement;
    const file = new File(
      ["310000,Shanghai\n110000,Beijing\n# note\n440000,Shenzhen"],
      "codes.csv",
      { type: "text/csv" }
    );
    Object.defineProperty(input, "files", { value: [file] });
    input.dispatchEvent(new Event("change", { bubbles: true }));
    await flushPromises();

    expect(
      document.querySelector<HTMLTextAreaElement>("#dq-code-table-values")
        ?.value
    ).toBe("110000\n310000\n440000");
    wrapper.unmount();
  });

  it("refuses to save a code table with no values", async () => {
    const wrapper = await mountPage();

    await wrapper.find("[data-add-code-table]").trigger("click");
    await flushPromises();
    setInput("#dq-code-table-title", "Empty table");
    await flushPromises();
    document.body.querySelector<HTMLElement>("[data-save-code-table]")?.click();
    await flushPromises();

    expect(mocks.createQualityCodeTable).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      i18n.global.t("dataQualitySettings.codeTableValuesRequired")
    );
    wrapper.unmount();
  });

  it("surfaces the server's refusal when a referenced table is deleted", async () => {
    mocks.listAllQualityCodeTables.mockResolvedValue([codeTableSummary()]);
    mocks.deleteQualityCodeTable.mockRejectedValue(
      new ConnectError(
        "code table is referenced by quality rule orders-have-rows",
        Code.FailedPrecondition
      )
    );
    const wrapper = await mountPage();

    await wrapper
      .find(`button[title="${i18n.global.t("common.delete")}"]`)
      .trigger("click");
    await flushPromises();
    dialogButton(i18n.global.t("common.delete"))?.click();
    await flushPromises();

    expect(mocks.deleteQualityCodeTable).toHaveBeenCalledWith(MUNICIPAL);
    expect(wrapper.text()).toContain(
      "code table is referenced by quality rule orders-have-rows"
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.codeTableDeleteFailed")
    );
    wrapper.unmount();
  });

  it("hides the code-table card without the list permission", async () => {
    const wrapper = await mountPage([
      "metaxisdata.settings.get",
      "metaxisdata.settings.update",
    ]);

    expect(mocks.listAllQualityCodeTables).not.toHaveBeenCalled();
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQualitySettings.codeTablesSection")
    );
    wrapper.unmount();
  });

  it("shows the stored weekly digest schedule", async () => {
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        dataQuality: create(DataQualitySettingSchema, {
          weeklyDigest: create(QualityDigestSettingSchema, {
            enabled: true,
            dayOfWeek: DayOfWeek.FRIDAY,
            time: "18:30",
            timezone: "Asia/Shanghai",
          }),
        }),
      })
    );
    const wrapper = await mountPage();

    expect(
      wrapper.find("#dq-setting-digest-enabled").attributes("data-state")
    ).toBe("checked");
    expect(
      (
        fieldInput(wrapper, i18n.global.t("dataQualitySettings.digestTime"))
          ?.element as HTMLInputElement
      ).value
    ).toBe("18:30");
    expect(
      (
        fieldInput(wrapper, i18n.global.t("dataQualitySettings.digestTimezone"))
          ?.element as HTMLInputElement
      ).value
    ).toBe("Asia/Shanghai");
    expect(wrapper.html()).toContain(
      i18n.global.t("dataQualitySettings.weekdayFriday")
    );
  });

  it("saves the weekly digest with the rest of the settings", async () => {
    const wrapper = await mountPage();

    await wrapper.find("#dq-setting-digest-enabled").trigger("click");
    await flushPromises();
    await digestDaySelect(wrapper)?.vm.$emit(
      "update:modelValue",
      String(DayOfWeek.SUNDAY)
    );
    await flushPromises();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.digestTime")
    )?.setValue("07:05");
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.digestTimezone")
    )?.setValue("Europe/Berlin");

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.updateDataQualitySetting).toHaveBeenCalledTimes(1);
    const setting = mocks.updateDataQualitySetting.mock.calls[0]?.[0];
    expect(setting.weeklyDigest).toMatchObject({
      enabled: true,
      dayOfWeek: DayOfWeek.SUNDAY,
      time: "07:05",
      timezone: "Europe/Berlin",
    });
  });

  it("carries the untouched digest so saving cannot clear it", async () => {
    const wrapper = await mountPage();

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    const setting = mocks.updateDataQualitySetting.mock.calls[0]?.[0];
    // The whole data_quality field is replaced, so even an untouched digest has
    // to travel with the request.
    expect(setting.weeklyDigest).toMatchObject({
      enabled: false,
      dayOfWeek: DayOfWeek.MONDAY,
      time: "09:00",
      timezone: "UTC",
    });
  });

  it("surfaces the server's digest validation error", async () => {
    const message =
      'weekly_digest.timezone "asia/shanghai" is not a valid IANA time zone';
    mocks.updateDataQualitySetting.mockRejectedValueOnce(
      new ConnectError(message, Code.InvalidArgument)
    );
    const wrapper = await mountPage();

    await wrapper.find("#dq-setting-digest-enabled").trigger("click");
    await flushPromises();
    // The browser accepts the lower-case name, so only the server can reject it.
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.digestTimezone")
    )?.setValue("asia/shanghai");
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(message);
  });

  it("keeps the digest controls read-only without settings.update", async () => {
    const wrapper = await mountPage(["metaxisdata.settings.get"]);

    expect(
      wrapper.find("#dq-setting-digest-enabled").attributes("disabled")
    ).toBeDefined();
    expect(
      (
        fieldInput(wrapper, i18n.global.t("dataQualitySettings.digestTimezone"))
          ?.element as HTMLInputElement
      ).disabled
    ).toBe(true);
  });
});
