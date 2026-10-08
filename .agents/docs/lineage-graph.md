# Lineage graph: field-level interaction — Reference

> Status: **implemented** (`feat/column-lineage-trail`, `feat/column-expand`). Maintenance reference for the field trail and the field-scoped expansion on the lineage graph page.
> Related: [frontend-ui.md](frontend-ui.md) (node/edge visual encoding and layout), [lineage-semantics.md](lineage-semantics.md) (what the edges mean), [lineage-analyzer.md](lineage-analyzer.md).

## What it is

The graph draws relations, so a column's flow is not something it can answer on its own: the same object appears once, and a field of it is highlighted, named, and walked. Two interactions do that, and they share one step function:

- **Selecting a field** highlights that field's whole flow — the *trail*: both directed closures of the `(guid, column)` pair, the nodes on them, and the relations in between. Everything else on the canvas steps back.
- **Right-clicking a field row** offers `Expand column upstream` / `Expand column downstream`, which draws only the objects that field's lineage reaches, instead of everything the direction holds.

## The trail

`collectFieldTrail(view, { pivot, validNodeIds, maxRelations })` (`frontend/src/lib/lineageTrail.ts:152`) is pure — no Vue, no API — and returns the pairs, the per-node columns, the edge ids, both sides' counts, and a `truncated` flag.

| Rule | Why |
| --- | --- |
| Two directed closures, never one connected component | Upstream follows only relations naming the pair as their *target*, downstream only ones naming it as their *source*. On the dev estate the undirected component of `v_customer_360.customer_id` is **1302 pairs**; its two directed closures are **2 upstream and 6 downstream** — a bidirectional walk goes up and then back down. |
| A relation that names no column ends the walk | All 61 `JOIN` relations store an empty `target_column`: the source column decides which rows the target receives, which is real lineage but says nothing about the target's columns. The relation is highlighted and its object is on the trail; the walk does not continue through it. (23 `INDIRECT` relations and one table-level edge have the same shape.) |
| The trail is what is drawn | It is walked over `graphView()`, so it can only highlight a path the canvas shows. A direction nobody expanded is not part of it. |
| An empty trail dims nothing | A field whose relations are not drawn yields no edges; fading the whole canvas would read as "this field has no lineage" instead of "nothing here carries it". |
| No layout change | The participating column joins the node's existing path line (`e2e.e2e_dwd · customer_id`), which is height-neutral. A separate chip row would change node heights and re-lay out the graph on every click. |
| The trail is computed before the origin filter | Hiding a source breaks the trail visibly instead of quietly rewriting it. |
| No depth limit, with a defensive cap | Bounded by a relation budget (default 2000, `lineageTrail.ts:162,204`), reported as `truncated` (`:225`); a trail on real data is a dozen relations. |

Edge highlighting is driven from one place: `buildLineageEdges` takes `highlightedEdgeIds`, and a null set dims nothing (`frontend/src/lib/lineageGraph.ts:213,219`). The old one-hop `collectColumnEdgeIds` is gone. The detail panel gains a `Field trail` section: objects and relations per side, a `truncated` note, and *Fit to this field*.

## Field-scoped expansion

A direction is expanded **either whole or field by field, never both**. They live in separate maps on the page (`expandedDirections` / `expandedColumns`, both keyed `direction:guid`, `frontend/src/pages/LineageGraphPage.vue:464,471`), and `directionView` (`:1259`) resolves the precedence: a whole-node expansion wins, then the fields that name a relation on this node's side, then nothing.

