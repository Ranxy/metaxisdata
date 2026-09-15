package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"

	// The analyzers register themselves in an init function, and the API package
	// does not import them (the server does, in backend/server/ultimate.go), so
	// a test that exercises analysis has to pull the engine in itself.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/mysql"
)

func TestAnalyzeSQLScopeContextReadsTheSegmentCount(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		scope string
		want  catalog.AnalysisContext
	}{
		{
			name:  "database scope leaves the schema empty",
			scope: "1;shop",
			want:  catalog.AnalysisContext{InstanceID: "1", Database: "shop"},
		},
		{
			name:  "schema scope carries the schema",
			scope: "1;shop;public",
			want:  catalog.AnalysisContext{InstanceID: "1", Database: "shop", Schema: "public"},
		},
		{
			name:  "an empty schema segment is kept as empty",
			scope: "1;shop;",
			want:  catalog.AnalysisContext{InstanceID: "1", Database: "shop"},
		},
		{
			name:  "the separator inside a name stays escaped",
			scope: "1;db%3B2;public",
			want:  catalog.AnalysisContext{InstanceID: "1", Database: "db;2", Schema: "public"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := analyzeSQLScopeContext(tc.scope)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	for _, scope := range []string{"", "1", "1;", ";shop", "1;;public", "1;shop;public;table"} {
		_, err := analyzeSQLScopeContext(scope)
		require.Error(t, err, "scope %q must be rejected", scope)
	}
}

// A bare SELECT has no target in the metadata registry. The analyzers report a
// synthetic "__result__" table for it, and turning that into a GUID would
// invent an object that can never be looked up.
func TestBuildAnalyzeSQLRelationsKeepsTemporaryTargetsUnaddressed(t *testing.T) {
	t.Parallel()

	analysisContext := catalog.AnalysisContext{InstanceID: "1", Database: "shop"}

	relations := analyzeMySQL(t, analysisContext, "SELECT amount FROM orders")
	require.Len(t, relations, 1)
	require.Empty(t, relations[0].GetTargetGuid(), "a bare SELECT has no target object")
	require.Equal(t, "amount", relations[0].GetTargetColumn())
	require.True(t, relations[0].GetIsTemp())
	require.Equal(t, "1;shop;;orders", relations[0].GetSourceGuid())
	require.Equal(t, "amount", relations[0].GetSourceColumn())
}

func TestBuildAnalyzeSQLRelationsAddressesRealTargets(t *testing.T) {
	t.Parallel()

	analysisContext := catalog.AnalysisContext{InstanceID: "1", Database: "shop"}

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{
			name: "create view",
			sql:  "CREATE VIEW daily AS SELECT amount FROM orders",
		},
		{
			name: "insert into select",
			sql:  "INSERT INTO daily (amount) SELECT amount FROM orders",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			relations := analyzeMySQL(t, analysisContext, tc.sql)
			require.NotEmpty(t, relations)

			for _, relation := range relations {
				require.False(t, relation.GetIsTemp(), "the target is a real object")
				require.Equal(t, "1;shop;;daily", relation.GetTargetGuid())
				require.Equal(t, "1;shop;;orders", relation.GetSourceGuid())
			}
		})
	}
}

// The CLI's default hides temporary relations, so it matters exactly when they
// exist. They survive the conversion only for a statement whose result is not
// written anywhere: as soon as a real target exists, every `__result__` edge is
// dropped as a duplicate of it. A bare SELECT is therefore the case where
// hiding them hides the whole answer.
func TestBuildAnalyzeSQLRelationsOnlyReportsTemporaryRelationsForQueryOnlyStatements(t *testing.T) {
	t.Parallel()

	analysisContext := catalog.AnalysisContext{InstanceID: "1", Database: "shop"}

	for _, sql := range []string{
		"SELECT amount FROM orders",
		"WITH recent AS (SELECT amount FROM orders) SELECT amount FROM recent",
	} {
		relations := analyzeMySQL(t, analysisContext, sql)
		require.NotEmpty(t, relations, "sql %q", sql)
		for _, relation := range relations {
			require.True(t, relation.GetIsTemp(), "sql %q has no real target, so every relation is temporary", sql)
		}
	}

	for _, sql := range []string{
		"CREATE VIEW daily AS SELECT amount FROM orders",
		"INSERT INTO daily (amount) SELECT amount FROM orders",
	} {
		relations := analyzeMySQL(t, analysisContext, sql)
		require.NotEmpty(t, relations, "sql %q", sql)
		for _, relation := range relations {
			require.False(t, relation.GetIsTemp(), "sql %q writes somewhere real, so no temporary relation survives", sql)
		}
	}
}

// A schema-scoped statement resolves into the schema it was analyzed with,
// which is what makes a PostgreSQL scope different from a MySQL one.
func TestBuildAnalyzeSQLRelationsUsesTheScopeSchema(t *testing.T) {
	t.Parallel()

	relations := analyzeMySQL(t, catalog.AnalysisContext{InstanceID: "1", Database: "shop", Schema: "public"}, "SELECT amount FROM orders")
	require.Len(t, relations, 1)
	require.Equal(t, "1;shop;public;orders", relations[0].GetSourceGuid())
}

func TestBuildAnalyzeSQLRelationsKeepsExplicitQualifiers(t *testing.T) {
	t.Parallel()

	// A statement that names its own database is not overwritten by the scope.
	relations := analyzeMySQL(t, catalog.AnalysisContext{InstanceID: "1", Database: "shop"}, "SELECT amount FROM other.orders")
	require.Len(t, relations, 1)
	require.Equal(t, "1;other;;orders", relations[0].GetSourceGuid())
}

// analyzeMySQL runs the real analyzer through the same helper the handler uses,
// so the assertions above are about the conversion rather than about SQL
// parsing.
func analyzeMySQL(t *testing.T, analysisContext catalog.AnalysisContext, sql string) []*v1pb.AnalyzeSQLRelation {
	t.Helper()
	analyzed, err := lineage.GetAnalyzeRelation(catalog.WithAnalysisContext(t.Context(), analysisContext), storepb.Engine_MYSQL, sql)
	require.NoError(t, err)

	converted, guids := buildAnalyzeSQLRelations(analysisContext, analyzed)
	require.Len(t, guids, len(converted))
	return converted
}
