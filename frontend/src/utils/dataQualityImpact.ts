import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  type GetQualityTargetImpactResponse,
  type QualityConsumptionGap,
  type QualityImpactAsset,
  type QualityRootCauseCandidate,
  type QualityTarget,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  formatTargetPath,
  type QualityTargetMetadataRoute,
  type QualityTargetWorkspaceRoute,
  qualityTargetMetadataRoute,
  qualityTargetWorkspaceRoute,
  targetScopeLabel,
} from "@/utils/dataQuality";

/**
 * One target's lineage impact as a view model: which recorded downstream assets
 * its data reaches, and which upstream targets may explain a failure.
 *
 * The server has already ordered both lists (downstream nearest first; root
 * causes strongest first), so the helpers below keep that order and never
 * re-sort. What they own is the wording and the links: a distance as a catalog
 * key, the monitoring state as one of three values, the two route shapes, and
 * the truncation footnotes. Keeping those pure lets the card and its tests
 * agree on what each state means without mounting anything.
 */

/** How many lineage levels downstream the impact card walks. */
export const IMPACT_DEPTH = 2;

/** The root-cause window: the 24 hours before the reference instant. */
export const IMPACT_WINDOW_HOURS = 24;

/** A catalog key and its params; a view passes it straight to `t`. */
export interface ImpactText {
  key: string;
  params: Record<string, string | number>;
}

/**
 * A downstream asset's monitoring state.
 *
 * A missing summary means no non-archived check exists — "unmonitored", never
 * healthy. A summary with no enabled rule means every check is paused; that is
 * not a health verdict either. Only the third state has an outcome to show.
 */
export type ImpactMonitoringState = "unmonitored" | "allPaused" | "monitored";

/** One downstream asset row: identity, distance and monitoring state. */
export interface ImpactDownstreamRow {
  /** Stable within the response: the GUID + type, else the row's index. */
  key: string;
  target: QualityTarget | undefined;
  /** The asset's full display path, e.g. `prod · shop · public.orders`. */
  path: string;
  distance: ImpactText;
  summary: QualityTargetSummary | undefined;
  monitoring: ImpactMonitoringState;
  metadataRoute: QualityTargetMetadataRoute | null;
}

/** One upstream root-cause candidate row with its evidence. */
export interface ImpactRootCauseRow {
  /** Stable within the response: the GUID + type, else the row's index. */
  key: string;
  target: QualityTarget | undefined;
  /** The asset's full display path, e.g. `prod · shop · public.orders`. */
  path: string;
  distance: ImpactText;
  failingRuleCount: number;
  failingExecutionCount: number;
  /** The newest failing execution's resource name, "" when none was recorded. */
  latestFailureExecution: string;
  latestFailureTime: Timestamp | undefined;
  structureChangedTime: Timestamp | undefined;
  metadataRoute: QualityTargetMetadataRoute | null;
  executionRoute: ImpactExecutionRoute | null;
}

/** Where one failing execution opens, shaped for a RouterLink `to`. */
export interface ImpactExecutionRoute {
  name: "DataQualityExecutionDetail";
  params: { executionId: string[] };
}

/**
 * The asset's full display path: `instance · database · schema.object.column`.
 * Falls back to the GUID when the path snapshot is empty, and to "-" when even
 * that is absent, so a row never renders blank.
 */
export function impactTargetPath(target: QualityTarget | undefined): string {
  const name = formatTargetPath(target?.path);
  if (name === "-") {
    return target?.metaGuid?.trim() || "-";
  }
  const scope = targetScopeLabel(target?.path);
  return scope === "-" ? name : `${scope} · ${name}`;
}

/**
 * How far a row sits from the target, as catalog copy. Downstream distances are
 * positive (1 is a direct dependent); upstream ones are negative (-1 is a
 * direct source). Zero is not a distance the server reports for a row, so it
 * gets a neutral fallback rather than a fabricated hop count.
 */
export function impactDistanceText(distance: number): ImpactText {
  switch (distance) {
    case 1:
      return { key: "dataQuality.impact.distanceDirectDependent", params: {} };
    case -1:
      return { key: "dataQuality.impact.distanceDirectSource", params: {} };
    default:
      if (distance > 1) {
        return {
          key: "dataQuality.impact.distanceHops",
          params: { count: distance },
        };
      }
      if (distance < -1) {
        return {
          key: "dataQuality.impact.distanceHopsUpstream",
          params: { count: -distance },
        };
      }
      return { key: "dataQuality.impact.distanceUnknown", params: {} };
  }
}

/** The three-state monitoring mapping; a missing summary is "unmonitored". */
export function impactMonitoringState(
  summary: QualityTargetSummary | undefined
): ImpactMonitoringState {
  if (!summary) {
    return "unmonitored";
  }
  return summary.enabledRuleCount <= 0 ? "allPaused" : "monitored";
}

/**
 * The root-cause window's length in hours, read back from the response. Both
 * bounds are needed and the order must make sense; otherwise the caller falls
 * back to the requested 24 hours.
 */
export function impactWindowHours(
  start: Timestamp | undefined,
  end: Timestamp | undefined
): number | undefined {
  const from = epochSeconds(start);
  const to = epochSeconds(end);
  if (from === undefined || to === undefined || to <= from) {
    return undefined;
  }
  return Math.round((to - from) / 3_600);
}

/** The window caption: "in the {hours} hours before this run". */
export function impactWindowText(
  response: Pick<GetQualityTargetImpactResponse, "windowStart" | "windowEnd">
): ImpactText {
  return {
    key: "dataQuality.impact.window",
    params: {
      hours:
        impactWindowHours(response.windowStart, response.windowEnd) ??
        IMPACT_WINDOW_HOURS,
    },
  };
}

