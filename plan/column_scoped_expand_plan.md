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
  separate maps on the page (`expandedDirections` / `expandedColumns`, keyed
  `direction:guid`); `graphView()` keeps every relation once the direction is whole,
  only the ones a field names while the expansion is that field's, and nothing until
  something is expanded there. A whole-node expansion therefore supersedes the field
  scopes recorded before it, which is exactly the upgrade path a reader wants.
- **The node's own Expand stays on offer after a field expansion.** "Show me this
  field's lineage" narrows the canvas; "show me the whole object" widens it back, and
  hiding the second because the first happened would strand the reader with Reset as
  the only way out.
- **The walk is the trail walk, one hop at a time.** `columnStep` is the step
  `collectFieldTrail` already takes: upstream follows only relations that name the pair
  as their *target*, downstream only ones that name it as their *source*. It stops where
  the trail stops — a relation that reaches the far object without naming a field of it
  is on the field's lineage, and its object is drawn, but the walk cannot continue
  through a field it does not know. The dev estate stores every `JOIN` with an empty
  `target_column`, so this is the common case, not a corner. The step also reports those
  objects, because they are drawn: they are what `walkColumnExpansion` puts on the canvas
  and what the view has to frame.
- **The depth control bounds a field expansion exactly as it bounds a node one, and a
  field remembers how deep it was taken.** The walk fetches each level before the next,
  so the metadata type of a neighbour is known by the time it is asked for, and it is
  deduplicated across the walk. Each visited pair records the levels that were still
  below it (`depth` in `expandedColumns`), which is what lets the menu offer a field
  again once the control asks for more than it has already drawn — without it, raising
  the depth to 2 and clicking the same field would silently do nothing.
- **Fields accumulate.** Expanding a second field adds a scope rather than replacing
  the first; the union is what the direction draws. There is no per-field "undo" —
  Reset is the way back, as it already is for node expansions.
- **An item that would do nothing is not there.** Neither side is offered while the node
  itself has expanded that direction, and a field is offered only when the page can see
  that expanding it would draw something: the field names a relation on this node's side,
  and its walk is not already as deep as the control asks. A direction that has not been
  fetched answers `null` rather than "nothing there", because hiding an item that works is
  worse than showing one that turns out to have nothing to draw. On the root, whose two
  directions are expanded on arrival, a field menu therefore carries View Schema and
  nothing else.
