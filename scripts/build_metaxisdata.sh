#!/usr/bin/env bash
# Build the self-contained metaxisdata server binary: the SPA built from
# frontend/ is embedded in the binary, so one file serves the API and the web
# UI — the same combination the Docker image ships (release profile, embedded
# frontend). Run the binary with PG_URL pointing at PostgreSQL.
#
# Usage:
#   scripts/build_metaxisdata.sh                       # release profile -> build/metaxisdata
#   scripts/build_metaxisdata.sh --dev                 # dev profile, local development only
#   VERSION=v1.2.3 scripts/build_metaxisdata.sh
#   scripts/build_metaxisdata.sh --release-assets      # the release assets + SHA256SUMS -> build/
#
# --release / --dev (or RELEASE=true/false) select the profile: the release tag
# makes the server run ReleaseModeProd. Release is the default because the
# binary is meant for deployment; `make build` covers the daily dev build.
#
# --release-assets builds the asset set the release workflow publishes
# (.github/workflows/release-binaries.yml): a static binary per platform
# (linux amd64/arm64, darwin amd64/arm64, windows amd64), named
# metaxisdata-<goos>-<goarch>[.exe], plus a SHA256SUMS beside it. Every target
# is built with an explicit GOOS/GOARCH — the runner's own platform plays no
# part — and it implies the release profile (rejecting --dev).
#
# The build metadata that `metaxisdata --version` and GET /api/version report
# comes from VERSION / GIT_COMMIT / BUILD_TIME (scripts/build_init.sh).
#
# The build needs the Go toolchain and pnpm, with outbound access for the module
# download and the npm install; set GOPROXY / https_proxy / NPM_REGISTRY in the
# environment as usual.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

RELEASE="${RELEASE:-true}"
ASSETS=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --release)
      RELEASE=true
      shift
      ;;
    --dev)
      RELEASE=false
      shift
      ;;
    --release-assets)
      ASSETS=true
      shift
      ;;
    --help|-h)
      echo "Usage: $0 [--release|--dev] [--release-assets]"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done
if [[ "${ASSETS}" == "true" && "${RELEASE}" != "true" ]]; then
  echo "--release-assets implies --release; it cannot be combined with --dev" >&2
  exit 1
fi

# pnpm_frontend runs the frontend's package commands. The frontend pins its
# pnpm release (frontend/package.json packageManager), so prefer corepack,
# which resolves exactly that pin — and resolves it relative to frontend/,
# because the repo root carries no package.json for it to read. CI and the
# Docker build install the pinned release directly, and a host without
# corepack falls back to the pnpm on PATH (which must then match the pin).
# No command is retried; a genuine pnpm failure propagates.
if command -v corepack >/dev/null 2>&1; then
  pnpm_frontend() {
    (cd frontend && corepack pnpm "$@")
  }
else
  pnpm_frontend() {
    pnpm --dir frontend "$@"
  }
fi

echo "Building frontend..."
pnpm_frontend i --frozen-lockfile
pnpm_frontend build
# Start from an empty SPA directory before copying: the //go:embed pattern
# resolves either way, and a stale frontend_dist would otherwise be compiled
# in. The .gitkeep placeholder keeps the pattern resolvable and the tree clean.
rm -rf backend/server/frontend_dist
mkdir -p backend/server/frontend_dist
cp -r frontend/dist/. backend/server/frontend_dist/
touch backend/server/frontend_dist/.gitkeep

BUILD_TAGS="embed_frontend"
PROFILE="release (prod profile)"
if [[ "${RELEASE}" != "true" ]]; then
  PROFILE="dev profile, local development only"
else
  BUILD_TAGS="${BUILD_TAGS} release"
fi

LDFLAGS="-w -s \
	-X github.com/Ranxy/metaxisdata/backend/common/version.Version=${VERSION} \
	-X github.com/Ranxy/metaxisdata/backend/common/version.GitCommit=${GIT_COMMIT} \
	-X github.com/Ranxy/metaxisdata/backend/common/version.BuildTime=${BUILD_TIME}"

# build_one cross-compiles one target. CGO_ENABLED=0 keeps every binary
# static, so the same file runs on any host of its platform without a C
# toolchain — the property the Docker image's cross-compiled build and the
# release assets rely on.
build_one() {
  local goos="$1" goarch="$2" out="$3"
  echo "Building ${out}..."
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
  	go build -tags "${BUILD_TAGS}" -ldflags "${LDFLAGS}" -p=16 \
  	-o "${out}" ./backend/bin/server/main.go
}

echo "Building server binary (${VERSION}, ${PROFILE})..."
if [[ "${ASSETS}" == "true" ]]; then
  for spec in \
  	"linux amd64 metaxisdata-linux-amd64" \
  	"linux arm64 metaxisdata-linux-arm64" \
  	"darwin amd64 metaxisdata-darwin-amd64" \
  	"darwin arm64 metaxisdata-darwin-arm64" \
  	"windows amd64 metaxisdata-windows-amd64.exe"; do
  	read -r goos goarch asset <<<"${spec}"
  	build_one "${goos}" "${goarch}" "build/${asset}"
  done
  (cd build && sha256sum \
  	metaxisdata-linux-amd64 \
  	metaxisdata-linux-arm64 \
  	metaxisdata-darwin-amd64 \
  	metaxisdata-darwin-arm64 \
  	metaxisdata-windows-amd64.exe \
  	> SHA256SUMS)

  echo ""
  echo "Release assets in build/:"
  cat build/SHA256SUMS
  echo "Verify one before running it: sha256sum -c build/SHA256SUMS --ignore-missing"
else
  build_one "$(go env GOOS)" "$(go env GOARCH)" build/metaxisdata

  echo ""
  echo "Build complete:"
  echo "  build/metaxisdata  (server, ${PROFILE}, frontend embedded)"
  echo "Run it with PG_URL pointing at PostgreSQL, e.g.:"
  echo "  PG_URL='postgres://<user>:<password>@<db-host>:5432/<database>' ./build/metaxisdata --port 8083"
fi
