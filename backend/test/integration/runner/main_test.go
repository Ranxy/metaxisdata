//go:build integration

package runner

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

var (
	sharedMySQLEnv      *integrationenv.ServiceEnv
	sharedMySQLCleanup  func()
	sharedPostgresEnv   *integrationenv.ServiceEnv
	sharedPostgresClean func()
)

func TestMain(m *testing.M) {
	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error
	sharedMySQLEnv, sharedMySQLCleanup, err = integrationenv.StartMySQLServiceEnv(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start shared MySQL integration env: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("MySQL integration environment setup took %v\n", time.Since(start))

	sharedPostgresEnv, sharedPostgresClean, err = integrationenv.StartPostgresServiceEnv(ctx)
	if err != nil {
		sharedMySQLCleanup()
		fmt.Fprintf(os.Stderr, "failed to start shared PostgreSQL integration env: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("PostgreSQL integration environment setup took %v\n", time.Since(start))

	code := m.Run()

	fmt.Printf("total integration test time: %v\n", time.Since(start))
	sharedPostgresClean()
	sharedMySQLCleanup()
	os.Exit(code)
}

func sharedMySQLServiceEnv(t *testing.T) *integrationenv.ServiceEnv {
	t.Helper()
	require.NotNil(t, sharedMySQLEnv)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	require.NoError(t, sharedMySQLEnv.ResetMySQLSource(ctx))
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("shared MySQL integration server logs:\n%s", sharedMySQLEnv.ServerLogs())
		}
	})
	return sharedMySQLEnv
}

func sharedPostgresServiceEnv(t *testing.T) *integrationenv.ServiceEnv {
	t.Helper()
	require.NotNil(t, sharedPostgresEnv)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	require.NoError(t, sharedPostgresEnv.ResetPostgresSource(ctx))
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("shared PostgreSQL integration server logs:\n%s", sharedPostgresEnv.ServerLogs())
		}
	})
	return sharedPostgresEnv
}
