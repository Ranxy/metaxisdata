import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  ConfirmQualityMemoryRequest_Decision,
  QualityInvestigationEvidenceSchema,
  QualityInvestigationHypothesisSchema,
  QualityInvestigationSchema,
  QualityInvestigationTrigger,
  QualityInvestigationVerdict,
  QualityMemoryKind,
  QualityMemorySchema,
  QualityMemoryStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityKnowledgeCard from "./QualityKnowledgeCard.vue";

const mocks = vi.hoisted(() => ({
  listQualityInvestigations: vi.fn(),
  listQualityMemories: vi.fn(),
  confirmQualityMemory: vi.fn(),
}));

vi.mock("@/api/data-quality", () => mocks);
vi.mock("@/lib/notify", () => ({
  notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}));

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function investigationFixture() {
  return create(QualityInvestigationSchema, {
    name: "workspaces/ws1/qualityInvestigations/i1",
    trigger: QualityInvestigationTrigger.EXECUTION,
    triggerReference: "qualityExecutions/7",
    verdict: QualityInvestigationVerdict.ROOT_CAUSE,
    summary: "the upstream batch was short",
    evidence: [
      create(QualityInvestigationEvidenceSchema, {
        kind: "EXECUTION",
        reference: "qualityExecutions/7",
      }),
    ],
    hypotheses: [
      create(QualityInvestigationHypothesisSchema, {
        statement: "upstream delivered fewer rows",
        status: "CONFIRMED",
      }),
    ],
  });
}

function memoryFixture(
  status: QualityMemoryStatus = QualityMemoryStatus.PROPOSED
) {
  return create(QualityMemorySchema, {
    name: "workspaces/ws1/qualityMemories/m1",
    kind: QualityMemoryKind.DECISION,
    statement: "the sparsity is deliberate",
    status,
  });
}

function mountCard(canConfirm = true) {
  return mount(QualityKnowledgeCard, {
    props: { targetGuid: "inst;db;public;orders", canConfirm },
    global: { plugins: [i18n] },
  });
}

describe("QualityKnowledgeCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listQualityInvestigations.mockResolvedValue([investigationFixture()]);
    mocks.listQualityMemories.mockResolvedValue([memoryFixture()]);
    mocks.confirmQualityMemory.mockResolvedValue(
      memoryFixture(QualityMemoryStatus.CONFIRMED)
    );
  });

  it("renders the investigation and its evidence", async () => {
    const wrapper = mountCard();
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("dataQuality.knowledge.verdictRootCause")
    );
    expect(wrapper.text()).toContain("the upstream batch was short");
    expect(wrapper.text()).toContain("qualityExecutions/7");
    wrapper.unmount();
  });

  it("confirms a proposed memory and updates it in place", async () => {
    const wrapper = mountCard();
    await flushPromises();

    const confirm = wrapper
      .findAll("button")
      .find(
        (button) => button.text() === label("dataQuality.knowledge.confirm")
      );
    expect(confirm).toBeTruthy();
    await confirm!.trigger("click");
    await flushPromises();

    expect(mocks.confirmQualityMemory).toHaveBeenCalledWith(
      "workspaces/ws1/qualityMemories/m1",
      ConfirmQualityMemoryRequest_Decision.CONFIRM
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.knowledge.statusConfirmed")
    );
    expect(
      wrapper
        .findAll("button")
        .some(
          (button) => button.text() === label("dataQuality.knowledge.confirm")
        )
    ).toBe(false);
    wrapper.unmount();
  });

  it("offers no decision to a reader and shows an empty state", async () => {
    mocks.listQualityInvestigations.mockResolvedValue([]);
    mocks.listQualityMemories.mockResolvedValue([]);
    const wrapper = mountCard(false);
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("dataQuality.knowledge.investigationsEmpty")
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.knowledge.memoriesEmpty")
    );
    expect(wrapper.findAll("button")).toHaveLength(0);
    wrapper.unmount();
  });
});
