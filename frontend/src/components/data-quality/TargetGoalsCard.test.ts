import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  QualityAgentGoalPhase,
  QualityAgentGoalSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import TargetGoalsCard from "./TargetGoalsCard.vue";

const mocks = vi.hoisted(() => ({
  listQualityAgentGoals: vi.fn(),
  pauseQualityAgentGoal: vi.fn(),
  resumeQualityAgentGoal: vi.fn(),
  completeQualityAgentGoal: vi.fn(),
  clearQualityAgentGoal: vi.fn(),
}));

vi.mock("@/api/data-quality", () => mocks);
vi.mock("@/lib/notify", () => ({
  notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}));

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function goalFixture(phase: QualityAgentGoalPhase) {
  return create(QualityAgentGoalSchema, {
    name: "workspaces/ws1/qualityAgentGoals/g1",
    phase,
    revision: 4,
    roundsStarted: 1,
    maxRounds: 8,
    objective: "keep orders trustworthy",
    blockedCode: phase === QualityAgentGoalPhase.BLOCKED ? "no-work" : "",
    blockedReason:
      phase === QualityAgentGoalPhase.BLOCKED ? "nothing to do" : "",
  });
}

function mountCard() {
  return mount(TargetGoalsCard, {
    props: { targetGuid: "inst;db;;orders", canManage: true },
    global: { plugins: [i18n] },
  });
}

describe("TargetGoalsCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listQualityAgentGoals.mockResolvedValue([
      goalFixture(QualityAgentGoalPhase.BLOCKED),
    ]);
    mocks.resumeQualityAgentGoal.mockResolvedValue(
      goalFixture(QualityAgentGoalPhase.ACTIVE)
    );
    mocks.clearQualityAgentGoal.mockImplementation(async () => {
      mocks.listQualityAgentGoals.mockResolvedValue([]);
    });
  });

  it("resumes a blocked goal with the revision it read", async () => {
    const wrapper = mountCard();
    await flushPromises();

    expect(wrapper.text()).toContain(label("dataQuality.goals.phaseBlocked"));
    expect(wrapper.text()).toContain("no-work");
    const resume = wrapper
      .findAll("button")
      .find((button) => button.text() === label("dataQuality.goals.resume"));
    await resume!.trigger("click");
    await flushPromises();

    expect(mocks.resumeQualityAgentGoal).toHaveBeenCalledWith(
      "workspaces/ws1/qualityAgentGoals/g1",
      4
    );
    expect(wrapper.text()).toContain(label("dataQuality.goals.phaseActive"));
  });

  it("clears a goal only after confirmation", async () => {
    const wrapper = mountCard();
    await flushPromises();

    const clear = wrapper
      .findAll("button")
      .find((button) => button.text() === label("dataQuality.goals.clear"));
    await clear!.trigger("click");
    await flushPromises();
    expect(mocks.clearQualityAgentGoal).not.toHaveBeenCalled();

    const confirm = [
      ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
    ].find(
      (button) =>
        button.textContent?.trim() === label("dataQuality.goals.clearConfirm")
    );
    confirm?.click();
    await flushPromises();

    expect(mocks.clearQualityAgentGoal).toHaveBeenCalledWith(
      "workspaces/ws1/qualityAgentGoals/g1",
      4
    );
    // The list is re-read after the clear, so the removed row disappears.
    expect(mocks.listQualityAgentGoals).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  // A stale revision or a failed action reloads the card so the controls match the
  // runner's state, instead of leaving an unusable row.
  it("reloads after a failed action", async () => {
    mocks.resumeQualityAgentGoal.mockRejectedValueOnce(
      new Error("stale revision")
    );
    const wrapper = mountCard();
    await flushPromises();

    const resume = wrapper
      .findAll("button")
      .find((button) => button.text() === label("dataQuality.goals.resume"));
    await resume!.trigger("click");
    await flushPromises();

    expect(mocks.listQualityAgentGoals).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  // A reader sees the goals and their state but none of the controls.
  it("hides the controls from a reader", async () => {
    const wrapper = mount(TargetGoalsCard, {
      props: { targetGuid: "inst;db;;orders", canManage: false },
      global: { plugins: [i18n] },
    });
    await flushPromises();

    expect(wrapper.text()).toContain(label("dataQuality.goals.phaseBlocked"));
    expect(wrapper.findAll("button")).toHaveLength(0);
    wrapper.unmount();
  });
});
