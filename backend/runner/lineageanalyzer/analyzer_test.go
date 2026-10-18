package lineageanalyzer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestBuildSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		objectName  string
		engine      storepb.Engine
		metaType    storepb.MetaType
		resource    *store.MetaRegistryResource
		wantDef     string
		wantWrapped string
	}{
		{
			name:        "mysql view uses backticks",
			engine:      storepb.Engine_MYSQL,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: "CREATE VIEW `active_users` AS SELECT id FROM users",
		},
		{
			name:        "postgres view uses double quotes",
			engine:      storepb.Engine_POSTGRES,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: `CREATE VIEW "active_users" AS SELECT id FROM users`,
		},
		{
			name:        "postgres materialized view uses correct keyword",
			engine:      storepb.Engine_POSTGRES,
			metaType:    storepb.MetaType_MATERIALIZED_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: `CREATE MATERIALIZED VIEW "active_users" AS SELECT id FROM users`,
		},
		{
			name:        "unsupported engine falls back to bare identifier",
			engine:      storepb.Engine_ENGINE_UNSPECIFIED,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: "CREATE VIEW active_users AS SELECT id FROM users",
		},
		{
			name:        "manual sql uses raw statement",
			engine:      storepb.Engine_POSTGRES,
			metaType:    storepb.MetaType_MANUAL_SQL,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ManualSqlMetadata{ManualSqlMetadata: &storepb.ManualSQLMetadata{SqlText: "INSERT INTO summary SELECT id, name FROM users"}}}},
			wantDef:     "INSERT INTO summary SELECT id, name FROM users",
			wantWrapped: "INSERT INTO summary SELECT id, name FROM users",
		},
		{
			// StarRocks stores SHOW CREATE MATERIALIZED VIEW output verbatim, so
			// the definition is already a statement and must not be wrapped
			// again ("... AS CREATE MATERIALIZED VIEW ..." does not parse).
			name:     "starrocks materialized view definition is already a statement",
			engine:   storepb.Engine_STARROCKS,
			metaType: storepb.MetaType_MATERIALIZED_VIEW,
			resource: &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
				Definition: "CREATE MATERIALIZED VIEW `mv1` (`id`, `cnt`)\nDISTRIBUTED BY HASH(`id`)\nREFRESH ASYNC\nAS SELECT id, count(*) AS cnt FROM t1 GROUP BY id",
			}}}},
			wantDef:     "CREATE MATERIALIZED VIEW `mv1` (`id`, `cnt`)\nDISTRIBUTED BY HASH(`id`)\nREFRESH ASYNC\nAS SELECT id, count(*) AS cnt FROM t1 GROUP BY id",
			wantWrapped: "CREATE MATERIALIZED VIEW `mv1` (`id`, `cnt`)\nDISTRIBUTED BY HASH(`id`)\nREFRESH ASYNC\nAS SELECT id, count(*) AS cnt FROM t1 GROUP BY id",
		},
		{
			name:        "starrocks view uses backticks",
			engine:      storepb.Engine_STARROCKS,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: "CREATE VIEW `active_users` AS SELECT id FROM users",
		},
		{
			// Doris quotes identifiers with backticks, so a view name that is a
			// keyword or carries odd characters survives the wrap. Its definition is
			// a bare query body, like the MySQL family's.
			name:        "doris view uses backticks",
			engine:      storepb.Engine_DORIS,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: "CREATE VIEW `active_users` AS SELECT id FROM users",
		},
		{
			// MariaDB shares the MySQL driver and keeps a bare query body, so its
			// view identifier must be quoted exactly like MySQL's. A reserved word
			// is the case that silently loses lineage when it is not.
			name:        "mariadb view quotes a reserved name",
			objectName:  "order",
			engine:      storepb.Engine_MARIADB,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: "CREATE VIEW `order` AS SELECT id FROM users",
		},
		{
			name:        "starrocks view quotes a reserved name",
			objectName:  "order",
			engine:      storepb.Engine_STARROCKS,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: "CREATE VIEW `order` AS SELECT id FROM users",
		},
		{
			// T-SQL brackets an identifier and escapes a closing bracket by doubling it.
			name:        "mssql view quotes a reserved name",
			objectName:  "order",
			engine:      storepb.Engine_MSSQL,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: "CREATE VIEW [order] AS SELECT id FROM users",
		},
		{
			name:        "mysql view escapes a backtick in the name",
			objectName:  "a`b",
			engine:      storepb.Engine_MYSQL,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT id FROM users"}}}},
			wantDef:     "SELECT id FROM users",
			wantWrapped: "CREATE VIEW `a``b` AS SELECT id FROM users",
		},
		{
			// A complete DDL is commonly stored with a leading comment. It must not be
			// wrapped again; "-- note\nCREATE MATERIALIZED VIEW ..." parses as a whole
			// statement, while "CREATE ... AS -- note\nCREATE ..." does not.
			name:     "starrocks materialized view with a leading line comment stays as-is",
			engine:   storepb.Engine_STARROCKS,
			metaType: storepb.MetaType_MATERIALIZED_VIEW,
			resource: &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
				Definition: "-- refreshed hourly\nCREATE MATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
			}}}},
			wantDef:     "-- refreshed hourly\nCREATE MATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
			wantWrapped: "-- refreshed hourly\nCREATE MATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
		},
		{
			name:     "starrocks materialized view with a leading block comment stays as-is",
			engine:   storepb.Engine_STARROCKS,
			metaType: storepb.MetaType_MATERIALIZED_VIEW,
			resource: &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
				Definition: "/* owner: data-platform */\nCREATE MATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
			}}}},
			wantDef:     "/* owner: data-platform */\nCREATE MATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
			wantWrapped: "/* owner: data-platform */\nCREATE MATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
		},
		{
			name:     "starrocks materialized view with CREATE on its own line stays as-is",
			engine:   storepb.Engine_STARROCKS,
			metaType: storepb.MetaType_MATERIALIZED_VIEW,
			resource: &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
				Definition: "CREATE\nMATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
			}}}},
			wantDef:     "CREATE\nMATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
			wantWrapped: "CREATE\nMATERIALIZED VIEW `mv1` AS SELECT id FROM t1",
		},
		{
			// A comment-prefixed bare body is still a body: the comment is not a
			// complete statement, so it must be wrapped.
			name:        "a bare body with a leading comment is still wrapped",
			engine:      storepb.Engine_MYSQL,
			metaType:    storepb.MetaType_VIEW,
			resource:    &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "-- note\nSELECT id FROM users"}}}},
			wantDef:     "-- note\nSELECT id FROM users",
			wantWrapped: "CREATE VIEW `active_users` AS -- note\nSELECT id FROM users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objectName := tt.objectName
			if objectName == "" {
				objectName = "active_users"
			}
			gotDef, gotWrapped, err := buildSQL(objectName, tt.engine, tt.metaType, tt.resource)
			if err != nil {
				t.Fatalf("buildSQL() error = %v", err)
			}
			if gotDef != tt.wantDef {
				t.Fatalf("buildSQL() definition = %q, want %q", gotDef, tt.wantDef)
			}
			if gotWrapped != tt.wantWrapped {
				t.Fatalf("buildSQL() wrapped = %q, want %q", gotWrapped, tt.wantWrapped)
			}
		})
	}
}

