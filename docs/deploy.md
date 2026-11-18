> **Language / 语言:** [English](deploy.md) | [中文](deploy_zh.md)

# Deploying Metaxisdata

Metaxisdata ships as a single self-contained binary: the server with the web
app embedded, running the release profile. Every release publishes it two
ways — a container image on GHCR, and prebuilt binaries on GitHub Releases
for hosts that run the binary directly — and both deliver the same server.
PostgreSQL is the only external dependency, and the schema migrates itself
on startup, so an empty database needs nothing but a connection URL.

The server serves plain HTTP on port 8083. For production, terminate HTTPS
in a reverse proxy in front of it — step 4.

## Prerequisites

- PostgreSQL, reachable from the server, with the database already created.
- A database user allowed to create tables **and extensions** in it: the
  migrations run `CREATE EXTENSION IF NOT EXISTS pg_trgm` for the metadata
  search index (step 2).
- Docker — there are no other host dependencies; everything needed at runtime
  is inside the image.
- For the prebuilt binary, no Docker and no other runtime dependency either:
  the binary is static and embeds the web app. Outgoing TLS needs a CA
  certificate bundle, and an MSSQL source configured with a `timezone`
  parameter needs a zone database (IANA tzdata) — the image ships both for
  these two reasons; mainstream distributions provide them already.
- To build either artifact yourself: the scripts under [scripts/](../scripts/)
  describe their toolchain requirements and their version, proxy and mirror
  settings in their own headers. The image build needs BuildKit (Docker
  20.10+); the binary build needs the Go toolchain, Node 24 (the frontend's
  `engines` field) and the frontend's pinned pnpm release, which
  [scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh) resolves via
  corepack.

## 1. Get the server

Both channels publish the same artifact: the SPA is embedded, the prod
profile is compiled in, and `GET /api/version` reports the release the binary
came from. Pick the one that fits the host.

### Pull the released image

Every release publishes the image to GHCR for `linux/amd64` and `linux/arm64`.
Pull the alias you want to track:

```bash
docker pull ghcr.io/ranxy/metaxisdata:latest
```

