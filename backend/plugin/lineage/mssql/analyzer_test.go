package mssql

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestSplitStatements pins the script splitter, which is the one piece of the dialect
// the runner calls on text the analyzer never sees whole. The parse is the primary
// splitter and the lexical scan is the fallback, so both halves are pinned here.
func TestSplitStatements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "one statement",
			sql:  "SELECT a FROM t",
			want: []string{"SELECT a FROM t"},
		},
		{
			name: "two statements",
			sql:  "SELECT a FROM t; SELECT b FROM u",
			want: []string{"SELECT a FROM t;", " SELECT b FROM u"},
		},
		{
			name: "go separates batches and is not a statement",
			sql:  "SELECT a FROM t\nGO\nSELECT b FROM u",
			want: []string{"SELECT a FROM t", "\nSELECT b FROM u"},
		},
		{
			name: "blank and comment-only input carries no statement",
			sql:  "  \n-- nothing here\n/* nor here */\n",
			want: nil,
		},
		{
			name: "a terminator inside a literal does not split",
			sql:  "SELECT ';' AS a FROM t",
			want: []string{"SELECT ';' AS a FROM t"},
		},
		{
			name: "a terminator inside a bracketed identifier does not split",
			sql:  "SELECT [a;b] FROM t",
			want: []string{"SELECT [a;b] FROM t"},
		},
		{
			name: "a terminator inside a nested block comment does not split",
			sql:  "SELECT /* a; /* b; */ still; */ a FROM t",
			want: []string{"SELECT /* a; /* b; */ still; */ a FROM t"},
		},
		{
			name: "the fallback keeps a statement whose text span the parser lost",
			sql:  "SELECT a FROM t1 UNION ALL SELECT b FROM t2; SELECT c FROM t3",
			want: []string{"SELECT a FROM t1 UNION ALL SELECT b FROM t2", " SELECT c FROM t3"},
		},
		{
			name: "a with clause in front of a dml statement stays one statement",
			sql:  "WITH c AS (SELECT a FROM t) INSERT INTO t2 (a) SELECT a FROM c",
			want: []string{"WITH c AS (SELECT a FROM t) INSERT INTO t2 (a) SELECT a FROM c"},
		},
		{
			name: "a semicolon inside a procedure body keeps the procedure whole",
			sql:  "CREATE PROCEDURE p AS BEGIN SELECT a FROM t; SELECT b FROM u; END;",
			want: []string{"CREATE PROCEDURE p AS BEGIN SELECT a FROM t; SELECT b FROM u; END;"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, SplitStatements(test.sql))
		})
	}
}

// TestCrossDatabaseQualifierIsReported pins the one identifier rule this dialect has
// that its siblings do not: an edge records a relation's schema and the registry fills
// its database in from the analysis context, so a name that points at another database
// cannot be recorded at all. It is reported as a gap instead of being read as a
// same-named object of the current database, which would be a wrong edge.
func TestCrossDatabaseQualifierIsReported(t *testing.T) {
	t.Parallel()

	ctx := catalog.WithAnalysisContext(context.TODO(), catalog.AnalysisContext{
		InstanceID: "i", Database: "main", Schema: "dbo",
	})
	analyzer := NewAnalyzer(ctx, "SELECT other.dbo.t.a FROM other.dbo.t", nil)
	relations, err := analyzer.AnalyzeRelations()

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, unsupported.Error(), "unresolved reference in a relation: other.dbo.t")
	require.Empty(t, relations, "an edge into another database would name the wrong object")
}

// TestSameDatabaseQualifierIsAccepted pins the other half: three-part names that agree
// with the analysis context are ordinary schema-qualified names, and MSSQL writes them
// often.
func TestSameDatabaseQualifierIsAccepted(t *testing.T) {
	t.Parallel()

	ctx := catalog.WithAnalysisContext(context.TODO(), catalog.AnalysisContext{
		InstanceID: "i", Database: "main", Schema: "dbo",
	})
	relations, err := NewAnalyzer(ctx, "SELECT main.dbo.t.a FROM main.dbo.t", nil).AnalyzeRelations()
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "dbo", relations[0].Source.Table.Schema)
	require.Equal(t, "t", relations[0].Source.Table.Name)
}

// TestKeywordAliasIsNotAnAlias pins the workaround for two parser defects: `FROM t1
// NATURAL JOIN t2` makes NATURAL t1's alias, and `MERGE INTO tgt USING src` makes USING
// the target's alias. A relation addressed by a keyword would not resolve any of the
// references the statement actually makes.
func TestKeywordAliasIsNotAnAlias(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT t1.a FROM t1 NATURAL JOIN t2", nil)
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "t1", relations[0].Source.Table.Name)
	require.Equal(t, "a", relations[0].Source.Name)

	relations, err = analyzeSQL("MERGE INTO tgt USING src ON tgt.id = src.id WHEN MATCHED THEN UPDATE SET tgt.a = src.a", nil)
	require.NoError(t, err)
	// The assignment is one edge per source column; the ON condition is an influence on
	// the target's rows, whose target column is empty.
	assigned := relationsOf(relations, "a")
	require.Len(t, assigned, 1)
	require.Equal(t, "src", assigned[0].Source.Table.Name)
	require.Equal(t, "tgt", assigned[0].Target.Table.Name)
}

