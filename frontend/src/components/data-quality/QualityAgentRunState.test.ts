import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  type QualityAgentSession,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityAgentRunState from "./QualityAgentRunState.vue";

const START = new Date("2026-11-07T10:00:00Z");

function sessionFixture(
  overrides: MessageInitShape<typeof QualityAgentSessionSchema> = {}
): QualityAgentSession {
  return create(QualityAgentSessionSchema, {
    name: "workspaces/ws/qualityAgentSessions/session-1",
    state: QualityAgentSessionState.BLOCKED,
    running: false,
    wakeCount: 3,
    restartCount: 1,
    lastActivityTime: timestampFromDate(START),
    ...overrides,
  });
}

function mountRunState(
  overrides: MessageInitShape<typeof QualityAgentSessionSchema> = {},
  props: Partial<{
    currentTurn: number;
    pollError: string;
    stale: boolean;
  }> = {}
) {
  return mount(QualityAgentRunState, {
    props: {
      session: sessionFixture(overrides),
      heartbeat: { running: false, stale: props.stale ?? false },
      statusLabel: "Blocked; waiting for a person.",
      nextWakeLabel: "-",
      lastActivityLabel: "23 hours ago",
      currentTurn: props.currentTurn,
      pollError: props.pollError ?? "",
    },
    global: {
      plugins: [i18n],
    },
  });
}

describe("QualityAgentRunState", () => {
  beforeEach(() => {
    i18n.global.locale.value = "en-US";
  });

  it("shows the state, its counters and the blocked code", () => {
    const wrapper = mountRunState({
      blockedCode: "DRAFT_CHECKS_EMPTY_QUEUE",
      blockedReason: "Every drafting call came back empty.",
    });

    expect(wrapper.text()).toContain("Blocked; waiting for a person.");
    expect(wrapper.text()).toContain("DRAFT_CHECKS_EMPTY_QUEUE");
    expect(wrapper.text()).toContain("Every drafting call came back empty.");
    expect(wrapper.text()).toContain("23 hours ago");
  });

  it("clamps a long blocked reason and expands it", async () => {
    const wrapper = mountRunState({
      blockedReason: "A very long model reason about the blocker.",
    });

    const showMore = wrapper
      .findAll("button")
      .find((button) => button.text() === "Show more");
    expect(showMore).toBeDefined();
    expect(wrapper.html()).toContain("line-clamp-4");

    await showMore?.trigger("click");

    expect(wrapper.text()).toContain("Show less");
    expect(wrapper.html()).not.toContain("line-clamp-4");
  });

  it("shows the model turn while a wake is in flight and surfaces a poll failure", () => {
    const wrapper = mountRunState(
      {},
      { currentTurn: 4, pollError: "The poll failed.", stale: true }
    );

    expect(wrapper.text()).toContain("Turn 4");
    expect(wrapper.text()).toContain("The poll failed.");
    expect(wrapper.text()).toContain(
      "The lease is still live but the transcript has not grown for ten minutes"
    );
  });
});