/** The downstream section, in the server's order (nearest first). */
export function impactDownstreamRows(
  response: Pick<GetQualityTargetImpactResponse, "downstreamAssets">
): ImpactDownstreamRow[] {
  return response.downstreamAssets.map(
    (asset: QualityImpactAsset, index: number) => ({
      key: rowKey(asset.target, "downstream", index),
      target: asset.target,
      path: impactTargetPath(asset.target),
      distance: impactDistanceText(asset.distance),
      summary: asset.summary,
      monitoring: impactMonitoringState(asset.summary),
      metadataRoute: impactMetadataRoute(asset.target),
    })
  );
}

/** The upstream section, in the server's order (strongest first). */
export function impactRootCauseRows(
  response: Pick<GetQualityTargetImpactResponse, "rootCauses">
): ImpactRootCauseRow[] {
  return response.rootCauses.map(
    (candidate: QualityRootCauseCandidate, index: number) => ({
      key: rowKey(candidate.target, "rootCause", index),
      target: candidate.target,
      path: impactTargetPath(candidate.target),
      distance: impactDistanceText(candidate.distance),
      failingRuleCount: candidate.failingRuleCount,
      failingExecutionCount: candidate.failingExecutionCount,
      latestFailureExecution: candidate.recentFailingExecutions[0] ?? "",
      latestFailureTime: candidate.latestFailureTime,
      structureChangedTime: candidate.structureChangedTime,
      metadataRoute: impactMetadataRoute(candidate.target),
      executionRoute: impactExecutionRoute(
        candidate.recentFailingExecutions[0]
      ),
    })
  );
}

/**
 * The honesty footnotes under each section: a walk the budget cut short, and
 * the objects it walked through but did not list. Ordered downstream first,
 * then upstream, matching the sections above.
 */
export function impactFootnotes(
  response: Pick<
    GetQualityTargetImpactResponse,
    | "downstreamTruncated"
    | "omittedDownstreamCount"
    | "upstreamTruncated"
    | "omittedRootCauseCount"
  >
): ImpactText[] {
  const notes: ImpactText[] = [];
  if (response.downstreamTruncated) {
    notes.push({
      key: "dataQuality.impact.footnoteDownstreamTruncated",
      params: {},
    });
  }
  if (response.omittedDownstreamCount > 0) {
    notes.push({
      key: "dataQuality.impact.footnoteOmittedDownstream",
      params: { count: response.omittedDownstreamCount },
    });
  }
  if (response.upstreamTruncated) {
    notes.push({
      key: "dataQuality.impact.footnoteUpstreamTruncated",
      params: {},
    });
  }
  if (response.omittedRootCauseCount > 0) {
    notes.push({
      key: "dataQuality.impact.footnoteOmittedRootCauses",
      params: { count: response.omittedRootCauseCount },
    });
  }
  return notes;
}

/** The metadata detail page an asset row opens, or null without a GUID/type. */
export function impactMetadataRoute(
  target: Pick<QualityTarget, "metaGuid" | "metaType"> | undefined
): QualityTargetMetadataRoute | null {
  return qualityTargetMetadataRoute(target);
}

/** The execution page a root cause's newest failure opens. */
export function impactExecutionRoute(
  name: string | undefined
): ImpactExecutionRoute | null {
  const trimmed = name?.trim() ?? "";
  if (!trimmed) {
    return null;
  }
  return {
    name: "DataQualityExecutionDetail",
    params: { executionId: trimmed.split("/") },
  };
}

/** The target workspace a consumption gap opens. */
export function impactGapWorkspaceRoute(
  target: QualityTarget | undefined
): QualityTargetWorkspaceRoute | null {
  return qualityTargetWorkspaceRoute(
    target?.metaGuid ?? "",
    target?.metaType ?? MetaType.UNSPECIFIED
  );
}

/** One unmonitored consumed asset row, shared by the card and its dialog. */
export interface ConsumptionGapRow {
  /** Stable within the response: the GUID + type, else the row's index. */
  key: string;
  target: QualityTarget | undefined;
  /** The asset's full display path, e.g. `prod · shop · public.orders`. */
  path: string;
  /** How many distinct assets its lineage reaches directly. */
  downstreamAssetCount: number;
  workspaceRoute: QualityTargetWorkspaceRoute | null;
}

/**
 * The gap list as rows, in the server's order (most direct readers first). The
 * order is the ranking; neither caller re-sorts it.
 */
export function consumptionGapRows(
  gaps: readonly QualityConsumptionGap[]
): ConsumptionGapRow[] {
  return gaps.map((gap, index) => ({
    key: rowKey(gap.target, "gap", index),
    target: gap.target,
    path: impactTargetPath(gap.target),
    downstreamAssetCount: gap.downstreamAssetCount,
    workspaceRoute: impactGapWorkspaceRoute(gap.target),
  }));
}

// ---- internals ----

/** A row's identity within its section, stable when a response is re-read. */
function rowKey(
  target: QualityTarget | undefined,
  prefix: string,
  index: number
): string {
  const guid = target?.metaGuid?.trim() ?? "";
  return guid ? `${target?.metaType ?? 0}:${guid}` : `${prefix}:${index}`;
}

/** A Timestamp's epoch seconds, or undefined when unset (zero is unset). */
function epochSeconds(timestamp: Timestamp | undefined): number | undefined {
  if (!timestamp) {
    return undefined;
  }
  const seconds = Number(timestamp.seconds ?? 0);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined;
}
