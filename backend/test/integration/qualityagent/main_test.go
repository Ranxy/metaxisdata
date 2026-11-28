//go:build integration

package qualityagent

import (
	"context"
	"fmt"
	"os"
	"testing"

	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// testEnv is one PostgreSQL-backed service environment for the quality-agent suite.
// It owns its workspace switches, so enabling model analysis or the agent runner here
// cannot disturb the broad data-quality suite in another package.
var testEnv *integrationenv.ServiceEnv

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())

	if err := integrationenv.ValidateIntegrationEnv(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "invalid integration service configuration: %v\n", err)
		cancel()
		//nolint:revive // A configuration error must fail the binary.
		os.Exit(1)
	}
	if !integrationenv.ExternalServicesConfigured() && !integrationenv.DockerAvailable(ctx) {
		_, _ = fmt.Println("skipping quality-agent integration tests: docker is unavailable")
		cancel()
		return
	}

	env, cleanup, err := integrationenv.StartPostgresServiceEnv(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to start the quality-agent integration environment: %v\n", err)
		cancel()
		//nolint:revive // A failed environment is not a skipped test.
		os.Exit(1)
	}
	testEnv = env
	m.Run()
	cleanup()
	cancel()
}
