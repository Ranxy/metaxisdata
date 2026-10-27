import {
  QualityExecutionTrigger,
  type QualityRun,
} from "@/types/proto-es/v1/data_quality_service_pb";

/**
 * The most checks one scheduled batch covers. It mirrors the runner's
 * maxExecutionsPerBatch: only a target larger than this is split across batches,
 * so a batch below the cap is never a slice of an oversize target.
 */
export const QUALITY_RUN_COMPOSITION_CAP = 100;

/**
 * The target's enabled-check count when a scheduled batch covered only a slice of
 * an oversize target, or 0 when the batch covered the whole target (or is manual).
 * Disclosure is limited to batches at the composition cap: a retry batch, or a
 * batch that ran before a check was added, holds fewer executions without being a
 * partial view, and must not be labeled as one.
 */
export function partialQualityRunChecks(
  run: Pick<QualityRun, "trigger" | "counts"> | undefined,
  targetRuleCount = 0
): number {
  const total = run?.counts?.total ?? 0;
  if (run?.trigger !== QualityExecutionTrigger.SCHEDULED) {
    return 0;
  }
  if (total < QUALITY_RUN_COMPOSITION_CAP) {
    return 0;
  }
  return targetRuleCount > total ? targetRuleCount : 0;
}
