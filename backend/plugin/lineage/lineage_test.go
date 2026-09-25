// Package lineage_test pins the contract the root package adds on top of the
// dialect analyzers: a script is split into statements here, each statement is fed
// to a single-statement analyzer, and their edges are merged.
package lineage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// analyzer is the root package over the engines this build supports: the dialects
// export their registrations and the process assembles them, which is what the
// tests here exercise. A nil catalog is enough for the statements they use.
var analyzer = lineage.NewAnalyzer(nil, engines.Registrations()...)

// scriptEngines are the engines that register a splitter, so every one of them has
// to agree on what a multi-statement script means.
var scriptEngines = []storepb.Engine{
	storepb.Engine_MYSQL,
	storepb.Engine_TIDB,
	storepb.Engine_MARIADB,
	storepb.Engine_POSTGRES,
	storepb.Engine_STARROCKS,
}

// TestScriptStatementsStayIndependent pins the unified multi-statement policy.
// Before the split moved above the dialects the engines disagreed: the MySQL
// family rejected a script whole, so a MANUAL_SQL script failed and the runner
// cleared the object's stored lineage, while PostgreSQL analyzed it.
func TestScriptStatementsStayIndependent(t *testing.T) {
	t.Parallel()

	for _, engine := range scriptEngines {
		t.Run(engine.String(), func(t *testing.T) {
			t.Parallel()

			relations, err := analyzer.Analyze(context.Background(), engine,
				"SELECT a FROM t1; SELECT b FROM t2")
			require.NoError(t, err)
			require.True(t, hasScriptEdge(relations, "t1", "a", model.ResultTableName, "a"),
				"the first statement's edge is missing: %s", testutil.FormatRelations(relations))
			require.True(t, hasScriptEdge(relations, "t2", "b", model.ResultTableName, "b"),
				"the second statement's edge is missing: %s", testutil.FormatRelations(relations))
		})
	}
}

// TestScriptStatementsDoNotShareScope pins that each statement is analyzed as if
// it were alone. Both cases are the ones the PostgreSQL corpus used to pin while
// that analyzer walked a whole script itself: a predicate collected while
// analyzing one statement must not reach the next statement's result, and a CTE a
// statement declares must not resolve in the statement after it.
func TestScriptStatementsDoNotShareScope(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES} {
		t.Run(engine.String(), func(t *testing.T) {
			t.Parallel()

			// The UPDATE's subquery WHERE decides which rows the subquery yields,
			// and it used to be attributed to the next statement's result.
			relations, err := analyzer.Analyze(context.Background(), engine,
				"UPDATE t SET a = (SELECT y FROM s WHERE s.z > 1); SELECT x FROM u")
			require.NoError(t, err)
			require.True(t, hasScriptEdge(relations, "s", "y", "t", "a"),
				"the UPDATE's own edge is missing: %s", testutil.FormatRelations(relations))
			require.True(t, hasScriptEdge(relations, "u", "x", model.ResultTableName, "x"),
				"the SELECT's own edge is missing: %s", testutil.FormatRelations(relations))
			for _, relation := range relations {
				require.NotEqual(t, "s.z", relation.Source.Table.Name+"."+relation.Source.Name,
					"the subquery predicate leaked out of its statement: %s", testutil.FormatRelations(relations))
			}

			// The second statement names `c`, which only the first statement
			// declared, so it is a stored relation the second statement reads.
			relations, err = analyzer.Analyze(context.Background(), engine,
				"WITH c AS (SELECT x FROM s) SELECT x FROM c; SELECT y FROM c")
			require.NoError(t, err)
			require.True(t, hasScriptEdge(relations, "s", "x", model.ResultTableName, "x"),
				"the first statement's edge is missing: %s", testutil.FormatRelations(relations))
			require.True(t, hasScriptEdge(relations, "c", "y", model.ResultTableName, "y"),
				"the CTE leaked into the next statement, or its edge is missing: %s", testutil.FormatRelations(relations))
		})
	}
}

