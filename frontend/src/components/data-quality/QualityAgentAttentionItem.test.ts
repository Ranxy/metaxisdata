import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  type QualityAgentAttention,
  QualityAgentAttentionKind,
  QualityAgentAttentionSchema,
  QualityAgentAttentionStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import QualityAgentAttentionItem from "./QualityAgentAttentionItem.vue";

const mocks = vi.hoisted(() => ({
  replyQualityAgentAttention: vi.fn(),
  dismissQualityAgentAttention: vi.fn(),
  notifySuccess: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  replyQualityAgentAttention: mocks.replyQualityAgentAttention,
  dismissQualityAgentAttention: mocks.dismissQualityAgentAttention,
}));

vi.mock("@/lib/notify", () => ({
  notify: {
    success: mocks.notifySuccess,
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
}));

const ATTENTION = "workspaces/ws/qualityAgentAttentions/a1";
const SESSION = "workspaces/ws/qualityAgentSessions/s1";

function attentionFixture(targetless = false): QualityAgentAttention {
  return create(QualityAgentAttentionSchema, {
    name: ATTENTION,
    kind: QualityAgentAttentionKind.PROPOSAL_PENDING,
    status: QualityAgentAttentionStatus.OPEN,
    session: SESSION,
    title: "有检查草案等待审核",
    question: "该目标有 5 条草案未被自动采纳，需要你决定。",
    pendingDraftTitles: [
      "Price must be non-negative",
      "Discount must be a fraction",
    ],
    createTime: timestampFromDate(new Date("2026-11-19T06:00:00Z")),
    ...(targetless
      ? {}
      : {
          target: create(QualityTargetSchema, {
            metaGuid: "inst-1;sales;public;orders",
            metaType: MetaType.TABLE,
            path: create(QualityTargetPathSchema, {
              instance: "inst-1",
              database: "sales",
              schema: "public",
              object: "orders",
            }),
          }),
        }),
  });
}

async function mountItem(
  canReply = true,
  canApprove = false,
  approveBusy = false,
  targetless = false
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/agents/:workspace/:agentId",
        name: "QualityAgent",
        component: { template: "<div />" },
      },
      {
        path: "/agents/:workspace/:agentId/sessions",
        name: "QualityAgentSessions",
        component: { template: "<div />" },
      },
      {
        path: "/agents/:workspace/:agentId/sessions/:sessionId",
        name: "QualityAgentSession",
        component: { template: "<div />" },
      },
      {
        path: "/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/agents/ws/agent-1");
  await router.isReady();
  const wrapper = mount(QualityAgentAttentionItem, {
    props: {
      attention: attentionFixture(targetless),
      agentName: "workspaces/ws/qualityAgents/agent-1",
      canReply,
      canApprove,
      approveBusy,
    },
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return wrapper;
}

describe("QualityAgentAttentionItem", () => {
  beforeEach(() => {
    i18n.global.locale.value = "en-US";
    mocks.replyQualityAgentAttention.mockReset();
    mocks.dismissQualityAgentAttention.mockReset();
    mocks.replyQualityAgentAttention.mockResolvedValue(undefined);
  });

  it("shows the target, expands, lists drafts and replies inline", async () => {
    const wrapper = await mountItem();

    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("inst-1 · sales");
    expect(wrapper.text()).toContain("Check drafts");
    expect(wrapper.find("textarea").exists()).toBe(false);

    const trigger = wrapper.findAll("button")[0];
    await trigger.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Price must be non-negative");
    expect(wrapper.text()).toContain("Discount must be a fraction");

    await wrapper.find("textarea").setValue("请采纳这些草案");
    const send = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Send and wake"));
    await send?.trigger("click");
    await flushPromises();

    expect(mocks.replyQualityAgentAttention).toHaveBeenCalledWith(
      ATTENTION,
      "请采纳这些草案",
      expect.any(Object)
    );
    expect(wrapper.emitted("resolved")?.[0]).toEqual([ATTENTION]);
    wrapper.unmount();
  });

  it("keeps an administrator read-only", async () => {
    const wrapper = await mountItem(false);
    const trigger = wrapper.findAll("button")[0];
    await trigger.trigger("click");
    await flushPromises();
    expect(wrapper.find("textarea").exists()).toBe(false);
    expect(wrapper.text()).toContain(
      i18n.global.t("qualityAgent.attentionReadOnly")
    );
    wrapper.unmount();
  });

  it("emits approve for a pending proposal only when the viewer can approve", async () => {
    const approveLabel = i18n.global.t("qualityAgent.attentionApprovePending");

    const readOnly = await mountItem(true, false);
    await readOnly.findAll("button")[0].trigger("click");
    await flushPromises();
    expect(
      readOnly
        .findAll("button")
        .some((button) => button.text().includes(approveLabel))
    ).toBe(false);
    readOnly.unmount();

    const approvable = await mountItem(true, true);
    await approvable.findAll("button")[0].trigger("click");
    await flushPromises();
    const approve = approvable
      .findAll("button")
      .find((button) => button.text().includes(approveLabel));
    expect(approve).toBeDefined();
    // The hint says the write is the viewer's decision, not the agent's policy.
    expect(approvable.text()).toContain(
      i18n.global.t("qualityAgent.attentionApproveHint")
    );

    await approve?.trigger("click");
    expect(approvable.emitted("approve")).toEqual([
      ["inst-1;sales;public;orders"],
    ]);
    // Approval goes through the parent's create path, not the reply path.
    expect(mocks.replyQualityAgentAttention).not.toHaveBeenCalled();
    approvable.unmount();

    // A parent approval in flight disables the action instead of starting a second one.
    const busy = await mountItem(true, true, true);
    await busy.findAll("button")[0].trigger("click");
    await flushPromises();
    const busyApprove = busy
      .findAll("button")
      .find((button) => button.text().includes(approveLabel));
    expect(busyApprove?.attributes("disabled")).toBeDefined();
    await busyApprove?.trigger("click");
    expect(busy.emitted("approve")).toBeUndefined();
    busy.unmount();
  });

  it("does not offer a whole-agent approval for a targetless attention", async () => {
    const wrapper = await mountItem(true, true, false, true);
    await wrapper.findAll("button")[0].trigger("click");
    await flushPromises();
    expect(wrapper.text()).not.toContain(
      i18n.global.t("qualityAgent.attentionApprovePending")
    );
    expect(wrapper.emitted("approve")).toBeUndefined();
    wrapper.unmount();
  });

  it("offers approval to an administrator who cannot reply", async () => {
    const wrapper = await mountItem(false, true);
    await wrapper.findAll("button")[0].trigger("click");
    await flushPromises();
    expect(wrapper.find("textarea").exists()).toBe(false);
    expect(wrapper.text()).toContain(
      i18n.global.t("qualityAgent.attentionApprovePending")
    );
    wrapper.unmount();
  });
});
