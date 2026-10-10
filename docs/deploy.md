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
- To build the image yourself: BuildKit (Docker 20.10+; recent Docker Desktop
  and Engine enable it by default) and outbound access for the Go modules and
  the npm install.
- To build the binary yourself: the Go toolchain, Node 24 (the frontend's
  `engines` field) and the frontend's pinned pnpm release
  (scripts/build_metaxisdata.sh resolves it via corepack), with outbound access
  for the Go modules and the npm install.

## 1. Get the server

Both channels publish the same artifact: the SPA is embedded, the prod
profile is compiled in, and `GET /api/version` reports the release the binary
came from. Pick the one that fits the host.

### Pull the released image

Every release publishes the image to GHCR for `linux/amd64` and `linux/arm64`.
These are the tags the newest release, `v0.1.0`, published; pull the one you
want to track:

```bash
docker pull ghcr.io/ranxy/metaxisdata:v0.1.0   # the release tag as published
docker pull ghcr.io/ranxy/metaxisdata:0.1.0    # the bare semver
docker pull ghcr.io/ranxy/metaxisdata:0.1      # the minor line
docker pull ghcr.io/ranxy/metaxisdata:latest   # the newest non-prerelease
```

- `v0.1.0` and `0.1.0` name that exact release.
- `0.1` follows the newest release on the minor line.
- `latest` follows the newest release that is not a prerelease. Prereleases
  publish their own tags, such as `v0.2.0-rc.1`, but never move an alias — a
  deployment tracking `latest` never receives a release candidate.
- Every release also carries a `sha-<commit>` tag, pinned to its exact commit.

### Build the image yourself

[scripts/build_metaxisdata_docker.sh](../scripts/build_metaxisdata_docker.sh)
builds the image from
[scripts/docker/Dockerfile.server](../scripts/docker/Dockerfile.server), and
`make docker-build` runs the same script:

```bash
scripts/build_metaxisdata_docker.sh                  # release profile -> :dev and :latest
VERSION=v0.1.0 scripts/build_metaxisdata_docker.sh   # release profile -> :v0.1.0 and :latest
make docker-build                                    # same as the first command

scripts/build_metaxisdata_docker.sh --dev            # dev profile, local experiments only
make docker-build-dev
```

Every build tags the image under `IMAGE` (default `metaxisdata/metaxisdata`);
only a release build also applies `:latest`. The image contains the server
binary only — the `mxd` CLI is a separate artifact, built with
`make build-cli`.

Build options:

| Variable | Effect |
| --- | --- |
| `VERSION` | Image tag, and the version the binary reports. Default `dev`. |
| `IMAGE` | Image name. Default `metaxisdata/metaxisdata`. |
| `BUILD_PROXY` | Proxy for the Go module download and the npm install. |
| `GOPROXY` | Go module proxy. Defaults to this machine's `go env GOPROXY`. |
| `NPM_REGISTRY` | npm registry. Defaults to this machine's `npm config get registry`. |
| `APK_MIRROR` | Alpine CDN replacement, e.g. `https://mirrors.aliyun.com/alpine`. The value is written verbatim into `/etc/apk/repositories` inside the image and is visible in `docker history`, so it must never contain credentials. |

Do not export `HTTPS_PROXY` for the build: BuildKit injects the standard proxy
variables into every stage, including the runtime stage, credentials included.
`BUILD_PROXY` is a custom argument that only the build stages declare, so the
value you pass stays confined to the build.

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

```bash
# Linux (amd64)
curl -fsSL -o metaxisdata https://github.com/Ranxy/metaxisdata/releases/latest/download/metaxisdata-linux-amd64
chmod +x metaxisdata

# Linux (arm64)
curl -fsSL -o metaxisdata https://github.com/Ranxy/metaxisdata/releases/latest/download/metaxisdata-linux-arm64
chmod +x metaxisdata

# macOS (Apple Silicon)
curl -fsSL -o metaxisdata https://github.com/Ranxy/metaxisdata/releases/latest/download/metaxisdata-darwin-arm64
chmod +x metaxisdata

# macOS (Intel)
curl -fsSL -o metaxisdata https://github.com/Ranxy/metaxisdata/releases/latest/download/metaxisdata-darwin-amd64
chmod +x metaxisdata
```

