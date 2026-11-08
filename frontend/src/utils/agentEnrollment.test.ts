import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  QualityAgentDatabaseEnrollmentSummarySchema,
  type QualityAgentEnrollment,
  QualityAgentEnrollmentSchema,
  QualityAgentEnrollmentState,
  QualityAgentEnrollmentStateCountSchema,
  QualityAgentEnrollmentSummarySchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  databaseRefLabel,
  databaseStateCounts,
  ENROLLMENT_STATE_ORDER,
  enrollmentBlockedReasonLabel,
  enrollmentMatchesSearch,
  enrollmentPercent,
  enrollmentPriorityReasonLabels,
  enrollmentStateCount,
  enrollmentStateCounts,
  enrollmentStateKey,
  enrollmentStateRank,
  enrollmentStateVariant,
  enrollmentTargetName,
  enrollmentTargetPath,
  enrollmentWorkloadCount,
  enrollmentWorkloadEstimate,
} from "./agentEnrollment";

const TARGET = "inst1;db1;public;orders";

function enrollmentFixture(
  overrides: MessageInitShape<typeof QualityAgentEnrollmentSchema> = {}
): QualityAgentEnrollment {
  return create(QualityAgentEnrollmentSchema, {
    name: "workspaces/ws/qualityAgentEnrollments/1",
    target: create(QualityTargetSchema, {
      metaGuid: TARGET,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "inst1",
        database: "db1",
        schema: "public",
        object: "orders",
      }),
    }),
    database: "instances/inst1/databases/db1",
    state: QualityAgentEnrollmentState.PENDING,
    ...overrides,
  });
}

describe("enrollment state vocabulary", () => {
  it("maps every state to its label key", () => {
    expect(enrollmentStateKey(QualityAgentEnrollmentState.PENDING)).toBe(
      "dataQuality.agent.enrollment.state.pending"
    );
    expect(enrollmentStateKey(QualityAgentEnrollmentState.ACTIVE)).toBe(
      "dataQuality.agent.enrollment.state.active"
    );
    expect(enrollmentStateKey(QualityAgentEnrollmentState.DORMANT)).toBe(
      "dataQuality.agent.enrollment.state.dormant"
    );
    expect(enrollmentStateKey(QualityAgentEnrollmentState.EXCLUDED)).toBe(
      "dataQuality.agent.enrollment.state.excluded"
    );
    expect(enrollmentStateKey(QualityAgentEnrollmentState.OUT_OF_SCOPE)).toBe(
      "dataQuality.agent.enrollment.state.outOfScope"
    );
    expect(enrollmentStateKey(QualityAgentEnrollmentState.GONE)).toBe(
      "dataQuality.agent.enrollment.state.gone"
    );
    expect(enrollmentStateKey(QualityAgentEnrollmentState.UNSPECIFIED)).toBe(
      "dataQuality.agent.enrollment.state.unspecified"
    );
  });

  it("maps every state to its badge variant", () => {
    expect(enrollmentStateVariant(QualityAgentEnrollmentState.PENDING)).toBe(
      "info"
    );
    expect(enrollmentStateVariant(QualityAgentEnrollmentState.ACTIVE)).toBe(
      "success"
    );
    expect(enrollmentStateVariant(QualityAgentEnrollmentState.DORMANT)).toBe(
      "secondary"
    );
    expect(enrollmentStateVariant(QualityAgentEnrollmentState.EXCLUDED)).toBe(
      "warning"
    );
    expect(
      enrollmentStateVariant(QualityAgentEnrollmentState.OUT_OF_SCOPE)
    ).toBe("neutral");
    expect(enrollmentStateVariant(QualityAgentEnrollmentState.GONE)).toBe(
      "outline"
    );
    expect(
      enrollmentStateVariant(QualityAgentEnrollmentState.UNSPECIFIED)
    ).toBe("outline");
  });

  it("orders the summary's states by the queue's workflow", () => {
    expect(ENROLLMENT_STATE_ORDER).toEqual([
      QualityAgentEnrollmentState.PENDING,
      QualityAgentEnrollmentState.ACTIVE,
      QualityAgentEnrollmentState.DORMANT,
      QualityAgentEnrollmentState.EXCLUDED,
      QualityAgentEnrollmentState.OUT_OF_SCOPE,
      QualityAgentEnrollmentState.GONE,
    ]);
    expect(enrollmentStateRank(QualityAgentEnrollmentState.PENDING)).toBe(0);
    expect(enrollmentStateRank(QualityAgentEnrollmentState.GONE)).toBe(5);
    expect(enrollmentStateRank(QualityAgentEnrollmentState.UNSPECIFIED)).toBe(
      -1
    );
  });
});

