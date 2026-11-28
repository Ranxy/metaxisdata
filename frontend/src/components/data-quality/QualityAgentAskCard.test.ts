import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  type QualityAgentAsk,
  QualityAgentAskKind,
  QualityAgentAskSchema,
  QualityAgentAskStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityAgentAskCard from "./QualityAgentAskCard.vue";

vi.mock("@/components/common/AsyncMarkdown", () => ({
  AsyncMarkdown: {
    props: ["content"],
    template: `<div class="markdown">{{ content }}</div>`,
  },
}));

function askFixture(
  kind = QualityAgentAskKind.QUESTION,
  status = QualityAgentAskStatus.OPEN
): QualityAgentAsk {
  return create(QualityAgentAskSchema, {
    name: "workspaces/ws/qualityAgentAsks/a1",
    kind,
    status,
    title: "A decision is needed",
    question: "Keep the check?",
  });
}

async function mountCard(
  ask: QualityAgentAsk,
  canReply = true,
  canApprove = false
) {
  const wrapper = mount(QualityAgentAskCard, {
    props: { ask, canReply, canApprove, busy: false },
    global: { plugins: [i18n] },
  });
  await wrapper.vm.$nextTick();
  return wrapper;
}

describe("QualityAgentAskCard", () => {
  it("sends the typed answer for the ask", async () => {
    const ask = askFixture();
    const wrapper = await mountCard(ask);
    const textarea = wrapper.find("textarea");
    expect(textarea.exists()).toBe(true);
    await textarea.setValue("Keep it; the sparse batch is expected.");
    const buttons = wrapper.findAll("button");
    await buttons[buttons.length - 1]?.trigger("click");
    expect(wrapper.emitted("reply")).toEqual([
      [ask, "Keep it; the sparse batch is expected."],
    ]);
    expect((textarea.element as HTMLTextAreaElement).value).toBe("");
  });

  it("emits a dismissal through the same owner channel", async () => {
    const ask = askFixture();
    const wrapper = await mountCard(ask);
    const button = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Dismiss"));
    expect(button).toBeDefined();
    await button?.trigger("click");
    expect(wrapper.emitted("dismiss")).toEqual([[ask]]);
  });

  it("offers approval only for an open approval ask", async () => {
    const approval = await mountCard(
      askFixture(QualityAgentAskKind.APPROVAL),
      true,
      true
    );
    const button = approval
      .findAll("button")
      .find((candidate) => candidate.text().includes("Approve"));
    expect(button).toBeDefined();
    await button?.trigger("click");
    expect(approval.emitted("approve")).toEqual([
      [askFixture(QualityAgentAskKind.APPROVAL)],
    ]);

    const question = await mountCard(
      askFixture(QualityAgentAskKind.QUESTION),
      true,
      true
    );
    expect(
      question
        .findAll("button")
        .some((candidate) => candidate.text().includes("Approve"))
    ).toBe(false);
  });

  it("marks an answered ask as waiting for the lead, without a composer", async () => {
    const wrapper = await mountCard(
      askFixture(QualityAgentAskKind.QUESTION, QualityAgentAskStatus.ANSWERED)
    );
    expect(wrapper.find("textarea").exists()).toBe(false);
    expect(wrapper.text()).toContain("routing your reply");
  });

  it("clamps a long question and expands it on request", async () => {
    const long = askFixture();
    long.question = "A very long question. ".repeat(40);
    const wrapper = await mountCard(long);
    expect(wrapper.find(".max-h-40").exists()).toBe(true);

    const toggle = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Show the full question"));
    expect(toggle).toBeDefined();
    await toggle?.trigger("click");
    expect(wrapper.find(".max-h-40").exists()).toBe(false);
    expect(wrapper.text()).toContain("Show less");
  });
});
