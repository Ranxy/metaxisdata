import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  QualityExecutionTrigger,
  QualityRunSchema,
  QualityStatusCountsSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  partialQualityRunChecks,
  QUALITY_RUN_COMPOSITION_CAP,
} from "./qualityRun";

function scheduledRun(total: number) {
  return create(QualityRunSchema, {
    trigger: QualityExecutionTrigger.SCHEDULED,
    counts: create(QualityStatusCountsSchema, { total }),
  });
}

describe("partialQualityRunChecks", () => {
  it("does not call a complete batch partial when a check was added later", () => {
    expect(partialQualityRunChecks(scheduledRun(4), 5)).toBe(0);
  });

  it("does not call a retry batch partial", () => {
    expect(partialQualityRunChecks(scheduledRun(2), 130)).toBe(0);
  });

  it("discloses the target's enabled count at the composition cap", () => {
    expect(
      partialQualityRunChecks(scheduledRun(QUALITY_RUN_COMPOSITION_CAP), 130)
    ).toBe(130);
  });

  it("never treats a manual selection as partial", () => {
    const run = scheduledRun(QUALITY_RUN_COMPOSITION_CAP);
    run.trigger = QualityExecutionTrigger.MANUAL;
    expect(partialQualityRunChecks(run, 130)).toBe(0);
  });

  it("handles a missing run or count", () => {
    expect(partialQualityRunChecks(undefined, 130)).toBe(0);
    expect(partialQualityRunChecks(create(QualityRunSchema, {}), 130)).toBe(0);
  });
});
