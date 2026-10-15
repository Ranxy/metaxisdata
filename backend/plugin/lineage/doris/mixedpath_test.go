package doris

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRewriteMixedVariantPaths pins the rewrite and what it refuses. The engine runs
// both spellings and answers the same value, so the all-bracket form is the same path;
// `a[1].b` is not, because it reads a field of an array element rather than a subcolumn,
// and it is left to fail rather than rewritten into something else.
func TestRewriteMixedVariantPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sql       string
		want      string
		wantEdits []textEdit
	}{
		{
			name: "one mixed segment",
			sql:  "SELECT v['user'].name FROM ev",
			want: "SELECT v['user']['name'] FROM ev",
			wantEdits: []textEdit{{
				rewrittenStart: 16, rewrittenEnd: 24, originalStart: 16, originalEnd: 21,
			}},
		},
		{
			name: "a dotted tail",
			sql:  "SELECT v['user'].name.deep FROM ev",
			want: "SELECT v['user']['name']['deep'] FROM ev",
			wantEdits: []textEdit{
				{rewrittenStart: 16, rewrittenEnd: 24, originalStart: 16, originalEnd: 21},
				{rewrittenStart: 24, rewrittenEnd: 32, originalStart: 21, originalEnd: 26},
			},
		},
		{
			name: "a key that is not an identifier stays a string",
			sql:  "SELECT v['user name'].x FROM t",
			want: "SELECT v['user name']['x'] FROM t",
		},
		{name: "the all-bracket form needs nothing", sql: "SELECT v['user']['name'] FROM ev", want: ""},
		{name: "an ordinary qualified name needs nothing", sql: "SELECT t.name FROM t", want: ""},
		{
			name: "an array element's field is not a subcolumn",
			sql:  "SELECT a[1].b FROM t",
			want: "",
		},
		{
			name: "only the mixed path is rewritten",
			sql:  "SELECT v['a'].b, w.x FROM t",
			want: "SELECT v['a']['b'], w.x FROM t",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, from, edits, ok := rewriteMixedVariantPaths(tc.sql)
			require.Equal(t, tc.want != "", ok, "sql=%q", tc.sql)
			require.Equal(t, tc.want, got, "sql=%q", tc.sql)
			if !ok {
				return
			}
			require.Equal(t, tc.sql, from)
			if tc.wantEdits != nil {
				require.Equal(t, tc.wantEdits, edits)
			}
			// Every edit has to stand for text the original really holds.
			for _, e := range edits {
				require.LessOrEqual(t, e.originalEnd, len(tc.sql), "edit %v", e)
				require.LessOrEqual(t, e.originalStart, e.originalEnd, "edit %v", e)
			}
		})
	}
}

// TestAMixedVARIANTpathKeepsItsLineageAndItsName pins the recovery end to end. The engine
// runs the mixed form (on 4.1.4 `SELECT v['user'].name FROM ev` answers `alice` and names
// the column `v['user'].name`) while omni stops at the dot, which used to cost the
// statement its whole lineage; the rewritten statement is analyzed instead, and an
// expression is named the way the statement wrote it, so the name the registry sees is
// the name the engine reports.
func TestAMixedVARIANTpathKeepsItsLineageAndItsName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want []string
	}{{
		name: "an unaliased select item keeps the path as its name",
		sql:  "SELECT v['user'].name FROM ev",
		want: []string{"ev.v -> __result__.v['user'].name"},
	}, {
		name: "a dotted tail keeps the whole path as its name",
		sql:  "SELECT v['user'].name.deep FROM ev",
		want: []string{"ev.v -> __result__.v['user'].name.deep"},
	}, {
		name: "an alias still names the column",
		sql:  "SELECT v['user'].name AS n FROM ev",
		want: []string{"ev.v -> __result__.n"},
	}, {
		name: "an insert writes the declared target",
		sql:  "INSERT INTO t2 (c) SELECT v['user'].name FROM ev",
		want: []string{"ev.v -> t2.c"},
	}, {
		name: "a predicate is an influence on the rows",
		sql:  "SELECT id FROM ev WHERE v['user'].name = 'alice'",
		want: []string{"ev.id -> __result__.id", "ev.v -> __result__."},
	}, {
		name: "a CTE exposes the path under the name the statement wrote",
		sql:  "WITH c AS (SELECT v['user'].name FROM ev) SELECT `v['user'].name` AS n FROM c",
		want: []string{"ev.v -> __result__.n"},
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requireEdgesWithCatalog(t, tc.sql, variantCatalog(), tc.want)
		})
	}
}