```powershell
# Windows (PowerShell)
curl.exe -fsSL -o metaxisdata.exe https://github.com/Ranxy/metaxisdata/releases/latest/download/metaxisdata-windows-amd64.exe
```

The `releases/latest/download/…` URL resolves to the newest release that is
**not** a prerelease, so a prerelease will not answer it; name the tag in the
URL instead — `…/releases/download/<tag>/metaxisdata-linux-amd64`.

The downloaded binary is the one [scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh)
produces: SPA embedded, prod profile, and `metaxisdata --version` reports the
release tag.

### Build the binary yourself

[scripts/build_metaxisdata.sh](../scripts/build_metaxisdata.sh) builds the SPA
and compiles the self-contained binary (`make build-binary` runs the same
script):

```bash
scripts/build_metaxisdata.sh                # release profile -> build/metaxisdata
VERSION=v0.1.0 scripts/build_metaxisdata.sh
make build-binary                           # same as the first command

scripts/build_metaxisdata.sh --dev          # dev profile, local experiments only

scripts/build_metaxisdata.sh --release-assets   # the five release-platform assets + SHA256SUMS -> build/
```

The binary reports its version, commit, and build time from the
`VERSION`, `GIT_COMMIT`, and `BUILD_TIME` environment variables, so the last
command with the release tag set reproduces exactly what
[release-binaries.yml](../.github/workflows/release-binaries.yml) uploads —
that workflow is one script invocation plus the upload; `--dev` builds are
for local experiments only.

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
  ghcr.io/ranxy/metaxisdata:v0.1.0
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

The `PG_URL` and encryption-key variables below are read by the server itself
and work identically for the binary. The other `METAXISDATA_*` variables are
the image entrypoint's mappings — on a bare host, pass the matching flags
instead: `--port`, `--debug`, `--enable-json-logging`, `--cors-allow-origins`,
`--trusted-proxies`.

```bash
PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ./metaxisdata --trusted-proxies 10.0.0.0/8 --cors-allow-origins https://metaxisdata.example.com
```

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
| `METAXISDATA_DEBUG=true` | Debug-level logging, and exposes `/debug/pprof` and `/metrics` to anyone who can reach the server. |
| `METAXISDATA_CORS_ALLOW_ORIGINS` | Comma-separated browser origins allowed to call the API with credentials. Only needed when the SPA is served from a different origin; empty installs no CORS middleware at all. |
| `METAXISDATA_TRUSTED_PROXIES` | Comma-separated peer IPs/CIDRs whose `X-Forwarded-For` may be believed when recording the audit source address (step 4). |

### Health and build metadata

| Endpoint | Answers |
| --- | --- |
| `GET /healthz` | `OK` while the process serves requests. The image's `HEALTHCHECK` polls it. |
| `GET /api/version` | The `version`, `git_commit`, and `build_time` the image was built from. The SPA's user menu and Settings → General read it. |

Both endpoints are anonymous and reveal nothing beyond which release is
running.

### Operational notes

- Back up the database, not the container: the container mounts no volume and
  keeps no local state.
- The image needs one writable path, `/tmp`, for a scratch file the MSSQL
  driver uses. On a hardened deployment, use `--read-only --tmpfs /tmp`; a
  bare `--read-only` removes the only writable directory the image has.
- Without `TZ`, the clock and every log timestamp are UTC.
- The entrypoint maps the environment variables above onto server flags and
  passes the rest of the command through unchanged. An explicit argument wins
  over a mapped one, so `docker run … ghcr.io/ranxy/metaxisdata:v0.1.0 --debug`
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