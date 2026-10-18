//go:build integration

package runner

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// ReplaceColumnLineageIfCurrent is the CAS contract that keeps an analysis from
// resurrecting a deleted object or overwriting a definition it did not read. The
// guard runs inside the same transaction as the edge and version write, so these
// tests assert both the sentinel error and the absence of a partial write.
func TestReplaceColumnLineageIfCurrentRealServerIntegration(t *testing.T) {
	t.Parallel()

	t.Run("stale hash writes neither edges nor a version", func(t *testing.T) {
		t.Parallel()
		env, ctx, guid := setupLineageAnalysisObject(t, "replace-stale")

		// The caller read hash A, then the object was re-synced before the analysis
		// finished writing. The result describes a definition that is no longer
		// stored, so the write must be dropped entirely.
		staleHash := lineageAnalysisHash(ctx, t, env, guid)
		updateLineageAnalysisObject(ctx, t, env, guid, "SELECT id, name FROM t")
		require.NotEqual(t, staleHash, lineageAnalysisHash(ctx, t, env, guid),
			"the update must move the stored hash, or the test would exercise the current-hash path")

		err := env.Store.ReplaceColumnLineageIfCurrent(ctx, guid, storepb.MetaType_VIEW, staleHash,
			lineage.AnalyzerVersion, lineageEdges(guid), nil)
		require.ErrorIs(t, err, store.ErrAnalysisStale)

		require.Zero(t, countMetadataRows(ctx, t, env,
			`SELECT count(*) FROM column_lineage WHERE meta_guid = $1`, guid))
		version, err := env.Store.GetColumnLineageVersion(ctx, guid, storepb.MetaType_VIEW)
		require.NoError(t, err)
		require.Nil(t, version, "a stale result must not create an analysis-state row")
	})

	t.Run("deleted object is gone and is not recreated", func(t *testing.T) {
		t.Parallel()
		env, ctx, guid := setupLineageAnalysisObject(t, "replace-gone")
		currentHash := lineageAnalysisHash(ctx, t, env, guid)

		resource, err := env.Store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
			GUID: &guid, ObjectType: pointerTo(storepb.MetaType_VIEW),
		})
		require.NoError(t, err)
		require.NotNil(t, resource)
		tx, err := env.Store.GetDB().BeginTx(ctx, nil)
		require.NoError(t, err)
		require.NoError(t, env.Store.BatchDeleteMetaRegistry(ctx, tx, []*store.MetaRegistryResource{resource}))
		require.NoError(t, tx.Commit())

		err = env.Store.ReplaceColumnLineageIfCurrent(ctx, guid, storepb.MetaType_VIEW, currentHash,
			lineage.AnalyzerVersion, lineageEdges(guid), nil)
		require.ErrorIs(t, err, store.ErrAnalysisGone)

		require.Zero(t, countMetadataRows(ctx, t, env,
			`SELECT count(*) FROM column_lineage WHERE meta_guid = $1`, guid))
		version, err := env.Store.GetColumnLineageVersion(ctx, guid, storepb.MetaType_VIEW)
		require.NoError(t, err)
		require.Nil(t, version, "lineage for a deleted object must not be recreated")
		registry, err := env.Store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
			GUID: &guid, ObjectType: pointerTo(storepb.MetaType_VIEW),
		})
		require.NoError(t, err)
		require.Nil(t, registry)
	})

	t.Run("current hash writes edges and version together", func(t *testing.T) {
		t.Parallel()
		env, ctx, guid := setupLineageAnalysisObject(t, "replace-current")
		currentHash := lineageAnalysisHash(ctx, t, env, guid)
		edges := lineageEdges(guid)

		require.NoError(t, env.Store.ReplaceColumnLineageIfCurrent(ctx, guid, storepb.MetaType_VIEW,
			currentHash, lineage.AnalyzerVersion, edges, nil))

		require.Equal(t, int64(len(edges)), countMetadataRows(ctx, t, env,
			`SELECT count(*) FROM column_lineage WHERE meta_guid = $1`, guid))
		version, err := env.Store.GetColumnLineageVersion(ctx, guid, storepb.MetaType_VIEW)
		require.NoError(t, err)
		require.NotNil(t, version)
		require.Equal(t, currentHash, version.MetaHash)
		require.Equal(t, lineage.AnalyzerVersion, version.AnalyzerVersion)
		require.Nil(t, version.ErrorMessage)
	})
}

