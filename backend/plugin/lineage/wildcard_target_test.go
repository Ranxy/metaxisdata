package lineage_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// everyDialect is the engine set the cross-dialect rules are checked against.
var everyDialect = []storepb.Engine{
	storepb.Engine_MYSQL,
	storepb.Engine_TIDB,
	storepb.Engine_MARIADB,
	storepb.Engine_POSTGRES,
	storepb.Engine_STARROCKS,
	storepb.Engine_DORIS,
	storepb.Engine_MSSQL,
}

// forDialects builds the same statement for the named engines, prefixing the
// separator T-SQL needs before a statement that starts with WITH. The engines left
// out are the ones whose grammar or traversal does not reach this shape, and every
// omission has to be justified in the case that makes it.
func forDialects(sql string, want ...storepb.Engine) map[storepb.Engine]string {
	out := make(map[storepb.Engine]string, len(want))
	for _, engine := range want {
		if engine == storepb.Engine_MSSQL {
			out[engine] = ";" + sql
			continue
		}
		out[engine] = sql
	}
	return out
}

// wildcardTargetAnalyzer resolves the wildcard cases against a catalog that
// describes the relations they read and write: `src` and `dst` share their column
// names, `dst2` renames the second one and `dst3` adds a third, so a target list
// filled from the wrong position — or filled entirely from its first entry — shows
// up in the edge set.
func wildcardTargetAnalyzer() *lineage.Analyzer {
	cat := testutil.NewMemoryCatalogProvide()
	add := func(name string, columns ...string) {
		meta := &catalog.TableMeta{ID: model.ObjectIdentifier{Name: name}}
		for _, column := range columns {
			meta.Columns = append(meta.Columns, catalog.ColumnMeta{Name: column})
		}
		cat.AddTable(meta)
	}
	add("src", "k", "name", "x")
	add("dst", "k", "name")
	add("dst2", "k", "full_name")
	add("dst3", "k", "name", "x")
	return lineage.NewAnalyzer(cat, engines.Registrations()...)
}

// requireEdgesOfStatement asserts the exact endpoint set a statement produces.
func requireEdgesOfStatement(t *testing.T, analyzer *lineage.Analyzer, engine storepb.Engine, sql string, want []string) {
	t.Helper()

	relations, err := analyzer.Analyze(context.Background(), engine, sql)
	require.NoError(t, err, "%s: %s", engine, sql)

	got := make([]string, 0, len(relations))
	for _, relation := range relations {
		got = append(got, fmt.Sprintf("%s.%s -> %s.%s",
			relation.Source.Table.Name, relation.Source.Name,
			relation.Target.Table.Name, relation.Target.Name))
	}
	slices.Sort(got)
	slices.Sort(want)
	require.Equal(t, want, got, "%s:\n  %s\nall edges:%s", engine, sql, testutil.FormatRelations(relations))
}

// TestAWildcardOverAQueryLocalRelationFillsItsDeclaredTargetList pins the rule the
// six dialects but Doris got wrong: a `*` over a query-local relation whose columns
// the analyzer knows expands into one output column per column, from the position the
// wildcard sits at, so a writer's declared target list — which it reads one entry per
// output column — names every one of them. Keeping the wildcard whole put every column
// of the relation on the list's first entry (`src.name -> dst.k` and no edge to
// `dst.name`).
//
// The engines agree on the rule and were read statement by statement: `INSERT INTO dst
// (k, name) SELECT * FROM (SELECT k, name FROM src) d` writes each output column to the
// target at its own position in MySQL 8.3, StarRocks 4.1, Doris 4.1.4, PostgreSQL 16 and
// MSSQL 2019 (checked both with a positional list and with a renamed one, and with the
// wildcard followed by another output so the positions after it matter too).
//
// The MySQL family and StarRocks cannot spell `WITH … INSERT` (MySQL-family traversal
// drops the CTE as a reported gap; StarRocks' grammar rejects it), so the CTE half of
// the rule is pinned through a view body there. `* EXCEPT (…)` over a query-local
// relation is Doris's and StarRocks' spelling; both engines apply it (Doris 4.1.4 and
// StarRocks 4.1 each return `(1, 'z')` for the statement below), and both reject the
// shape whose exclusion list leaves no column at all.
func TestAWildcardOverAQueryLocalRelationFillsItsDeclaredTargetList(t *testing.T) {
	t.Parallel()

	analyzer := wildcardTargetAnalyzer()

	tests := []struct {
		name string
		sql  map[storepb.Engine]string
		want []string
	}{
		{
			name: "a positional declared target list",
			sql: forDialects("INSERT INTO dst (k, name) SELECT * FROM (SELECT k, name FROM src) d",
				everyDialect...),
			want: []string{"src.k -> dst.k", "src.name -> dst.name"},
		},
		{
			name: "a renamed declared target list",
			sql: forDialects("INSERT INTO dst2 (k, full_name) SELECT * FROM (SELECT k, name FROM src) d",
				everyDialect...),
			want: []string{"src.k -> dst2.k", "src.name -> dst2.full_name"},
		},
		{
			name: "a table-qualified wildcard before another output",
			sql: forDialects("INSERT INTO dst3 (k, name, x) SELECT d.*, name AS x FROM (SELECT k, name FROM src) d",
				everyDialect...),
			want: []string{"src.k -> dst3.k", "src.name -> dst3.name", "src.name -> dst3.x"},
		},
		{
			name: "an unqualified wildcard before another output",
			sql: forDialects("INSERT INTO dst3 (k, name, x) SELECT *, name AS x FROM (SELECT k, name FROM src) d",
				everyDialect...),
			want: []string{"src.k -> dst3.k", "src.name -> dst3.name", "src.name -> dst3.x"},
		},
		{
			name: "a wildcard over a query-local relation built on another one",
			sql: forDialects("INSERT INTO dst2 (k, full_name) SELECT * FROM (SELECT * FROM (SELECT k, name FROM src) inner_d) outer_d",
				everyDialect...),
			want: []string{"src.k -> dst2.k", "src.name -> dst2.full_name"},
		},
		{
			name: "a CTE standing before the INSERT",
			sql: forDialects("WITH c AS (SELECT k, name FROM src) INSERT INTO dst2 (k, full_name) SELECT * FROM c",
				storepb.Engine_POSTGRES, storepb.Engine_DORIS, storepb.Engine_MSSQL),
			want: []string{"src.k -> dst2.k", "src.name -> dst2.full_name"},
		},
		{
			name: "a CTE inside a view body",
			sql: forDialects("CREATE VIEW v (k, full_name) AS WITH c AS (SELECT k, name FROM src) SELECT * FROM c",
				everyDialect...),
			want: []string{"src.k -> v.k", "src.name -> v.full_name"},
		},
		{
			// The exclusion sits between two kept columns, so the entries after it have to
			// shift: reading the relation's own list instead of its outputs would name
			// `name` for the second target, which the engine never writes.
			name: "an EXCEPT list over a query-local relation",
			sql: forDialects("INSERT INTO dst2 (k, full_name) SELECT * EXCEPT (name) FROM (SELECT k, name, x FROM src) d",
				storepb.Engine_DORIS, storepb.Engine_STARROCKS),
			want: []string{"src.k -> dst2.k", "src.x -> dst2.full_name"},
		},
	}
	for _, tc := range tests {
		for _, engine := range everyDialect {
			sql, ok := tc.sql[engine]
			if !ok {
				continue
			}
			t.Run(tc.name+"/"+strings.ToLower(engine.String()), func(t *testing.T) {
				t.Parallel()

				requireEdgesOfStatement(t, analyzer, engine, sql, tc.want)
			})
		}
	}
}

