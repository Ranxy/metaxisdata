import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  QualityAgentSessionMode,
  QualityAgentSessionRole,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityAgentSubagentList from "./QualityAgentSubagentList.vue";

async function mountList() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/agents/:workspace/:agentId",
        name: "QualityAgent",
        component: { template: "<div />" },
      },
      {
        path: "/agents/:workspace/:agentId/sessions/:sessionId",
        name: "QualityAgentSession",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/agents/ws/agent-1");
  await router.isReady();
  const sessions = [
    create(QualityAgentSessionSchema, {
      name: "workspaces/ws/qualityAgentSessions/s1",
      role: QualityAgentSessionRole.SUBAGENT,
      subagentName: "orders",
      state: QualityAgentSessionState.ENABLED,
      mode: QualityAgentSessionMode.MONITOR,
      target: create(QualityTargetSchema, { metaGuid: "inst;db;;orders" }),
    }),
  ];
  const wrapper = mount(QualityAgentSubagentList, {
    props: {
      sessions,
      loading: false,
      error: "",
      sessionRoute: () => ({
        name: "QualityAgentSession",
        params: { workspace: "ws", agentId: "agent-1", sessionId: "s1" },
      }),
    },
    global: { plugins: [router, i18n] },
  });
  await wrapper.vm.$nextTick();
  return wrapper;
}

describe("QualityAgentSubagentList", () => {
  it("names the subagent and links its read-only page", async () => {
    const wrapper = await mountList();
    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("Monitor");
    const link = wrapper.find("a");
    expect(link.attributes("href")).toBe("/agents/ws/agent-1/sessions/s1");
  });
});