// MarkAnalysisFailedIfCurrent clears the previous definition's edges and records the
// failure in one transaction.
//
// The original T4 scenario no longer exists: the old implementation cleared the edges
// in one transaction and wrote the version row in another, so a reader (or a crash)
// could observe "edges cleared, version still holding the old hash". Both statements
// now share a transaction that is guarded by the expected hash, so that intermediate
// state cannot exist; this test asserts the committed result of that single write.
func TestMarkAnalysisFailedIfCurrentRealServerIntegration(t *testing.T) {
	t.Parallel()

	const failureMessage = "lineage analysis failed: unsupported statement"

	t.Run("current hash clears edges and records the failure", func(t *testing.T) {
		t.Parallel()
		env, ctx, guid := setupLineageAnalysisObject(t, "failure-current")
		currentHash := lineageAnalysisHash(ctx, t, env, guid)
		seedLineageResult(ctx, t, env, guid, currentHash, lineageEdges(guid))

		require.NoError(t, env.Store.MarkAnalysisFailedIfCurrent(ctx, guid, storepb.MetaType_VIEW,
			currentHash, lineage.AnalyzerVersion, failureMessage))

		require.Zero(t, countMetadataRows(ctx, t, env,
			`SELECT count(*) FROM column_lineage WHERE meta_guid = $1`, guid),
			"a deterministic failure must drop the edges of the definition it replaced")
		version, err := env.Store.GetColumnLineageVersion(ctx, guid, storepb.MetaType_VIEW)
		require.NoError(t, err)
		require.NotNil(t, version)
		require.Equal(t, currentHash, version.MetaHash)
		require.Equal(t, lineage.AnalyzerVersion, version.AnalyzerVersion)
		require.NotNil(t, version.ErrorMessage)
		require.Equal(t, failureMessage, *version.ErrorMessage)
	})

	t.Run("stale hash leaves the previous result untouched", func(t *testing.T) {
		t.Parallel()
		env, ctx, guid := setupLineageAnalysisObject(t, "failure-stale")
		oldHash := lineageAnalysisHash(ctx, t, env, guid)
		seedLineageResult(ctx, t, env, guid, oldHash, lineageEdges(guid))

		// The registry moved on after the analysis read the old definition. The
		// failure describes SQL that is no longer stored, so it must not clear the
		// previous definition's edges or move the version row.
		updateLineageAnalysisObject(ctx, t, env, guid, "SELECT id, name FROM t")

		err := env.Store.MarkAnalysisFailedIfCurrent(ctx, guid, storepb.MetaType_VIEW,
			oldHash, lineage.AnalyzerVersion, failureMessage)
		require.ErrorIs(t, err, store.ErrAnalysisStale)

		require.Equal(t, int64(2), countMetadataRows(ctx, t, env,
			`SELECT count(*) FROM column_lineage WHERE meta_guid = $1`, guid))
		version, err := env.Store.GetColumnLineageVersion(ctx, guid, storepb.MetaType_VIEW)
		require.NoError(t, err)
		require.NotNil(t, version)
		require.Equal(t, oldHash, version.MetaHash)
		require.Nil(t, version.ErrorMessage)
	})
}

// RecordAnalysisError reports a transient failure beside the last successful result:
// it updates error_message and analyzed_at only. In particular it must not touch the
// hash (which would make the object look unanalyzed and re-queue it forever) or the
// stored edges.
func TestRecordAnalysisErrorRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, guid := setupLineageAnalysisObject(t, "record-error")
	currentHash := lineageAnalysisHash(ctx, t, env, guid)
	seedLineageResult(ctx, t, env, guid, currentHash, lineageEdges(guid))

	const message = "failed to get meta registry: transient connection failure"
	require.NoError(t, env.Store.RecordAnalysisError(ctx, guid, storepb.MetaType_VIEW, message))

	version, err := env.Store.GetColumnLineageVersion(ctx, guid, storepb.MetaType_VIEW)
	require.NoError(t, err)
	require.NotNil(t, version)
	require.Equal(t, currentHash, version.MetaHash, "a transient failure must keep the last successful hash")
	require.Equal(t, lineage.AnalyzerVersion, version.AnalyzerVersion)
	require.NotNil(t, version.ErrorMessage)
	require.Equal(t, message, *version.ErrorMessage)
	require.Equal(t, int64(2), countMetadataRows(ctx, t, env,
		`SELECT count(*) FROM column_lineage WHERE meta_guid = $1`, guid),
		"a transient failure must leave the stored edges alone")

	// A row that does not exist is left alone rather than recreated with a NULL
	// hash for an object the registry no longer has.
	missingGUID := guid + "-missing"
	require.NoError(t, env.Store.RecordAnalysisError(ctx, missingGUID, storepb.MetaType_VIEW, message))
	missing, err := env.Store.GetColumnLineageVersion(ctx, missingGUID, storepb.MetaType_VIEW)
	require.NoError(t, err)
	require.Nil(t, missing)
}