- **The field is selected by the expansion.** Expansion alone is invisible when the
  field has nothing on that side; selecting it fills the detail panel's `Selected
  field` and `Field trail` sections either way, and highlights the relations the
  action just drew. `markSelectedColumn` points the panel and the trail at a field
  without redrawing, so the action pays for one redraw (`rebuildGraph`), not two.
- **Every node the expansion draws opens its field list.** The field that ties a
  revealed card to the pivot is the thing the reader came to see, and a card that has
  to be clicked open first hides it. The page therefore owns whether a list is on
  screen (`fieldsVisible` in the node data, from `fieldsVisibleGuids`) rather than the
  card keeping a private flag — a node can be opened by an expansion it did not ask
  for. `walkColumnExpansion` reports every object it draws, the pairs it only lands on
  at the last level and the ones a relation reaches without naming a field included.
- **The view frames what the click touched, not the whole graph.** The pivot and the
  nodes it reached are fitted (`fitView({ nodes })`), so a card opened near the bottom
  of the canvas — where its list used to run off the edge, or under the minimap — is
  brought back into the visible area instead of being left where the click found it.
  The fit reads the card heights off the DOM first, because Vue Flow's own measurement of
  a card that grew in place lags; it watches only the nodes it is about to fit, and waits
  at most ~300ms, so a click cannot hang on a card whose two heights never agree.
- **The field in hand is scrolled into view, once per request.** An open list is
  alphabetical and capped at 200px, so on a wide table the field the graph is about sorts
  below the fold. The card brings that row into view — the field the reader just clicked
  when this card is the pivot, otherwise the field the trail runs through — only when it
  is out of view, and only far enough to reach the edge. It runs on a page-bumped
  `revealToken` rather than on every render, so a redraw for any other reason leaves a
  reader where they scrolled to. It re-checks over the next frames because the card is
  restacked while an expansion finishes. The correction is computed in layout pixels
  (`scrollTop`'s own unit) from screen-space rects scaled by the canvas' zoom.
- **The list takes the wheel (`nowheel`).** Vue Flow's zoom handler claims every wheel
  event that does not pass through an element marked `nowheel`, so without the class a
  wheel over a field list zoomed the graph and left the 1px scrollbar as the only way to
  read the rest of a long list.
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
| `frontend/src/lib/lineageTrail.ts` | `ColumnPair` / `columnPairKey` are exported (the trail's private `pairKey` became public). `columnStep(data, pair, direction)` is the one-hop step a field expansion walks, and `walkColumnExpansion({ start, direction, depth, load })` is the whole walk over it: the pairs to scope with the depth each was walked to, and every object it draws. `load` is the caller's fetch, so the walk is a pure function of what the server answers and is unit-tested without a page. |
| `frontend/src/components/lineage/LineageNode.vue` | One context menu per field row: `View Schema`, then `Expand column upstream` / `Expand column downstream`, each hidden when the page says it would do nothing (`canExpandColumn` over `columnExpandableUpstream/Downstream`). The field list renders from `data.fieldsVisible` and carries `nowheel`, and the card reveals the field in hand when the page bumps `revealToken` (`revealTrailField`, re-checked over the next frames). `expand-column` emit, `LineageDirection` from `lib/`. |
| `frontend/src/pages/LineageGraphPage.vue` | `expandedColumns` (field → depth), `addColumnExpansion`, `expandableColumns`, `fieldScopesFor`, `directionView`, `handleExpandColumn` over `walkColumnExpansion`, `openFieldLists`, `requestFieldReveal`, `markSelectedColumn` (the field click's select half, factored out of `handleSelectColumn`), and `fitViewWhenMeasured` bounded by wall clock. Reset, the depth control and an object change all reach the cards: the first two refresh what a field expansion would offer, the last closes every list. `hasExpandedBeyondRoot` counts field scopes, so a field expansion earns the Reset that takes it back. |
| `frontend/src/locales/{en-US,zh-CN}.json` | `lineageGraph.expandColumnUpstream` / `expandColumnDownstream`. |

## Testing

`lineageGraph.test.ts` covers `fieldScopedRelations`: the field is matched on the
node's own side, and which side that is flips with the direction. `lineageTrail.test.ts`
covers `columnStep` (the far object's own field is the next pair; a relation naming
another field is not followed; a relation that names no field of the far object yields
the object and no next pair) and `walkColumnExpansion`: the depth each pair is scoped
to at depth 1 and at depth 3, a diamond walked once, the last level's objects still
reported, the object of a table-level edge reported without being walked through, and
one `load` per object visited.

On the dev estate, `v_customer_360.paid_amount` upstream is **1 object** while the
node's upstream is 4; at depth 2 the same click reaches
`payments.amount → v_customer_payments.paid_amount → v_customer_360.paid_amount` and
opens all three field lists on `amount` / `paid_amount` / `paid_amount`.
Verified through the page in a real Chrome over CDP: the field menu holds no node-level
items, exactly the named field's relations are drawn, the trail and the detail panel
follow the field, the node's `Expand upstream` afterwards draws all four objects and
then leaves the field menu with nothing to expand, and Reset returns the graph to its
initial twelve nodes.

The reveal was checked where it is hardest: `dwd_order_fact.region` upstream reaches
`v_order_base`, a 31-field table whose list is 200px of a 744px column with `region` at
index 22. After the click the card's list came up scrolled to `region` (scrollTop 354 =
the row's own offset), and the fit brought both cards inside the canvas — the pivot's
list is the one that used to run off the bottom edge. The menu rules were checked
against the store: `first_order_date` (a target-only field) offers upstream and not
downstream, `paid_amount` offers upstream again once the depth control is raised to 2,
and a wheel over an open list scrolls it (0 → 161) while the canvas zoom stays put.

## Not covered

- No filtering: a field expansion only ever *adds* the relations that field names. It
  cannot hide relations another expansion already drew — narrowing an expanded canvas
  back down needs a filter, not an expansion.
- What a field expansion would draw is answered from the relations already fetched; a
  direction nobody has fetched keeps both items rather than guessing. A field whose
  relations the server stored without naming *it* on this side (a `JOIN` key, a
  table-level edge) therefore has nothing to expand, and its menu offers nothing there
  once that direction is loaded. The graph's field list has the same blind spot: it
  names the columns the loaded relations mention, and a relation with an empty column
  mentions none.
- Every field row carries its own radix `ContextMenu` root. Content is mounted only
  while a menu is open, so the cost is one root per row on a card whose list is open —
  fine for the dozens of fields the dev estate has, worth revisiting if a card ever
  lists hundreds.