// A wrapped statement must parse in its own dialect: the identifier quoting is
// the only part of the wrapper that differs by engine, and getting it wrong is a
// permanent lineage loss (the parse-failure path clears the object's edges and
// records the current hash as analyzed, so the hourly scan stops re-queueing it).
func TestBuildSQLWrapsParseForEveryRegisteredEngine(t *testing.T) {
	t.Parallel()

	analyzer := lineage.NewAnalyzer(nil, engines.Registrations()...)
	for _, registration := range engines.Registrations() {
		engine := registration.Engine
		t.Run(engine.String(), func(t *testing.T) {
			t.Parallel()

			resource := &store.MetaRegistryResource{Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Definition: "SELECT 1 AS id"}}}}
			_, wrapped, err := buildSQL("order", engine, storepb.MetaType_VIEW, resource)
			require.NoError(t, err)
			_, err = analyzer.Analyze(context.Background(), engine, wrapped)
			require.NoErrorf(t, err, "engine %s cannot parse its own wrapped view %q", engine, wrapped)
		})
	}
}

// An object is re-analyzed when it was never analyzed, when its metadata changed,
// or when the stored result came from different analyzer rules. The last case is
// what makes a deployment that changes the analyzers refresh lineage by itself.
func TestNeedsAnalysis(t *testing.T) {
	t.Parallel()

	hash := []byte("meta-hash")
	tests := []struct {
		name     string
		resource *store.MetaRegistryResource
		version  *store.ColumnLineageVersion
		want     bool
	}{
		{
			name:     "never analyzed",
			resource: &store.MetaRegistryResource{MetaHash: hash},
			want:     true,
		},
		{
			name:     "same hash and analyzer version",
			resource: &store.MetaRegistryResource{MetaHash: hash},
			version:  &store.ColumnLineageVersion{MetaHash: hash, AnalyzerVersion: lineage.AnalyzerVersion},
			want:     false,
		},
		{
			name:     "metadata changed",
			resource: &store.MetaRegistryResource{MetaHash: []byte("new-hash")},
			version:  &store.ColumnLineageVersion{MetaHash: hash, AnalyzerVersion: lineage.AnalyzerVersion},
			want:     true,
		},
		{
			name:     "analyzer version changed",
			resource: &store.MetaRegistryResource{MetaHash: hash},
			version:  &store.ColumnLineageVersion{MetaHash: hash, AnalyzerVersion: "0"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, needsAnalysis(tt.resource, tt.version))
		})
	}
}