var lineageAnalysisGUIDSequence atomic.Int64

// setupLineageAnalysisObject seeds one analyzable registry row under a unique GUID and
// returns the store, context, and GUID. The row and everything written for it are
// removed when the test ends.
func setupLineageAnalysisObject(t *testing.T, purpose string) (*integrationenv.ServiceEnv, context.Context, string) {
	t.Helper()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	guid := fmt.Sprintf("it;lineage-cas;%d-%d;%s",
		time.Now().UnixNano(), lineageAnalysisGUIDSequence.Add(1), purpose)
	updateLineageAnalysisObject(ctx, t, env, guid, "SELECT id FROM t")
	t.Cleanup(func() {
		cleanupLineageAnalysisObject(env, guid)
	})
	return env, ctx, guid
}

// updateLineageAnalysisObject upserts the stored definition (and therefore the meta
// hash) of a test object, simulating a schema sync that changed it after a caller read
// the previous hash.
func updateLineageAnalysisObject(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guid, definition string) {
	t.Helper()

	metadata := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: definition}},
	}
	metadataBytes, metaHash, err := store.CalcStoreMetaHash(metadata)
	require.NoError(t, err)

	tx, err := env.Store.GetDB().BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = env.Store.BatchCreateMetaRegistryResource(ctx, tx, []*store.CreateMetaRegistryResourceMessage{{
		MetaRegistryResource: store.MetaRegistryResource{
			GUID:       guid,
			ObjectType: storepb.MetaType_VIEW,
			MetaHash:   metaHash,
		},
		MetadataBytes: metadataBytes,
	}})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

// lineageAnalysisHash reads the hash the analyzer would have captured before writing.
func lineageAnalysisHash(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guid string) []byte {
	t.Helper()

	resource, err := env.Store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
		GUID: &guid, ObjectType: pointerTo(storepb.MetaType_VIEW),
	})
	require.NoError(t, err)
	require.NotNil(t, resource)
	require.NotEmpty(t, resource.MetaHash)
	return resource.MetaHash
}

// seedLineageResult writes a successful analysis at hash, so the failure and error
// paths have a previous result to leave alone.
func seedLineageResult(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guid string, hash []byte, edges []*store.ColumnLineage) {
	t.Helper()

	require.NoError(t, env.Store.ReplaceColumnLineageIfCurrent(ctx, guid, storepb.MetaType_VIEW,
		hash, lineage.AnalyzerVersion, edges, nil))
}

// lineageEdges returns the two edges a one-column view produces: the source column into
// the view and the view column as its own target.
func lineageEdges(guid string) []*store.ColumnLineage {
	const column = "id"
	transformation := make([]model.Transformation, 0)
	return []*store.ColumnLineage{
		{
			MetaGUID:       guid,
			MetaType:       storepb.MetaType_VIEW,
			SourceGUID:     "it;lineage-cas;source-table",
			SourceColumn:   column,
			SourceType:     storepb.MetaType_TABLE,
			TargetGUID:     guid,
			TargetColumn:   column,
			TargetType:     storepb.MetaType_VIEW,
			RelationType:   model.RelationTypeDirect,
			Transformation: transformation,
		},
		{
			MetaGUID:       guid,
			MetaType:       storepb.MetaType_VIEW,
			SourceGUID:     guid,
			SourceColumn:   column,
			SourceType:     storepb.MetaType_VIEW,
			TargetGUID:     "it;lineage-cas;target-table",
			TargetColumn:   column,
			TargetType:     storepb.MetaType_TABLE,
			RelationType:   model.RelationTypeDirect,
			Transformation: transformation,
		},
	}
}

func cleanupLineageAnalysisObject(env *integrationenv.ServiceEnv, guid string) {
	ctx := context.Background()
	db := env.Store.GetDB()
	_, _ = db.ExecContext(ctx, `DELETE FROM column_lineage WHERE meta_guid = $1 OR source_guid = $1 OR target_guid = $1`, guid)
	_, _ = db.ExecContext(ctx, `DELETE FROM column_lineage_version WHERE meta_guid = $1`, guid)
	_, _ = db.ExecContext(ctx, `DELETE FROM meta_registry_resource_history WHERE guid = $1`, guid)
	_, _ = db.ExecContext(ctx, `DELETE FROM meta_registry_resource WHERE guid = $1`, guid)
}

func pointerTo[T any](value T) *T {
	return &value
}
