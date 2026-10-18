/**
 * The badge variant an OpenLineage run state renders with. Every table reads it
 * from here so a failed run cannot look red on Jobs and black on Events.
 *
 * The member union is spelled out rather than imported from the Badge
 * component: `utils/` must not depend on `components/`.
 */
export type OpenLineageStatusVariant =
  | "success"
  | "destructive"
  | "secondary"
  | "outline";

/**
 * Maps an OpenLineage eventType to its badge variant. The states are the ones
 * the spec's run state machine produces, so an unfinished run is outlined and a
 * failed one is destructive; anything else is a state this build does not know.
 */
export function openLineageStatusVariant(
  eventType: string | undefined
): OpenLineageStatusVariant {
  switch ((eventType ?? "").toUpperCase()) {
    case "COMPLETE":
      return "success";
    case "FAIL":
    case "FAILED":
      return "destructive";
    case "START":
    case "RUNNING":
      return "outline";
    default:
      return "secondary";
  }
}

/**
 * The i18n key for an OpenLineage run state. The badge variant says how a state
 * should look; this says what it is called, so a zh-CN UI never prints the raw
 * enum member the producer sent.
 */
export type OpenLineageStatusKey =
  | "openlineage.statusComplete"
  | "openlineage.statusStart"
  | "openlineage.statusRunning"
  | "openlineage.statusFail"
  | "openlineage.statusAbort"
  | "openlineage.statusOther"
  | "openlineage.statusUnknown";

export function openLineageStatusKey(
  eventType: string | undefined
): OpenLineageStatusKey {
  switch ((eventType ?? "").toUpperCase()) {
    case "COMPLETE":
      return "openlineage.statusComplete";
    case "START":
      return "openlineage.statusStart";
    case "RUNNING":
      return "openlineage.statusRunning";
    case "FAIL":
    case "FAILED":
      return "openlineage.statusFail";
    case "ABORT":
      return "openlineage.statusAbort";
    case "OTHER":
      return "openlineage.statusOther";
    default:
      return "openlineage.statusUnknown";
  }
}
