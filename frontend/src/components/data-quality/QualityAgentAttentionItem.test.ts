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

function attentionFixture(): QualityAgentAttention {
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
  });
}

async function mountItem(canReply = true) {
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
      attention: attentionFixture(),
      agentName: "workspaces/ws/qualityAgents/agent-1",
      canReply,
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
    expect(wrapper.text()).toContain("Administrators can read only");
    wrapper.unmount();
  });
});
