#!/usr/bin/env bash
# Build the metaxisdata server docker image: the SPA built from frontend/ and
# embedded in the server binary, running the release (prod) profile.
#
# Usage:
#   scripts/build_metaxisdata_docker.sh                  # -> metaxisdata/metaxisdata:dev
#   VERSION=v1.2.3 scripts/build_metaxisdata_docker.sh  # -> metaxisdata/metaxisdata:v1.2.3
#   scripts/build_metaxisdata_docker.sh --dev           # dev profile, :dev only (no :latest)
#   IMAGE=registry.internal/metaxisdata IMAGE_ALT= scripts/build_metaxisdata_docker.sh
#
# Network knobs, all optional:
#   BUILD_PROXY     proxy for the Go module download and the npm install. It is a
#                   custom argument name so the value only reaches the build
#                   stages' own env, instead of BuildKit injecting a standard
#                   http_proxy into every stage: a proxy set through the docker
#                   CLI config is applied by the builder itself, credentials
#                   included, and this Dockerfile cannot stop that.
#   GOPROXY         Go module proxy; defaults to this machine's `go env GOPROXY`.
#   NPM_REGISTRY    npm registry; defaults to this machine's `npm config get
#                   registry`.
#   APK_MIRROR      Alpine CDN replacement, e.g. https://mirrors.aliyun.com/alpine.
#                   Written verbatim into /etc/apk/repositories inside the image,
#                   so it must never carry credentials.
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
# Only a release build may claim :latest. The dev profile is not a CORS hole,
# but :latest should name the configuration a deployment is expected to run,
# and a local experiment is not that.
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

# Print every tag that was created, including the alias: IMAGE_ALT= leaves
# :latest on IMAGE alone, and a reader of this summary should not have to
# remember the rules to know what just appeared in the local store.
echo ""
echo "Images:"
echo "  ${IMAGE}:${VERSION}"
if [[ "${RELEASE}" == "true" ]]; then
  echo "  ${IMAGE}:latest"
fi
if [[ -n "${IMAGE_ALT}" ]]; then
  echo "  ${IMAGE_ALT}:${VERSION}"
  if [[ "${RELEASE}" == "true" ]]; then
    echo "  ${IMAGE_ALT}:latest"
  fi
fi
echo "Run one with PG_URL pointing at PostgreSQL, or use make docker-up for a local trial."
