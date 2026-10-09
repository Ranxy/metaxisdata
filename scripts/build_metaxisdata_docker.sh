#!/usr/bin/env bash
# Build the metaxisdata server docker image: the SPA built from frontend/ and
# embedded in the server binary, running the release (prod) profile.
#
# Usage:
#   scripts/build_metaxisdata_docker.sh                  # -> metaxisdata/metaxisdata:dev
#   VERSION=v1.2.3 scripts/build_metaxisdata_docker.sh  # -> metaxisdata/metaxisdata:v1.2.3
#   scripts/build_metaxisdata_docker.sh --dev           # dev profile (local only)
#   IMAGE=registry.internal/metaxisdata IMAGE_ALT= scripts/build_metaxisdata_docker.sh
#
# Network knobs, all optional:
#   BUILD_PROXY     proxy for the Go module download and the npm install, passed
#                   under a custom arg name so BuildKit never injects it into
#                   the runtime image. Do not export a global HTTPS_PROXY
#                   instead: BuildKit auto-injects the standard names into every
#                   stage, including the shipped one.
#   GOPROXY         Go module proxy; defaults to this machine's `go env GOPROXY`.
#   NPM_REGISTRY    npm registry; defaults to this machine's `npm config get
#                   registry`.
#   APK_MIRROR      Alpine CDN replacement, e.g. https://mirrors.aliyun.com/alpine
#
# The image is tagged once per name with VERSION; a release build also claims
# :latest. IMAGE_ALT defaults to the ghcr.io mirror; set IMAGE_ALT= to build
# under IMAGE only.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

RELEASE="${RELEASE:-true}"
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
    --help|-h)
      echo "Usage: $0 [--release|--dev]"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done
case "${RELEASE}" in
  true|1|yes) RELEASE=true ;;
  false|0|no) RELEASE=false ;;
  *)
    echo "RELEASE must be true or false (got: ${RELEASE})" >&2
    exit 1
    ;;
esac

# Default the proxy knobs to what this machine already uses: a host configured
# for a regional module proxy or registry can build the image without repeating
# the setting, and an explicit environment value still wins.
GOPROXY="${GOPROXY:-$(go env GOPROXY 2>/dev/null || true)}"
NPM_REGISTRY="${NPM_REGISTRY:-$(npm config get registry 2>/dev/null || true)}"

. ./scripts/build_docker_common.sh
collect_common_build_args
BUILD_ARGS+=(--build-arg "RELEASE=${RELEASE}")

IMAGE="${IMAGE:-metaxisdata/metaxisdata}"
# Unset means "the default mirror"; empty means "this name only".
if [[ -z "${IMAGE_ALT+x}" ]]; then
  IMAGE_ALT="ghcr.io/ranxy/metaxisdata"
fi

TAGS=(--tag "${IMAGE}:${VERSION}")
if [[ -n "${IMAGE_ALT}" ]]; then
  TAGS+=(--tag "${IMAGE_ALT}:${VERSION}")
fi
# Only a release build may claim :latest — a dev image is the wide-open-CORS
# profile, and it must not become the tag a deployment pulls by default.
if [[ "${RELEASE}" == "true" ]]; then
  TAGS+=(--tag "${IMAGE}:latest")
  if [[ -n "${IMAGE_ALT}" ]]; then
    TAGS+=(--tag "${IMAGE_ALT}:latest")
  fi
fi

PROFILE="release (prod profile)"
if [[ "${RELEASE}" != "true" ]]; then
  PROFILE="dev profile, local development only"
fi

echo "Building metaxisdata server image ${VERSION}, ${PROFILE}..."
docker build -f ./scripts/docker/Dockerfile.server \
	"${BUILD_ARGS[@]}" \
	"${TAGS[@]}" \
	.

echo ""
echo "Images:"
echo "  ${IMAGE}:${VERSION}"
if [[ -n "${IMAGE_ALT}" ]]; then
  echo "  ${IMAGE_ALT}:${VERSION}"
fi
echo "Run one with PG_URL pointing at PostgreSQL, or use make docker-up for a local trial."
