import { describe, expect, it, vi } from "vitest";
import {
  MAX_SETTLE_PASSES,
  nodeHeightFor,
  settleNodeHeights,
} from "./lineageLayout";

function heights(entries: Array<[string, number]>): Map<string, number> {
  return new Map(entries);
}

/** A measurement that returns each queued height map in turn, then repeats the last. */
function measurementSequence(
  ...sequence: Array<Map<string, number> | null>
): () => Promise<Map<string, number> | null> {
  let index = 0;
  return async () => {
    const value = sequence[Math.min(index, sequence.length - 1)];
    index += 1;
    return value;
  };
}

describe("settleNodeHeights", () => {
  it("applies one pass and stops when the next measurement agrees", async () => {
    const applied: Map<string, number>[] = [];
    const result = await settleNodeHeights({
      current: () => heights([["a", 100]]),
      measure: measurementSequence(
        heights([["a", 120]]),
        heights([["a", 120]])
      ),
      apply: (next) => applied.push(next),
    });

    expect(applied).toHaveLength(1);
    expect(result).toEqual(heights([["a", 120]]));
  });

  it("does nothing when the first measurement already matches the layout", async () => {
    const apply = vi.fn();
    await settleNodeHeights({
      current: () => heights([["a", 120]]),
      measure: measurementSequence(heights([["a", 120]])),
      apply,
    });

    expect(apply).not.toHaveBeenCalled();
  });

  it("keeps the previous layout when the caller is not ready to measure", async () => {
    const apply = vi.fn();
    const result = await settleNodeHeights({
      current: () => heights([["a", 100]]),
      measure: measurementSequence(null),
      apply,
    });

    expect(apply).not.toHaveBeenCalled();
    expect(result).toBeNull();
  });

  it("stops after the pass budget when the heights keep moving", async () => {
    let height = 100;
    const apply = vi.fn(() => {
      height += 10;
    });
    const result = await settleNodeHeights({
      current: () => heights([["a", height - 10]]),
      measure: async () => heights([["a", height]]),
      apply,
    });

    expect(apply).toHaveBeenCalledTimes(MAX_SETTLE_PASSES);
    expect(result).not.toBeNull();
  });

  it("measures every drawn node, not only the one that changed", async () => {
    const applied: Map<string, number>[] = [];
    await settleNodeHeights({
      current: () => heights([["a", 120]]),
      measure: measurementSequence(
        heights([
          ["a", 120],
          ["b", 80],
        ]),
        heights([
          ["a", 120],
          ["b", 80],
        ])
      ),
      apply: (next) => applied.push(new Map(next)),
    });

    expect(applied[0]).toEqual(
      heights([
        ["a", 120],
        ["b", 80],
      ])
    );
  });
});

describe("nodeHeightFor", () => {
  it("uses the rendered height when the canvas has one", () => {
    expect(nodeHeightFor("a", heights([["a", 140]]), () => 100)).toBe(140);
  });

  it("falls back to the guess for a node it has not drawn yet", () => {
    expect(nodeHeightFor("b", heights([["a", 140]]), () => 100)).toBe(100);
  });
});
