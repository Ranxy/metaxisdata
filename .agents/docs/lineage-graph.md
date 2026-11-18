# Lineage graph: field-level interaction — Reference

> Maintenance reference for the field trail and field-scoped expansion on the lineage graph page.
> Related: [frontend-ui.md](frontend-ui.md) (visual encoding), [lineage-semantics.md](lineage-semantics.md)
> (edges), [lineage-analyzer.md](lineage-analyzer.md).

## What it is

The graph draws relations, so a column's flow cannot be answered on the object itself. Selecting a
field highlights its *trail* — both directed closures of the `(guid, column)` pair, the nodes and
relations between them — and dims everything else; right-clicking a field row offers `Expand column
upstream` / `Expand column downstream`, which draws only the objects that field reaches.

## The trail

`collectFieldTrail(view, { pivot, validNodeIds, maxRelations })` (`frontend/src/lib/lineageTrail.ts`)
is pure, and returns the objects, the per-node columns, the edge ids, both sides' counts, and a
`truncated` flag.

| Rule | Why |
| --- | --- |
| Two directed closures, never one connected component | Upstream follows only relations naming the pair as their *target*, downstream only ones naming it as their *source*. A bidirectional walk goes up and then back down, so the undirected component is far larger than the two closures. |
| A relation that names no column ends the walk | A `JOIN` stores an empty `target_column`: the source column decides which rows the target receives, which is real lineage but says nothing about the target's columns. The relation and its object are on the trail; the walk does not continue through it. |
| The trail is what is drawn, on the node's existing layout | It is walked over `graphView()`, so it can only highlight a path the canvas shows; a direction nobody expanded is not part of it. The participating column joins the node's existing path line (`e2e.e2e_dwd · customer_id`); a separate chip row would change node heights and re-lay out the graph on every click. |
| Computed before the origin filter; an empty trail dims nothing | Hiding a source breaks the trail visibly instead of quietly rewriting it, and fading the whole canvas for a field whose relations are simply not drawn would read as "this field has no lineage". A relation budget (`maxRelations`, default 2000) reports `truncated` instead of silently showing less. |
| One owner for edge highlighting | `buildLineageEdges` takes `highlightedEdgeIds`, and a null set dims nothing (`frontend/src/lib/lineageGraph.ts`); the old one-hop `collectColumnEdgeIds` is gone. |

## Field-scoped expansion

A direction is expanded **either whole or field by field, never both**, in separate maps in
`useLineageGraphData` (`expandedDirections` / `expandedColumns`, keyed `direction:guid`).
`directionView` resolves the precedence: a whole-node expansion wins, then the fields naming a
relation on this node's side, then nothing.

| Rule | Why |
| --- | --- |
| The walk is the trail walk, one hop at a time | `columnStep` is the step `collectFieldTrail` takes, and `walkColumnExpansion` walks it. It stops where the trail stops: a relation that reaches the far object without naming a field of it is on the field's lineage and its object is drawn, but the walk cannot continue through a field it does not know. `load` is the caller's fetch, so the walk is a pure function of what the server answers. |
| The map remembers each field's depth, and fields accumulate | `expandedColumns` records the levels still below each visited pair (`depth`), so raising the depth control and clicking the same field again works instead of silently doing nothing (`fieldScopesFor` / `addColumnExpansion`); a second field adds a scope and the union is what the direction draws. There is no per-field undo — Reset is the way back, as for node expansions. |
| The node's own Expand stays on offer, and an item that would do nothing is not there | "Show me this field's lineage" narrows the canvas; "show me the whole object" widens it back, and hiding the second would strand the reader with Reset as the only way out. `canExpandColumn` (`LineageNode.vue`) offers a side only when the field names a relation on this node's side and the walk is not already as deep as the control asks. The node data carries `null` for a direction not yet fetched rather than an empty set — hiding an item that works is worse than showing one with nothing to draw. On the root, whose two directions are expanded on arrival, a field menu carries View Schema and nothing else. |
| The expansion selects the field and opens every drawn node's field list | Expansion alone is invisible when the field has nothing on that side; selecting it fills `Selected field` and `Field trail` either way. `markSelectedColumn` (`useLineageSelection`) points the panel and the trail at the field without redrawing, so the action pays for one redraw. The field tying a revealed card to the pivot is what the reader came to see, so every node the expansion draws opens its list: the page owns visibility (`fieldsVisibleGuids` → `fieldsVisible` in node data) because a node can be opened by an expansion it did not ask for. |
| The view frames what the click touched, and the field in hand comes into view | `fitViewWhenMeasured` (`LineageGraphPage.vue`) fits the pivot and the nodes it reached, reading card heights off the DOM first (Vue Flow's own measurement of a card that grew in place lags), waiting at most ~300ms. The field is revealed once per action — the list is alphabetical and capped at 200px, so it sorts below the fold — on a page-bumped `revealToken`, re-checked over the next frames because the card is restacked while an expansion finishes. `nowheel` stops Vue Flow's zoom handler from claiming the scroll, and each field row's own `ContextMenu` stays inside the node's trigger, so radix-vue's handled `contextmenu` keeps the node menu shut. |

## Invariants

- The trail never fetches. It is a function of the relations already loaded, and a field whose ancestors were never expanded shows a one-hop trail. A sibling column is never on it: the directed walk, not an undirected one, is what makes that true.
- `fieldScopedRelations(relations, direction, fields)` (`lineageGraph.ts`) matches the field on **this node's side** — the target's field for an upstream relation, the source's for a downstream one — and that side flips with the direction. One stored relation is counted once however many responses report it, and the two writers of one relation (analyzer vs OpenLineage) are counted apart.

## Known limits

Do not expect to follow a field through a `JOIN`: the relation names no target column, so the walk
stops. A field expansion only ever *adds* relations the field names — never narrows the canvas —
and a side the store named no column on gets no menu item.

## Code map

| Path | Holds |
| --- | --- |
| `frontend/src/lib/{lineageTrail,lineageGraph}.ts` | `collectFieldTrail`, `columnStep`, `walkColumnExpansion`, `ColumnPair`/`columnPairKey`, `ColumnWalk`, `FieldTrail`. Pure; and `LineageDirection`, `fieldScopedRelations`, `buildLineageEdges` with `highlightedEdgeIds`. |
| `frontend/src/composables/{useLineageGraphData,useLineageSelection}.ts` | `expandedDirections`, `expandedColumns`, `fieldScopesFor`, `addColumnExpansion`, `revealRequest`; `markSelectedColumn`, `handleSelectColumn`. |
| `frontend/src/pages/LineageGraphPage.vue`, `components/lineage/{LineageNode,LineageNodeDetailPanel}.vue` | `directionView`, `handleExpandColumn` (the `expand-column` emit; i18n `lineageGraph.expandColumnUpstream` / `expandColumnDownstream`), `fitViewWhenMeasured`, `fieldsVisibleGuids`; `onTrail`/`dimmed`, inline trail columns, field list, per-field menu, `canExpandColumn`; the `Field trail` section and *Fit to this field*. |
| Tests | `lineageTrail.test.ts` (17 cases: chain, diamond, cycle, join stop, drawn-graph bound, duplicate relation, two writers, cap, empty trail, `columnStep`, `walkColumnExpansion`), `lineageGraph.test.ts` (`fieldScopedRelations`), `useLineageGraphData.test.ts` (expansions, reveal, degree read), `useLineageSelection.test.ts`. Gate: `pnpm --dir frontend biome:check && pnpm --dir frontend lint && pnpm --dir frontend type-check && pnpm --dir frontend test run`. |