// The relation GUIDs are resolved in one batch query, and the analyzed object's own GUID is
// left out: its type is known, and a query that repeats it would only duplicate a row.
func TestTypeLookupGUIDsExcludesTheAnalyzedObject(t *testing.T) {
	t.Parallel()

	lookups := map[string]struct{}{"a": {}, "b": {}, "self": {}}
	require.Equal(t, []string{"a", "b"}, typeLookupGUIDs(lookups, "self"))
	require.Empty(t, typeLookupGUIDs(map[string]struct{}{"self": {}}, "self"))
	require.Empty(t, typeLookupGUIDs(nil, "self"))
}

// One tick takes at most one bounded batch. A cold start queues the whole registry, and
// leaving the rest queued is what lets event-driven work interleave with the backfill.
func TestTakeAnalyzeBatchCapsAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	require.Equal(t, MaxGoroutines*100, maxAnalyzeBatch, "the batch is a small multiple of the worker count")
	require.Positive(t, analyzeObjectTimeout)

	analyzer := &Analyzer{}
	for i := range 5 {
		analyzer.QueueAnalysis(fmt.Sprintf("guid-%d", i), storepb.MetaType_VIEW)
	}

	keys := analyzer.takeAnalyzeBatch(&analyzer.analyzeMap, time.Now(), 2)
	require.Len(t, keys, 2)
	require.Equal(t, 3, queuedCount(analyzer), "keys over the cap stay queued for the next tick")

	keys = analyzer.takeAnalyzeBatch(&analyzer.analyzeMap, time.Now(), maxAnalyzeBatch)
	require.Len(t, keys, 3)
	require.Zero(t, queuedCount(analyzer))
}

// Event-driven work is drained before the hourly scan's backlog, so a cold-start backfill
// cannot push a just-saved statement behind the rest of the registry. An event also
// supersedes a scan entry for the same object.
func TestDrainPrefersEventsOverTheScanBacklog(t *testing.T) {
	t.Parallel()

	analyzer := &Analyzer{}
	const backlog = 5
	for i := range backlog {
		analyzer.queueScan(fmt.Sprintf("scan-%d", i), storepb.MetaType_VIEW)
	}
	analyzer.QueueAnalysis("event", storepb.MetaType_VIEW)
	require.Equal(t, backlog+1, queuedCount(analyzer))

	// The scan pass re-queueing an object an event already queued must not duplicate it.
	analyzer.queueScan("event", storepb.MetaType_VIEW)
	require.Equal(t, backlog+1, queuedCount(analyzer))

	now := time.Now()
	keys := analyzer.takeAnalyzeBatch(&analyzer.analyzeMap, now, maxAnalyzeBatch)
	require.Equal(t, []analyzeKey{{MetaGUID: "event", MetaType: storepb.MetaType_VIEW}}, keys)
	require.Equal(t, backlog, queuedCount(analyzer))
}

