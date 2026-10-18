//go:build integration

package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Two writers can replace the same object's edges at once — the analyzer and the
// OpenLineage revalidation path share one process — so the edge table has to carry the
// uniqueness rule itself. The migration creates it, and the production insert relies on
// ON CONFLICT DO NOTHING; this checks the schema half against real PostgreSQL.
func TestColumnLineageEdgeUniquenessRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedMySQLServiceEnvNoReset(t)
	ctx := context.Background()
	db := env.Store.GetDB()

	var indexDef string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = current_schema()
			AND tablename = 'column_lineage'
			AND indexname = 'idx_column_lineage_edge'
	`).Scan(&indexDef))
	require.Contains(t, indexDef, "UNIQUE")
	for _, column := range []string{
		"meta_guid", "meta_type", "source_guid", "source_column",
		"target_guid", "target_column", "relation_type",
	} {
		require.Contains(t, indexDef, column)
	}

	// The identity the index encodes is the edge's endpoints and relation type: a second
	// row for the same identity must be refused, whatever its transformation says.
	guid := fmt.Sprintf("it_edge_unique_%s", t.Name())
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM column_lineage WHERE meta_guid = $1`, guid)
	})
	insert := `
		INSERT INTO column_lineage (
			meta_guid, meta_type, source_guid, source_column, source_type,
			target_guid, target_column, target_type, relation_type, transformation
		) VALUES ($1, 1, $2, 'c1', 1, $3, 'c2', 1, 1, '[]')
	`
	_, err := db.ExecContext(ctx, insert, guid, guid+"_source", guid+"_target")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, insert, guid, guid+"_source", guid+"_target")
	require.Error(t, err, "a duplicate edge must be refused by the unique index")
	require.Contains(t, err.Error(), "idx_column_lineage_edge")
}
