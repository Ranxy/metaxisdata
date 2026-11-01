import {
  QualityStatus,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";

/**
 * The trust signal a registry object carries: whether data-quality checks watch
 * it, and how it is doing.
 *
 * The metadata browser asks `ListQualityTargets` for the objects on screen in
 * one selection read; the answer only carries the monitored ones, so a missing
 * entry *is* the "not monitored" state. These helpers keep that mapping pure —
 * the key an object occupies, the dot's tone, and its column coverage — while
 * the words and the layout live in the components.
 */

/** The registry object types a quality contract can target. */
const QUALITY_SIGNAL_META_TYPES: ReadonlySet<MetaType> = new Set([
  MetaType.TABLE,
  MetaType.VIEW,
  MetaType.MATERIALIZED_VIEW,
  MetaType.EXTERNAL_TABLE,
]);

/** Whether an object of this type can carry quality checks. */
export function isQualitySignalMetaType(metaType: MetaType): boolean {
  return QUALITY_SIGNAL_META_TYPES.has(metaType);
}

/**
 * The key one object's signal occupies. A GUID alone does not identify a
 * registry row across object types, so the meta type is part of the key.
 */
export function qualitySignalKey(guid: string, metaType: MetaType): string {
  return `${metaType}:${guid}`;
}

/** The key a returned summary occupies, or "" when it names no target. */
export function summarySignalKey(summary: QualityTargetSummary): string {
  const guid = summary.target?.metaGuid?.trim() ?? "";
  const metaType = summary.target?.metaType ?? MetaType.UNSPECIFIED;
  if (!guid || !isQualitySignalMetaType(metaType)) {
    return "";
  }
  return qualitySignalKey(guid, metaType);
}

/** The dot tone a signal earns; `neutral` is the only state a missing one gets. */
export type QualitySignalTone =
  | "success"
  | "warning"
  | "danger"
  | "destructive"
  | "info"
  | "neutral";

/**
 * Whether every check on the target is paused.
 *
 * The selection read aggregates enabled *and* paused checks, so a target whose
 * checks are all paused is still returned — with no enabled rule. That is not a
 * health verdict and not "monitored": nothing is being verified.
 */
export function qualitySignalAllPaused(
  signal: QualityTargetSummary | undefined
): boolean {
  return signal !== undefined && signal.enabledRuleCount <= 0;
}

/**
 * The tone for the worst latest result. A target with no judged result, and a
 * target whose checks are all paused, is neutral — never green: "nothing is
 * being verified" is not "everything passed".
 */
export function qualitySignalTone(
  signal: QualityTargetSummary | undefined
): QualitySignalTone {
  if (!signal || qualitySignalAllPaused(signal)) {
    return "neutral";
  }
  switch (signal.worstStatus) {
    case QualityStatus.PASS:
      return "success";
    case QualityStatus.WARN:
      return "warning";
    case QualityStatus.FAIL:
      return "destructive";
    case QualityStatus.ERROR:
      return "danger";
    default:
      return "neutral";
  }
}

/** A target's column coverage: how many current columns a check watches. */
export interface QualityColumnCoverage {
  covered: number;
  total: number;
}

/**
 * The signal's column coverage, or null when there is no current column list to
 * cover. A `0/0` answer means the target no longer resolves, which is not the
 * same fact as "none of its columns are watched".
 */
export function qualitySignalCoverage(
  signal: QualityTargetSummary | undefined
): QualityColumnCoverage | null {
  const total = signal?.totalColumnCount ?? 0;
  if (!signal || total <= 0) {
    return null;
  }
  return {
    covered: Math.max(0, Math.min(signal.coveredColumnCount, total)),
    total,
  };
}
