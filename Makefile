.PHONY: run build build-release build-cli build-embed frontend-dist test-integration-smoke test-integration-mysql test-integration
run:
	go run ./backend/bin/server/main.go

# Development build: the server runs with the dev profile (ReleaseModeDev),
# which installs a wide-open CORS middleware. Use build-release for anything
# that is not a local development machine.
build:
	go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go

# Release build: the release tag selects the prod profile (ReleaseModeProd).
build-release:
	go build -ldflags "-w -s" -p=16 -tags release -o ./build/metaxisdata ./backend/bin/server/main.go

# The agent CLI. It is a separate artifact from the server, so it is not part
# of build-release.
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
	go build -ldflags "-w -s" -p=16 -tags "release embed_frontend" -o ./build/metaxisdata ./backend/bin/server/main.go

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
