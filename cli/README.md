# mxd

`mxd` is the command line client for a metaxisdata server. It is built for
agents: every command writes **exactly one JSON document to stdout**, and
progress, warnings and errors go to **stderr**. Failures carry a stable `code`
and a stable exit status, so a caller can decide what to do next without parsing
prose.

## Install and sign in

```console
$ make build-cli                 # produces ./build/mxd
$ mxd auth login --server https://mx.example.com
```

The command prints a URL and a code. The URL already carries the code, so
opening it goes straight to the confirmation screen; `mxd` polls until you
decide, then stores the token in `~/.config/metaxisdata/config.json` (mode
0600). Pass `--no-prefill-url` to print the bare page address instead, for a
workflow where the code has to be typed.

The confirmation screen always shows the code next to the client name and
version, the source address and the time, and always requires an explicit
decision, so a request you did not start is still recognisable.

The address comes from the workspace's **external URL** setting. A fresh or
locally run workspace usually has none, and rather than leave you with nothing to
open, `mxd` falls back to `<server>/device`. That is correct whenever the server
also serves the web application; if you run the SPA separately (the usual local
setup, where Vite serves it on `:3000`), set the workspace external URL to that
address and the server will name the right page itself.

The server address is saved **with** the token, because a token is issued by one
server; later commands do not need `--server` again. `mxd auth logout` revokes
the token and keeps the address.

`--login-timeout` (default 10m) bounds the wait for that approval. It is
separate from `--timeout` (default 30s), which bounds a single request: the
approval waits for a person, not for a server.

To keep several identities on one machine, point each one at its own file:

```console
$ METAXISDATA_CONFIG=/tmp/agent-a/mxd.json mxd auth login --server https://mx.example.com
```

For CI, `mxd auth login --service-account <email>` exchanges a service account's
key, read from `METAXISDATA_SERVICE_KEY`, for an API token. That token lasts an
hour, so a long-lived job should inject `METAXISDATA_TOKEN` instead.

## Analysis scopes

A SQL statement cannot be resolved without a database context: the statement
names an object down to `database.schema.table` at most, and never names the
instance. Every `lineage sql` call therefore needs at least one scope.

Scopes are read from **`METAXISDATA_SCOPES`**, only for the duration of the
command:

```console
$ export METAXISDATA_SCOPES='dev=1;shop,prod=9;shop;public'
```

Each entry is either a bare GUID or `name=guid`. `--scope` narrows the selection
for one call; it accepts a configured name, a GUID, or `all`.

**Scopes are never written to a file.** Several agents can share a machine while
serving different projects, and a remembered scope set would be silently
overwritten by whichever agent ran last. Which scopes a project uses is a
per-project decision: keep the list in that project's own documentation and
export it before invoking `mxd`.

A scope is resolved against every listed scope **independently**, and the results
are reported per scope. Selecting several scopes does not merge contexts and does
not discover cross-environment relations that are absent from the metadata.

### Where the GUIDs come from

Do not construct a GUID by hand: the segment layout differs per engine (the
MySQL-family schema segment is empty, PostgreSQL needs it). Copy the values the
server reports.

```console
$ mxd database list --instance 1          # each row carries "guid", e.g. "1;shop"
$ mxd meta list '1;shop;' --type SCHEMA   # each row carries "guid", e.g. "1;shop;public"
```

## Commands

```console
mxd auth login [--server <url>] [--no-browser] [--no-prefill-url] [--login-timeout 10m]
mxd auth status
mxd auth logout

mxd config show          # effective server, credential source and scopes
mxd config path

mxd instance list
mxd database list [--instance <id>]

mxd meta list <parent-guid> [--type TABLE]
mxd meta get <guid> [--type]
mxd meta search <keyword> [--type] [--parent-guid-prefix]
mxd meta ddl <guid> [--type]

mxd lineage sql [--scope <name|guid|all>]... --file <path|-> [--depth N] [--include-temp]
mxd lineage graph <guid> [--depth N] [--direction up|down|both] [--column <name>]

mxd skill install [--dir <path>]
mxd skill show

mxd version
```

### Teaching your agent

`mxd` carries an agent skill and installs it for you:

```console
$ mxd skill install
{"changed": true, "name": "mxd-cli", "path": "/home/u/.agents/skills/mxd-cli/SKILL.md"}
```

