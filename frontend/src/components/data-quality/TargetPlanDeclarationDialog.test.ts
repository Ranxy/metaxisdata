import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
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

/** The derived plan and the declaration differ so a prefill regression is visible. */
function contractFixture(
  options: { declared?: boolean; duplicate?: boolean } = {}
): QualityContract {
  const derived = [
    create(QualityPlanCoverageSchema, {
      property: QualityCoverageProperty.VOLUME,
    }),
  ];
  const declaration = options.duplicate
    ? [
        create(QualityPlanCoverageSchema, {
          property: QualityCoverageProperty.VOLUME,
          column: "email",
        }),
        create(QualityPlanCoverageSchema, {
          property: QualityCoverageProperty.VOLUME,
          column: " email ",
        }),
      ]
    : [
        create(QualityPlanCoverageSchema, {
          property: QualityCoverageProperty.FRESHNESS,
          column: "created_at",
          minChecks: 2,
        }),
      ];
  return create(QualityContractSchema, {
    publishedVersion: 3,
    stagedBaseVersion: options.declared ? 3 : 0,
    settings: create(QualityContractSettingsSchema),
    plan: create(QualityPlanSchema, { coverage: derived }),
    stagedPlan:
      options.declared || options.duplicate
        ? create(QualityPlanSchema, { coverage: declaration })
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

async function saveAndCapture() {
  dialogButton(label("dataQuality.planDeclaration.save"))?.click();
  await flushPromises();
  return mocks.updateQualityContract.mock.calls[0]?.[0] as
    | {
        draft: {
          coverage: {
            property: QualityCoverageProperty;
            column: string;
            minChecks: number;
          }[];
        };
        stagedBaseVersion: number;
      }
    | undefined;
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

  it("prefills from the derived plan when nothing is declared", async () => {
    const wrapper = mountDialog(contractFixture());
    await flushPromises();
    const options = await saveAndCapture();

    expect(options?.stagedBaseVersion).toBe(3);
    expect(options?.draft.coverage).toHaveLength(1);
    expect(options?.draft.coverage[0]?.property).toBe(
      QualityCoverageProperty.VOLUME
    );
    expect(wrapper.emitted("update:open")?.[0]).toEqual([false]);
    expect(wrapper.emitted("updated")).toBeTruthy();
    expect(notifyMock.success).toHaveBeenCalledWith(
      label("dataQuality.planDeclaration.saved")
    );
    wrapper.unmount();
  });

  it("prefills from the declaration when one exists", async () => {
    const wrapper = mountDialog(contractFixture({ declared: true }));
    await flushPromises();
    const options = await saveAndCapture();

    expect(options?.draft.coverage).toHaveLength(1);
    expect(options?.draft.coverage[0]).toMatchObject({
      property: QualityCoverageProperty.FRESHNESS,
      column: "created_at",
      minChecks: 2,
    });
    wrapper.unmount();
  });

  it("saves an empty declaration when the declaration is cleared", async () => {
    const wrapper = mountDialog(contractFixture({ declared: true }));
    await flushPromises();

    dialogButton(label("dataQuality.planDeclaration.clear"))?.click();
    await flushPromises();
    const options = await saveAndCapture();
    expect(options?.draft.coverage).toHaveLength(0);
    wrapper.unmount();
  });

  it("refuses a duplicated declaration, including whitespace variants", async () => {
    const wrapper = mountDialog(contractFixture({ duplicate: true }));
    await flushPromises();

    dialogButton(label("dataQuality.planDeclaration.save"))?.click();
    await flushPromises();

    expect(mocks.updateQualityContract).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      label("dataQuality.planDeclaration.duplicate")
    );
    wrapper.unmount();
  });

  it("clamps a typed minimum check count to the server's bounds", async () => {
    const wrapper = mountDialog(contractFixture());
    await flushPromises();

    const minChecks = document.body.querySelector<HTMLInputElement>(
      'input[type="number"]'
    );
    expect(minChecks).toBeTruthy();
    if (minChecks) {
      minChecks.value = "99";
      minChecks.dispatchEvent(new Event("input"));
      minChecks.dispatchEvent(new Event("blur"));
      await flushPromises();
    }
    const options = await saveAndCapture();
    expect(options?.draft.coverage[0]?.minChecks).toBe(10);
    wrapper.unmount();
  });

  it("shows a save failure instead of closing the dialog", async () => {
    mocks.updateQualityContract.mockRejectedValueOnce(
      new ConnectError("boom", Code.Internal)
    );
    const wrapper = mountDialog(contractFixture());
    await flushPromises();

    dialogButton(label("dataQuality.planDeclaration.save"))?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.planDeclaration.saveFailed")
    );
    expect(wrapper.emitted("update:open")).toBeFalsy();
    wrapper.unmount();
  });

  it("asks for a reload when the declaration changed underneath the dialog", async () => {
    mocks.updateQualityContract.mockRejectedValueOnce(
      new ConnectError("stale", Code.Aborted)
    );
    const wrapper = mountDialog(contractFixture());
    await flushPromises();

    dialogButton(label("dataQuality.planDeclaration.save"))?.click();
    await flushPromises();

    expect(wrapper.emitted("refresh")).toBeTruthy();
    expect(notifyMock.warning).toHaveBeenCalledWith(
      label("dataQuality.planDeclaration.conflict")
    );
    expect(wrapper.emitted("update:open")?.[0]).toEqual([false]);
    wrapper.unmount();
  });
});
