import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  GetQualityContractRevisionResponseSchema,
  type QualityContractCheck,
  QualityContractCheckSchema,
  type QualityContractRevision,
  QualityContractRevisionSchema,
  QualityRuleType,
  QualitySeverity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import ContractHistoryDrawer from "./ContractHistoryDrawer.vue";

const mocks = vi.hoisted(() => ({
  listQualityContractRevisions: vi.fn(),
  getQualityContractRevision: vi.fn(),
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityContractRevisions: mocks.listQualityContractRevisions,
  getQualityContractRevision: mocks.getQualityContractRevision,
}));

vi.mock("@/api/user", () => ({
  listAllUsers: mocks.listAllUsers,
}));

const TABLE = "inst1;db1;public;orders";

/** Radix opens a select on pointerdown and calls pointer-capture APIs jsdom lacks. */
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function dialogButton(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.trim() === text);
}

function revisionFixture(
  version: number,
  options: {
    checkCount?: number;
    note?: string;
    added?: number;
    modified?: number;
    removed?: number;
  } = {}
): QualityContractRevision {
  return create(QualityContractRevisionSchema, {
    version,
    publishedAt: timestampFromDate(new Date()),
    publishedBy: "users/alice",
    note: options.note ?? "",
    checkCount: options.checkCount ?? 3,
    changeSummary: {
      added: options.added ?? 0,
      modified: options.modified ?? 0,
      removed: options.removed ?? 0,
    },
  });
}

function checkFixture(
  id: string,
  options: {
    revision?: number;
    title?: string;
    severity?: QualitySeverity;
    column?: string;
  } = {}
): QualityContractCheck {
  return create(QualityContractCheckSchema, {
    rule: `workspaces/ws/qualityRules/${id}`,
    ruleRevision: options.revision ?? 1,
    title: options.title ?? id,
    ruleType: QualityRuleType.ROW_COUNT,
    severity: options.severity ?? QualitySeverity.WARNING,
    column: options.column ?? "",
  });
}

function revisionResponse(version: number, checks: QualityContractCheck[]) {
  return create(GetQualityContractRevisionResponseSchema, {
    revision: revisionFixture(version),
    checks,
  });
}

