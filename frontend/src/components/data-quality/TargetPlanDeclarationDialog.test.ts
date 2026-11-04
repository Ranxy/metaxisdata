import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  type QualityContract,
  QualityContractSchema,
  QualityContractSettingsSchema,
  QualityCoverageProperty,
  QualityPlanCoverageSchema,
  QualityPlanSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetPlanDeclarationDialog from "./TargetPlanDeclarationDialog.vue";

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

function contractFixture(
  options: { derived?: boolean; declared?: boolean; duplicate?: boolean } = {}
): QualityContract {
  const coverage = options.duplicate
    ? [
        create(QualityPlanCoverageSchema, {
          property: QualityCoverageProperty.VOLUME,
        }),
        create(QualityPlanCoverageSchema, {
          property: QualityCoverageProperty.VOLUME,
        }),
      ]
    : [
        create(QualityPlanCoverageSchema, {
          property: QualityCoverageProperty.VOLUME,
        }),
        create(QualityPlanCoverageSchema, {
          property: QualityCoverageProperty.FRESHNESS,
          column: "created_at",
          minChecks: 2,
        }),
      ];
  return create(QualityContractSchema, {
    publishedVersion: 3,
    stagedBaseVersion: 3,
    settings: create(QualityContractSettingsSchema),
    plan:
      (options.derived ?? true)
        ? create(QualityPlanSchema, { coverage })
        : undefined,
    stagedPlan: options.declared
      ? create(QualityPlanSchema, { coverage })
      : undefined,
  });
}

function mountDialog(contract?: QualityContract) {
  return mount(TargetPlanDeclarationDialog, {
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

describe("TargetPlanDeclarationDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.listAllMetadata.mockResolvedValue([]);
    mocks.updateQualityContract.mockImplementation(
      async (options: { settings: unknown }) =>
        create(QualityContractSchema, {
          publishedVersion: 3,
          stagedBaseVersion: 3,
          settings: options.settings as never,
        })
    );
  });

  it("starts from the derived plan and saves it as a declaration", async () => {
    mountDialog(contractFixture());
    await flushPromises();

    const save = dialogButton(label("dataQuality.planDeclaration.save"));
    expect(save).toBeTruthy();
    save?.click();
    await flushPromises();

    expect(mocks.updateQualityContract).toHaveBeenCalledTimes(1);
    const options = mocks.updateQualityContract.mock.calls[0][0] as {
      draft: {
        coverage: {
          property: QualityCoverageProperty;
          column: string;
          minChecks: number;
        }[];
      };
      stagedBaseVersion: number;
    };
    expect(options.stagedBaseVersion).toBe(3);
    expect(options.draft.coverage).toHaveLength(2);
    expect(options.draft.coverage[1]).toMatchObject({
      property: QualityCoverageProperty.FRESHNESS,
      column: "created_at",
      minChecks: 2,
    });
    expect(notifyMock.success).toHaveBeenCalledWith(
      label("dataQuality.planDeclaration.saved")
    );
  });

  it("saves an empty declaration when the declaration is cleared", async () => {
    mountDialog(contractFixture({ declared: true }));
    await flushPromises();

    dialogButton(label("dataQuality.planDeclaration.clear"))?.click();
    await flushPromises();
    dialogButton(label("dataQuality.planDeclaration.save"))?.click();
    await flushPromises();

    const options = mocks.updateQualityContract.mock.calls[0][0] as {
      draft: { coverage: unknown[] };
    };
    expect(options.draft.coverage).toHaveLength(0);
  });

  it("refuses a duplicated declaration before calling the server", async () => {
    mountDialog(contractFixture({ duplicate: true }));
    await flushPromises();

    dialogButton(label("dataQuality.planDeclaration.save"))?.click();
    await flushPromises();

    expect(mocks.updateQualityContract).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      label("dataQuality.planDeclaration.duplicate")
    );
  });
});