describe("enrollment reasons", () => {
  it("labels each known priority signal and passes an unknown one through", () => {
    expect(
      enrollmentPriorityReasonLabels(
        "rules,failing,consumers:12,critical,important,schema-changed,new,unmonitored"
      )
    ).toEqual([
      { key: "dataQuality.agent.enrollment.prioritySignal.rules" },
      { key: "dataQuality.agent.enrollment.prioritySignal.failing" },
      {
        key: "dataQuality.agent.enrollment.prioritySignal.consumers",
        params: { count: 12 },
      },
      { key: "dataQuality.agent.enrollment.prioritySignal.critical" },
      { key: "dataQuality.agent.enrollment.prioritySignal.important" },
      { key: "dataQuality.agent.enrollment.prioritySignal.schemaChanged" },
      { key: "dataQuality.agent.enrollment.prioritySignal.new" },
      { key: "dataQuality.agent.enrollment.prioritySignal.unmonitored" },
    ]);
    expect(enrollmentPriorityReasonLabels("mystery,two")).toEqual([
      { text: "mystery" },
      { text: "two" },
    ]);
  });

  it("skips blanks and keeps a malformed consumers token as written", () => {
    expect(enrollmentPriorityReasonLabels("")).toEqual([]);
    expect(enrollmentPriorityReasonLabels(" , rules ,, ")).toEqual([
      { key: "dataQuality.agent.enrollment.prioritySignal.rules" },
    ]);
    expect(enrollmentPriorityReasonLabels("consumers:0")).toEqual([
      { text: "consumers:0" },
    ]);
    expect(enrollmentPriorityReasonLabels("consumers:many")).toEqual([
      { text: "consumers:many" },
    ]);
  });

  it("translates a known blocked reason and passes an unknown one through", () => {
    expect(enrollmentBlockedReasonLabel("")).toBeUndefined();
    expect(enrollmentBlockedReasonLabel("   ")).toBeUndefined();
    expect(enrollmentBlockedReasonLabel("no-model")).toEqual({
      key: "dataQuality.agent.enrollment.blocked.noModel",
    });
    expect(enrollmentBlockedReasonLabel("admission-limit")).toEqual({
      key: "dataQuality.agent.enrollment.blocked.admissionLimit",
    });
    expect(
      enrollmentBlockedReasonLabel("the database left the runner's scope")
    ).toEqual({ text: "the database left the runner's scope" });
  });
});

describe("enrollment display", () => {
  it("renders a database reference as instance and database names", () => {
    expect(databaseRefLabel("instances/inst1/databases/db1")).toBe(
      "inst1 · db1"
    );
    expect(databaseRefLabel("instances//databases/db1")).toBe(
      "instances//databases/db1"
    );
    expect(databaseRefLabel("")).toBe("-");
    expect(databaseRefLabel("db1")).toBe("db1");
  });

  it("names and paths the target, falling back to the GUID", () => {
    expect(enrollmentTargetName(enrollmentFixture())).toBe("orders");
    expect(enrollmentTargetPath(enrollmentFixture())).toBe("public.orders");
    expect(
      enrollmentTargetName(
        enrollmentFixture({
          target: create(QualityTargetSchema, { metaGuid: TARGET }),
        })
      )
    ).toBe(TARGET);
    expect(
      enrollmentTargetName(
        enrollmentFixture({ target: create(QualityTargetSchema, {}) })
      )
    ).toBe("-");
    expect(
      enrollmentTargetPath(
        enrollmentFixture({ target: create(QualityTargetSchema, {}) })
      )
    ).toBe("-");
  });

  it("matches a search over the path, GUID and database", () => {
    const enrollment = enrollmentFixture();

    expect(enrollmentMatchesSearch(enrollment, " ")).toBe(true);
    expect(enrollmentMatchesSearch(enrollment, "ORDERS")).toBe(true);
    expect(enrollmentMatchesSearch(enrollment, "public")).toBe(true);
    expect(enrollmentMatchesSearch(enrollment, "instances/inst1")).toBe(true);
    expect(enrollmentMatchesSearch(enrollment, "missing")).toBe(false);
  });
});

