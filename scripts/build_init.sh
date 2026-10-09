#!/usr/bin/env bash
# Shared variables for the metaxisdata build scripts. Source from the repo root.
set -euo pipefail

# Build metadata. VERSION names the image tag and is baked into the binary, and
# GIT_COMMIT/BUILD_TIME are what `metaxisdata --version` and the SPA's user menu
# report, so an image can always be traced back to the commit it came from. The
# default matches the version package's own: a build nobody named.
VERSION="${VERSION:-dev}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
