package mssql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	nodes "github.com/bytebase/omni/mssql/ast"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
)

// TestRecoversWithClauseFromDroppedStatement pins the recovery itself: the T-SQL parser
// reads a CTE list that is not followed by SELECT and then throws it away, so the list is
// taken from the source text instead. See .agents/docs/omni-upstream-defects.md section 10.1.
func TestRecoversWithClauseFromDroppedStatement(t *testing.T) {
	t.Parallel()

	withClause, ok := recoverWithClause("WITH c (x) AS (SELECT a FROM t) INSERT INTO t2 (a) SELECT x FROM c")
	require.True(t, ok)
	require.Len(t, withClause.CTEs.Items, 1)

	// A statement holding no data-modification keyword has no boundary to cut at, and one
	// whose prefix does not parse recovers nothing.
	_, ok = recoverWithClause("SELECT a FROM t")
	require.False(t, ok)
	_, ok = recoverWithClause("WITH c AS (SELECT a FROM t" /* unterminated */)
	require.False(t, ok)
}

// TestWithClauseBoundaryIgnoresNestedKeywords pins that the cut is made at the statement's
// own keyword: one inside a CTE body belongs to that body, and a bracketed name that spells
// one is a name.
func TestWithClauseBoundaryIgnoresNestedKeywords(t *testing.T) {
	t.Parallel()

	suffix := "UPDATE x SET a = 1 FROM x JOIN d ON x.id = d.a"
	sql := "WITH [insert] AS (SELECT a FROM t), d AS (SELECT a FROM [insert] WHERE b = 'delete') " + suffix
	boundary, ok := withClauseBoundary(sql)
	require.True(t, ok)
	require.Equal(t, len(sql)-len(suffix), boundary)
}

// TestRecoveryOnlyAttachesToADataModificationStatement pins the other half of the seam: the
// recovery has no target on a query, and a statement that already carries a CTE list keeps
// the one it has.
func TestRecoveryOnlyAttachesToADataModificationStatement(t *testing.T) {
	t.Parallel()

	withClause, ok := recoverWithClause("WITH c AS (SELECT a FROM t) DELETE FROM x WHERE a = 1")
	require.True(t, ok)

	require.False(t, attachWithClause(&nodes.SelectStmt{}, withClause))
	require.False(t, attachWithClause(nil, withClause))

	carrying := &nodes.InsertStmt{WithClause: withClause}
	require.False(t, attachWithClause(carrying, withClause))
	require.Equal(t, withClause, carrying.WithClause)
}

// TestWithClauseBeforeEachDMLKindIsAnalyzed pins that every data-modification statement the
// parser drops a CTE list from is analyzed with the definitions recovered. The edges each one
// produces are pinned by the corpus; what is checked here is that none of them fails.
func TestWithClauseBeforeEachDMLKindIsAnalyzed(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"WITH c AS (SELECT a FROM t) INSERT INTO t2 (a) SELECT a FROM c",
		"WITH c AS (SELECT id FROM t) UPDATE x SET a = 1 FROM x JOIN c ON x.id = c.id",
		"WITH c AS (SELECT id FROM t) DELETE x FROM x JOIN c ON x.id = c.id",
		"WITH c AS (SELECT a FROM s) MERGE INTO tgt USING c ON tgt.a = c.a WHEN MATCHED THEN UPDATE SET tgt.a = c.a",
	} {
		relations, err := analyzeSQL(sql, nil)
		require.NoError(t, err, sql)
		require.NotEmpty(t, relations, sql)
	}
}

// TestWithClauseBeforeDMLSurvivesTheScriptSplitter pins the half the runner sees: it hands the
// analyzer one statement at a time, and the statements around this one must not be lost with
// the text span the parser could not account for.
func TestWithClauseBeforeDMLSurvivesTheScriptSplitter(t *testing.T) {
	t.Parallel()

	analyzer := lineage.NewAnalyzer(nil, Registration())
	relations, err := analyzer.Analyze(context.TODO(), storepb.Engine_MSSQL,
		"WITH c AS (SELECT a FROM t) INSERT INTO t2 (a) SELECT a FROM c;\nGO\nSELECT b FROM u")
	require.NoError(t, err)
	require.Len(t, relations, 2, "both the CTE write and the statement after it are analyzed")
	require.Equal(t, "t", relations[0].Source.Table.Name)
	require.Equal(t, "u", relations[1].Source.Table.Name)
}
