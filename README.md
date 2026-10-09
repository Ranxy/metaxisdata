> **Language / 语言:** [English](README.md) | [中文](README_zh.md)

# Metaxisdata

Metaxisdata is a self-hosted data governance and metadata platform. It connects
to MySQL/TiDB and PostgreSQL instances, syncs their schemas into a searchable
metadata registry, and derives table- and column-level lineage from SQL
definitions and ingested OpenLineage events.

## Features

- **Metadata registry** — Sync the schemas of your instances into one
  searchable registry. Every table and column carries its DDL and a history of
  what changed.
- **Table and column lineage** — Lineage is analyzed from view,
  materialized-view, and manual SQL, precise down to the column. Expand the
  graph by depth, or follow a single column's trail.
- **OpenLineage** — Ingest run events, map namespaces, and issue API keys.
  Browse jobs, datasets, and events, with links back to Airflow.
- **SQL explanation** — LLM-assisted explanation of a SQL statement, scoped to
  the metadata you select, with caching.
- **Access control and audit** — Users, groups, named roles, and an
  access-policy editor. Audited operations are recorded in an audit log that is
  kept forever.
- **Agent integration** — The `mxd` CLI
  ([cli/README.md](cli/README.md)) gives an agent one JSON document per
  command, stable exit codes, and device login. With MCP enabled, the registry
  is also available as read-only tools behind an OAuth 2.1 authorization server
  ([docs/mcp.md](docs/mcp.md)).

## Quick start

You need Docker and a PostgreSQL database the container can reach. Images are
published to GHCR for `linux/amd64` and `linux/arm64`:

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://user:password@db-host:5432/metaxisdata?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:latest
```

Without Docker at hand, every release also publishes the same server as
static binaries for five platforms — download one from GitHub Releases and
run it directly; the deploy guide's *Get the server* section shows how.

Open `http://<host>:8083` and:

1. **Create the first account.** It becomes the workspace administrator.
2. **Add an instance and sync its schema.**

Optional next steps:

- **Close self-service signup.** Turn on *Disallow self-service signup* in
  *Settings* → *General*, so only administrators can create users.
- **Serve HTTPS.** The container serves plain HTTP by design. Terminate HTTPS
  in a reverse proxy in front of it, and set `METAXISDATA_TRUSTED_PROXIES` to
  the proxy's address so the audit log records clients instead of the proxy.
- **Protect stored credentials.** Set `METAXISDATA_ENCRYPTION_KEY` so a
  database dump alone cannot decrypt the credentials stored for your instances
  — and keep that key safe: losing it leaves the stored credentials unreadable.
- **Connect MCP clients.** Enable the MCP endpoint in *Settings* → *General*
  — it requires the workspace external URL — then follow
  [docs/mcp.md](docs/mcp.md).

> The complete deployment guide — every environment variable, the health and
> version endpoints, the prebuilt binary, and building the image or the binary
> yourself — is [docs/deploy.md](docs/deploy.md).

## Trying it locally

[docker-compose.yml](docker-compose.yml) starts PostgreSQL and the server
together, so one command is enough to look at a built image on your own
machine:

```bash
make docker-up      # build the image, then start PostgreSQL + the server on http://localhost:8083
make docker-down    # stop both; the compose data volume is kept
```

Open <http://localhost:8083> and create the first account. The stack makes
taking a build for a spin easy — published port, hardcoded development
password — and is not meant for production. Details and caveats are in
[docs/local-trial.md](docs/local-trial.md).

## Screenshots

**Metadata browser** — the table list of a synced database, with row counts,
sizes, columns, and indexes.

![Metadata browser: the table list of a synced database, with row counts, sizes, columns and indexes](docs/images/metaxisdata_metadata_small.png)

**Table and column lineage** — a table's upstream and downstream edges,
expanded down to per-column trails.

![Lineage graph: a table's upstream and downstream edges, expanded down to column-level field trails](docs/images/metaxisdata_lineage_small.png)

## How it works

- One Go server binary serves the ConnectRPC API and the frontend — a Vue 3
  single-page application embedded in the image — on port 8083. When MCP is
  enabled, the MCP endpoint is served on the same port.
- PostgreSQL is the only external dependency. The schema migrates itself on
  startup, so a fresh database needs nothing but a connection URL.
- Background runners sync schemas, analyze and validate lineage, and run
  maintenance tasks.

## Development

```bash
# Backend — requires PG_URL; port 8083 matches the Vite proxy
PG_URL='postgres://dev:dev@localhost:5432/metaxisdata?sslmode=disable' go run ./backend/bin/server/main.go --debug

# Frontend — Node 24+, Vite on :3000, proxying API, OAuth, and MCP routes to the backend
pnpm --dir frontend install
pnpm --dir frontend dev

# Build
make build           # dev profile
make build-release   # release profile
make build-embed     # release profile with the SPA embedded in the binary
make build-cli       # the mxd client, ./build/mxd
```

A build without the embed tag serves a placeholder page instead of the SPA:
run the Vite dev server, or build with `make build-embed`.

`go test ./...` needs no database. The integration suites run the real server
against PostgreSQL and MySQL behind the `integration` build tag, and skip when
Docker is unavailable:

```bash
make test-integration-smoke
make test-integration
```

Development conventions, lint rules, and the full command set:
[AGENTS.md](AGENTS.md).

## Tech stack

- **Backend**: Go, PostgreSQL, ConnectRPC (gRPC/HTTP), Protobuf / buf
- **Frontend**: Vue 3, TypeScript, Vite, Tailwind CSS, shadcn-vue
- **CLI**: Go