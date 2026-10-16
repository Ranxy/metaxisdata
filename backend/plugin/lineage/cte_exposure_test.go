package lineage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// cteExposureAnalyzer resolves the exposure cases against a catalog describing the two
// source tables they read.
func cteExposureAnalyzer() *lineage.Analyzer {
	cat := testutil.NewMemoryCatalogProvide()
	add := func(name string, columns ...string) {
		meta := &catalog.TableMeta{ID: model.ObjectIdentifier{Name: name}}
		for _, column := range columns {
			meta.Columns = append(meta.Columns, catalog.ColumnMeta{Name: column})
		}
		cat.AddTable(meta)
	}
	add("t", "k")
	add("src", "k", "name")
	return lineage.NewAnalyzer(cat, engines.Registrations()...)
}

// TestACTEExposesTheColumnsItsBodyProduces pins what a CTE offers to whatever reads it:
// the columns its body produces, with a declared list renaming a prefix of them — never
// the names its lineage happens to carry. An output column with no source (a literal, an
// aggregate) has no lineage to record and is a column of the relation all the same.
//
// Describing a CTE from its lineage targets instead left that column out of the exposure,
// which cost two things on statements the engines run. A scope holding another relation
// denied the name — `WITH c AS (SELECT 1 AS x, k FROM t) SELECT c.k, x FROM c, t` is a
// reported gap in MySQL 8.3, TiDB, MariaDB, StarRocks 4.1 and MSSQL 2019 (the last also
// for the qualified `c.x`), while PostgreSQL 16 and Doris 4.1.4 were clean because their
// exposure was already the body's output list. And since a wildcard over the relation is
// filled positionally, a missing entry shifted a declared target list behind it:
// `CREATE VIEW v (full_name, k) AS WITH c AS (SELECT 1 AS full_name, k FROM src) SELECT *
// FROM c` recorded `src.k -> v.full_name` in five dialects where the engine writes the
// constant into `full_name` and the edge belongs to `v.k` (all five engines were read
// statement by statement, and the views they build hold `k = src.k`, `full_name = 1`).
//
// A declared list shorter than the body is where the engines part company, and the rule
// follows the ones that accept it: PostgreSQL 16 and Doris 4.1.4 expose `(a, name)` for
// `WITH c (a) AS (SELECT k, name FROM src)`, so the outputs the list does not reach keep
// their own names. MySQL 8.3, TiDB, MariaDB, StarRocks 4.1 and MSSQL 2019 accept a
// declaration only when its arity matches the body (MySQL and StarRocks answer
// `ERROR 1353`, T-SQL rejects either direction), so for every statement they execute this
// is the same list. The last two cases are those engines' alone: PostgreSQL takes the
// short declaration on a derived table's alias list too, which the others reject or do
// not spell.
func TestACTEExposesTheColumnsItsBodyProduces(t *testing.T) {
	t.Parallel()

	analyzer := cteExposureAnalyzer()

	tests := []struct {
		name string
		sql  map[storepb.Engine]string
		want []string
	}{
		{
			name: "a column with no source is exposed to a two-relation scope",
			sql: forDialects("WITH c AS (SELECT 1 AS x, k FROM t) SELECT c.k, x FROM c, t",
				everyDialect...),
			want: []string{"t.k -> __result__.k"},
		},
		{
			// The body is one wildcard output column, so the declaration is the only
			// account of the relation's columns and each name it gives takes an edge.
			name: "a declared list longer than a wildcard body is kept",
			sql: forDialects("WITH c (a, b) AS (SELECT * FROM undescribed) SELECT * FROM c",
				everyDialect...),
			want: []string{"undescribed.* -> __result__.a", "undescribed.* -> __result__.b"},
		},
		{
			name: "and a qualified reference to it resolves",
			sql: forDialects("WITH c AS (SELECT 1 AS x, k FROM t) SELECT c.x FROM c",
				everyDialect...),
			want: []string{},
		},
		{
			name: "a declared target list behind a wildcard over the CTE takes the right entry",
			sql: forDialects("CREATE VIEW v (full_name, k) AS WITH c AS (SELECT 1 AS full_name, k FROM src) SELECT * FROM c",
				everyDialect...),
			want: []string{"src.k -> v.k"},
		},
		{
			name: "a list shorter than the body renames what it reaches and leaves the rest",
			sql: forDialects("WITH c (a) AS (SELECT k, name FROM src) SELECT a, name FROM c",
				storepb.Engine_POSTGRES, storepb.Engine_DORIS),
			want: []string{"src.k -> __result__.a", "src.name -> __result__.name"},
		},
		{
			name: "the same rule on a derived table's declared list",
			sql: forDialects("SELECT m FROM (SELECT k, name FROM src) d (m)",
				storepb.Engine_POSTGRES),
			want: []string{"src.k -> __result__.m"},
		},
	}
	for _, tc := range tests {
		for _, engine := range everyDialect {
			sql, ok := tc.sql[engine]
			if !ok {
				continue
			}
			t.Run(tc.name+"/"+engine.String(), func(t *testing.T) {
				t.Parallel()

				requireEdgesOfStatement(t, analyzer, engine, sql, tc.want)
			})
		}
	}
}

// TestAColumnTheCTEBodyDoesNotProduceIsStillAGap keeps the other half of the rule: a
// complete exposure is not a licence to answer anything. `z` is not a column of either
// relation in scope, so the reference stays the reported gap it was — including when the
// CTE's exposure is the declared list of a body the analyzer could not enumerate, which is
// the reading that keeps it a *described* relation (an undescribed one answers any name
// through the fallback a lone relation keeps).
func TestAColumnTheCTEBodyDoesNotProduceIsStillAGap(t *testing.T) {
	t.Parallel()

	analyzer := cteExposureAnalyzer()
	for _, sql := range []string{
		"WITH c AS (SELECT k FROM t) SELECT z FROM c, t",
		"WITH c (a, b) AS (SELECT * FROM undescribed) SELECT z FROM c, t",
	} {
		for _, engine := range everyDialect {
			relations, err := analyzer.Analyze(context.Background(), engine, sql)
			var unsupported *lineage.UnsupportedStatementError
			require.ErrorAs(t, err, &unsupported, "%s: %s", engine, sql)
			require.Contains(t, err.Error(), "unresolved reference in an output column: z",
				"%s: %s", engine, sql)
			require.Empty(t, relations, "%s: %s", engine, sql)
		}
	}
}
