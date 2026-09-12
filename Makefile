.PHONY: run build build-release test-integration-smoke test-integration-mysql
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

test-integration-smoke:
	go test -count=1 -tags=integration ./backend/test/integration/...

test-integration:
	go test -v -count=1 -tags=integration -run 'RealServerIntegration' ./backend/test/integration/runner
