import { describe, expect, it } from "vitest";
import { State } from "@/types/proto-es/v1/common_pb";
import { stateBadgeVariant } from "./state";

describe("stateBadgeVariant", () => {
  it("renders an active resource as success", () => {
    expect(stateBadgeVariant(State.ACTIVE)).toBe("success");
  });

  it("renders a deleted resource as destructive", () => {
    expect(stateBadgeVariant(State.DELETED)).toBe("destructive");
  });

  it("falls back to secondary for an unstated state", () => {
    expect(stateBadgeVariant(State.STATE_UNSPECIFIED)).toBe("secondary");
    // A value from a newer server that this client does not know yet.
    expect(stateBadgeVariant(99 as State)).toBe("secondary");
  });
});