async function mountDrawer() {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: ["metaxisdata.users.list"],
  });
  const wrapper = mount(ContractHistoryDrawer, {
    attachTo: document.body,
    props: { open: true, targetGuid: TABLE, metaType: MetaType.TABLE },
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("ContractHistoryDrawer", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.listAllUsers.mockResolvedValue([
      create(UserSchema, {
        name: "users/alice",
        email: "alice@example.com",
      }),
    ]);
    mocks.listQualityContractRevisions.mockResolvedValue({
      items: [revisionFixture(2, { checkCount: 1 })],
      nextPageToken: "",
    });
  });

  it("lists the published versions with their size, note and publisher", async () => {
    mocks.listQualityContractRevisions.mockResolvedValue({
      items: [
        revisionFixture(3, {
          checkCount: 2,
          note: "Quarterly review",
          added: 1,
          modified: 2,
        }),
        revisionFixture(2, { checkCount: 1 }),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountDrawer();

    expect(mocks.listQualityContractRevisions).toHaveBeenCalledWith(
      expect.objectContaining({
        targetGuid: TABLE,
        metaType: MetaType.TABLE,
        pageSize: 20,
        pageToken: "",
      })
    );
    const text = document.body.textContent ?? "";
    expect(text).toContain(
      label("dataQuality.contract.statePublished", { version: 3 })
    );
    expect(text).toContain(
      label("dataQuality.contract.checkCount", { count: 2 })
    );
    expect(text).toContain("Quarterly review");
    expect(text).toContain(
      label("dataQuality.contract.changeAdded", { count: 1 })
    );
    expect(text).toContain(
      label("dataQuality.contract.changeModified", { count: 2 })
    );
    // The resource name is resolved through the member directory.
    expect(text).toContain("alice@example.com");
    wrapper.unmount();
  });

  it("diffs a version against its predecessor by rule", async () => {
    mocks.listQualityContractRevisions.mockResolvedValue({
      items: [revisionFixture(3, { checkCount: 3 })],
      nextPageToken: "",
    });
    mocks.getQualityContractRevision.mockImplementation(
      ({ version }: { version: number }) =>
        Promise.resolve(
          version === 3
            ? revisionResponse(3, [
                checkFixture("rows", { revision: 2, title: "Rows exist" }),
                checkFixture("fresh", { title: "Data is fresh" }),
                checkFixture("same", { title: "Unchanged rule" }),
              ])
            : revisionResponse(2, [
                checkFixture("rows", { revision: 1, title: "Rows exist" }),
                checkFixture("gone", { title: "Dropped rule" }),
                checkFixture("same", { title: "Unchanged rule" }),
              ])
        )
    );
    const wrapper = await mountDrawer();

    dialogButton(
      label("dataQuality.contract.diffTitle", { version: 3 })
    )?.click();
    await flushPromises();

    expect(mocks.getQualityContractRevision).toHaveBeenCalledTimes(2);
    expect(mocks.getQualityContractRevision).toHaveBeenNthCalledWith(1, {
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      version: 3,
    });
    expect(mocks.getQualityContractRevision).toHaveBeenNthCalledWith(2, {
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      version: 2,
    });

    const text = document.body.textContent ?? "";
    expect(text).toContain(label("dataQuality.contract.diffAdded"));
    expect(text).toContain("Data is fresh");
    expect(text).toContain(label("dataQuality.contract.diffModified"));
    expect(text).toContain("Rows exist");
    expect(text).toContain(label("dataQuality.contract.diffRemoved"));
    expect(text).toContain("Dropped rule");
    // A rule whose recorded revision, title, severity and column are unchanged is
    // not listed under any of the three groups.
    expect(text).not.toContain("Unchanged rule");
    wrapper.unmount();
  });

  it("treats the first version as all new and reads no predecessor", async () => {
    mocks.listQualityContractRevisions.mockResolvedValue({
      items: [revisionFixture(1, { checkCount: 1 })],
      nextPageToken: "",
    });
    mocks.getQualityContractRevision.mockResolvedValue(
      revisionResponse(1, [checkFixture("first", { title: "First promise" })])
    );
    const wrapper = await mountDrawer();

    dialogButton(
      label("dataQuality.contract.diffTitle", { version: 1 })
    )?.click();
    await flushPromises();

    expect(mocks.getQualityContractRevision).toHaveBeenCalledTimes(1);
    expect(mocks.getQualityContractRevision).toHaveBeenCalledWith({
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      version: 1,
    });
    const text = document.body.textContent ?? "";
    expect(text).toContain(label("dataQuality.contract.diffAdded"));
    expect(text).toContain("First promise");
    wrapper.unmount();
  });

  it("says nothing changed when the two snapshots match", async () => {
    mocks.listQualityContractRevisions.mockResolvedValue({
      items: [revisionFixture(2, { checkCount: 1 })],
      nextPageToken: "",
    });
    const checks = [checkFixture("same", { title: "Unchanged rule" })];
    mocks.getQualityContractRevision.mockImplementation(
      ({ version }: { version: number }) =>
        Promise.resolve(revisionResponse(version, checks))
    );
    const wrapper = await mountDrawer();

    dialogButton(
      label("dataQuality.contract.diffTitle", { version: 2 })
    )?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.contract.diffEmpty")
    );
    wrapper.unmount();
  });

  it("shows the empty state before anything is published", async () => {
    mocks.listQualityContractRevisions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    const wrapper = await mountDrawer();

    expect(document.body.textContent).toContain(
      label("dataQuality.contract.historyEmpty")
    );
    wrapper.unmount();
  });

  it("shows a list failure with a retry that re-reads", async () => {
    mocks.listQualityContractRevisions.mockRejectedValue(
      new Error("history down")
    );
    const wrapper = await mountDrawer();

    expect(document.body.textContent).toContain(
      label("dataQuality.contract.loadFailed")
    );
    dialogButton(label("common.retry"))?.click();
    await flushPromises();
    expect(mocks.listQualityContractRevisions).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("walks the cursor to the next page", async () => {
    mocks.listQualityContractRevisions
      .mockResolvedValueOnce({
        items: [revisionFixture(3)],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        items: [revisionFixture(2)],
        nextPageToken: "",
      });
    const wrapper = await mountDrawer();

    dialogButton(label("common.next"))?.click();
    await flushPromises();

    expect(mocks.listQualityContractRevisions).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "page-2" })
    );
    wrapper.unmount();
  });
});
