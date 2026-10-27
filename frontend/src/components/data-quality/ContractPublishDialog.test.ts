import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  PublishQualityContractResponseSchema,
  type QualityContract,
  QualityContractChangeSummarySchema,
  QualityContractSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import ContractPublishDialog from "./ContractPublishDialog.vue";

const mocks = vi.hoisted(() => ({
  publishQualityContract: vi.fn(),
}));

const notifyMock = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  publishQualityContract: mocks.publishQualityContract,
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
  options: {
    version?: number;
    changes?: boolean;
    added?: number;
    modified?: number;
    removed?: number;
  } = {}
): QualityContract {
  return create(QualityContractSchema, {
    publishedVersion: options.version ?? 2,
    hasUnpublishedChanges: options.changes ?? true,
    changeSummary: create(QualityContractChangeSummarySchema, {
      added: options.added ?? 1,
      modified: options.modified ?? 2,
      removed: options.removed ?? 0,
    }),
  });
}

function mountDialog(contract: QualityContract = contractFixture()) {
  return mount(ContractPublishDialog, {
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

describe("ContractPublishDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
  });

  it("shows what the next version records and names it", async () => {
    const wrapper = mountDialog();
    await flushPromises();

    const text = document.body.textContent ?? "";
    expect(text).toContain(
      label("dataQuality.contract.changeAdded", { count: 1 })
    );
    expect(text).toContain(
      label("dataQuality.contract.changeModified", { count: 2 })
    );
    expect(text).toContain(
      label("dataQuality.contract.changeRemoved", { count: 0 })
    );
    // The next version is one past the published one.
    expect(
      dialogButton(label("dataQuality.contract.publishConfirm", { version: 3 }))
    ).toBeDefined();
    // The note is capped at 500 characters by the service.
    expect(
      document.querySelector("#dq-contract-note")?.getAttribute("maxlength")
    ).toBe("500");
    wrapper.unmount();
  });

  it("disables the confirm when nothing changed", async () => {
    const wrapper = mountDialog(contractFixture({ changes: false }));
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.contract.publishNoChanges")
    );
    const confirm = dialogButton(
      label("dataQuality.contract.publishConfirm", { version: 3 })
    );
    expect(confirm?.hasAttribute("disabled")).toBe(true);
    wrapper.unmount();
  });

  it("publishes with the note and reports the returned contract", async () => {
    const published = contractFixture({ version: 3, changes: false });
    mocks.publishQualityContract.mockResolvedValue(
      create(PublishQualityContractResponseSchema, {
        contract: published,
        noChanges: false,
      })
    );
    const wrapper = mountDialog();
    await flushPromises();

    const note =
      document.querySelector<HTMLTextAreaElement>("#dq-contract-note");
    expect(note).not.toBeNull();
    note!.value = "Quarterly review";
    note!.dispatchEvent(new Event("input", { bubbles: true }));
    await flushPromises();

    dialogButton(
      label("dataQuality.contract.publishConfirm", { version: 3 })
    )?.click();
    await flushPromises();

    expect(mocks.publishQualityContract).toHaveBeenCalledWith({
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      note: "Quarterly review",
    });
    expect(notifyMock.success).toHaveBeenCalledWith(
      label("dataQuality.contract.published")
    );
    expect(wrapper.emitted("published")).toEqual([[published]]);
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("reports a no-op publish without claiming a new version", async () => {
    mocks.publishQualityContract.mockResolvedValue(
      create(PublishQualityContractResponseSchema, {
        contract: contractFixture({ version: 2, changes: false }),
        noChanges: true,
      })
    );
    const wrapper = mountDialog();
    await flushPromises();

    dialogButton(
      label("dataQuality.contract.publishConfirm", { version: 3 })
    )?.click();
    await flushPromises();

    expect(notifyMock.success).not.toHaveBeenCalled();
    expect(wrapper.emitted("published")).toBeUndefined();
    expect(wrapper.emitted("update:open")).toBeUndefined();
    expect(document.body.textContent).toContain(
      label("dataQuality.contract.publishNoChanges")
    );
    wrapper.unmount();
  });

  it("shows the publish failure in place and stays open", async () => {
    mocks.publishQualityContract.mockRejectedValue(
      new ConnectError("rule is not compilable", Code.FailedPrecondition)
    );
    const wrapper = mountDialog();
    await flushPromises();

    dialogButton(
      label("dataQuality.contract.publishConfirm", { version: 3 })
    )?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.contract.publishFailed")
    );
    expect(document.body.textContent).toContain("rule is not compilable");
    expect(wrapper.emitted("published")).toBeUndefined();
    expect(wrapper.emitted("update:open")).toBeUndefined();
    wrapper.unmount();
  });
});
