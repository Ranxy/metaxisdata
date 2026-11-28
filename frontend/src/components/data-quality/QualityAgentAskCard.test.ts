import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  QualityAgentAnswerSchema,
  type QualityAgentAsk,
  QualityAgentAskKind,
  QualityAgentAskSchema,
  QualityAgentAskStatus,
  QualityAgentQuestionOptionSchema,
  QualityAgentQuestionSchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
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
      [
        ask,
        [
          create(QualityAgentAnswerSchema, {
            id: "q1",
            selected: [],
            custom: "Keep it; the sparse batch is expected.",
          }),
        ],
      ],
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
    const ask = askFixture(
      QualityAgentAskKind.QUESTION,
      QualityAgentAskStatus.ANSWERED
    );
    ask.answerMessageSeq = 1n;
    const wrapper = await mountCard(ask);
    expect(wrapper.find("textarea").exists()).toBe(false);
    expect(wrapper.text()).toContain("Answer delivered to the target");
  });

  it("sends the selected option for a choice question", async () => {
    const ask = askFixture();
    ask.questions = [
      create(QualityAgentQuestionSchema, {
        id: "q1",
        question: "Keep the check?",
        options: [
          create(QualityAgentQuestionOptionSchema, { label: "Keep it" }),
          create(QualityAgentQuestionOptionSchema, { label: "Pause it" }),
        ],
      }),
    ];
    const wrapper = await mountCard(ask);
    const keep = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Keep it"));
    expect(keep).toBeDefined();
    await keep?.trigger("click");
    const send = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Send answer");
    await send?.trigger("click");
    expect(wrapper.emitted("reply")).toEqual([
      [
        ask,
        [
          create(QualityAgentAnswerSchema, {
            id: "q1",
            selected: ["Keep it"],
            custom: "",
          }),
        ],
      ],
    ]);
  });

  it("lets a single-select custom answer replace the choice", async () => {
    const ask = askFixture();
    ask.questions = [
      create(QualityAgentQuestionSchema, {
        id: "q1",
        question: "Keep the check?",
        options: [
          create(QualityAgentQuestionOptionSchema, { label: "Keep it" }),
        ],
      }),
    ];
    const wrapper = await mountCard(ask);
    const keep = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Keep it"));
    await keep?.trigger("click");
    const textarea = wrapper.find("textarea");
    await textarea.setValue("Keep it but raise the timeout");
    const send = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Send answer");
    await send?.trigger("click");
    expect(wrapper.emitted("reply")).toEqual([
      [
        ask,
        [
          create(QualityAgentAnswerSchema, {
            id: "q1",
            selected: [],
            custom: "Keep it but raise the timeout",
          }),
        ],
      ],
    ]);
  });

  it("shows the recorded selection on an answered card", async () => {
    const ask = askFixture(
      QualityAgentAskKind.QUESTION,
      QualityAgentAskStatus.ANSWERED
    );
    ask.questions = [
      create(QualityAgentQuestionSchema, {
        id: "q1",
        question: "Keep the check?",
        options: [
          create(QualityAgentQuestionOptionSchema, { label: "Keep it" }),
          create(QualityAgentQuestionOptionSchema, { label: "Pause it" }),
        ],
      }),
    ];
    ask.answers = [
      create(QualityAgentAnswerSchema, {
        id: "q1",
        selected: ["Keep it"],
      }),
    ];
    const wrapper = await mountCard(ask, false);
    const keep = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Keep it"));
    expect(keep?.classes()).toContain("border-primary");
    expect(wrapper.text()).toContain("Keep it");
  });

  it("collects one answer per question in a batch", async () => {
    const ask = askFixture();
    ask.questions = [
      create(QualityAgentQuestionSchema, {
        id: "first",
        question: "First?",
        options: [create(QualityAgentQuestionOptionSchema, { label: "Yes" })],
        requests: ["workspaces/ws/qualityAgentRequests/r1"],
      }),
      create(QualityAgentQuestionSchema, {
        id: "second",
        question: "Second?",
        options: [create(QualityAgentQuestionOptionSchema, { label: "No" })],
        requests: ["workspaces/ws/qualityAgentRequests/r2"],
      }),
    ];
    const wrapper = await mountCard(ask);
    for (const label of ["Yes", "No"]) {
      const option = wrapper
        .findAll("button")
        .find((candidate) => candidate.text().includes(label));
      await option?.trigger("click");
    }
    const send = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Send answer");
    await send?.trigger("click");
    const emitted = wrapper.emitted("reply")?.[0] as [
      QualityAgentAsk,
      { id: string; selected: string[]; custom: string }[],
    ];
    expect(emitted[1].map((answer) => answer.id)).toEqual(["first", "second"]);
    expect(emitted[1][0].selected).toEqual(["Yes"]);
    expect(emitted[1][1].selected).toEqual(["No"]);
  });

  it("skips one question in a batch", async () => {
    const ask = askFixture();
    ask.questions = [
      create(QualityAgentQuestionSchema, {
        id: "first",
        question: "First?",
        options: [create(QualityAgentQuestionOptionSchema, { label: "Yes" })],
        requests: ["workspaces/ws/qualityAgentRequests/r1"],
      }),
      create(QualityAgentQuestionSchema, {
        id: "second",
        question: "Second?",
        options: [create(QualityAgentQuestionOptionSchema, { label: "No" })],
        requests: ["workspaces/ws/qualityAgentRequests/r2"],
      }),
    ];
    const wrapper = await mountCard(ask);
    const yes = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Yes"));
    await yes?.trigger("click");
    const skip = wrapper
      .findAll("button")
      .filter((candidate) => candidate.text() === "Skip this question");
    expect(skip).toHaveLength(2);
    await skip[1]?.trigger("click");
    expect(wrapper.text()).toContain("This question will be skipped");
    const send = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Send answer");
    await send?.trigger("click");
    const emitted = wrapper.emitted("reply")?.[0] as [
      QualityAgentAsk,
      { id: string; selected: string[]; custom: string }[],
    ];
    expect(emitted[1]).toEqual([
      create(QualityAgentAnswerSchema, {
        id: "first",
        selected: ["Yes"],
        custom: "",
      }),
      create(QualityAgentAnswerSchema, {
        id: "second",
        selected: [],
        custom: "",
      }),
    ]);
  });

  it("names the target each question covers", async () => {
    const ask = askFixture();
    ask.targets = [
      create(QualityTargetSchema, {
        metaGuid: "g1",
        path: create(QualityTargetPathSchema, { object: "orders" }),
      }),
    ];
    ask.questions = [
      create(QualityAgentQuestionSchema, {
        id: "first",
        question: "First?",
        requests: ["workspaces/ws/qualityAgentRequests/r1"],
      }),
      create(QualityAgentQuestionSchema, {
        id: "second",
        question: "Second?",
        requests: ["workspaces/ws/qualityAgentRequests/r2"],
      }),
    ];
    const wrapper = await mountCard(ask);
    expect(wrapper.text().match(/Covers orders · 1 request/g)).toHaveLength(2);
  });

  it("shows an agent-withdrawn ask as cancelled", async () => {
    const wrapper = await mountCard(
      askFixture(QualityAgentAskKind.QUESTION, QualityAgentAskStatus.CANCELLED),
      false
    );
    expect(wrapper.text()).toContain("Cancelled");
    expect(wrapper.text()).toContain("The agent withdrew this question.");
    expect(wrapper.text()).not.toContain("Answer delivered to the target");
  });

  it("does not claim an answer for a resolved approval with no recorded answer", async () => {
    const wrapper = await mountCard(
      askFixture(QualityAgentAskKind.APPROVAL, QualityAgentAskStatus.RESOLVED),
      false
    );
    expect(wrapper.text()).not.toContain("Answer delivered to the target");
    expect(wrapper.text()).toContain("Resolved");
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