`latest` follows the newest release that is not a prerelease. Every release
also publishes its release tag, the bare semver, the minor line and a
`sha-<commit>` tag; the [releases page](https://github.com/Ranxy/metaxisdata/releases)
names the newest release, and its tag can replace `latest` to pin a version.
A prerelease publishes its own tags but never moves an alias.

### Build the image yourself

[scripts/build_metaxisdata_docker.sh](../scripts/build_metaxisdata_docker.sh)
builds the image from
[scripts/docker/Dockerfile.server](../scripts/docker/Dockerfile.server), and
`make docker-build` runs the same script. A release build tags the image with
`VERSION` and also claims `:latest`; `--dev` builds the dev profile for local
experiments only. The image contains the server binary only — the `mxd` CLI is
a separate artifact, built with `make build-cli`. The proxy, registry and
mirror knobs are documented in the script's header; `APK_MIRROR` is written
verbatim into the image's `/etc/apk/repositories` and visible in
`docker history`, so it must never contain credentials.

### Download the prebuilt binary

For hosts that do not run Docker, every release publishes static binaries for
five platforms, plus a `SHA256SUMS` listing every asset's checksum:

| Platform | Asset |
| --- | --- |
| Linux (amd64) | `metaxisdata-linux-amd64` |
| Linux (arm64) | `metaxisdata-linux-arm64` |
| macOS (Apple Silicon) | `metaxisdata-darwin-arm64` |
| macOS (Intel) | `metaxisdata-darwin-amd64` |
| Windows (amd64) | `metaxisdata-windows-amd64.exe` |

Download the asset for your platform from the release's download path:

```bash
curl -fsSL -o metaxisdata \
  https://github.com/Ranxy/metaxisdata/releases/latest/download/metaxisdata-linux-amd64
chmod +x metaxisdata
```

`releases/latest/download/…` resolves to the newest release that is **not** a
prerelease, so a prerelease will not answer it; name the tag in the URL
instead — `…/releases/download/<tag>/<asset>`. On Windows, use `curl.exe` and
the `.exe` asset.

The downloaded binary is the one [scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh)
produces: SPA embedded, prod profile, and `metaxisdata --version` reports the
release tag.

### Build the binary yourself

[scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh) builds the SPA
and compiles the self-contained binary (`make build-binary` runs the same
script). `--release-assets` produces the five release-platform assets and
`SHA256SUMS` in one invocation, with an explicit `GOOS`/`GOARCH` per target;
`--dev` builds the dev profile for local experiments only. The script's header
documents the build metadata and network settings it reads.

## 2. Prepare PostgreSQL

Point `PG_URL` at an existing database. The migrator creates its tables on the
first start, so an empty database is enough:

```
postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable
```

The migrations also create one extension, for the metadata search index:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

`pg_trgm` is standard contrib and ships with every supported PostgreSQL
distribution. If the application user may not create extensions, run that
statement as an administrator first; the migration then finds it already
present.

## 3. Run the server

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:latest
```

The container runs as uid 1001 and reports health at `/healthz`:

```bash
curl -fsS http://localhost:8083/healthz
```

### Run the binary directly

The prebuilt binary (or `build/metaxisdata` from
[scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh)) takes the
same `PG_URL` and runs in the foreground — put it behind your service manager
(systemd, launchd, NSSM) just like the container is behind Docker:

```bash
PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ./metaxisdata --port 8083
```

`metaxisdata --version` prints the version, commit, and build time. Upgrading
is replacing the binary and restarting: pending migrations apply on startup,
so keep the PostgreSQL backup discipline of the container flow (the notes
below).

**Never run an old and a new binary side by side.** A binary that predates the
connected-app refresh credential deletes and reinserts the grant row on a
refresh, which silently turns a revoked connection back into an unrevoked one.
Stop every old replica before starting the new binary. Rolling back has the
same effect: if you must, restore the pre-upgrade database backup and rotate
the deployment's signing secret (the `AUTH_SECRET` setting) — every outstanding
token stops verifying, and revocations made after the backup are gone with it,
so revoke the affected connected apps again.

The `PG_URL` and encryption-key variables below are read by the server itself
and work identically for the binary. The other `METAXISDATA_*` variables are
the image entrypoint's mappings — on a bare host, pass the matching flags
instead: `--port`, `--debug`, `--enable-json-logging`, `--cors-allow-origins`,
`--trusted-proxies`, `--metrics-addr`.

```bash
PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ./metaxisdata --trusted-proxies 10.0.0.0/8 --cors-allow-origins https://metaxisdata.example.com
```

### Run one server process

Run a single server process per metadata database. The background runners (schema
sync, lineage analysis, ingested-lineage revalidation, maintenance) keep their
queues, per-database sync gates, user-visible sync operations and revalidation
state in memory, so a second replica would duplicate every sync and analysis
against the same instances and split each user's sync-result messages between
processes. Scaling the metadata database does not make the server replicable;
scale PostgreSQL, not the process. A restart loses queued work but converges on
the next scan. See [security-posture.md](security-posture.md).

### First steps

1. Open `http://<host>:8083` and create the first account. It becomes the
   workspace administrator.
2. Optional: turn on **Disallow self-service signup** in *Settings* →
   *General*, so only administrators can create users.
3. Add an instance and sync its schema.
4. To serve the registry to MCP clients, enable the MCP endpoint on the same
   page. It requires the workspace external URL; see [mcp.md](mcp.md).

### Environment variables

| Environment variable | Description |
| --- | --- |
| `PG_URL` | PostgreSQL connection URL (required). |
| `METAXISDATA_PORT` | Listening port, default 8083. Change the port here rather than with `--port`: the image's health check follows this variable only. |
| `METAXISDATA_ENCRYPTION_KEY` | Optional. By default, the key that encrypts stored source credentials is kept in the database itself, so anyone who gains access to the database can decrypt every credential. Set it to have that data key wrapped by a key kept outside the database — then a leaked database alone no longer exposes the stored credentials. The cost: the key is then the only way to open them. If it is lost, the server refuses to start, and every stored credential — for every instance and every LLM provider — is unreadable and must be entered again. See [security-posture.md](security-posture.md). |
| `METAXISDATA_ENCRYPTION_KEY_PREVIOUS` | Previous keys, comma-separated, during a rotation. |
| `METAXISDATA_JSON_LOGGING=true` | JSON logs instead of text. |
| `METAXISDATA_DEBUG=true` | Debug-level logging, and exposes `/debug/pprof` to anyone who can reach the server. |
| `METAXISDATA_CORS_ALLOW_ORIGINS` | Comma-separated browser origins allowed to call the API with credentials. Only needed when the SPA is served from a different origin; empty installs no CORS middleware at all. |
| `METAXISDATA_TRUSTED_PROXIES` | Comma-separated peer IPs/CIDRs whose `X-Forwarded-For` may be believed when recording the audit source address (step 4). |
| `METAXISDATA_METRICS_ADDR` | Optional. Address for the Prometheus endpoint, for example `127.0.0.1:9090`; unset disables it. The endpoint is anonymous and is **never** served on the API port, so bind it to an interface your scraper can reach and nothing else. See [Prometheus metrics](#prometheus-metrics). |

Two further variables are host settings rather than deployment configuration,
and the server reads them from its environment: `SSH_AUTH_SOCK`, the ssh-agent
socket the SSH tunnel uses when a data source carries no inline SSH private
key, and `TZ`, the process time zone that log timestamps follow (unset means
UTC).

### Health and build metadata

| Endpoint | Answers |
| --- | --- |
| `GET /healthz` | `OK` while the process serves requests. The image's `HEALTHCHECK` polls it. |
| `GET /api/version` | The `version`, `git_commit`, and `build_time` the image was built from. The SPA's user menu and Settings → General read it. |

Both endpoints are anonymous and reveal nothing beyond which release is
running.

### Prometheus metrics

Set `METAXISDATA_METRICS_ADDR` (or pass `--metrics-addr` when running the binary
directly) to serve Prometheus metrics on a dedicated listener:

```bash
PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ./metaxisdata --port 8083 --metrics-addr 127.0.0.1:9090
```

The listener serves only `GET` and `HEAD` on `/metrics`; it is not a second API,
and the API port never serves metrics. The endpoint is pull-only and anonymous,
so keep the address on a trusted interface or an internal network, and point
Prometheus at `http://<metrics-addr>/metrics`. `--debug` only controls logging
and pprof.

The endpoint exports the standard Go and process metrics, `metaxisdata_build_info`
with the release version and commit, and the server's own `metaxisdata_*`
series: schema syncs, lineage analyses, OpenLineage ingestion, data-quality
executions, LLM calls, HTTP/RPC traffic, queue depths and the metadata
connection pool. Labels are bounded enums such as engine, status, result, reason,
method and route; instance, database, table, GUID, SQL and user data never appear
in a label, so the label set cannot grow with the registry.

Counters are process-local: a restart resets them, and because the deployment is
one server process there is no second replica to sum with.

### Operational notes

- Back up the database, not the container: the container mounts no volume and
  keeps no local state.
- The image needs one writable path, `/tmp`, for a scratch file the MSSQL
  driver uses. On a hardened deployment, use `--read-only --tmpfs /tmp`; a
  bare `--read-only` removes the only writable directory the image has.
- The entrypoint maps the environment variables above onto server flags and
  passes the rest of the command through unchanged. An explicit argument wins
  over a mapped one, so `docker run … ghcr.io/ranxy/metaxisdata:latest --debug`
  still works.

## 4. Terminate HTTPS in a reverse proxy

Put a reverse proxy with HTTPS in front of the server. Then tell the server
the proxy's address, so the audit log records the client instead of the
proxy — set `METAXISDATA_TRUSTED_PROXIES` to the proxy's address or CIDR:

```bash
-e METAXISDATA_TRUSTED_PROXIES=10.0.0.0/8
```

Without it, the audit log records the proxy's address as the source of every
request.

`METAXISDATA_TRUSTED_PROXIES` is the image entrypoint's mapping of the
flag; a binary run directly takes the flag itself
(`./metaxisdata --trusted-proxies 10.0.0.0/8`).

The notification stream needs no proxy timeout tuning: the server sends
keep-alive heartbeats, so an idle connection outlives a proxy's default read
timeout.

## Local trial

Running a throwaway instance on your own machine is a different job from
deploying one: [local-trial.md](local-trial.md) covers the compose stack and
how to point it at a published image.
