---
name: mxd-cli
description: Answers questions about a metaxisdata deployment with the mxd command line client — what a table or view looks like, what a SQL statement reads and writes, and what depends on what. Use when the user asks about schema structure, column-level or table-level lineage, up/downstream impact, where a column comes from, or hands over a SQL statement to analyze, and expects an answer from the platform rather than from the source files. Do not use it to read a codebase; open the files directly for that.
---

# Querying metaxisdata with `mxd`

`mxd` is the client for a metaxisdata deployment. It answers metadata and lineage
questions from the registry, so prefer it over reading SQL files by hand: the
registry is what the lineage runner actually analyzed.

`mxd --help` and `mxd <command> --help` are the authoritative reference for
flags. This skill is the agent-facing condensation: the rules that decide
whether a call works, and the shapes to parse. `mxd skill show` prints it.

## The three rules that decide success

### 1. Signing in needs a human

`mxd auth login --server <url>` prints a URL and a code. **Someone must approve
the request in a browser.** An agent cannot complete this alone.

- `mxd auth status` says who the CLI is signed in as, if anyone.
- If a command exits **2**, the token is gone: ask the user to run
  `mxd auth login`, then retry. Do not retry in a loop.

### 2. Analysis scopes come from the user, never from you

A SQL statement is resolved against a *scope* — one database, or one schema. The
statement itself cannot say which instance it belongs to, so there is no way to
guess correctly.

Scopes are read from `METAXISDATA_SCOPES`, which the user (or their harness)
exports. **Never invent a scope, and never construct a GUID.**

- Missing scope → exit **1**, `code: "scope_required"`. Report the `hint` to the
  user verbatim and **ask which scopes this project uses**. Do not try a nearby
  database, and do not fall back to guessing.
- `mxd config show` prints the scopes this process actually has, with
  `scopeSource` = `env:METAXISDATA_SCOPES` or `none`. Check it before concluding
  anything about a scope error.
- `--scope <name>` narrows the selection for one call; `--scope all` uses every
  configured one. Both are per-invocation, and neither invents a scope.

### 3. GUIDs are copied from output, never built

A GUID is `instance;database;schema;object`, but the layout depends on the
engine — MySQL-family databases have an *empty* schema segment (`1;shop;;orders`)
while PostgreSQL needs it (`1;shop;public;orders`). Hand-building one produces a
GUID that silently matches nothing.

Get them from the server:

```bash
mxd database list --instance 1          # each row: name, guid, ...
mxd meta list '1;shop' --type SCHEMA    # each row carries its own guid
mxd meta search orders --type TABLE     # search by name when the GUID is unknown
```

`meta search` is the entry point when you only have a name.

## Workflow: discover → locate → analyze

| Step | Command | Gives you |
|---|---|---|
| Find the object | `mxd meta search <keyword> [--type TABLE]` | GUIDs matching a name |
| List a level | `mxd meta list <parent-guid> [--type TABLE]` | direct children, each with a GUID |
| Read the structure | `mxd meta get <guid> [--type]` | columns, indexes, keys, partitions |
| Read the DDL | `mxd meta ddl <guid> [--type]` | the definition as the source engine reports it |
| Trace stored lineage | `mxd lineage graph <guid> --depth 3 --direction both` | the multi-level graph |
| Analyze a statement | `mxd lineage sql --file q.sql [--depth N]` | what the statement reads and writes |

```bash
# Structure of a table, then what depends on it.
mxd meta search orders --type TABLE
mxd meta get '1;shop;;orders' --type TABLE
mxd lineage graph '1;shop;;orders' --depth 3 --direction down

# A statement. --file is required; use - for stdin. There is no positional SQL.
mxd lineage sql --file etl.sql --depth 2
mxd lineage sql --file - <<'SQL'
INSERT INTO daily (amount) SELECT amount FROM orders;
SQL
```

## Reading the output

stdout is **exactly one JSON document**; progress, warnings and errors go to
**stderr**. Parse stdout only.

- Field names are lowerCamelCase (`sourceGuid`), enum values are names
  (`"TABLE"`, `"DIRECT"`), and unset fields are present rather than omitted: an
  empty list is `[]`, an unset string is `""`.
- Lists page themselves; `"truncated": true` plus `"nextPageToken"` means the
  cap (`--max-items`, default 1000) was reached.
- `lineage sql` always groups by scope, even for one scope:
  `{"results": [{"scopeName", "scopeGuid", "relations", "graphs", "warnings"}]}`.

### Temporary relations

`lineage sql` hides relations whose target exists only inside the statement.
They matter in one case: the statement's result is not written anywhere — any
bare `SELECT`. Then the entry says so, rather than looking empty:

```json
{"scopeName": "dev", "scopeGuid": "1;shop", "relations": [],
 "tempRelationsHidden": 2,
 "warnings": ["all 2 relations of this scope are temporary: ... pass --include-temp to see them"]}
```

If the question is "which tables does this query read", that answer is already
in the relations; if it is "where do these output columns come from", re-run
with `--include-temp` — the `targetColumn` is then the output alias.

### What the graph is

`lineage graph` is **column-level**, and the table-level view is the collapse of
those edges. An object whose SQL yields no column-level relation at all — a bare
`SELECT count(*)`, or something never synced — does not appear at all.
`--direction up` is upstream (where data comes from), `down` is downstream
(what it feeds); `distance` is negative upstream, positive downstream.
`--column <name>` keeps only the edges touching that column.

## Failure playbook

| Exit | `code` | What it means | What to do |
|---|---|---|---|
| 1 | `scope_required` | no analysis scope configured | ask the user which scopes this project uses |
| 1 | `server_required` | no server address yet | ask the user to run `mxd auth login --server <url>` |
| 1 | `invalid_argument` | bad flags, or an empty/oversized statement | fix the call; the envelope carries a `hint` |
| 1 | `config_invalid` | the credentials file is unreadable | report the path; the user must fix or remove it |
| 2 | `unauthenticated` | token missing, expired, or revoked | `mxd auth login` (a human approves) |
| 2 | `device_session_expired` | the login request expired or was denied | start a new `mxd auth login` |
| 3 | `not_found` | no such GUID, or a wrong `--type` | re-run `meta search`; check the type |
| 4 | `permission_denied` | the account lacks the permission | tell the user; do not retry |
| 5 | `unavailable` | the server is not reachable | retry later; it is not a bad request |
| 5 | `resource_exhausted` | rate limited | back off, do not retry immediately |
| 6 | `timeout` | the request or `--login-timeout` ran out | retry, or raise `--timeout` |

Every failure is one JSON object on stderr: `{"error": {"code", "message",
"hint"}}`. Read `code`; `hint` is written to be passed on to the user.

## Notes that save a round trip

- `--format table` is for humans. Leave the default JSON when parsing.
- `--type` disambiguates a GUID that could be several things; `meta search`
  already returns the type, so pass what it returned.
- `--depth` on `lineage graph` is 1–10 (default 3); on `lineage sql` it is 0–10
  and means levels *beyond* the statement's own relations.
- `--scope all` is a convenience, not a default: prefer asking the user for the
  one or two scopes the question is actually about.
- Selecting several scopes runs the analysis once per scope and reports each
  separately. It does **not** merge contexts, and it does not reveal
  cross-environment flow that is absent from the registry.
- A table-level question ("what feeds this table") is answered by the collapsed
  graph, not by `is_temp` relations; `--direction up --depth 2` is usually
  enough.