| Rule | Why |
| --- | --- |
| The walk is the trail walk, one hop at a time | `columnStep` (`lineageTrail.ts:276`) is the step `collectFieldTrail` takes, and `walkColumnExpansion` (`:333`) walks it. It stops where the trail stops: a relation that reaches the far object without naming a field of it is on the field's lineage and its object is drawn, but the walk cannot continue through a field it does not know. `load` is the caller's fetch, so the walk is a pure function of what the server answers. |
| A field remembers how deep it was taken | Each visited pair records the levels still below it (`depth` in `expandedColumns`), so raising the depth control and clicking the same field again works instead of silently doing nothing (`:997`). |
| Fields accumulate | A second field adds a scope; the union is what the direction draws. There is no per-field undo — Reset is the way back, as for node expansions. |
| The node's own Expand stays on offer | "Show me this field's lineage" narrows the canvas; "show me the whole object" widens it back. Hiding the second would strand the reader with Reset as the only way out. |
| An item that would do nothing is not there | `canExpandColumn` (`LineageNode.vue:333`) offers a side only when the field names a relation on this node's side and the walk is not already as deep as the control asks. A direction that has not been fetched answers `null` rather than "nothing there" — hiding an item that works is worse than showing one with nothing to draw. On the root, whose two directions are expanded on arrival, a field menu carries View Schema and nothing else. |
| The expansion selects the field | Expansion alone is invisible when the field has nothing on that side; selecting it fills `Selected field` and `Field trail` either way. `markSelectedColumn` (`:1565`) points the panel and the trail at the field without redrawing, so the action pays for one redraw. |
| Every node the expansion draws opens its field list | The field tying a revealed card to the pivot is what the reader came to see. The page owns visibility (`fieldsVisibleGuids` → `fieldsVisible` in node data, `:551,1209`) rather than the card keeping a private flag, because a node can be opened by an expansion it did not ask for. |
| The view frames what the click touched | `fitViewWhenMeasured` (`:1696`) fits the pivot and the nodes it reached, reading card heights off the DOM first (Vue Flow's own measurement of a card that grew in place lags), watching only those nodes and waiting at most ~300ms. |
| The field in hand is scrolled into view once per request | An open list is alphabetical and capped at 200px, so the field the graph is about sorts below the fold. The card reveals it — the clicked field on the pivot, otherwise the one the trail runs through — only when out of view, on a page-bumped `revealToken` (`:1213`), re-checked over the next frames because the card is restacked while an expansion finishes. |
| The list takes the wheel | `nowheel` (`LineageNode.vue:120`) — Vue Flow's zoom handler claims every wheel event that does not pass through that class. |
| The field menu is a nested context menu | Each field row carries its own `ContextMenu` inside the node's trigger; radix-vue calls `preventDefault()` on a handled `contextmenu` and the ancestor checks `defaultPrevented`, so the node menu stays shut. Verified with a real mouse over CDP, not only synthetic events. |

Emits and labels: `expand-column` (`LineageNode.vue:308`) handled by `handleExpandColumn` (`LineageGraphPage.vue:1648`); i18n keys `lineageGraph.expandColumnUpstream` / `expandColumnDownstream` in both locales.

## Invariants

- The trail never fetches. It is a function of the relations already loaded, and a field whose ancestors were never expanded shows a one-hop trail.
- `fieldScopedRelations(relations, direction, fields)` (`lineageGraph.ts:182`) matches the field on **this node's side** — the target's field for an upstream relation, the source's for a downstream one — and that side flips with the direction.
- One stored relation is counted once however many responses report it, and the two writers of one relation (analyzer vs OpenLineage) are counted apart.
- A sibling column is never on the trail — the directed walk, not an undirected one, is what makes that true.

## Failure modes / known limits

| What a reader should not expect | Why |
| --- | --- |
| Following a field through a `JOIN` | The relation names no target column, so the walk stops. Continuing would be a heuristic about column identity, not a fact the store records. |
| A field expansion narrowing the canvas | It only ever *adds* relations the field names; hiding relations another expansion drew needs a filter, not an expansion. |
| Expanding a field whose side the store never named | What the expansion would draw comes from the loaded relations; a `JOIN` key or table-level edge names no column, so the menu offers nothing there once the direction is loaded. The graph's field list has the same blind spot. |
| The detail panel's related runs following the field | They stay one hop, by decision. |
| A trail that is not cut short | Past the relation budget the trail reports `truncated` rather than silently showing less. |

## Open items

- There is no `GetColumnTrail` RPC: the trail is walked over the drawn graph only. Following a field automatically would need one, rather than a round trip per level.
- Every field row carries its own radix `ContextMenu` root, mounted only while a menu is open. That is fine for the dozens of fields the dev estate has, and worth revisiting if a card ever lists hundreds.

## Where things live

| Path | Holds |
| --- | --- |
| `frontend/src/lib/lineageTrail.ts` | `collectFieldTrail` (`:152`), `columnStep` (`:276`), `walkColumnExpansion` (`:333`), `ColumnPair`/`columnPairKey`, `ColumnWalk`, `FieldTrail`. Pure. |
| `frontend/src/lib/lineageGraph.ts` | `LineageDirection` (`:38`), `fieldScopedRelations` (`:182`), `buildLineageEdges` with `highlightedEdgeIds` (`:213`). |
| `frontend/src/pages/LineageGraphPage.vue` | `expandedDirections`/`expandedColumns` (`:464,471`), `fieldScopesFor` (`:984`), `addColumnExpansion` (`:997`), `directionView` (`:1259`), `markSelectedColumn` (`:1565`), `handleExpandColumn` (`:1648`), `fitViewWhenMeasured` (`:1696`). |
| `frontend/src/components/lineage/LineageNode.vue` | `onTrail` ring and `dimmed` step-back (`:354,357`), inline trail columns (`:363`), field list with `nowheel` (`:120`), per-field menu (`:156-169`), reveal (`:389`). |
| Tests | `frontend/src/lib/lineageTrail.test.ts` (17 cases: chain, diamond, cycle, join stop, drawn-graph bound, duplicate relation, two writers, cap, empty trail, `columnStep`, `walkColumnExpansion`), `frontend/src/lib/lineageGraph.test.ts` (`fieldScopedRelations`). Gate: `pnpm --dir frontend biome:check && pnpm --dir frontend lint && pnpm --dir frontend type-check && pnpm --dir frontend test run`. |
