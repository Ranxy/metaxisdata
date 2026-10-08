# Plan: Field-Scoped Expansion on the Lineage Graph

## TL;DR

A node's `Expand upstream` / `Expand downstream` draws everything that direction
holds, whatever fields are involved. Right-clicking one of the node's **fields**
now offers `Expand column upstream` / `Expand column downstream` instead: the walk
only follows relations that name that field, so a neighbour is drawn only when the
field's lineage actually reaches it. The field is selected as part of the action,
so the trail it just drew is the one highlighted on the canvas.

Shipped in `feat/column-expand`.

## Decisions

- **A direction is expanded whole or field by field, never both.** The two live in
  separate sets on the page (`expandedDirections` / `expandedColumns`, keyed
  `direction:guid`); `graphView()` keeps every relation once the direction is whole,
  only the ones a field names while the expansion is that field's, and nothing until
  something is expanded there. A whole-node expansion therefore supersedes the field
  scopes recorded before it, which is exactly the upgrade path a reader wants.
- **The node's own Expand stays on offer after a field expansion.** "Show me this
  field's lineage" narrows the canvas; "show me the whole object" widens it back, and
  hiding the second because the first happened would strand the reader with Reset as
  the only way out.
- **The walk is the trail walk, one hop at a time.** `columnNeighbourPairs` is the
  step `collectFieldTrail` already takes: upstream follows only relations that name
  the pair as their *target*, downstream only ones that name it as their *source*. It
  stops where the trail stops — a relation that reaches the far object without naming
  a field of it is on the field's lineage, and its object is drawn, but the walk
  cannot continue through a field it does not know. The dev estate stores every `JOIN`
  with an empty `target_column`, so this is the common case, not a corner.
- **The depth control bounds a field expansion exactly as it bounds a node one.** The
  walk fetches each level before the next, so the metadata type of a neighbour is
  known by the time it is asked for, and it is deduplicated across the walk.
- **Fields accumulate.** Expanding a second field adds a scope rather than replacing
  the first; the union is what the direction draws. There is no per-field "undo" —
  Reset is the way back, as it already is for node expansions.
- **An item that would do nothing is not there.** A field already expanded that way
  is not offered again, and neither side is offered while the node itself has
  expanded that direction — the same rule the node's menu already follows. On the
  root, whose two directions are expanded on arrival, a field menu therefore carries
  View Schema and nothing else.
- **The field is selected by the expansion.** Expansion alone is invisible when the
  field has nothing on that side; selecting it fills the detail panel's `Selected
  field` and `Field trail` sections either way, and highlights the relations the
  action just drew. `markSelectedColumn` points the panel and the trail at a field
  without redrawing, so the action pays for one redraw (`rebuildGraph`), not two.
- **The field menu is a nested context menu.** Each field row carries its own
  `ContextMenu` inside the node's trigger; radix-vue's trigger calls
  `preventDefault()` on a handled `contextmenu`, and its ancestor's handler checks
  `defaultPrevented`, so the node menu stays shut. Verified with a real mouse (CDP
  `Input.dispatchMouseEvent`), not only with synthetic events: one right-click on a
  field opens exactly one menu, and it is the field's.

## Where it lives

| File | Change |
| --- | --- |
| `frontend/src/lib/lineageGraph.ts` | `LineageDirection` moves here from the page. `fieldScopedRelations(relations, direction, fields)` keeps the relations that name one of `fields` on **this node's side** — the target's field for an upstream relation, the source's for a downstream one. |
| `frontend/src/lib/lineageTrail.ts` | `ColumnPair` / `columnPairKey` are exported (the trail's private `pairKey` became public). `columnNeighbourPairs(data, pair, direction)` is the one-hop step a field expansion walks. |
| `frontend/src/components/lineage/LineageNode.vue` | One context menu per field row: `View Schema`, then `Expand column upstream` / `Expand column downstream`, each hidden when it would do nothing (`canExpandColumn`). New `columnExpandedUpstream` / `columnExpandedDownstream` data and an `expand-column` emit. |
| `frontend/src/pages/LineageGraphPage.vue` | `expandedColumns`, `addColumnExpansion`, `directionView`, `handleExpandColumn`, and `markSelectedColumn` (the field click's select half, factored out of `handleSelectColumn`). Reset and object change clear the field scopes too; `hasExpandedBeyondRoot` counts them, so a field expansion earns the Reset that takes it back. |
| `frontend/src/locales/{en-US,zh-CN}.json` | `lineageGraph.expandColumnUpstream` / `expandColumnDownstream`. |

## Testing

`lineageGraph.test.ts` covers `fieldScopedRelations`: the field is matched on the
node's own side, and which side that is flips with the direction. `lineageTrail.test.ts`
covers `columnNeighbourPairs`: the far object's own field is the next pair, a relation
naming another field is not followed, and a relation that names no field of the far
object yields no next pair.

On the dev estate, `v_customer_360.paid_amount` upstream is **1 object** while the
node's upstream is 4; `dwd_order_fact.paid_amount` upstream is 1 object at depth 1 and
2 at depth 2 (`payments.amount → v_customer_payments.paid_amount → dwd_order_fact.paid_amount`).
Verified through the page in a real Chrome over CDP: the field menu holds no node-level
items, exactly the named field's relations are drawn, the trail and the detail panel
follow the field, the node's `Expand upstream` afterwards draws all four objects and
then leaves the field menu with nothing to expand, and Reset returns the graph to its
initial twelve nodes.

## Not covered

- No filtering: a field expansion only ever *adds* the relations that field names. It
  cannot hide relations another expansion already drew — narrowing an expanded canvas
  back down needs a filter, not an expansion.
- A field whose relations the server stored without naming that column (a `JOIN` key,
  a table-level edge) has no upstream or downstream to expand, so its menu offers
  nothing on that side. The graph's field list has the same blind spot: it names the
  columns the loaded relations mention, and a relation with an empty column mentions
  none.
