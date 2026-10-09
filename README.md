> **Language / 语言:** [English](README.md) | [中文](README_zh.md)

> **Note:** The only published artifact is a container image, for `linux/amd64` and `linux/arm64`; no prebuilt binaries are released.

# Metaxisdata

Metaxisdata is a **self-hosted data governance and metadata platform**. It syncs
the schemas of your database instances into a central metadata registry and
derives **table- and column-level lineage** from SQL definitions and from
ingested OpenLineage events.

## What it does

- **Metadata registry**: Sync the schemas of your instances into one searchable
  registry, with each object's DDL beside it and a history of what changed.
- **Table and column lineage**: Column-precise lineage analysed from view,
  materialised-view and manual SQL, expandable by depth and by field.
- **OpenLineage**: Ingest OpenLineage run events, map namespaces, issue API keys,
  and browse jobs, datasets and events with links back to Airflow.
- **IAM and audit**: Users, groups, named roles and an access-policy editor;
  audited operations are recorded in an audit log that is kept forever.
- **Agent integration**: The `mxd` CLI gives an agent one JSON document per
  command, stable exit codes and device login ([cli/README.md](cli/README.md)).
  With MCP enabled, the registry is also available as read-only tools behind an
  OAuth 2.1 authorization server ([docs/mcp.md](docs/mcp.md)).

## Architecture overview

Metaxisdata is one Go server binary backed by PostgreSQL:

- The server serves the ConnectRPC API, its REST gateway and the Vue 3 SPA —
  embedded in the released image — and, when enabled, the MCP endpoint.
- PostgreSQL is the only external dependency. The schema migrates itself on
  startup, so a fresh database needs nothing but a connection URL.
- Background runners sync schemas, analyse and validate lineage, and do
  maintenance work.
- `mxd` is a separate artifact and is never part of the server image.

## Quick start

### Production deployment

The image is published to GHCR on every GitHub Release, for `linux/amd64` and
`linux/arm64`:

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://user:password@db-host:5432/metaxisdata?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:latest
```

If the pull fails, no release has been published yet: `make docker-build` builds
the same image from a checkout.

Then open `http://<host>:8083` — the port published above — and:

1. **Prepare PostgreSQL** and point `PG_URL` at it; the schema migrates itself on
   the first start.
2. **Create the first account immediately.** It becomes the workspace
   administrator, and signup stays open until an administrator turns on
   *Disallow self-service signup* in *Settings* → *General*.
3. **Add an instance** in the UI and sync its schema.
4. **Optional**: turn on MCP in *Settings* → *General* — which also needs the
   workspace *external URL*.
5. **Terminate HTTPS in a reverse proxy** in front of the server: the image
   serves plain HTTP on purpose. Set `METAXISDATA_TRUSTED_PROXIES` to the
   proxy's address, or the audit log records the proxy as the source of every
   request.

> The full deployment guide — every environment variable, the health and version
> endpoints, and how to build the image yourself — is
> [docs/deploy.md](docs/deploy.md).

### Try it locally

No PostgreSQL of your own? [docker-compose.yml](docker-compose.yml) starts both,
so `make docker-up` is enough to look at a built image:

```bash
make docker-up      # build, then start PostgreSQL + the server on http://localhost:8083
make docker-down    # stop both; `docker compose down -v` also drops the store
```

Open <http://localhost:8083> and create the first account — it becomes the
workspace administrator. The port is published and the development password is
hardcoded, so treat it as a way to look at a built image, not as a production
topology. The compose file, running a published image instead, and the `:dev`
tag caveat are in [docs/local-trial.md](docs/local-trial.md).

## Screenshots

**Metadata browser** — every table of a synced database, with row counts, sizes, columns and indexes.

![Metadata browser: the table list of a synced database, with row counts, sizes, columns and indexes](docs/images/metaxisdata_metadata_small.png)

**Table and column lineage** — upstream and downstream edges, expandable down to per-column trails.

![Lineage graph: a table's upstream and downstream edges, expanded down to column-level field trails](docs/images/metaxisdata_lineage_small.png)

## Development

```bash
# Backend — requires PG_URL; port 8083 matches the Vite proxy
PG_URL='postgres://dev:dev@localhost:5432/metaxisdata?sslmode=disable' go run ./backend/bin/server/main.go --debug

# Frontend — Vite on :3000, proxying the API, OAuth and MCP routes to the backend
pnpm --dir frontend install
pnpm --dir frontend dev

# Build
make build           # dev profile
make build-release   # release profile
make build-embed     # release profile with the SPA embedded in the binary
make build-cli       # the mxd client, ./build/mxd
```

A plain build does not embed the SPA: it serves a placeholder page, so run the
Vite dev server, or build with `make build-embed`.

`go test ./...` is hermetic and needs no database. The integration suites below
run the real server against PostgreSQL and MySQL behind the `integration` build
tag, and skip when Docker is unavailable:

```bash
make test-integration-smoke
make test-integration
```

Development conventions, lint rules and the full command set are in
[AGENTS.md](AGENTS.md).

## Tech stack

- **Backend**: Go, PostgreSQL, ConnectRPC (gRPC/HTTP), Protobuf / buf
- **Frontend**: Vue 3, TypeScript, Vite, Tailwind CSS, shadcn-vue
- **CLI**: Go
