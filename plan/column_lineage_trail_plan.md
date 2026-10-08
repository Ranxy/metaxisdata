# Plan: Field-Level Lineage Trail on the Graph

## TL;DR

Selecting a column in the lineage graph currently highlights the edges *incident to it* and nothing else. Make it highlight the column's whole flow instead: walk its ancestors, its descendants, and every relation on a path between them; ring the nodes on that trail, name the participating column on each of them, and step the rest of the canvas back.

Shipped in `feat/column-lineage-trail`. Phase 1 only — the trail is walked over the relations the canvas has already drawn, with no extra fetching.

## Decisions

- **Two directed closures, never one connected component.** Upstream follows only relations that name the current pair as their *target*; downstream only relations that name it as their *source*. On the dev estate the undirected component of `v_customer_360.customer_id` is **1302 pairs**; its two directed closures are **2 upstream and 6 downstream**. A single bidirectional walk lets a hop go up and then back down, which is what produces the 1302.
- **A relation that names no column ends the walk.** All 61 `JOIN` relations store an empty `target_column` — the source column decides which rows the target receives, which is real lineage but says nothing about the target's columns. The edge is highlighted and the object is on the trail; the walk does not continue through it. (23 `INDIRECT` relations and one table-level edge have the same shape.)
- **The trail is what is drawn.** It is walked over `graphView()` — the expanded directions the edges come from — so it can only ever highlight a path the canvas shows. A relation nobody has expanded is not part of it.
- **An empty trail dims nothing.** A field whose relations are not drawn yet yields no edges; fading the whole canvas for it would read as "this field has no lineage" instead of "nothing here carries it".
- **No layout change.** The participating column joins the node's existing path line (`e2e.e2e_dwd · customer_id`), which is height-neutral. A separate chip row would change node heights and re-lay out the whole graph on every click.
- **The trail is computed before the origin filter.** Hiding a source breaks the trail visibly rather than quietly rewriting it.
- **No depth limit, with a defensive cap.** The walks are bounded by a relation budget (default 2000) and report `truncated`; on real data a trail is a dozen relations.

## Where it lives

| File | Change |
| --- | --- |
| `frontend/src/lib/lineageTrail.ts` | New. `collectFieldTrail(view, { pivot, validNodeIds, maxRelations })` returns the pairs, the per-node columns, the edge ids and both sides' counts. Pure, no Vue, no API. |
| `frontend/src/lib/lineageGraph.ts` | `collectColumnEdgeIds` (one hop) is gone. `buildLineageEdges` takes `highlightedEdgeIds`; a null set dims nothing. |
| `frontend/src/pages/LineageGraphPage.vue` | `fieldTrail` is refreshed wherever the graph is rebuilt, and drives node data and edges from one place — the old code highlighted columns from `nodeDataMap` and edges from the drawn view, so a column could light up with no edge to carry it. A field click now also loads the node's relations the way a node click does, so the panel (and the trail summary in it) is populated for nodes that were never expanded. |
| `frontend/src/components/lineage/LineageNode.vue` | `onTrail` rings the node, `dimmed` steps it back, and `highlightedColumns` (now the trail's columns) is named inline. |

Detail panel gains a `Field trail` section: objects and relations per side, a `truncated` note, and *Fit to this field* (`fitView({ nodes: trailNodes })`).

## Testing

`lineageTrail.test.ts` covers the chain, the diamond, the cycle, the join stop, the drawn-graph bound, one stored relation counted once however many responses report it, the two writers of one relation counted apart, the cap, and the empty trail. The regression that matters most is **"a sibling column is not on the trail"**: an undirected walk fails it.

## Not covered

- Nothing is fetched, so a field whose ancestors were never expanded shows a one-hop trail. Following the field automatically is the next step if it is wanted; a `GetColumnTrail` RPC would be the way to do it without one round trip per level.
- The detail panel's related-runs list stays one-hop, by decision.
- `JOIN` arms stop at the influenced table even when a same-named column demonstrably continues downstream. Continuing through a join would be a heuristic about column identity, not a fact the store records.
