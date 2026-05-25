//go:build integration

package runner

import (
	"context"
	"fmt"
	"os"
	"sync"
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

type sharedEnvStartResult struct {
	name     string
	env      *integrationenv.ServiceEnv
	cleanup  func()
	err      error
	duration time.Duration
}

func TestMain(m *testing.M) {
	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make(chan sharedEnvStartResult, 2)
	var wg sync.WaitGroup
	startEnv := func(name string, fn func(context.Context) (*integrationenv.ServiceEnv, func(), error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			envStart := time.Now()
			env, cleanup, err := fn(ctx)
			results <- sharedEnvStartResult{
				name:     name,
				env:      env,
				cleanup:  cleanup,
				err:      err,
				duration: time.Since(envStart),
			}
		}()
	}

	startEnv("MySQL", integrationenv.StartMySQLServiceEnv)
	startEnv("PostgreSQL", integrationenv.StartPostgresServiceEnv)

	var mysqlResult sharedEnvStartResult
	var postgresResult sharedEnvStartResult
	for range 2 {
		result := <-results
		switch result.name {
		case "MySQL":
			mysqlResult = result
		case "PostgreSQL":
			postgresResult = result
		}
	}
	wg.Wait()
	close(results)

	if mysqlResult.err != nil || postgresResult.err != nil {
		if mysqlResult.cleanup != nil {
			mysqlResult.cleanup()
		}
		if postgresResult.cleanup != nil {
			postgresResult.cleanup()
		}
		integrationenv.CleanupIntegrationServerBinaryCache()
		if mysqlResult.err != nil {
			fmt.Fprintf(os.Stderr, "failed to start shared MySQL integration env: %v\n", mysqlResult.err)
		}
		if postgresResult.err != nil {
			fmt.Fprintf(os.Stderr, "failed to start shared PostgreSQL integration env: %v\n", postgresResult.err)
		}
		os.Exit(1)
	}

	sharedMySQLEnv = mysqlResult.env
	sharedMySQLCleanup = mysqlResult.cleanup
	fmt.Printf("MySQL integration environment setup took %v\n", mysqlResult.duration)

	sharedPostgresEnv = postgresResult.env
	sharedPostgresClean = postgresResult.cleanup
	fmt.Printf("PostgreSQL integration environment setup took %v\n", postgresResult.duration)
	fmt.Printf("combined integration environment setup took %v\n", time.Since(start))

	code := m.Run()

	fmt.Printf("total integration test time: %v\n", time.Since(start))
	sharedPostgresClean()
	sharedMySQLCleanup()
	integrationenv.CleanupIntegrationServerBinaryCache()
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

func sharedMySQLServiceEnvNoReset(t *testing.T) *integrationenv.ServiceEnv {
	t.Helper()
	require.NotNil(t, sharedMySQLEnv)
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

func sharedPostgresServiceEnvNoReset(t *testing.T) *integrationenv.ServiceEnv {
	t.Helper()
	require.NotNil(t, sharedPostgresEnv)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("shared PostgreSQL integration server logs:\n%s", sharedPostgresEnv.ServerLogs())
		}
	})
	return sharedPostgresEnv
}
