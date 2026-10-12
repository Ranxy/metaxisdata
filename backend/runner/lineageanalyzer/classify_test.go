package lineageanalyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// classifyContext is the scope a reported statement is resolved against: the statement itself
// only ever names objects down to database/schema level.
var classifyContext = catalog.AnalysisContext{InstanceID: "inst-1", Database: "shop"}

const classifyStatementGUID = "inst-1;shop;;__application_sql__/abc123"

func TestClassifyApplicationSQL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		engine     storepb.Engine
		definition string
		relations  []model.ColumnRelation
		wantKind   store.ApplicationSQLKind
		wantTarget []string
	}{
		{
			name:       "a write the analyzer answered",
			engine:     storepb.Engine_MYSQL,
			definition: "INSERT INTO orders (id) SELECT id FROM staging",
			relations: []model.ColumnRelation{{
				Source: model.Column{Table: model.ObjectIdentifier{Name: "staging"}, Name: "id"},
				Target: model.Column{Table: model.ObjectIdentifier{Name: "orders"}, Name: "id"},
			}},
			wantKind: store.ApplicationSQLKindWrite,
		},
		{
			// The ORM's row insert: every written value comes from a placeholder, so the
			// analyzer records nothing and the target has to be read from the statement.
			name:       "a constant insert is still a write",
			engine:     storepb.Engine_MYSQL,
			definition: "INSERT INTO orders (id, total) VALUES (?, ?)",
			wantKind:   store.ApplicationSQLKindWrite,
			wantTarget: []string{"inst-1;shop;;orders"},
		},
		{
			name:       "a constant insert with a qualified target",
			engine:     storepb.Engine_MYSQL,
			definition: "INSERT INTO otherdb.orders (id) VALUES (?)",
			wantKind:   store.ApplicationSQLKindWrite,
			// The qualifier is the database in the MySQL family, and the analysis context
			// supplies the rest.
			wantTarget: []string{"inst-1;otherdb;;orders"},
		},
		{
			// A DELETE that references no column has no relation either, and is a write.
			name:       "a bare delete is a write",
			engine:     storepb.Engine_MYSQL,
			definition: "DELETE FROM orders",
			wantKind:   store.ApplicationSQLKindWrite,
			wantTarget: []string{"inst-1;shop;;orders"},
		},
		{
			name:       "a postgres constant insert",
			engine:     storepb.Engine_POSTGRES,
			definition: "INSERT INTO orders (id) VALUES (?)",
			wantKind:   store.ApplicationSQLKindWrite,
			wantTarget: []string{"inst-1;shop;;orders"},
		},
		{
			// A read keeps its node through the source edge the caller writes, and needs no
			// out-edge.
			name:       "a read",
			engine:     storepb.Engine_MYSQL,
			definition: "SELECT id FROM orders WHERE status = ?",
			relations: []model.ColumnRelation{{
				Source: model.Column{Table: model.ObjectIdentifier{Name: "orders"}, Name: "id"},
				Target: model.Column{Table: model.ObjectIdentifier{Name: model.ResultTableName}, Name: "id"},
				IsTemp: true,
			}},
			wantKind: store.ApplicationSQLKindRead,
		},
		{
			// Neither an edge nor a write: the statement stays in the registry and never
			// reaches the canvas, which is where manual SQL of the same shape ends up too.
			name:       "a constant select is neither",
			engine:     storepb.Engine_MYSQL,
			definition: "SELECT 1",
			wantKind:   store.ApplicationSQLKindRead,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			kind, edges := classifyApplicationSQL(classifyContext, c.engine, c.definition, c.relations, classifyStatementGUID)
			require.Equal(t, c.wantKind, kind)

			var targets []string
			for _, edge := range edges {
				require.Equal(t, classifyStatementGUID, edge.SourceGUID, "a constant write's out-edge starts at the statement")
				require.Equal(t, classifyStatementGUID, edge.MetaGUID)
				require.Equal(t, storepb.MetaType_APPLICATION_SQL, edge.MetaType)
				// A table-level edge: it names a table, not a column pair.
				require.Empty(t, edge.SourceColumn)
				require.Empty(t, edge.TargetColumn)
				require.Empty(t, edge.Transformation)
				require.Equal(t, model.RelationTypeDirect, edge.RelationType)
				targets = append(targets, edge.TargetGUID)
			}
			require.ElementsMatch(t, c.wantTarget, targets)
		})
	}
}

// TestClassifyApplicationSQLNeverPointsAtItself pins that a statement whose target resolves
// back to the statement's own GUID yields no edge: a self-edge is the one shape the graph walk
// drops, so writing one would only add a row nothing reads.
func TestClassifyApplicationSQLNeverPointsAtItself(t *testing.T) {
	t.Parallel()

	_, edges := classifyApplicationSQL(classifyContext, storepb.Engine_MYSQL,
		"INSERT INTO __application_sql__/abc123 (a) VALUES (?)", nil, classifyStatementGUID)
	require.Empty(t, edges)
}

// TestClassifyApplicationSQLDeduplicatesTargets pins that a statement naming the same table
// twice writes one edge per table, not one per mention.
func TestClassifyApplicationSQLDeduplicatesTargets(t *testing.T) {
	t.Parallel()

	kind, edges := classifyApplicationSQL(classifyContext, storepb.Engine_MYSQL,
		"INSERT INTO orders (a) VALUES (?), (?)", nil, classifyStatementGUID)
	require.Equal(t, store.ApplicationSQLKindWrite, kind)
	require.Len(t, edges, 1)
}
