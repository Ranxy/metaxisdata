/**
 * The measured-height convergence of the lineage canvas.
 *
 * A node's height cannot be predicted: how many lines its action row wraps to
 * depends on the locale and on which actions it offers. The browser is the only
 * thing that knows, so the canvas re-stacks with the heights the cards actually
 * rendered at. Positions do not feed back into heights, so the loop normally
 * converges in one pass; it survives a first measurement taken before layout
 * finished. These functions are pure over their callbacks so the four-pass
 * convergence can be tested without a canvas.
 */

/** How many restacking passes the convergence may take. */
export const MAX_SETTLE_PASSES = 4;

export interface SettleNodeHeightsOptions {
  /** The height map the current layout was built with. */
  current: () => Map<string, number>;
  /**
   * The heights the browser gave every drawn node, or null before every node
   * that belongs on the canvas has been drawn (a caller retries).
   */
  measure: () => Promise<Map<string, number> | null>;
  /** Re-stack the graph with a changed height map. */
  apply: (heights: Map<string, number>) => void;
}

/**
 * Re-stacks with the measured heights until a pass changes nothing, the pass
 * budget runs out, or the nodes are not all drawn yet.
 *
 * Returns the heights the last applied stack used, or null when the caller's
 * measurement was not ready and the previous layout was kept.
 */
export async function settleNodeHeights(
  options: SettleNodeHeightsOptions
): Promise<Map<string, number> | null> {
  let current = options.current();
  for (let pass = 0; pass < MAX_SETTLE_PASSES; pass++) {
    const heights = await options.measure();
    if (!heights) {
      return null;
    }
    let changed = false;
    for (const [guid, height] of heights) {
      if (current.get(guid) !== height) {
        changed = true;
        break;
      }
    }
    if (!changed) {
      return heights;
    }
    current = heights;
    options.apply(heights);
  }
  return current;
}

/**
 * The height to place a node at: the height it rendered at last, or the guess
 * for a node the canvas has not drawn yet.
 */
export function nodeHeightFor(
  guid: string,
  renderedHeights: Map<string, number>,
  guess: (guid: string) => number
): number {
  return renderedHeights.get(guid) ?? guess(guid);
}