// TestAWildcardOverARepeatedColumnNameKeepsTheWholeReading pins the second thing the rule
// must not change. A query-local relation's lineage is addressed by target name, so a
// relation exposing one name twice cannot be read column by column: PostgreSQL 16 returns
// two columns for `SELECT * FROM (SELECT k AS x, name AS x FROM src) d` and writes k to
// the first target and name to the second, but nothing in the analysis tells the two
// columns apart, so the relation keeps the whole-wildcard reading — every column of it on
// the target at the wildcard's position — instead of handing the same lineage to both
// entries of the declared list. Doris and StarRocks accept the shape as well (they return
// two columns both named x); the MySQL family and MSSQL reject it, so it is pinned where
// it is real SQL.
func TestAWildcardOverARepeatedColumnNameKeepsTheWholeReading(t *testing.T) {
	t.Parallel()

	analyzer := wildcardTargetAnalyzer()
	requireEdgesOfStatement(t, analyzer, storepb.Engine_POSTGRES,
		"INSERT INTO dst2 (k, full_name) SELECT * FROM (SELECT k AS x, name AS x FROM src) d",
		[]string{"src.k -> dst2.k", "src.name -> dst2.k"})
	// The same refusal covers names that differ only by case, which the trace compares
	// as one name.
	requireEdgesOfStatement(t, analyzer, storepb.Engine_POSTGRES,
		`INSERT INTO dst2 (k, full_name) SELECT * FROM (SELECT k AS "K", name AS "k" FROM src) d`,
		[]string{"src.k -> dst2.k", "src.name -> dst2.k"})
}

// TestAQueryLocalWildcardKeepsItsOwnExposure pins what the rule must not change: a
// wildcard over a relation whose columns the analyzer cannot name stays one output
// column whose whole lineage is answered, because nothing can say which of the
// relation's columns is which. The relation here is a derived table over a table the
// catalog does not describe, so the derived table exposes the single `*` its body
// produced.
//
// The entries a declared target list does not reach are not named — there is one output
// column, so only the target at its position is recorded. That is the pre-existing
// lenient reading for a relation the analyzer cannot describe, and it is why the rule
// above is stated for a relation whose columns are known.
func TestAQueryLocalWildcardKeepsItsOwnExposure(t *testing.T) {
	t.Parallel()

	analyzer := wildcardTargetAnalyzer()
	requireEdgesOfStatement(t, analyzer, storepb.Engine_MYSQL,
		"INSERT INTO dst (k, name) SELECT * FROM (SELECT * FROM undescribed) d",
		[]string{"undescribed.* -> dst.k"})
	requireEdgesOfStatement(t, analyzer, storepb.Engine_MYSQL,
		"INSERT INTO dst3 (x, k, name) SELECT name AS x, * FROM (SELECT * FROM undescribed) d",
		[]string{"undescribed.* -> dst3.k", "undescribed.* -> dst3.x"})
}
