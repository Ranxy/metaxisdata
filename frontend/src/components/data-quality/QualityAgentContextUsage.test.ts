import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  type QualityAgentSessionContextUsage,
  QualityAgentSessionContextUsageSchema,
  QualityAgentSessionMode,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityAgentContextUsage from "./QualityAgentContextUsage.vue";

function usage(
  overrides: MessageInitShape<typeof QualityAgentSessionContextUsageSchema> = {}
): QualityAgentSessionContextUsage {
  return create(QualityAgentSessionContextUsageSchema, {
    usedTokens: 1000,
    contextWindow: 4000,
    maxOutputTokens: 1000,
    compactionTriggerTokens: 3000,
    model: "test-model",
    wake: 2,
    turn: 1,
    measuredAt: timestampFromDate(new Date("2026-11-21T10:00:00Z")),
    estimated: false,
    ...overrides,
  });
}

function mountUsage(props: {
  usage?: QualityAgentSessionContextUsage;
  agentModel?: string;
  agentModelKnown?: boolean;
  sessionMode?: QualityAgentSessionMode;
}) {
  return mount(QualityAgentContextUsage, {
    props,
    global: { plugins: [i18n] },
  });
}

describe("QualityAgentContextUsage", () => {
  beforeEach(() => {
    i18n.global.locale.value = "en-US";
  });

  it("renders the used/window reading, its share and the compaction point", () => {
    const wrapper = mountUsage({
      usage: usage(),
      agentModel: "test-model",
      agentModelKnown: true,
    });
    expect(wrapper.text()).toContain("1,000 / 4,000");
    expect(wrapper.text()).toContain("25%");
    expect(wrapper.text()).toContain("compacts near 3,000");
    expect(wrapper.text()).toContain("provider-reported");
    expect(wrapper.text()).toContain("test-model");
    expect(
      wrapper.find('[role="progressbar"]').attributes("aria-valuemax")
    ).toBe("4000");
  });

  it("distinguishes an estimate from a provider count", () => {
    const wrapper = mountUsage({
      usage: usage({ estimated: true }),
      agentModel: "test-model",
      agentModelKnown: true,
    });
    expect(wrapper.text()).toContain("estimated");
    expect(wrapper.text()).not.toContain("provider-reported");
  });

  it("turns the bar warning at the trigger and critical at the window", () => {
    expect(
      mountUsage({ usage: usage({ usedTokens: 3000 }) })
        .find(".bg-warning")
        .exists()
    ).toBe(true);
    expect(
      mountUsage({ usage: usage({ usedTokens: 4000 }) })
        .find(".bg-destructive")
        .exists()
    ).toBe(true);
    expect(mountUsage({ usage: usage() }).find(".bg-info").exists()).toBe(true);
  });

  it("floors the percentage so a full bar is always critical", () => {
    const almostFull = mountUsage({
      usage: usage({ usedTokens: 3999, compactionTriggerTokens: 4000 }),
    });
    expect(almostFull.text()).toContain("99%");
    const full = mountUsage({ usage: usage({ usedTokens: 4000 }) });
    expect(full.text()).toContain("100%");
  });

  it("keeps ARIA inside the window even when the sample overflows", () => {
    const wrapper = mountUsage({
      usage: usage({ usedTokens: 5000, contextWindow: 4000 }),
    });
    const bar = wrapper.find('[role="progressbar"]');
    expect(bar.attributes("aria-valuenow")).toBe("4000");
    expect(bar.attributes("aria-valuetext")).toContain(
      "5,000 / 4,000 tokens (100%)"
    );
  });

  it("explains a missing measurement instead of drawing a zero bar", () => {
    expect(
      mountUsage({ agentModel: "test-model", agentModelKnown: true }).text()
    ).toContain("No model input measured yet.");
    expect(mountUsage({ agentModelKnown: true }).text()).toContain(
      "No model is configured"
    );
    expect(
      mountUsage({
        agentModelKnown: true,
        sessionMode: QualityAgentSessionMode.TRIAGE,
      }).text()
    ).toContain("this triage session waits for one");
    expect(mountUsage({}).text()).toContain(
      "Agent model information is unavailable."
    );
    expect(mountUsage({}).find('[role="progressbar"]').exists()).toBe(false);
  });

  it("says when the sample predates the agent's current model", () => {
    const wrapper = mountUsage({
      usage: usage({ model: "old-model" }),
      agentModel: "new-model",
      agentModelKnown: true,
    });
    expect(wrapper.text()).toContain("The agent model changed to new-model");
  });

  it("says when the agent no longer has a model", () => {
    const wrapper = mountUsage({
      usage: usage({ model: "old-model" }),
      agentModel: "",
      agentModelKnown: true,
    });
    expect(wrapper.text()).toContain("The agent has no model configured now");
  });

  it("does not claim a compaction point the model pair does not have", () => {
    const wrapper = mountUsage({
      usage: usage({ compactionTriggerTokens: 0 }),
      agentModel: "test-model",
      agentModelKnown: true,
    });
    expect(wrapper.text()).toContain("this model pair cannot compact");
    expect(wrapper.text()).not.toContain("compacts near");
  });
});
