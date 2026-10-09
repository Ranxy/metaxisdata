> **Language / 语言:** [English](deploy.md) | [中文](deploy_zh.md)

# Deploy

Metaxisdata deploys as a single container: the server binary with the built SPA
embedded, running the release (prod) profile. All state lives in PostgreSQL, and
the schema migrates itself on startup, so an empty database needs nothing but a
connection URL.

The container serves plain HTTP on 8083. For production, put a reverse proxy with
HTTPS in front of it (§4).

## Prerequisites

- PostgreSQL, reachable from the server, with a database that already exists.
- A user that may create tables **and extensions** in it — the migrations run
  `CREATE EXTENSION IF NOT EXISTS pg_trgm` for the metadata search index (§2).
- To pull the released image: none — `docker pull` is enough.
- To build the image yourself: Docker with BuildKit (Docker 20.10+; recent
  Docker Desktop and Engine enable it by default), and outbound access for the
  Go modules and the npm install.

## 1. Get the image

### 1a. Pull the released image (recommended)

Every GitHub Release publishes an image to GHCR for `linux/amd64` and
`linux/arm64`. Pull the tag you want to track:

```bash
docker pull ghcr.io/ranxy/metaxisdata:v1.2.3   # the release tag as published
docker pull ghcr.io/ranxy/metaxisdata:1.2.3    # the bare semver
docker pull ghcr.io/ranxy/metaxisdata:1.2      # the minor line
docker pull ghcr.io/ranxy/metaxisdata:latest   # the newest non-prerelease
```

`:latest` only moves to a release that is not a prerelease, so a deployment
tracking it is never handed a release candidate. Every release also carries a
`sha-` tag, if you would rather pin an exact commit.

If the pull fails, no release has been published yet — build the image instead
(§1b).

### 1b. Build the image yourself

The image is built by
[scripts/build_metaxisdata_docker.sh](../scripts/build_metaxisdata_docker.sh)
from [scripts/docker/Dockerfile.server](../scripts/docker/Dockerfile.server).
`make docker-build` runs the same script:

```bash
scripts/build_metaxisdata_docker.sh                 # release, tags :dev and :latest
VERSION=v1.2.3 scripts/build_metaxisdata_docker.sh # release, tags :v1.2.3 and :latest
make docker-build                                  # same as the first line

scripts/build_metaxisdata_docker.sh --dev          # dev profile, local experiments only
make docker-build-dev
```

Every build tags the image under `IMAGE` (default `metaxisdata/metaxisdata`).
Only a release build claims `:latest`; a dev build does not.

Build options:

| Variable | Effect |
| --- | --- |
| `VERSION` | Image tag **and** the version the binary reports. Default `dev`. |
| `IMAGE` | Image name. Default `metaxisdata/metaxisdata`. |
| `BUILD_PROXY` | Proxy for the Go module download and the npm install. |
| `GOPROXY` | Go module proxy. Defaults to this machine's `go env GOPROXY`. |
| `NPM_REGISTRY` | npm registry. Defaults to this machine's `npm config get registry`. |
| `APK_MIRROR` | Alpine CDN replacement, e.g. `https://mirrors.aliyun.com/alpine`. Written verbatim into `/etc/apk/repositories` **inside the published image** and shown by `docker history`, so never put credentials in it. |

Do not export a global `HTTPS_PROXY` for `docker build`: BuildKit injects it into
every stage, the runtime stage included, credentials and all. `BUILD_PROXY` is a
custom argument only the build stages declare, so the value you pass is confined
to them.

## 2. Prepare PostgreSQL

Point `PG_URL` at an existing database. The migrator creates its tables on the
first start, so an empty database is enough:

```
postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable
```

The migrations also run one extension, for the metadata search index:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

`pg_trgm` is standard contrib and ships with every supported PostgreSQL
distribution. If the application user may not create extensions, run that line
as an admin first; the migration then finds it already present.

## 3. Start the server

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:v1.2.3
```

The container runs as uid 1001 and checks `/healthz`. Verify with:

```bash
curl -fsS http://localhost:8083/healthz
```

Open `http://<host>:8083` and create the first account. It becomes the workspace
administrator; signup stays open until an administrator turns on *Disallow
self-service signup* in *Settings* → *General*. Then add an instance and sync
its schema. To hand the registry to an MCP client, turn on the MCP endpoint in
the same place — it also needs the workspace *External URL*.

Server environment variables:

| Environment variable | Description |
| --- | --- |
| `PG_URL` | PostgreSQL connection URL (required). |
| `METAXISDATA_PORT` | Listening port, default 8083. Change the port here rather than with `--port`: the image's health check follows this variable only. |
| `METAXISDATA_ENCRYPTION_KEY` | Wraps the per-deployment data key that encrypts stored source credentials, so a database dump alone cannot decrypt them. Set it before the first start; see [security-posture.md](security-posture.md). |
| `METAXISDATA_ENCRYPTION_KEY_PREVIOUS` | Previous keys, comma-separated, during a rotation. |
| `METAXISDATA_JSON_LOGGING=true` | JSON logs instead of text. |
| `METAXISDATA_DEBUG=true` | Debug-level logging, and turns on the `/debug/pprof` and `/metrics` endpoints for anyone who can reach the server. |
| `METAXISDATA_CORS_ALLOW_ORIGINS` | Comma-separated browser origins allowed to call the API with credentials. Only needed when the SPA is served from a different origin; empty installs no CORS middleware at all. |
| `METAXISDATA_TRUSTED_PROXIES` | Comma-separated peer IPs/CIDRs whose `X-Forwarded-For` may be believed when recording an audit source address (§4). |

Health and build metadata:

| Endpoint | Answers |
| --- | --- |
| `GET /healthz` | `OK` while the process serves requests. The image's `HEALTHCHECK` polls it. |
| `GET /api/version` | The `version`, `git_commit` and `build_time` the image was built from. The SPA's user menu and Settings → General read it. |

Both are anonymous, and neither reveals anything beyond which release is running.

Notes:

- Back up the database, not the container. The container mounts no volume and
  keeps no local state.
- The image needs one writable path, `/tmp`, for a scratch file the MSSQL driver
  uses. A hardened deployment should use `--read-only --tmpfs /tmp`, not a bare
  `--read-only`, which removes the only writable directory the image has.
- Without `TZ`, the clock and every log timestamp are UTC.
- The entrypoint maps these variables onto server flags and passes the rest of
  the command through; an explicit argument wins over a mapped one, so
  `docker run … ghcr.io/ranxy/metaxisdata:v1.2.3 --debug` still works.

## 4. External access

Put a reverse proxy with HTTPS in front of the server, then tell the server the
proxy's address so the audit log records the client instead. Set
`METAXISDATA_TRUSTED_PROXIES` to the proxy's address or CIDR:

```bash
-e METAXISDATA_TRUSTED_PROXIES=10.0.0.0/8
```

Without it, the audit log records the proxy's address as the source of every
request.

No timeout tuning is needed for the notification stream: the server sends a
keep-alive heartbeat, so a proxy does not end an idle connection on its own
default read timeout.

## Local trial

Running a throwaway instance on your own machine is a different job from
deploying one — [local-trial.md](local-trial.md) covers the compose stack, its
caveats, and how to point it at a published image instead of building one.
