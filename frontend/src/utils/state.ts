import { State } from "@/types/proto-es/v1/common_pb";

/**
 * The badge variant a resource state renders with. Every table reads it from
 * here so one state cannot look green on one page and black on the next.
 *
 * The member union is spelled out rather than imported from the Badge
 * component: `utils/` must not depend on `components/`, and the three values
 * are exactly the ones the state machine can produce.
 */
export type StateBadgeVariant = "success" | "destructive" | "secondary";

export function stateBadgeVariant(state: State): StateBadgeVariant {
  switch (state) {
    case State.ACTIVE:
      return "success";
    case State.DELETED:
      return "destructive";
    default:
      return "secondary";
  }
}
