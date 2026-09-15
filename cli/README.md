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

The command prints a URL and a code. Open the URL, sign in, and type the code on
the confirmation page. `mxd` polls until you decide, then stores the token in
`~/.config/metaxisdata/config.json` (mode 0600).

The address comes from the workspace's **external URL** setting. A fresh or
locally run workspace usually has none, and rather than leave you with nothing to
open, `mxd` falls back to `<server>/device`. That is correct whenever the server
also serves the web application; if you run the SPA separately (the usual local
setup, where Vite serves it on `:3000`), set the workspace external URL to that
address and the server will name the right page itself.

The server address is saved **with** the token, because a token is issued by one
server; later commands do not need `--server` again. `mxd auth logout` revokes
the token and keeps the address.

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
mxd auth login [--server <url>] [--no-browser] [--prefill-url]
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

mxd lineage sql [--scope <name|guid|all>]... --file <path|-> [--depth N]
mxd lineage graph <guid> [--depth N] [--direction up|down|both] [--column <name>]

mxd version
```

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
branch on how many scopes it asked for. A relation whose `targetGuid` is empty
only exists inside the statement (the result of a bare `SELECT`); `targetColumn`
still carries the output alias.

## Exit codes

| Code | Meaning | What to do |
| --- | --- | --- |
| 0 | Success | — |
| 1 | Bad input: flags, an empty statement, `server_required`, `scope_required` | Fix the invocation; the error carries a `hint` |
| 2 | Not authenticated: the token is missing, expired or revoked, or the device login session is gone | `mxd auth login` |
| 3 | Not found | Check the GUID |
| 4 | Permission denied | Ask an administrator |
| 5 | Server error | Retry, then check the server logs |
| 6 | Timeout or cancellation | Retry, or raise `--timeout` |

## Notes

- `mxd` talks ConnectRPC with a bearer token, so it is unaffected by the
  cookie-based CSRF protection the web session uses.
- `--ca-cert <pem>` trusts an extra certificate for a self-signed deployment;
  `--insecure` skips verification and prints a warning.
- The token is stored in clear text. It is equivalent to a seven day session;
  the CLI never prints it.
