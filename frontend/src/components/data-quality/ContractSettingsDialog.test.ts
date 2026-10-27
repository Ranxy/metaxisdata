import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import RuleFilterEditor from "@/components/data-quality/RuleFilterEditor.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import {
  QualityContractSchema,
  type QualityContractSettings,
  QualityContractSettingsSchema,
  QualityFilterOperator,
  QualitySeverity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { buildFilter } from "@/utils/dataQualityForm";
import ContractSettingsDialog from "./ContractSettingsDialog.vue";

const mocks = vi.hoisted(() => ({
  updateQualityContract: vi.fn(),
  listAllMetadata: vi.fn(),
}));

const notifyMock = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  updateQualityContract: mocks.updateQualityContract,
}));

vi.mock("@/api/database", () => ({
  listAllMetadata: mocks.listAllMetadata,
}));

vi.mock("@/lib/notify", () => ({ notify: notifyMock }));

const TABLE = "inst1;db1;public;orders";

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function dialogButton(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.trim() === text);
}

/** One target filter clause as the settings message carries it. */
function filterWithRegion(): QualityContractSettings["filter"] {
  return buildFilter([
    {
      column: "region",
      op: QualityFilterOperator.EQ,
      values: "EU",
    },
  ]);
}

function mountDialog(settings?: QualityContractSettings) {
  return mount(ContractSettingsDialog, {
    attachTo: document.body,
    props: {
      open: true,
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      settings,
    },
    global: { plugins: [i18n] },
  });
}

describe("ContractSettingsDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.listAllMetadata.mockResolvedValue([
      {
        guid: `${TABLE};region`,
        type: {
          case: "columnMetadata",
          value: { name: "region", type: "text", nullable: true },
        },
      },
    ]);
    mocks.updateQualityContract.mockResolvedValue(
      create(QualityContractSchema, { publishedVersion: 0 })
    );
  });

  it("prefills the filter and the default severity", async () => {
    const wrapper = mountDialog(
      create(QualityContractSettingsSchema, {
        filter: filterWithRegion(),
        defaultSeverity: QualitySeverity.ERROR,
      })
    );
    await flushPromises();

    // The filter is edited as the same clause rows the rule editor uses.
    const filterEditor = wrapper.findComponent(RuleFilterEditor);
    expect(filterEditor.props("modelValue")).toEqual([
      { column: "region", op: QualityFilterOperator.EQ, values: "EU" },
    ]);

    // The severity is a select, whose label only renders once its menu opens; the
    // saved request is the observable proof that it was prefilled.
    dialogButton(label("dataQuality.contract.saveSettings"))?.click();
    await flushPromises();
    const [updated] = mocks.updateQualityContract.mock.calls[0] as [
      { settings: QualityContractSettings },
    ];
    expect(updated.settings.defaultSeverity).toBe(QualitySeverity.ERROR);
    // Scheduling is the target's, not a contract default.
    expect(updated.settings).not.toHaveProperty(
      "defaultScheduleIntervalSeconds"
    );
    wrapper.unmount();
  });

  it("saves the complete settings and reports the returned contract", async () => {
    const contract = create(QualityContractSchema, { publishedVersion: 1 });
    mocks.updateQualityContract.mockResolvedValue(contract);
    const wrapper = mountDialog(
      create(QualityContractSettingsSchema, {
        defaultSeverity: QualitySeverity.WARNING,
      })
    );
    await flushPromises();

    dialogButton(label("dataQuality.contract.saveSettings"))?.click();
    await flushPromises();

    expect(mocks.updateQualityContract).toHaveBeenCalledTimes(1);
    const [request] = mocks.updateQualityContract.mock.calls[0] as [
      {
        targetGuid: string;
        metaType: MetaType;
        settings: QualityContractSettings;
      },
    ];
    expect(request.targetGuid).toBe(TABLE);
    expect(request.metaType).toBe(MetaType.TABLE);
    expect(request.settings.defaultSeverity).toBe(QualitySeverity.WARNING);
    expect(request.settings.filter?.clauses).toEqual([]);

    expect(notifyMock.success).toHaveBeenCalledWith(
      label("dataQuality.contract.settingsSaved")
    );
    expect(wrapper.emitted("updated")).toEqual([[contract]]);
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("clears a default the form leaves empty", async () => {
    const wrapper = mountDialog();
    await flushPromises();

    dialogButton(label("dataQuality.contract.saveSettings"))?.click();
    await flushPromises();

    const [request] = mocks.updateQualityContract.mock.calls[0] as [
      { settings: QualityContractSettings },
    ];
    // UNSPECIFIED is the message's own "no default" value.
    expect(request.settings.defaultSeverity).toBe(QualitySeverity.UNSPECIFIED);
    wrapper.unmount();
  });

  it("offers the target's columns to the filter editor", async () => {
    const wrapper = mountDialog();
    await flushPromises();

    expect(mocks.listAllMetadata).toHaveBeenCalledWith({
      parentGuid: TABLE,
      metaType: MetaType.COLUMN,
    });
    expect(wrapper.findComponent(RuleFilterEditor).props("columns")).toEqual([
      { name: "region", type: "text", nullable: true },
    ]);
    wrapper.unmount();
  });

  it("shows the save failure in place and stays open", async () => {
    mocks.updateQualityContract.mockRejectedValue(
      new ConnectError("filter column unknown", Code.InvalidArgument)
    );
    const wrapper = mountDialog();
    await flushPromises();

    dialogButton(label("dataQuality.contract.saveSettings"))?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.contract.settingsFailed")
    );
    expect(document.body.textContent).toContain("filter column unknown");
    expect(wrapper.emitted("updated")).toBeUndefined();
    expect(wrapper.emitted("update:open")).toBeUndefined();
    wrapper.unmount();
  });

  it("lets the severity select choose no default at all", async () => {
    const wrapper = mountDialog(
      create(QualityContractSettingsSchema, {
        defaultSeverity: QualitySeverity.CRITICAL,
      })
    );
    await flushPromises();

    const severitySelect = wrapper
      .findAllComponents(Select)
      .find((candidate) => candidate.find("#dq-contract-severity").exists());
    severitySelect?.vm.$emit(
      "update:modelValue",
      String(QualitySeverity.UNSPECIFIED)
    );
    await flushPromises();

    dialogButton(label("dataQuality.contract.saveSettings"))?.click();
    await flushPromises();
    const [request] = mocks.updateQualityContract.mock.calls[0] as [
      { settings: QualityContractSettings },
    ];
    expect(request.settings.defaultSeverity).toBe(QualitySeverity.UNSPECIFIED);
    wrapper.unmount();
  });
});
