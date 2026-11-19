import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  QualityAgentMessageOrigin,
  QualityAgentMessageRole,
  QualityAgentSessionMessageSchema,
  QualityToolCallSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { groupMessagesByWake } from "@/utils/qualityAgentSession";
import QualityAgentTranscript from "./QualityAgentTranscript.vue";

const START = new Date("2026-11-07T10:00:00Z");
let seq = 0;

function message(
  overrides: Parameters<
    typeof create<typeof QualityAgentSessionMessageSchema>
  >[1]
) {
  seq += 1;
  return create(QualityAgentSessionMessageSchema, {
    name: `workspaces/ws/qualityAgentSessions/1/messages/${seq}`,
    seq: BigInt(seq),
    wake: 1,
    role: QualityAgentMessageRole.ASSISTANT,
    origin: QualityAgentMessageOrigin.MODEL,
    createTime: timestampFromDate(START),
    ...overrides,
  });
}

function mountTranscript(messages: ReturnType<typeof message>[]) {
  return mount(QualityAgentTranscript, {
    props: {
      groups: groupMessagesByWake(messages),
      sessionName: "workspaces/ws/qualityAgentSessions/1",
    },
    global: {
      plugins: [i18n],
      stubs: {
        AsyncMarkdown: {
          props: ["content"],
          template: '<div class="markdown">{{ content }}</div>',
        },
        RouterLink: { template: "<a><slot /></a>" },
      },
    },
  });
}

describe("QualityAgentTranscript", () => {
  beforeEach(() => {
    i18n.global.locale.value = "en-US";
    seq = 0;
  });

  it("renders the answer, the tool call with its arguments and the paired result", async () => {
    const call = create(QualityToolCallSchema, {
      id: "call-1",
      name: "get_target_summary",
      arguments: '{"target":"inst1;db1;public;orders"}',
    });
    const assistant = message({
      content: "Checking the target.",
      toolCalls: [call],
    });
    const result = message({
      role: QualityAgentMessageRole.TOOL,
      origin: QualityAgentMessageOrigin.MODEL,
      toolCallId: "call-1",
      toolName: "get_target_summary",
      content: '{"ruleCount":2}',
    });
    const wrapper = mountTranscript([assistant, result]);

    expect(wrapper.text()).toContain("get_target_summary");
    expect(wrapper.text()).toContain("target: inst1;db1;public;orders");

    const trigger = wrapper
      .findAll("button")
      .find((button) => button.text().includes("get_target_summary"));
    expect(trigger).toBeDefined();
    await trigger?.trigger("click");
    expect(wrapper.text()).toContain('{"ruleCount":2}');
    wrapper.unmount();
  });

  it("folds the model reasoning behind a Thinking trigger", async () => {
    const assistant = message({
      content: "The table is healthy.",
      reasoning: "check the row count before answering",
    });
    const wrapper = mountTranscript([assistant]);

    expect(wrapper.text()).toContain("Thinking");
    expect(wrapper.text()).not.toContain("check the row count");

    const trigger = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Thinking"));
    await trigger?.trigger("click");
    expect(wrapper.text()).toContain("check the row count before answering");
    wrapper.unmount();
  });

  it("hides the Thinking block when the provider returned no reasoning", () => {
    const wrapper = mountTranscript([
      message({ content: "No reasoning here." }),
    ]);
    expect(wrapper.text()).not.toContain("Thinking");
    wrapper.unmount();
  });

  it("keeps runner notes folded and shows a wake summary", async () => {
    const engine = message({
      role: QualityAgentMessageRole.ENGINE,
      origin: QualityAgentMessageOrigin.RUNNER,
      content: "wake finished: reviewed the failure",
    });
    const wrapper = mountTranscript([engine]);
    expect(wrapper.text()).toContain("reviewed the failure");
    expect(wrapper.text()).toContain("Engine");
    wrapper.unmount();
  });

  it("shows a tool result whose call row is missing", () => {
    const orphan = message({
      role: QualityAgentMessageRole.TOOL,
      origin: QualityAgentMessageOrigin.RUNNER,
      toolCallId: "lost-call",
      toolName: "get_memory",
      content: "no fact",
    });
    const wrapper = mountTranscript([orphan]);
    expect(wrapper.text()).toContain("get_memory");
    wrapper.unmount();
  });
});
