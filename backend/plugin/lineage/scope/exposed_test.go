package scope

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// outputColumns builds the body's outputs in order, the way an analyzer records them.
func outputColumns(aliases ...string) []OutputColumn {
	cols := make([]OutputColumn, 0, len(aliases))
	for _, alias := range aliases {
		cols = append(cols, OutputColumn{Alias: alias})
	}
	return cols
}

// wildcardOutput is the one output column the analyzer records for a body it could not
// enumerate — a `SELECT *` over a relation no catalog describes.
func wildcardOutput() []OutputColumn {
	return outputColumns(model.WildcardColumn)
}

// TestOutputColumnNamesRenamesAPrefixOfTheBody pins the rule the engine facts justify: a
// declared list renames the modelled outputs from the left and the outputs it does not
// reach keep their own names. PostgreSQL 16 and Doris 4.1.4 expose `(a, name)` for `WITH c
// (a) AS (SELECT k, name FROM t) SELECT …`, and the engines that accept a declared list
// only at equal arity (MySQL 8.3, TiDB, MariaDB, StarRocks 4.1, MSSQL 2019) execute nothing
// this rule reads differently. A longer list is a statement every one of them rejects, so
// the declaration is discarded rather than guessed at.
func TestOutputColumnNamesRenamesAPrefixOfTheBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		declared []string
		cols     []OutputColumn
		want     []string
	}{
		{
			name: "no declared list",
			cols: outputColumns("k", "name"),
			want: []string{"k", "name"},
		},
		{
			name:     "a list that covers the body",
			declared: []string{"a", "b"},
			cols:     outputColumns("k", "name"),
			want:     []string{"a", "b"},
		},
		{
			name:     "a list shorter than the body",
			declared: []string{"a"},
			cols:     outputColumns("k", "name"),
			want:     []string{"a", "name"},
		},
		{
			name:     "a list longer than the body",
			declared: []string{"a", "b", "c"},
			cols:     outputColumns("k", "name"),
			want:     []string{"k", "name"},
		},
		{
			name:     "a body with no outputs",
			declared: []string{"a"},
			want:     []string{},
		},
		{
			name: "an output with no name keeps the empty name it has",
			cols: outputColumns("", "name"),
			want: []string{"", "name"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, OutputColumnNames(tc.declared, tc.cols))
		})
	}
}

// TestExposedColumnNamesKeepsADeclarationTheBodyCannotContradict pins the exception the
// exposure adds to the naming rule. A body the analyzer could not enumerate is one wildcard
// output column, so its count is a lower bound and a longer declaration is not evidence
// that the engine rejects it: `WITH c (a, b) AS (SELECT * FROM undescribed) SELECT * FROM
// c` is engine-valid whenever `undescribed` really has two columns — MySQL 8.3, StarRocks
// 4.1, Doris 4.1.4, PostgreSQL 16 and MSSQL 2019 each expose `(a, b)` and return the row.
// Discarding the
// declaration left the relation undescribed instead: the two edges the names carry became
// one wildcard edge, an edge a declared target list expected went missing, and a name the
// relation does not have resolved silently.
func TestExposedColumnNamesKeepsADeclarationTheBodyCannotContradict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		declared []string
		cols     []OutputColumn
		want     []string
	}{
		{
			name:     "a declaration longer than a body modelled as a wildcard",
			declared: []string{"a", "b"},
			cols:     wildcardOutput(),
			want:     []string{"a", "b"},
		},
		{
			name:     "a declaration longer than a body the analyzer enumerated is still discarded",
			declared: []string{"a", "b"},
			cols:     outputColumns("k"),
			want:     []string{"k"},
		},
		{
			name:     "a declaration a wildcard body cannot contradict is kept either way",
			declared: []string{"a"},
			cols:     wildcardOutput(),
			want:     []string{"a"},
		},
		{
			name: "a wildcard body with no declaration stays unknown",
			cols: wildcardOutput(),
			want: []string{model.WildcardColumn},
		},
		{
			name:     "the rule is the naming rule everywhere else",
			declared: []string{"a"},
			cols:     outputColumns("k", "name"),
			want:     []string{"a", "name"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, ExposedColumnNames(tc.declared, tc.cols))
		})
	}
}

// TestExposedColumnNamesNeverAnswersFromAStaleList pins the half a caller must not lose:
// the returned list describes the body's outputs, so a column the body does not produce is
// never in it. That is what keeps a reference the engine rejects a gap rather than an edge.
func TestExposedColumnNamesNeverAnswersFromAStaleList(t *testing.T) {
	t.Parallel()

	names := ExposedColumnNames([]string{"k"}, outputColumns("k"))
	require.Equal(t, []string{"k"}, names)
	require.NotContains(t, names, "name")
}
