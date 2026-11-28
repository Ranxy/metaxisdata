import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import QualityAgentAnswerBubble from "./QualityAgentAnswerBubble.vue";

function mountBubble(content: string) {
  return mount(QualityAgentAnswerBubble, {
    props: { content },
    global: { plugins: [i18n] },
  });
}

describe("QualityAgentAnswerBubble", () => {
  it("renders the question and the selected answer", () => {
    const wrapper = mountBubble(
      JSON.stringify({
        kind: "answer_to_ask",
        ask: "workspaces/ws/qualityAgentAsks/a1",
        questions: [
          {
            id: "q1",
            header: "Timeout",
            question: "Should the check time out after 10 minutes?",
            options: [{ label: "Raise it" }, { label: "Pause it" }],
          },
        ],
        answers: [{ id: "q1", selected: ["Raise it"], custom: "" }],
      })
    );
    expect(wrapper.text()).toContain(
      "Should the check time out after 10 minutes?"
    );
    expect(wrapper.text()).toContain("Raise it");
    expect(wrapper.text()).not.toContain("Skipped");
  });

  it("renders a skipped question as skipped", () => {
    const wrapper = mountBubble(
      JSON.stringify({
        kind: "answer_to_ask",
        ask: "workspaces/ws/qualityAgentAsks/a1",
        questions: [{ id: "q1", question: "Keep the check?" }],
        answers: [{ id: "q1", selected: [] }],
      })
    );
    expect(wrapper.text()).toContain("Skipped");
  });

  it("falls back to the raw text when the answer cannot be paired", () => {
    const content = JSON.stringify({
      kind: "answer_to_ask",
      ask: "workspaces/ws/qualityAgentAsks/a1",
      questions: [{ id: "q1", question: "Keep the check?" }],
      answers: [],
    });
    const wrapper = mountBubble(content);
    expect(wrapper.text()).toContain(content);
  });

  it("falls back to the raw text for an ordinary message", () => {
    const wrapper = mountBubble("Just a note.");
    expect(wrapper.text()).toContain("Just a note.");
  });
});
