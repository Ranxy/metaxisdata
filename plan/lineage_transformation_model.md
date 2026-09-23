# Lineage transformation model: design decisions

> **Status: decided and implemented.** This records the model conflict the
> PostgreSQL review raised as **D3** and the decisions taken with it, so the next
> person to touch `model.Transformation` knows which of its shape is deliberate.
>
> Scope: `backend/plugin/lineage/model`, the four analyzers that produce
> transformations, `column_lineage.transformation` and the proto/frontend that
> render them.

## The conflict D3 named

`model.ColumnRelation` is a triple — (source column, target column,
`[]Transformation`) — while the analyzers collect output columns, each with
sources. A first implementation attached one transformation list to the *output
column* and shared it across the column's sources. That is wrong the moment two
sources of one output column were produced differently, and a set operation is
exactly that shape: `SELECT a + 1 AS x FROM t1 UNION ALL SELECT b + 2 AS x FROM
t2` makes one output column `x` whose two sources come from two expressions. The
column-level list gave `t2.b` the transformation `a + 1`, which no query
contains.

## Decision 1: a transformation travels with the source it came from

`scope.OutputColumn` holds `Sources []scope.ColumnSource`, and each
`ColumnSource` is `{Ref, Transform}`. There is no transformation on the output
column. An output column built from one expression still shares one list across
its sources, but that is a property of that expression, not of the model.

Consequence: an output column's sources can disagree about how they produced it,
which is the truth. `algorithm.MergeSetOpColumns` relies on it, and each arm's
sources keep their own list.

## Decision 2: one derivation is one edge, and the transformation is part of its identity

The graph is a set of edges. Two edges that connect the same columns are distinct
when their transformation lists differ, because they are different derivations:
`SELECT x + 1 AS a, x + 2 AS a FROM t` writes `a` twice from two expressions, and
`INSERT … ON CONFLICT DO UPDATE SET b = EXCLUDED.b` derives one target column both
from the proposed row and from the conflict path. The identity is compared
structurally — endpoints as identifier values, the list field by field through
`model.SameTransformations` — never through a rendering, so two spellings of the
same list cannot collide and one list cannot be split.

## Decision 3: the list is ordered, and the first entry is the outermost operation

`model.RelationTypeOf` reads `transform[0]`. The analyzers therefore put the
operation a consumer sees first: for a set-operation arm, the chain of set
operations that combine it into the result, outermost first
(`algorithm.ArmChain`). The alternative — deriving the relation type from the
whole list — would need a precedence table per dialect and would still have to
answer "which one does the UI show".

## Decision 4: a set operation records whether it keeps duplicate rows

`Transformation.All` distinguishes `UNION ALL` from `UNION`, and `INTERSECT ALL`
/ `EXCEPT ALL` from their plain forms. It is a field rather than a second
operation value because the *kind* is still union/intersect/except: the relation
type, the icon and the grouping in every consumer key off the kind, and the
quantifier is one bit on top of it. Every dialect's AST exposes it
(`SelectStmt.All`, `SetStmt.SetAll`, `SetOpStmt.All`), so the flag is never
inferred.

## What the model deliberately does not express

- **A recursive CTE's fixpoint.** A `WITH RECURSIVE` body's self-reference is
  registered as the CTE itself and contributes no lineage, because the analysis
  is a single pass. The alternative is an iterative fixpoint over the same scope
  graph, which changes the complexity of every analyzer for one statement shape.
- **A set operation's arm order or its duplicate-elimination point.** The chain
  says which operations combined the arm, not where rows were de-duplicated;
  a consumer that needs that has to read the SQL.
- **Expression structure.** `Expression` is text. A structured expression would
  have to be modelled once per dialect and versioned with them; the text is what
  the UI shows and what a human verifies against the SQL.
- **Column-level grouping of transformations.** An edge is one source's path; the
  grouping a reader wants ("this output column came from these columns") is
  reconstructed per output column by the consumer, as the transformation cell
  does.
- **A `SORT` or `GROUP_BY` producer.** Both exist as operation values for the
  OpenLineage subtype mapping; no SQL analyzer emits them.

## Where the data goes

`column_lineage.transformation` is a JSONB column (`NOT NULL DEFAULT '[]'`)
holding the plain `json.Marshal` of the `[]model.Transformation` list — this is
one of the columns whose shape comes from `backend/store`, not from a
`proto/store` message, so the JSON keys are the struct's `json` tags and not
protojson's camelCase. Reading goes through `backend/store`, and the API maps the
list to `v1pb.Transformation` (`convertTransformations`), which the frontend
renders in `LineageTransformationCell.vue`.

Adding a field is therefore backward-compatible for stored rows — an absent key
reads as the zero value — but the model, the proto, the mapping and the cell have
to move together, which is what `All` did.
