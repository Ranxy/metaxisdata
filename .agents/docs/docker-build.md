# Building the image and the release binaries

Metaxisdata publishes one deployable service two ways: a container image
(server binary, release profile, SPA embedded) and static binaries for five host
platforms. Staging, tagging and toolchain mechanics live in
`scripts/docker/Dockerfile.server` and the build scripts' own headers; this
reference keeps only the choices those files cannot explain.

## Decisions

| Decision | Why |
| --- | --- |
| Proxy settings use custom build arguments (`BUILD_PROXY`, `GOPROXY`, `NPM_REGISTRY`) | BuildKit injects the standard `http_proxy` variables into every stage, the runtime stage included, so a custom name keeps the value to the stages that declare it. An empty value falls back to the upstream default, which lets a compose file forward an unset variable without knowing the default. |
| `APK_MIRROR` is the one mirror the runtime stage consumes, so it must never carry credentials | The value is written verbatim into the published image's `/etc/apk/repositories` and is visible in `docker history`. |
| Aliases are gated on the release, and `flavor: latest=false` is what makes the `:latest` gate real | The minor line is enabled only when the release is not a prerelease. For `:latest`, `docker/metadata-action` derives `version.latest` from the first tag entry that produces a value and never revises it; under the default flavor a tag it cannot parse hands that decision to the ref entry, so a prerelease could move `:latest`. Turning the flavor off leaves the explicitly gated raw entry as the only source, independent of ordering. |
| The image targets `linux/amd64` and `linux/arm64` | Those are the platforms the server is deployed on. The build stages stay on the build platform and cross-compile, so only the runtime stage's package install runs under emulation — building the SPA and the Go binary under emulation would multiply the build time. |
| No image signing and no SBOM | The image is identified by its OCI labels (version, revision, build time), and the binaries are published with `SHA256SUMS`. A signature or SBOM is a supply-chain decision of its own, not a build default. |
| Only the server is published | The image carries the server binary; the release publishes the five server binaries plus `SHA256SUMS`. The `mxd` CLI is built separately and is not a release asset or a second image. |

The local compose stack is a trial environment, not a production topology; it
exists so a built image can be started together with PostgreSQL on one machine.

## Where things live

The image is built from `scripts/docker/Dockerfile.server` through the
entrypoint `scripts/docker/metaxisdata-entrypoint.sh`; the build scripts are
`scripts/build_metaxisdata_docker.sh` and `scripts/build_metaxisdata.sh`; the
release workflows are `release-image.yml` and `release-binaries.yml` under
`.github/workflows/`. Make targets: `docker-build`, `docker-build-dev`,
`build-binary`, `build-cli`.
