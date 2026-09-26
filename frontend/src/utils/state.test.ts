import { describe, expect, it } from "vitest";
import { State } from "@/types/proto-es/v1/common_pb";
import { stateBadgeVariant, stateLabel } from "./state";

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

describe("stateLabel", () => {
  // The lookup is the identity here, so an assertion names the key that the
  // server state resolved to.
  const t = (key: string) => key;

  it("names each state the server reports", () => {
    expect(stateLabel(State.ACTIVE, t)).toBe("common.stateActive");
    expect(stateLabel(State.DELETED, t)).toBe("common.stateDeleted");
  });

  it("separates an unstated state from an unrecognised one", () => {
    expect(stateLabel(State.STATE_UNSPECIFIED, t)).toBe(
      "common.stateUnspecified"
    );
    expect(stateLabel(99 as State, t)).toBe("common.stateUnknown");
  });
});
