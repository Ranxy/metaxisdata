import { describe, expect, it } from "vitest";
import { openLineageStatusVariant } from "./openlineageStatus";

describe("openLineageStatusVariant", () => {
  it("renders a finished run as success", () => {
    expect(openLineageStatusVariant("COMPLETE")).toBe("success");
  });

  it("renders a failed run as destructive", () => {
    expect(openLineageStatusVariant("FAIL")).toBe("destructive");
    expect(openLineageStatusVariant("FAILED")).toBe("destructive");
  });

  it("renders an unfinished run as an outline", () => {
    expect(openLineageStatusVariant("START")).toBe("outline");
    expect(openLineageStatusVariant("RUNNING")).toBe("outline");
  });

  it("matches the state whatever its case, since producers vary", () => {
    expect(openLineageStatusVariant("fail")).toBe("destructive");
    expect(openLineageStatusVariant("complete")).toBe("success");
  });

  it("falls back to secondary for a state it does not know", () => {
    expect(openLineageStatusVariant("ABORT")).toBe("secondary");
    expect(openLineageStatusVariant("")).toBe("secondary");
    expect(openLineageStatusVariant(undefined)).toBe("secondary");
  });
});