The skill is embedded in the binary, so a machine that has `mxd` needs no
checkout to give its agent the same instructions. It lands in
`~/.agents/skills/mxd-cli/SKILL.md`, where the agent runtime discovers it;
`--dir` targets another location, such as `~/.claude/skills` or a project's own
`.agents/skills`. An existing file is replaced, so re-running it after an
upgrade is how a machine picks up new wording.

`mxd skill show` prints the same document as JSON, for a harness that would
rather capture it than read it from a file.

`mxd config show` is the way to answer "which configuration is this process
actually using": it reports every effective value together with the layer it
came from (`flag`, `env:...`, `file:...`, or `none`).

Listings page through every response. When a listing hits `--max-items`
(default 1000) the envelope says `"truncated": true` and carries
`"nextPageToken"`. `meta list` without `--type` is the one exception: its
response carries a page token per meta type rather than one for the whole
response, so walking further needs `--type`.

## Reading the output

```console
$ mxd lineage sql --file etl.sql --depth 2
{
  "results": [
    {
      "scopeName": "dev",
      "scopeGuid": "1;shop",
      "relations": [
        {"sourceGuid": "1;shop;;orders", "sourceColumn": "amount",
         "targetGuid": "1;shop;;order_daily", "targetColumn": "amount",
         "isTemp": false}
      ],
      "graphs": [{"rootGuid": "1;shop;;order_daily", "nodes": [], "edges": []}],
      "warnings": []
    }
  ],
  "warnings": []
}
```

The result is grouped by scope even for a single scope, so a caller never has to
branch on how many scopes it asked for.

**Temporary relations are hidden by default.** Such a relation's target only
exists inside the statement. It matters in exactly one situation: when the
statement's result is not written anywhere, which is every bare `SELECT`. As soon
as the statement has a real target — a view, a table it inserts into — the
synthetic relations are duplicates of that target and the server drops them, so
nothing is hidden.

That means the default can leave a scope with no relations to show. When it does,
the entry carries `"tempRelationsHidden": N` and a warning naming the flag, so an
empty list never reads as "this statement has no lineage":

```json
{"results": [{"scopeName": "dev", "scopeGuid": "1;shop", "relations": [],
              "tempRelationsHidden": 2,
              "warnings": ["all 2 relations of this scope are temporary: ... pass --include-temp to see them"]}],
 "warnings": []}
```

`--include-temp` reports them; `targetGuid` is then empty and `targetColumn` is
the query's output alias. The flag only affects what is shown: the `--depth`
expansion never followed a temporary target, because a target that exists only
inside the statement has no graph to walk.

### The JSON shape

Protobuf messages are rendered with `protojson`, the same encoding the REST
gateway and the audit log use, so field names are lowerCamelCase (`sourceGuid`,
not `source_guid`) and enums are names (`"TABLE"`, not `4`). Unset fields are
emitted rather than omitted — an absent list is `[]`, an unset string is `""`,
a false flag is `false` — so the shape of the document does not change with the
data and a caller never has to tell "missing" from "empty".

## Exit codes

| Code | Meaning | What to do |
| --- | --- | --- |
| 0 | Success | — |
| 1 | Bad input: flags, an empty statement, `server_required`, `scope_required`, `config_invalid` | Fix the invocation; the error carries a `hint` |
| 2 | Not authenticated: the token is missing, expired or revoked, or the device login session is gone or was denied | `mxd auth login` |
| 3 | Not found | Check the GUID |
| 4 | Permission denied | Ask an administrator |
| 5 | Could not complete: `unavailable` (the server was not reachable), `resource_exhausted`, or a server fault | Retry, then check that the server is running and read its logs |
| 6 | Timeout or cancellation, local or remote, including `--login-timeout` running out | Retry, or raise `--timeout` / `--login-timeout` |

The error envelope also carries a `code` string, which is finer grained than the
exit status: `server_required`, `scope_required` and `config_invalid` all exit 1
but name different fixes, and a server that is not reachable reports
`unavailable` rather than the `internal` a fault of its own would give.

## Notes

- `mxd` talks ConnectRPC with a bearer token, so it is unaffected by the
  cookie-based CSRF protection the web session uses.
- `--ca-cert <pem>` trusts an extra certificate for a self-signed deployment;
  `--insecure` skips verification and prints a warning.
- The token is stored in clear text. It is equivalent to a seven day session;
  the CLI never prints it.
