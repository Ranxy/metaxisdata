.PHONY: run test-integration-smoke test-integration-mysql
run:
	go run ./backend/bin/server/main.go

test-integration-smoke:
	go test -count=1 -tags=integration ./backend/test/integration/...

test-integration-mysql:
	go test -count=1 -tags=integration -run ^TestMySQLSchemaSyncAndLineageIntegration$$ ./backend/test/integration/runner