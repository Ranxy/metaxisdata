import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import QualityAgentDismissBubble from "./QualityAgentDismissBubble.vue";

function mountBubble(content: string) {
  return mount(QualityAgentDismissBubble, {
    props: { content },
    global: { plugins: [i18n] },
  });
}

describe("QualityAgentDismissBubble", () => {
  it("renders the dismissed question instead of raw envelope text", () => {
    const content = JSON.stringify({
      kind: "dismiss_ask",
      ask: "workspaces/ws/qualityAgentAsks/a1",
      questions: [
        { id: "q1", header: "Mail rule", question: "Should mail be required?" },
        { id: "q2", question: "Should the empty tables pause?" },
      ],
    });
    const wrapper = mountBubble(content);
    expect(wrapper.text()).toContain("Ask dismissed");
    expect(wrapper.text()).toContain("Should mail be required?");
    expect(wrapper.text()).toContain("Should the empty tables pause?");
    expect(wrapper.text()).not.toContain(content);
  });

  it("falls back to a legacy plain dismissal", () => {
    const wrapper = mountBubble("Dismiss this request.");
    expect(wrapper.text()).toContain("Ask dismissed");
    expect(wrapper.text()).toContain("Dismiss this request.");
  });
});
