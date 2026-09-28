# Querying metaxisdata over MCP

This deployment serves its metadata registry and its lineage as read-only tools.
They answer from what the schema sync and the lineage runner actually analyzed, so
prefer them over reading SQL files by hand.

## How objects are addressed

An object is named the way a person would name it:

```json
{"instance": "prod-mysql", "database": "shop", "schema": "public", "name": "orders"}
```

Every value may be a name or the id a listing returned, and `schema` is only
needed on engines that have a named schema level. A `guid` from an earlier result
works too and short-circuits the rest — but you never have to build one: the
server resolves names itself, because the identifier layout differs per engine.

When a name matches nothing, or several things, the tool refuses and returns
`details.candidates` instead. That is a hint about the next call, not a failure to
work around: pick from the candidates, or narrow the reference.

## Which tool answers what

| Question | Tool |
| --- | --- |
| Which instances are registered? | `list_instances` |
| Which databases does an instance have? | `list_databases` |
| Where does this name live? | `search_metadata` |
| What is under this database or schema? | `list_metadata` |
| What does this table look like? | `get_metadata` |
| What is this object's definition? | `get_ddl` |
| What does this SQL statement read and write? | `analyze_sql` |
| What feeds this table, or what does it feed? | `get_lineage_graph` |
| What may I read? | `whoami` |

`search_metadata` is the entry point when you have only a name: it returns each
match's `guid`, `metaType` and `parentGuid`, which are what the other tools take.

## Analyzing a statement

A statement's unqualified names cannot say which instance they belong to, so
`analyze_sql` takes the scope explicitly:

```json
{"sql": "INSERT INTO daily (amount) SELECT amount FROM orders",
 "scopes": [{"instance": "prod-mysql", "database": "shop"}],
 "depth": 2}
```

Send more than one scope and the statement is analyzed once per scope,
independently: results are grouped by scope and never merged, because merging
scopes that may live on different instances would invent relationships the
registry does not have. Leave the scope out and the tool answers with the
databases that exist rather than guessing one.

The answer splits a statement's relations into two fields:

- `relations` — the real edges, into objects that exist in the registry;
- `temporaryRelations` — edges whose target exists only inside the statement. For
  a bare `SELECT` these are the answer to "where do these output columns come
  from": `targetColumn` is the query's output alias.

`diagnostics` reports what the analyzer could not represent — a statement shape it
does not model, or a reference it could not resolve — beside the relations it did
resolve, so a partial analysis never reads as a complete one. A statement that does
not parse at all fails the scope instead.

`depth` (0–10, default 0) also expands each resolved target into a lineage graph,
attached to its scope as `graphs`.

## Reading a lineage graph

`get_lineage_graph` is column-level; the table-level view is the collapse of those
edges, so an object whose definition yields no column-level relation — a
`SELECT count(*)`, or something never synced — does not appear at all. `direction`
picks `up` (where data comes from), `down` (what it feeds) or `both`, `depth`
(1–10, default 3) says how far to walk, and `column` keeps only the edges touching
one column.

## What a failure means

Every failure is one JSON object with a stable `code`:

| `code` | Meaning | What to do |
| --- | --- | --- |
| `invalid_argument` | a missing or malformed argument | fix the call; the message names the argument |
| `not_found` | no object matches the reference | use `details.candidates`, or search first |
| `ambiguous` | the name matches several objects | pick one from `details.candidates`, or pass its `guid` |
| `scope_required` | `analyze_sql` has no scope | pass one; `details.databases` lists what exists |
| `permission_denied` | the caller lacks the permission | report it; another tool may answer part of the question |
| `unauthenticated` | the client's authorization is gone | the client must authorize again |
| `unavailable` | the server could not be reached | retry later; it is not a bad request |
| `resource_exhausted` | rate limited | back off before retrying |
| `timeout` | the request ran out of time | retry, or ask for less at once |
| `internal` | a server fault | retry once, then report it |

Each failure also carries a `hint` written to be passed on to the user, and the
tools are annotated read-only: nothing here changes the workspace.
