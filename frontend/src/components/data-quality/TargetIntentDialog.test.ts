import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  QualityAssetCriticality,
  type QualityContract,
  QualityContractIntentExemptionSchema,
  QualityContractIntentSchema,
  QualityContractIntentSource,
  QualityContractIntentStatementSchema,
  QualityContractSchema,
  QualityContractSettingsSchema,
  QualityCoverageProperty,
  QualityFilterOperator,
  QualitySeverity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { buildFilter } from "@/utils/dataQualityForm";
import TargetIntentDialog from "./TargetIntentDialog.vue";

const mocks = vi.hoisted(() => ({
  updateQualityContract: vi.fn(),
}));

const notifyMock = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
}));

const directoryMock = vi.hoisted(() => ({
  emailOf: vi.fn((name: string) => name),
  load: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  updateQualityContract: mocks.updateQualityContract,
}));

vi.mock("@/lib/notify", () => ({ notify: notifyMock }));

vi.mock("@/composables/useUserDirectory", () => ({
  useUserDirectory: () => directoryMock,
}));

const TABLE = "inst1;db1;public;orders";

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function dialogButton(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.trim() === text);
}

function contractFixture(
  options: { withReason?: boolean } = {}
): QualityContract {
  return create(QualityContractSchema, {
    publishedVersion: 2,
    stagedBaseVersion: 2,
    settings: create(QualityContractSettingsSchema, {
      defaultSeverity: QualitySeverity.ERROR,
      filter: buildFilter([
        { column: "region", op: QualityFilterOperator.EQ, values: "EU" },
      ]),
      intent: create(QualityContractIntentSchema, {
        criticality: QualityAssetCriticality.CRITICAL,
        effectiveFrom: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
        statements: [
          create(QualityContractIntentStatementSchema, {
            property: QualityCoverageProperty.FRESHNESS,
            tolerance: "6h",
          }),
        ],
        dependents: ["inst1;db1;public;orders_upstream"],
        exemptions: [
          create(QualityContractIntentExemptionSchema, {
            property: QualityCoverageProperty.SCHEMA_STABILITY,
            reason: options.withReason === false ? "" : "pinned upstream",
            approvedBy: "users/7",
          }),
        ],
        source: QualityContractIntentSource.IMPORTED,
        domainRef: "domain/sales",
      }),
    }),
  });
}

function mountDialog(contract: QualityContract = contractFixture()) {
  return mount(TargetIntentDialog, {
    attachTo: document.body,
    props: {
      open: true,
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      contract,
    },
    global: { plugins: [i18n] },
  });
}

async function save() {
  dialogButton(label("dataQuality.intent.save"))?.click();
  await flushPromises();
  return mocks.updateQualityContract.mock.calls[0]?.[0] as
    | {
        settings: {
          defaultSeverity: QualitySeverity;
          filter?: { clauses: unknown[] };
          intent?: {
            criticality: QualityAssetCriticality;
            statements: {
              property: QualityCoverageProperty;
              tolerance: string;
            }[];
            dependents: string[];
            exemptions: { reason: string; expiresAt?: { seconds: bigint } }[];
            effectiveFrom?: { seconds: bigint };
            source: QualityContractIntentSource;
            domainRef: string;
          };
        };
      }
    | undefined;
}

describe("TargetIntentDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.updateQualityContract.mockImplementation(
      async (options: { settings: unknown }) =>
        create(QualityContractSchema, {
          publishedVersion: 2,
          stagedBaseVersion: 2,
          settings: options.settings as never,
        })
    );
  });

  it("saves the stored intent, preserving the fields it does not edit", async () => {
    const wrapper = mountDialog();
    await flushPromises();
    const options = await save();

    expect(options?.settings.defaultSeverity).toBe(QualitySeverity.ERROR);
    expect(options?.settings.filter?.clauses).toHaveLength(1);
    const intent = options?.settings.intent;
    expect(intent?.criticality).toBe(QualityAssetCriticality.CRITICAL);
    expect(intent?.statements[0]).toMatchObject({
      property: QualityCoverageProperty.FRESHNESS,
      tolerance: "6h",
    });
    expect(intent?.dependents).toEqual(["inst1;db1;public;orders_upstream"]);
    expect(intent?.exemptions[0]?.reason).toBe("pinned upstream");
    expect(intent?.source).toBe(QualityContractIntentSource.IMPORTED);
    expect(intent?.domainRef).toBe("domain/sales");
    expect(Number(intent?.effectiveFrom?.seconds)).toBe(
      Math.floor(new Date("2026-11-01T00:00:00Z").getTime() / 1000)
    );
    expect(notifyMock.success).toHaveBeenCalledWith(
      label("dataQuality.intent.saved")
    );
    expect(wrapper.emitted("updated")).toBeTruthy();
    expect(wrapper.emitted("update:open")?.[0]).toEqual([false]);
    wrapper.unmount();
  });

  it("refuses a waiver without a reason before calling the server", async () => {
    const wrapper = mountDialog(contractFixture({ withReason: false }));
    await flushPromises();
    await save();

    expect(mocks.updateQualityContract).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      label("dataQuality.intent.reasonRequired")
    );
    wrapper.unmount();
  });

  it("refuses the same promise twice", async () => {
    const contract = contractFixture();
    const intent = contract.settings?.intent;
    intent?.statements.push(
      create(QualityContractIntentStatementSchema, {
        property: QualityCoverageProperty.FRESHNESS,
        tolerance: "1d",
      })
    );
    const wrapper = mountDialog(contract);
    await flushPromises();
    await save();

    expect(mocks.updateQualityContract).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      label("dataQuality.intent.duplicate")
    );
    wrapper.unmount();
  });

  it("shows a save failure instead of closing the dialog", async () => {
    mocks.updateQualityContract.mockRejectedValueOnce(
      new ConnectError("boom", Code.Internal)
    );
    const wrapper = mountDialog();
    await flushPromises();
    await save();

    expect(document.body.textContent).toContain(
      label("dataQuality.intent.saveFailed")
    );
    expect(wrapper.emitted("update:open")).toBeFalsy();
    wrapper.unmount();
  });
});
