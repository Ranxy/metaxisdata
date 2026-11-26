import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  QualityAgentCaseActionKind,
  QualityAgentCaseActionSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityAgentCaseActivity from "./QualityAgentCaseActivity.vue";

const listQualityAgentCaseActions = vi.fn();

vi.mock("@/api/data-quality", () => ({
  listQualityAgentCaseActions: (...args: unknown[]) =>
    listQualityAgentCaseActions(...args),
}));

function action(overrides: Partial<Parameters<typeof create>[1]> = {}) {
  return create(QualityAgentCaseActionSchema, {
    name: "workspaces/ws/qualityAgentCaseActions/1",
    agent: "workspaces/ws/qualityAgents/a1",
    case: "workspaces/ws/qualityAgentCases/c9",
    check: "qualityRules/460",
    action: QualityAgentCaseActionKind.REVISE,
    field: "range.min",
    fromRevision: 3,
    toRevision: 4,
    class: "CONFIG_VALUE_INVALID",
    reason: "CONFIG_VALUE_INVALID",
    createTime: timestampFromDate(new Date("2026-11-20T10:00:00Z")),
    ...overrides,
  } as never);
}

function mountActivity(props: { caseName?: string } = {}) {
  return mount(QualityAgentCaseActivity, {
    props: { agentName: "workspaces/ws/qualityAgents/a1", ...props },
    global: { plugins: [i18n] },
  });
}

describe("QualityAgentCaseActivity", () => {
  beforeEach(() => {
    listQualityAgentCaseActions.mockReset();
  });

  it("lists a write with its field, revision move and the case filter it was given", async () => {
    listQualityAgentCaseActions.mockResolvedValue({
      actions: [action()],
      nextPageToken: "",
    });

    const wrapper = mountActivity({
      caseName: "workspaces/ws/qualityAgentCases/c9",
    });
    await flushPromises();

    expect(listQualityAgentCaseActions).toHaveBeenCalledWith(
      "workspaces/ws/qualityAgents/a1",
      expect.objectContaining({
        caseName: "workspaces/ws/qualityAgentCases/c9",
      })
    );
    const text = wrapper.text();
    expect(text).toContain("range.min");
    expect(text).toContain("revision 3 → 4");
    expect(text).toContain("CONFIG_VALUE_INVALID");
    expect(wrapper.text()).toContain("460");
  });

  it("says so when the agent has not written anything yet", async () => {
    listQualityAgentCaseActions.mockResolvedValue({
      actions: [],
      nextPageToken: "",
    });

    const wrapper = mountActivity();
    await flushPromises();

    expect(wrapper.text()).toContain("has not written to any check");
  });

  it("walks to the next page and appends instead of replacing", async () => {
    listQualityAgentCaseActions
      .mockResolvedValueOnce({
        actions: [action({ name: "workspaces/ws/qualityAgentCaseActions/2" })],
        nextPageToken: "page-2",
      })
      .mockResolvedValueOnce({
        actions: [
          action({
            name: "workspaces/ws/qualityAgentCaseActions/1",
            action: QualityAgentCaseActionKind.PROPOSE,
            field: "range.max",
            fromRevision: 4,
            toRevision: 4,
          }),
        ],
        nextPageToken: "",
      });

    const wrapper = mountActivity();
    await flushPromises();
    const button = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Load more"));
    expect(button).toBeTruthy();
    await button?.trigger("click");
    await flushPromises();

    expect(listQualityAgentCaseActions).toHaveBeenLastCalledWith(
      "workspaces/ws/qualityAgents/a1",
      expect.objectContaining({ pageToken: "page-2" })
    );
    expect(wrapper.text()).toContain("range.min");
    expect(wrapper.text()).toContain("range.max");
    expect(wrapper.text()).not.toContain("Load more");
  });
});