describe("enrollment summary helpers", () => {
  const summary = create(QualityAgentEnrollmentSummarySchema, {
    states: [
      create(QualityAgentEnrollmentStateCountSchema, {
        state: QualityAgentEnrollmentState.PENDING,
        count: 3,
      }),
      create(QualityAgentEnrollmentStateCountSchema, {
        state: QualityAgentEnrollmentState.ACTIVE,
        count: 2,
      }),
      create(QualityAgentEnrollmentStateCountSchema, {
        state: QualityAgentEnrollmentState.EXCLUDED,
        count: 1,
      }),
    ],
    databases: [
      create(QualityAgentDatabaseEnrollmentSummarySchema, {
        database: "instances/inst1/databases/db1",
        pending: 3,
        active: 2,
        dormant: 4,
        excluded: 1,
        outOfScope: 5,
        gone: 6,
      }),
    ],
  });

  it("reads the counts by state and treats a missing summary as empty", () => {
    const counts = enrollmentStateCounts(summary);
    expect(
      enrollmentStateCount(counts, QualityAgentEnrollmentState.PENDING)
    ).toBe(3);
    expect(
      enrollmentStateCount(counts, QualityAgentEnrollmentState.DORMANT)
    ).toBe(0);
    expect(enrollmentWorkloadCount(counts)).toBe(5);
    expect(enrollmentStateCounts(undefined).size).toBe(0);
  });

  it("reads one database's counts in the queue's order", () => {
    expect(databaseStateCounts(summary.databases[0])).toEqual([
      { state: QualityAgentEnrollmentState.PENDING, count: 3 },
      { state: QualityAgentEnrollmentState.ACTIVE, count: 2 },
      { state: QualityAgentEnrollmentState.DORMANT, count: 4 },
      { state: QualityAgentEnrollmentState.EXCLUDED, count: 1 },
      { state: QualityAgentEnrollmentState.OUT_OF_SCOPE, count: 5 },
      { state: QualityAgentEnrollmentState.GONE, count: 6 },
    ]);
  });

  it("turns counts into whole percentages safely", () => {
    expect(enrollmentPercent(1, 4)).toBe(25);
    expect(enrollmentPercent(2, 3)).toBe(67);
    expect(enrollmentPercent(-2, 4)).toBe(0);
    expect(enrollmentPercent(1, 0)).toBe(0);
    expect(enrollmentPercent(Number.NaN, 4)).toBe(0);
    expect(enrollmentPercent(1, Number.NaN)).toBe(0);
  });

  it("estimates daily wakes from the discovered targets and the active set", () => {
    expect(
      enrollmentWorkloadEstimate({
        tables: 60,
        reviewDays: 30,
        activeSessions: 4,
      })
    ).toBe(6);
    expect(
      enrollmentWorkloadEstimate({
        tables: 0,
        reviewDays: 30,
        activeSessions: 0,
      })
    ).toBe(0);
    // A zero review window means the server default, 30 days.
    expect(
      enrollmentWorkloadEstimate({
        tables: 30,
        reviewDays: 0,
        activeSessions: 1,
      })
    ).toBe(2);
    // Negative or non-finite inputs cannot produce a negative estimate.
    expect(
      enrollmentWorkloadEstimate({
        tables: -10,
        reviewDays: 30,
        activeSessions: Number.NaN,
      })
    ).toBe(0);
  });
});
