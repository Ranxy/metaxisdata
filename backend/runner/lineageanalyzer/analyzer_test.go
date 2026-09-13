package lineageanalyzer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestBuildSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotDef, gotWrapped, err := buildSQL("active_users", tt.engine, tt.metaType, tt.resource)
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
