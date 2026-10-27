import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ContractHistoryDrawerComponent from "@/components/data-quality/ContractHistoryDrawer.vue";
import ContractPublishDialog from "@/components/data-quality/ContractPublishDialog.vue";
import ContractSettingsDialog from "@/components/data-quality/ContractSettingsDialog.vue";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityContract,
  QualityContractSchema,
  QualityFilterOperator,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { buildFilter } from "@/utils/dataQualityForm";
import { formatRelativeTime } from "@/utils/datetime";
import TargetContractCard from "./TargetContractCard.vue";

const mocks = vi.hoisted(() => ({
  listAllUsers: vi.fn(),
  listAllMetadata: vi.fn(),
}));

vi.mock("@/api/user", () => ({
  listAllUsers: mocks.listAllUsers,
}));

vi.mock("@/api/database", () => ({
  listAllMetadata: mocks.listAllMetadata,
}));

const TABLE = "inst1;db1;public;orders";
const PUBLISHED_AT = timestampFromDate(new Date(Date.now() - 60 * 60 * 1000));

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function contractFixture(
  options: {
    version?: number;
    changes?: boolean;
    added?: number;
    modified?: number;
    removed?: number;
    withFilter?: boolean;
  } = {}
): QualityContract {
  return create(QualityContractSchema, {
    publishedVersion: options.version ?? 2,
    publishedAt: PUBLISHED_AT,
    publishedBy: "users/alice",
    hasUnpublishedChanges: options.changes ?? true,
    checkCount: 3,
    changeSummary: {
      added: options.added ?? 1,
      modified: options.modified ?? 2,
      removed: options.removed ?? 0,
    },
    settings: options.withFilter
      ? {
          filter: buildFilter([
            {
              column: "region",
              op: QualityFilterOperator.EQ,
              values: "EU",
            },
          ]),
        }
      : undefined,
  });
}

function buttonByText(
  wrapper: ReturnType<typeof mount>,
  text: string
): ReturnType<ReturnType<typeof mount>["find"]> {
  const button = wrapper
    .findAll("button")
    .find((candidate) => candidate.text().trim() === text);
  expect(button, `no button labelled ${text}`).toBeTruthy();
  return button as ReturnType<ReturnType<typeof mount>["find"]>;
}

async function mountCard(props: Record<string, unknown> = {}) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: ["metaxisdata.users.list"],
  });
  const wrapper = mount(TargetContractCard, {
    attachTo: document.body,
    props: {
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      contract: contractFixture(),
      loading: false,
      error: "",
      canPublish: true,
      canUpdate: true,
      ...props,
    },
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("TargetContractCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.listAllUsers.mockResolvedValue([
      create(UserSchema, { name: "users/alice", email: "alice@example.com" }),
    ]);
    mocks.listAllMetadata.mockResolvedValue([]);
  });

  it("shows the published version, its publisher and the drift it carries", async () => {
    const wrapper = await mountCard({
      contract: contractFixture({ version: 2, withFilter: true }),
    });

    const text = wrapper.text();
    expect(text).toContain(
      label("dataQuality.contract.statePublished", { version: 2 })
    );
    expect(text).toContain(
      label("dataQuality.contract.publishedAt", {
        time: formatRelativeTime(PUBLISHED_AT, "en-US"),
      })
    );
    expect(text).toContain("alice@example.com");
    expect(text).toContain(
      label("dataQuality.contract.unpublishedChanges", { count: 3 })
    );
    expect(text).toContain(label("dataQuality.contract.filterApplies"));
    wrapper.unmount();
  });

  it("invites the first publish when nothing has been published", async () => {
    const wrapper = await mountCard({
      contract: contractFixture({ version: 0, changes: false }),
    });

    const text = wrapper.text();
    expect(text).toContain(label("dataQuality.contract.stateNeverPublished"));
    expect(text).toContain(label("dataQuality.contract.neverPublishedHint"));
    // Nothing to publish and nothing to look back at.
    const publish = wrapper
      .findAll("button")
      .find(
        (candidate) =>
          candidate.text().trim() === label("dataQuality.contract.publish")
      );
    expect(publish?.attributes("disabled")).toBeDefined();
    expect(text).not.toContain(label("dataQuality.contract.history"));
    wrapper.unmount();
  });

  it("disables publish without the permission and says why", async () => {
    const wrapper = await mountCard({ canPublish: false });

    const publish = wrapper
      .findAll("button")
      .find(
        (candidate) =>
          candidate.text().trim() === label("dataQuality.contract.publish")
      );
    expect(publish?.attributes("title")).toBe(
      label("dataQuality.contract.noPermission")
    );
    expect(publish?.attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("disables publish when the live rules already match", async () => {
    const wrapper = await mountCard({
      contract: contractFixture({ changes: false }),
    });

    const publish = wrapper
      .findAll("button")
      .find(
        (candidate) =>
          candidate.text().trim() === label("dataQuality.contract.publish")
      );
    expect(publish?.attributes("title")).toBe(
      label("dataQuality.contract.publishNoChanges")
    );
    expect(publish?.attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("opens the publish dialog and reports the published contract", async () => {
    const wrapper = await mountCard();

    buttonByText(wrapper, label("dataQuality.contract.publish")).trigger(
      "click"
    );
    await flushPromises();
    const dialog = wrapper.findComponent(ContractPublishDialog);
    expect(dialog.props("open")).toBe(true);

    const published = contractFixture({ version: 3, changes: false });
    dialog.vm.$emit("published", published);
    await flushPromises();
    expect(wrapper.emitted("published")).toEqual([[published]]);
    wrapper.unmount();
  });

  it("opens the settings dialog and reports the updated contract", async () => {
    const wrapper = await mountCard();

    buttonByText(wrapper, label("dataQuality.contract.settings")).trigger(
      "click"
    );
    await flushPromises();
    const dialog = wrapper.findComponent(ContractSettingsDialog);
    expect(dialog.props("open")).toBe(true);

    const updated = contractFixture({ changes: false });
    dialog.vm.$emit("updated", updated);
    await flushPromises();
    expect(wrapper.emitted("updated")).toEqual([[updated]]);
    wrapper.unmount();
  });

  it("opens the history drawer for a target that has versions", async () => {
    const wrapper = await mountCard();

    buttonByText(wrapper, label("dataQuality.contract.history")).trigger(
      "click"
    );
    await flushPromises();
    const drawer = wrapper.findComponent(ContractHistoryDrawerComponent);
    expect(drawer.props("open")).toBe(true);
    wrapper.unmount();
  });

  it("shows a read failure with a retry the page owns", async () => {
    const wrapper = await mountCard({ contract: undefined, error: "down" });

    expect(wrapper.text()).toContain(label("dataQuality.contract.loadFailed"));
    buttonByText(wrapper, label("common.retry")).trigger("click");
    await flushPromises();
    expect(wrapper.emitted("retry")).toHaveLength(1);
    // A strip without a contract offers no entry points.
    expect(wrapper.findComponent(ContractPublishDialog).exists()).toBe(false);
    wrapper.unmount();
  });
});
