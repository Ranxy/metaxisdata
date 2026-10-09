#!/usr/bin/env bash
# Shared helpers for the metaxisdata docker build scripts. Source from the repo
# root, after (or through) scripts/build_init.sh.
set -euo pipefail

. ./scripts/build_init.sh

# collect_common_build_args fills the global BUILD_ARGS array with the build
# args the server image accepts: build metadata plus the optional network
# knobs. Unset knobs are omitted so the Dockerfile defaults apply, and none of
# them reaches the runtime stage.
collect_common_build_args() {
	BUILD_ARGS=(
		--build-arg "VERSION=${VERSION}"
		--build-arg "GIT_COMMIT=${GIT_COMMIT}"
		--build-arg "BUILD_TIME=${BUILD_TIME}"
	)
	if [[ -n "${BUILD_PROXY:-}" ]]; then
		BUILD_ARGS+=(--build-arg "BUILD_PROXY=${BUILD_PROXY}")
	fi
	if [[ -n "${GOPROXY:-}" ]]; then
		BUILD_ARGS+=(--build-arg "GOPROXY=${GOPROXY}")
	fi
	if [[ -n "${NPM_REGISTRY:-}" ]]; then
		BUILD_ARGS+=(--build-arg "NPM_REGISTRY=${NPM_REGISTRY}")
	fi
	if [[ -n "${APK_MIRROR:-}" ]]; then
		BUILD_ARGS+=(--build-arg "APK_MIRROR=${APK_MIRROR}")
	fi
}