// TestUnmodelledStatementIsAGapBesideTheOthers pins the error taxonomy the
// engines now share: a statement shape an analyzer does not model is a gap beside
// the edges the other statements produced, never a failure that discards them.
// StarRocks used to fail the whole script on a MERGE while PostgreSQL reported
// the same gap, which made "the runner keeps the lineage and records the gap" or
// "the runner clears the lineage" depend on the engine.
func TestUnmodelledStatementIsAGapBesideTheOthers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		engine storepb.Engine
		merge  string
	}{
		{
			engine: storepb.Engine_POSTGRES,
			merge:  "MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET a = s.a",
		},
		{
			engine: storepb.Engine_STARROCKS,
			merge:  "MERGE INTO t1 USING t2 ON t1.id = t2.id WHEN MATCHED THEN UPDATE SET t1.name = t2.tag",
		},
	} {
		t.Run(tc.engine.String(), func(t *testing.T) {
			t.Parallel()

			relations, err := analyzer.Analyze(context.Background(), tc.engine,
				"SELECT a FROM t1; "+tc.merge)

			var unsupported *lineage.UnsupportedStatementError
			require.ErrorAs(t, err, &unsupported, "a MERGE has to be reported, not silently skipped")
			require.Contains(t, unsupported.Error(), "MERGE")
			require.True(t, hasScriptEdge(relations, "t1", "a", model.ResultTableName, "a"),
				"the other statement's edge must survive: %s", testutil.FormatRelations(relations))
		})
	}
}

// TestScriptParseErrorFailsWhole pins that a script whose text does not parse
// yields no result at all: the caller discards what it stored, because nothing
// about a script that cannot be read can be trusted. It is the only failure that
// clears an object's lineage.
func TestScriptParseErrorFailsWhole(t *testing.T) {
	t.Parallel()

	for _, engine := range scriptEngines {
		t.Run(engine.String(), func(t *testing.T) {
			t.Parallel()

			relations, err := analyzer.Analyze(context.Background(), engine,
				"SELECT a FROM t1; THIS IS NOT SQL")
			require.Error(t, err)
			require.Nil(t, relations)
		})
	}
}

// TestPartialAnalysisReportsStructuredDiagnostics pins that a partial analysis
// says what is missing as data and not only as prose: the API layer maps the
// categories onto its own enum, so a caller can act on the cause instead of
// parsing the server's sentence. The rendered text stays the contract the corpus
// and the stored version message rely on.
func TestPartialAnalysisReportsStructuredDiagnostics(t *testing.T) {
	t.Parallel()

	relations, err := analyzer.Analyze(context.Background(), storepb.Engine_MYSQL,
		"SELECT id FROM a; WITH c AS (SELECT id FROM a) DELETE FROM dst WHERE id IN (SELECT id FROM c)")
	require.True(t, hasScriptEdge(relations, "a", "id", model.ResultTableName, "id"),
		"the other statement's edge must survive: %s", testutil.FormatRelations(relations))

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Len(t, unsupported.Diagnostics, 1)
	require.Equal(t, model.DiagnosticNotModelled, unsupported.Diagnostics[0].Category)
	require.Equal(t, "WITH before DELETE", unsupported.Diagnostics[0].Subject)
	require.Zero(t, unsupported.Diagnostics[0].Reference)
	require.Contains(t, unsupported.Diagnostics[0].Detail, "drops the CTE")
	require.Zero(t, unsupported.Omitted)
	require.Contains(t, unsupported.Error(), "not modelled: WITH before DELETE")
}

// hasScriptEdge reports whether an edge connects the two named columns.
func hasScriptEdge(relations []model.ColumnRelation, sourceTable, sourceColumn, targetTable, targetColumn string) bool {
	for _, relation := range relations {
		if relation.Source.Table.Name == sourceTable && relation.Source.Name == sourceColumn &&
			relation.Target.Table.Name == targetTable && relation.Target.Name == targetColumn {
			return true
		}
	}
	return false
}
