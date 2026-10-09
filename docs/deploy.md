> **Language / 语言:** [English](deploy.md) | [中文](deploy_zh.md)

# Deploying Metaxisdata

Metaxisdata ships as a single container image: the server binary with the web
app embedded, running the release profile. PostgreSQL is the only external
dependency, and the schema migrates itself on startup, so an empty database
needs nothing but a connection URL.

The container serves plain HTTP on port 8083. For production, terminate HTTPS
in a reverse proxy in front of it — step 4.

## Prerequisites

- PostgreSQL, reachable from the server, with the database already created.
- A database user allowed to create tables **and extensions** in it: the
  migrations run `CREATE EXTENSION IF NOT EXISTS pg_trgm` for the metadata
  search index (step 2).
- Docker — there are no other host dependencies; everything needed at runtime
  is inside the image.
- To build the image yourself: BuildKit (Docker 20.10+; recent Docker Desktop
  and Engine enable it by default) and outbound access for the Go modules and
  the npm install.

## 1. Get the image

### Pull the released image

Every release publishes the image to GHCR for `linux/amd64` and `linux/arm64`.
Pull the tag you want to track:

```bash
docker pull ghcr.io/ranxy/metaxisdata:v1.2.3   # the release tag as published
docker pull ghcr.io/ranxy/metaxisdata:1.2.3    # the bare semver
docker pull ghcr.io/ranxy/metaxisdata:1.2      # the minor line
docker pull ghcr.io/ranxy/metaxisdata:latest   # the newest non-prerelease
```

- `v1.2.3` and `1.2.3` name that exact release.
- `1.2` follows the newest release on the minor line.
- `latest` follows the newest release that is not a prerelease. Prereleases
  publish their own tags, such as `v1.2.3-rc.1`, but never move an alias — a
  deployment tracking `latest` never receives a release candidate.
- Every release also carries a `sha-<commit>` tag, pinned to its exact commit.

### Build the image yourself

[scripts/build_metaxisdata_docker.sh](../scripts/build_metaxisdata_docker.sh)
builds the image from
[scripts/docker/Dockerfile.server](../scripts/docker/Dockerfile.server), and
`make docker-build` runs the same script:

```bash
scripts/build_metaxisdata_docker.sh                  # release profile -> :dev and :latest
VERSION=v1.2.3 scripts/build_metaxisdata_docker.sh   # release profile -> :v1.2.3 and :latest
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
  ghcr.io/ranxy/metaxisdata:v1.2.3
```

The container runs as uid 1001 and reports health at `/healthz`:

```bash
curl -fsS http://localhost:8083/healthz
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
  over a mapped one, so `docker run … ghcr.io/ranxy/metaxisdata:v1.2.3 --debug`
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

The notification stream needs no proxy timeout tuning: the server sends
keep-alive heartbeats, so an idle connection outlives a proxy's default read
timeout.

## Local trial

Running a throwaway instance on your own machine is a different job from
deploying one: [local-trial.md](local-trial.md) covers the compose stack and
how to point it at a published image.