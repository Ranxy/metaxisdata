//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/migrator"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// newIsolatedMetadataStore creates a throwaway database with the full schema, so a
// store-level scenario cannot see the shared workspace's rows.
func newIsolatedMetadataStore(ctx context.Context, t *testing.T) *store.Store {
	t.Helper()

	env := sharedPostgresServiceEnvNoReset(t)
	resourceID, err := common.RandomString(8)
	require.NoError(t, err)
	databaseName := "it_dq_job_" + strings.ToLower(resourceID)
	require.NoError(t, env.ExecPostgres(ctx, "postgres",
		fmt.Sprintf("CREATE DATABASE %s;", quotePostgresIdentifier(databaseName))))
	t.Cleanup(func() {
		_ = env.ExecPostgres(context.Background(), "postgres", fmt.Sprintf(`
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = '%s' AND pid <> pg_backend_pid();`, quotePostgresStringLiteral(databaseName)))
		_ = env.ExecPostgres(context.Background(), "postgres",
			fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(databaseName)))
	})

	metadataURL, err := url.Parse(env.MetadataPGURL)
	require.NoError(t, err)
	metadataURL.Path = "/" + databaseName
	stores, err := store.New(ctx, metadataURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = stores.Close() })
	require.NoError(t, migrator.MigrateSchema(ctx, stores.GetDB()))
	return stores
}
