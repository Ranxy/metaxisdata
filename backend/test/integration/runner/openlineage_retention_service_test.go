//go:build integration

package runner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestRetentionPrunesTheLineageOfADeletedRunRealServerIntegration pins what the
// retention setting promises. A run's column-lineage edges are keyed by the run's
// own GUID and are read straight out of column_lineage, without joining the meta
// registry, so removing the run and its registry mirror left them behind: the
// graph kept showing lineage derived from data the operator had asked to drop.
// The prune has to take the edges and the analysis-version rows with the run.
func TestRetentionPrunesTheLineageOfADeletedRunRealServerIntegration(t *testing.T) {
	// Not parallel, alone among the tests that prune. DeleteOpenLineageRunsBefore is
	// global — it deletes every run older than its cutoff, not the caller's — and this
	// is the one test whose cutoff is wall-clock (two days ago). Running beside the
	// parallel tests, it therefore deleted the runs they seed with year-2000 event
	// times to pin their own windows, and their assertions failed; its own count
	// assertion failed in the same breath, because the count is the whole database's.
	// The sequential phase runs it before those tests have seeded anything.

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()

	namespace := fmt.Sprintf("retention-lineage-%d", time.Now().UnixNano())
	runGUID := "openlineage:run:TASK:" + namespace + ":job:run-1"
	expired := time.Now().UTC().Add(-48 * time.Hour)

	_, err := env.Store.GetDB().ExecContext(ctx, `
		INSERT INTO openlineage_run (guid, task_guid, run_id, job_namespace, job_name, job_type, event_type, event_time, raw_payload)
		VALUES ($1, $2, 'run-1', $3, 'job', 'TASK', 'COMPLETE', $4, '{}'::jsonb)
	`, runGUID, "openlineage:task:TASK:"+namespace+":job", namespace, expired)
	require.NoError(t, err)

	// The edges and the version row an ingested run leaves behind, owned by it.
	_, err = env.Store.GetDB().ExecContext(ctx, `
		INSERT INTO column_lineage (meta_guid, meta_type, source_guid, source_column, target_guid, target_column)
		VALUES ($1, $2, $3, 'a', $4, 'b')
	`, runGUID, int16(storepb.MetaType_OPENLINEAGE), namespace+"::src", namespace+"::tgt")
	require.NoError(t, err)
	_, err = env.Store.GetDB().ExecContext(ctx, `
		INSERT INTO column_lineage_version (meta_guid, meta_type, analyzed_at)
		VALUES ($1, $2, NOW())
	`, runGUID, int16(storepb.MetaType_OPENLINEAGE))
	require.NoError(t, err)

	count := func(table string) int {
		t.Helper()
		var rows int
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+table+` WHERE meta_guid = $1`, runGUID).Scan(&rows))
		return rows
	}
	require.Equal(t, 1, count("column_lineage"), "the edge has to exist before the prune means anything")
	require.Equal(t, 1, count("column_lineage_version"))

	deleted, err := env.Store.DeleteOpenLineageRunsBefore(ctx, expired.Add(24*time.Hour))
	require.NoError(t, err)
	// The count is the whole table's, so it is not this test's to pin exactly; what
	// it pins is that its own run is inside the window and goes with everything
	// derived from it.
	require.GreaterOrEqual(t, deleted, int64(1), "the prune has to take the expired run")
	var runs int
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM openlineage_run WHERE guid = $1`, runGUID).Scan(&runs))
	require.Zero(t, runs, "the run is inside the retention window")

	require.Zero(t, count("column_lineage"),
		"a pruned run must not keep contributing edges to the lineage graph")
	require.Zero(t, count("column_lineage_version"),
		"its analysis-version row goes with it")
}
