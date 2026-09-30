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