// A key still inside its retry backoff is not part of a batch, and it stays queued.
func TestTakeAnalyzeBatchSkipsBackingOffKeys(t *testing.T) {
	t.Parallel()

	analyzer := &Analyzer{}
	key := analyzeKey{MetaGUID: "guid", MetaType: storepb.MetaType_VIEW}
	analyzer.QueueAnalysis(key.MetaGUID, key.MetaType)
	analyzer.scheduleRetry(key)

	require.Empty(t, analyzer.takeAnalyzeBatch(&analyzer.analyzeMap, time.Now(), maxAnalyzeBatch))
	require.Equal(t, 1, queuedCount(analyzer))

	due := time.Now().Add(analysisRetryBackoff(1) + time.Second)
	require.Equal(t, []analyzeKey{key}, analyzer.takeAnalyzeBatch(&analyzer.analyzeMap, due, maxAnalyzeBatch))
	require.Zero(t, queuedCount(analyzer))
}

// An analysis cut short by its deadline, or by the runner shutting down, says nothing about
// the statement: it is retried and its stored lineage is left alone. A parse error is not
// transient and takes the deterministic failure path.
func TestAnalysisTimedOut(t *testing.T) {
	t.Parallel()

	require.True(t, analysisTimedOut(context.Background(), context.DeadlineExceeded))
	require.True(t, analysisTimedOut(context.Background(), fmt.Errorf("lookup: %w", context.DeadlineExceeded)))
	require.True(t, analysisTimedOut(context.Background(), context.Canceled))
	require.False(t, analysisTimedOut(context.Background(), errors.New("a statement the parser could not read")))

	expired, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	// Even an error that does not wrap a context error is transient when the context expired
	// underneath it: a driver may report the cancellation in its own words.
	require.True(t, analysisTimedOut(expired, errors.New("the database is gone")))
}

func queuedCount(analyzer *Analyzer) int {
	count := 0
	for _, queue := range []*sync.Map{&analyzer.analyzeMap, &analyzer.scanMap} {
		queue.Range(func(_, _ any) bool {
			count++
			return true
		})
	}
	return count
}

// A failed analysis is retried with a bounded backoff instead of waiting for
// the hourly full scan, and a fresh queue request resets that backoff.
func TestAnalyzerRetryBackoff(t *testing.T) {
	t.Parallel()

	require.Equal(t, 30*time.Second, analysisRetryBackoff(1))
	require.Equal(t, 2*time.Minute, analysisRetryBackoff(2))
	require.Equal(t, 5*time.Minute, analysisRetryBackoff(3))
}

func TestScheduleRetryBacksOffAndGivesUp(t *testing.T) {
	t.Parallel()

	analyzer := &Analyzer{}
	key := analyzeKey{MetaGUID: "guid", MetaType: storepb.MetaType_VIEW}

	analyzer.scheduleRetry(key)
	entry, ok := analyzer.retryMap.Load(key)
	require.True(t, ok)
	require.Equal(t, 1, entry.(analysisRetry).attempts)
	_, queued := analyzer.analyzeMap.Load(key)
	require.True(t, queued, "a retry must stay queued")

	analyzer.scheduleRetry(key)
	require.Equal(t, 2, mustRetry(t, analyzer, key).attempts)

	// A fresh queue request supersedes the pending backoff.
	analyzer.QueueAnalysis("guid", storepb.MetaType_VIEW)
	_, ok = analyzer.retryMap.Load(key)
	require.False(t, ok)

	// After the last allowed attempt the object is left to the full scan: the
	// next failure is the one that exceeds the cap.
	for range maxAnalysisRetries + 1 {
		analyzer.scheduleRetry(key)
	}
	_, ok = analyzer.retryMap.Load(key)
	require.False(t, ok)
	_, queued = analyzer.analyzeMap.Load(key)
	require.False(t, queued)
}

func mustRetry(t *testing.T, analyzer *Analyzer, key analyzeKey) analysisRetry {
	t.Helper()
	v, ok := analyzer.retryMap.Load(key)
	require.True(t, ok)
	entry, ok := v.(analysisRetry)
	require.True(t, ok)
	return entry
}

// An emptied definition takes the failure path, not the skip path: the previous
// definition's edges are cleared, so the version row must carry a non-empty
// message explaining why the graph is empty. A parse failure names its cause.
func TestAnalysisFailureMessage(t *testing.T) {
	t.Parallel()

	require.Equal(t, "lineage analysis skipped: the object has no SQL definition", analysisFailureMessage(nil))
	require.Equal(t, "lineage analysis failed: unexpected token", analysisFailureMessage(errors.New("unexpected token")))
}
