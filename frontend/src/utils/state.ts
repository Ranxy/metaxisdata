import { State } from "@/types/proto-es/v1/common_pb";
import type { Translate } from "./i18n";

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

/**
 * The label for a resource state. Kept next to the variant for the same
 * reason: the databases table and the instance detail's table used to carry
 * their own copy of this switch under two different locale sections, so the
 * same state could drift in wording as easily as it drifted in color.
 */
export function stateLabel(state: State, t: Translate): string {
  switch (state) {
    case State.ACTIVE:
      return t("common.stateActive");
    case State.DELETED:
      return t("common.stateDeleted");
    // The enum's zero value: the server said nothing about the state.
    case State.STATE_UNSPECIFIED:
      return t("common.stateUnspecified");
    // A value from a newer server that this client does not know yet.
    default:
      return t("common.stateUnknown");
  }
}
