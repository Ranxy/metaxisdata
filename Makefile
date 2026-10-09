.PHONY: run build build-release build-cli build-embed frontend-dist docker-build docker-build-dev docker-up docker-down test-integration-smoke test-integration-mysql test-integration

# Build metadata baked into the binary: `metaxisdata --version` and the SPA's
# user menu read it, so a binary built here can be matched to a commit. The
# docker build passes the same three values (scripts/build_init.sh).
VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/Ranxy/metaxisdata/backend/common/version
LDFLAGS := -w -s \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).GitCommit=$(GIT_COMMIT) \
	-X $(VERSION_PKG).BuildTime=$(BUILD_TIME)

run:
	go run ./backend/bin/server/main.go

# Development build: the server runs with the dev profile (ReleaseModeDev),
# which installs a wide-open CORS middleware. Use build-release for anything
# that is not a local development machine.
build:
	go build -ldflags "$(LDFLAGS)" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go

# Release build: the release tag selects the prod profile (ReleaseModeProd).
build-release:
	go build -ldflags "$(LDFLAGS)" -p=16 -tags release -o ./build/metaxisdata ./backend/bin/server/main.go

# The agent CLI. It is a separate artifact from the server, so it is not part
# of build-release — and it cannot import the server's version package, so it
# carries no build metadata.
build-cli:
	go build -ldflags "-w -s" -p=16 -o ./build/mxd ./cli

# Build the SPA and stage it where the embed_frontend build picks it up.
frontend-dist:
	pnpm --dir frontend build
	rm -rf backend/server/frontend_dist
	mkdir -p backend/server/frontend_dist
	cp -r frontend/dist/. backend/server/frontend_dist/
	touch backend/server/frontend_dist/.gitkeep

# Self-contained release build: the prod profile plus the built SPA embedded in
# the binary, so the server can be deployed without hosting frontend/dist.
build-embed: frontend-dist
	go build -ldflags "$(LDFLAGS)" -p=16 -tags "release embed_frontend" -o ./build/metaxisdata ./backend/bin/server/main.go

# The deployment image: SPA embedded, prod profile. Tags, mirrors and proxy
# build args: docs/deployment.md.
docker-build:
	scripts/build_metaxisdata_docker.sh

# Dev-profile image for local experiments. It is deliberately not tagged
# :latest, so it cannot become what a deployment pulls.
docker-build-dev:
	scripts/build_metaxisdata_docker.sh --dev

# PostgreSQL plus the server, for a look at a built image — the local trial of
# docker-compose.yml, not a production topology. --build so the stack always
# reflects the current checkout instead of a stale image. The build metadata and
# the module proxy/npm registry are derived from this machine, so the image
# reports its real commit and builds where the upstream defaults are
# unreachable; override with VERSION=… GOPROXY=… NPM_REGISTRY=… .
docker-up:
	VERSION="$(VERSION)" \
	GIT_COMMIT="$(GIT_COMMIT)" \
	BUILD_TIME="$(BUILD_TIME)" \
	GOPROXY="$(shell go env GOPROXY 2>/dev/null)" \
	NPM_REGISTRY="$(shell npm config get registry 2>/dev/null)" \
	docker compose up -d --build

docker-down:
	docker compose down

# Every integration-tagged test, including the migrator's fresh-install, upgrade
# and legacy-adoption paths. Skips (exit 0) when Docker is unavailable and no
# external services are configured.
test-integration-smoke:
	go test -count=1 -tags=integration ./backend/test/integration/... ./backend/migrator/...

# The MySQL real-server scenarios. The shared harness still starts both source
# databases, so this only narrows which tests run.
test-integration-mysql:
	go test -v -count=1 -tags=integration -run 'MySQL.*RealServerIntegration' ./backend/test/integration/runner

# The integration gate: all real-server scenarios plus the migrator paths.
test-integration:
	go test -v -count=1 -tags=integration -run 'RealServerIntegration' ./backend/test/integration/runner
	go test -count=1 -tags=integration ./backend/migrator/...
