package lineageanalyzer

import (
	"testing"

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
			engine:      storepb.Engine_CLICKHOUSE,
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
		tt := tt
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