// relationsOf returns the edges that write one target column.
func relationsOf(relations []model.ColumnRelation, column string) []model.ColumnRelation {
	out := make([]model.ColumnRelation, 0, len(relations))
	for _, relation := range relations {
		if relation.Target.Name == column {
			out = append(out, relation)
		}
	}
	return out
}

// TestQuotedKeywordAliasIsKept pins that the workaround does not misfire on a
// statement that really did write the keyword as an alias.
func TestQuotedKeywordAliasIsKept(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT [USING].a FROM t AS [USING]", nil)
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "t", relations[0].Source.Table.Name)
}

// TestCatalogIsReadOncePerRelation pins the analyzer-level catalog cache, which every
// dialect shares: a statement that names a relation twice reads it once however many
// references it makes.
func TestCatalogIsReadOncePerRelation(t *testing.T) {
	t.Parallel()

	cat := testutil.NewMemoryCatalogProvide()
	cat.AddTable(&catalog.TableMeta{
		ID:      model.ObjectIdentifier{Name: "t"},
		Columns: []catalog.ColumnMeta{{Name: "a"}, {Name: "b"}},
	})

	_, err := analyzeSQL("SELECT * FROM t x JOIN t y ON x.a = y.b", cat)
	require.NoError(t, err)
	require.Equal(t, 1, cat.Calls(), "the same relation was read more than once")
}

// TestUnaddressableTargetRecordsNoEdge pins that a data-modification statement whose target
// the registry cannot address returns no edge at all, rather than edges whose target has no
// schema and no name. MERGE is the shape that used to slip: every WHEN clause writes the
// target, so running them against an unknown target recorded `src.a -> .a` and
// `.* -> .__deletion__`, and the runner persists whatever the analyzer returns.
func TestUnaddressableTargetRecordsNoEdge(t *testing.T) {
	t.Parallel()

	ctx := catalog.WithAnalysisContext(context.TODO(), catalog.AnalysisContext{
		InstanceID: "i", Database: "main", Schema: "dbo",
	})
	for _, tc := range []struct{ name, sql string }{
		{"merge update", "MERGE INTO other.dbo.tgt USING src ON 1 = 1 WHEN MATCHED THEN UPDATE SET a = src.a"},
		{"merge delete", "MERGE INTO other.dbo.tgt USING src ON 1 = 1 WHEN NOT MATCHED BY SOURCE THEN DELETE"},
		{"merge insert", "MERGE INTO other.dbo.tgt USING src ON 1 = 1 WHEN NOT MATCHED THEN INSERT (a) VALUES (src.a)"},
		{"insert", "INSERT INTO other.dbo.t2 (a) SELECT b FROM t1"},
		{"update", "UPDATE other.dbo.t2 SET a = b"},
		{"delete", "DELETE FROM other.dbo.t2 WHERE a = 1"},
		{"select into", "SELECT a INTO other.dbo.t2 FROM t1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			relations, err := NewAnalyzer(ctx, tc.sql, nil).AnalyzeRelations()
			require.Error(t, err)
			require.Empty(t, relations, "an edge into another database would name the wrong object")
		})
	}
}

// TestCatalogFailureIsReportedAsAGap pins that a lookup the catalog could not answer is
// visible in the result: an outage must not be indistinguishable from a complete
// analysis.
func TestCatalogFailureIsReportedAsAGap(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT * FROM t", testutil.NewFailingCatalogProvide(errors.New("connection reset")))

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, unsupported.Error(), "catalog lookup failed for t: connection reset")
	require.NotEmpty(t, relations, "the wildcard fallback edge is still produced")
	require.Len(t, unsupported.Diagnostics, 1)
	require.Equal(t, model.DiagnosticCatalogUnavailable, unsupported.Diagnostics[0].Category)
}

// TestAQueryLocalColumnWithNoSourceIsNotAGap pins the other half of the rule that a query-local
// relation has to answer a reference. A column the relation does offer, but whose own lineage
// names no source, is a column built from constants: reporting it would turn every derived table
// that computes a literal into a partial analysis.
func TestAQueryLocalColumnWithNoSourceIsNotAGap(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT d.a, d.b FROM (SELECT a, 1 AS b FROM t) d", nil)
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "t", relations[0].Source.Table.Name)
	require.Equal(t, "a", relations[0].Source.Name)

	// The same rule for an inline table: its rows are literals, so it has no lineage to record and
	// a reference to it is not a gap.
	relations, err = analyzeSQL("SELECT v.x FROM (VALUES (1), (2)) v(x)", nil)
	require.NoError(t, err)
	require.Empty(t, relations)
}

// TestWithClauseBeforeEachDMLKindIsAnalyzed pins that a WITH clause in front of a
// data-modification statement is analyzed with its definitions. The parser attaches the CTE
// list to the DML statement, and every DML kind has to resolve its references through it; a
// regression that dropped the list again would leave those references unresolved. The edges each
// one produces are pinned by the corpus, so what is checked here is that none of them fails.
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
