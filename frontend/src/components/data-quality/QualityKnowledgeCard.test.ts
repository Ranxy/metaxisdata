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

function memoryFixture2() {
  return create(QualityMemorySchema, {
    name: "workspaces/ws1/qualityMemories/m2",
    kind: QualityMemoryKind.DECISION,
    statement: "the second statement",
    status: QualityMemoryStatus.PROPOSED,
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

  it("offers no decision to a reader, even for a proposed memory", async () => {
    const wrapper = mountCard(false);
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("dataQuality.knowledge.statusProposed")
    );
    expect(
      wrapper
        .findAll("button")
        .some(
          (button) => button.text() === label("dataQuality.knowledge.confirm")
        )
    ).toBe(false);
    expect(
      wrapper
        .findAll("button")
        .some(
          (button) => button.text() === label("dataQuality.knowledge.reject")
        )
    ).toBe(false);
    wrapper.unmount();
  });

  it("rejects a proposed memory and keeps only the affected row changed", async () => {
    mocks.listQualityMemories.mockResolvedValue([
      memoryFixture(),
      memoryFixture2(),
    ]);
    mocks.confirmQualityMemory.mockResolvedValue(
      memoryFixture(QualityMemoryStatus.REJECTED)
    );
    const wrapper = mountCard();
    await flushPromises();

    const reject = wrapper
      .findAll("button")
      .find(
        (button) => button.text() === label("dataQuality.knowledge.reject")
      );
    await reject!.trigger("click");
    await flushPromises();

    expect(mocks.confirmQualityMemory).toHaveBeenCalledWith(
      "workspaces/ws1/qualityMemories/m1",
      ConfirmQualityMemoryRequest_Decision.REJECT
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.knowledge.statusRejected")
    );
    expect(wrapper.text()).toContain("the second statement");
    wrapper.unmount();
  });

  it("reports a failed load and retries", async () => {
    mocks.listQualityInvestigations.mockRejectedValueOnce(new Error("boom"));
    const wrapper = mountCard();
    await flushPromises();
    expect(wrapper.text()).toContain(label("dataQuality.knowledge.loadFailed"));
    const retry = wrapper
      .findAll("button")
      .find((button) => button.text() === label("common.retry"));
    await retry!.trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("the upstream batch was short");
    wrapper.unmount();
  });
});
