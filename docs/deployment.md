# Deploying with Docker

Metaxisdata runs as a single container: the server binary with the built SPA embedded, running the release (prod) profile. PostgreSQL is the only external dependency, and the schema migrates itself on startup, so a fresh database needs nothing but a connection URL.

The image, the build scripts and the local compose file live in [scripts/docker/Dockerfile.server](../scripts/docker/Dockerfile.server), [scripts/build_metaxisdata_docker.sh](../scripts/build_metaxisdata_docker.sh) and [docker-compose.yml](../docker-compose.yml).

## Build

```bash
scripts/build_metaxisdata_docker.sh                 # release, tags :dev
VERSION=v1.2.3 scripts/build_metaxisdata_docker.sh # release, tags :v1.2.3 and :latest
make docker-build                                  # same as the first line

scripts/build_metaxisdata_docker.sh --dev          # dev profile, local experiments only
make docker-build-dev
```

Every build tags the image under `IMAGE` (default `metaxisdata/metaxisdata`) and, unless `IMAGE_ALT=` is set to empty, under the GHCR mirror `ghcr.io/ranxy/metaxisdata`. Only a release build also tags `:latest` — the dev profile installs a wide-open CORS middleware and must never become what a deployment pulls.

| Variable | Effect |
| --- | --- |
| `VERSION` | Image tag **and** the version the binary reports. Default `dev`. |
| `GIT_COMMIT`, `BUILD_TIME` | Metadata recorded in the binary. Default to the current commit and UTC now. |
| `IMAGE`, `IMAGE_ALT` | Image names. Set `IMAGE_ALT=` to build one name only. |
| `BUILD_PROXY` | Proxy for the Go module download and the npm install. |
| `GOPROXY` | Go module proxy. Defaults to this machine's `go env GOPROXY`. |
| `NPM_REGISTRY` | npm registry. Defaults to this machine's `npm config get registry`. |
| `APK_MIRROR` | Alpine CDN replacement, e.g. `https://mirrors.aliyun.com/alpine`. |

Do not export a global `HTTPS_PROXY` for the build instead of `BUILD_PROXY`: BuildKit auto-injects the standard proxy variables into every stage, including the runtime image. `BUILD_PROXY` is a custom argument that only the build stages declare, so nothing about the build environment is shipped.

## Released images

Publishing a GitHub Release builds both `linux/amd64` and `linux/arm64` and pushes them to GHCR ([workflow](../.github/workflows/release-image.yml)):

```bash
docker pull ghcr.io/ranxy/metaxisdata:v1.2.3   # the release tag as published
docker pull ghcr.io/ranxy/metaxisdata:1.2.3    # the bare semver
docker pull ghcr.io/ranxy/metaxisdata:1.2      # the minor line
docker pull ghcr.io/ranxy/metaxisdata:latest   # the newest non-prerelease
```

A prerelease gets its tag but never moves `:latest`, so a deployment tracking `:latest` is not handed a release candidate. A manual run of the workflow publishes `:sha-<commit>` only, because a branch name is not a version. The build stages run natively and the Go binary is cross-compiled, so arm64 costs a few minutes rather than an hour of emulation.

GHCR packages start private even when the repository is public: make the package public in its settings for an anonymous `docker pull`, or `docker login ghcr.io` first.

## Run

```bash
docker run -d --name metaxisdata \
  -p 8083:8083 \
  -e PG_URL='postgres://user:password@db-host:5432/metaxisdata?sslmode=disable' \
  ghcr.io/ranxy/metaxisdata:v1.2.3
```

The container runs as uid 1001 with no writable volume: all state is in PostgreSQL. The one exception is a scratch file the MSSQL driver writes under `/tmp`, which is writable in the image. The clock and all log timestamps are UTC.

| Environment variable | Effect |
| --- | --- |
| `PG_URL` | **Required.** PostgreSQL connection URL. |
| `METAXISDATA_ENCRYPTION_KEY` | Wraps the per-deployment data key that encrypts stored source credentials, so a database dump alone cannot decrypt them. Set it before the first start; see [security-posture.md](security-posture.md). |
| `METAXISDATA_ENCRYPTION_KEY_PREVIOUS` | The previous key, during rotation. |
| `METAXISDATA_PORT` | Listening port, default 8083. The health check follows it. |
| `METAXISDATA_JSON_LOGGING=true` | JSON logs instead of text. |
| `METAXISDATA_DEBUG=true` | Debug-level logging. |
| `METAXISDATA_CORS_ALLOW_ORIGINS` | Comma-separated browser origins allowed to call the API with credentials. Only needed when the SPA is served from a different origin; empty installs no CORS middleware at all. |
| `METAXISDATA_TRUSTED_PROXIES` | Comma-separated peer IPs/CIDRs whose `X-Forwarded-For` may be believed when recording an audit source address. Set this when a reverse proxy sits in front, or auditing records the proxy's address. |

The entrypoint maps these onto server flags and then passes the command's own arguments through; an explicit argument wins over a mapped one, and anything unmapped reaches the server unchanged (`docker run … metaxisdata/metaxisdata:v1.2.3 --debug`). Change the port with `METAXISDATA_PORT` rather than with `--port`: only the variable moves the health check along with the server.

### Health and build metadata

| Endpoint | Answers |
| --- | --- |
| `GET /healthz` | `OK` while the process serves requests. The image's `HEALTHCHECK` polls it. |
| `GET /api/version` | The `version`, `git_commit` and `build_time` the image was built from. The SPA's user menu and Settings → General read it. |

Both are anonymous, and neither reveals anything beyond which release is running.

### What the image does not do

- **No TLS.** Terminate HTTPS in a reverse proxy in front of it; the session cookie is `HttpOnly` and the server sets no HSTS, deliberately (see [security-posture.md](security-posture.md)).
- **No PostgreSQL.** Point `PG_URL` at one; any supported version works, and the migrator applies pending schema changes on startup.
- **No bootstrap.** The first account to sign up becomes the workspace administrator, and signup stays open until an administrator closes it in Settings → General. Create that account immediately after the first start.
- **No `mxd` CLI.** The client is a separate artifact (`make build-cli`); it is not part of this image.

## Local trial with compose

```bash
make docker-up                 # builds, then PostgreSQL + the server on http://localhost:8083
docker compose logs -f server
make docker-down               # add -v to drop the store as well

docker compose up -d           # the same, without the metadata the Makefile fills in
docker compose up -d --build   # …and rebuild the image first
```

[docker-compose.yml](../docker-compose.yml) publishes port 8083, keeps PostgreSQL on a named volume, and hardcodes a development password — it is a way to look at a built image, not a production topology.

`make docker-up` forwards `VERSION`, `GIT_COMMIT` and `BUILD_TIME` from the checkout, and the module proxy and npm registry from this machine, so the image reports its real commit and builds on a network where the upstream defaults are unreachable. A plain `docker compose up` gets the upstream defaults and reports `unknown` metadata instead. Either way the knobs are overridable: `VERSION=v1.2.3 make docker-up`, or `METAXISDATA_IMAGE=…` to run an image you built separately.
