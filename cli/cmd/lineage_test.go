package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func relation(source, target string, isTemp bool) *v1pb.AnalyzeSQLRelation {
	return &v1pb.AnalyzeSQLRelation{SourceGuid: source, TargetGuid: target, IsTemp: isTemp}
}

// A statement that writes somewhere real never reaches the CLI with temporary
// relations, because the server drops them as duplicates of the real target.
func TestVisibleRelationsHidesTemporaryOnesByDefault(t *testing.T) {
	t.Parallel()

	relations := []*v1pb.AnalyzeSQLRelation{
		relation("1;shop;;orders", "1;shop;;daily", false),
		relation("1;shop;;orders", "", true),
	}

	visible, hidden := visibleRelations(relations, false)
	require.Len(t, visible, 1)
	require.False(t, visible[0].GetIsTemp())
	require.Equal(t, 1, hidden)

	all, hidden := visibleRelations(relations, true)
	require.Len(t, all, 2)
	require.Zero(t, hidden)
}

// Hiding everything is allowed, but the count has to come back so the caller
// can say the scope is not empty, it is filtered.
func TestVisibleRelationsReportsAFullyHiddenScope(t *testing.T) {
	t.Parallel()

	relations := []*v1pb.AnalyzeSQLRelation{
		relation("1;shop;;orders", "", true),
		relation("1;shop;;users", "", true),
	}

	visible, hidden := visibleRelations(relations, false)
	require.Empty(t, visible)
	require.Equal(t, 2, hidden)
}

func TestVisibleRelationsOnAnEmptyScope(t *testing.T) {
	t.Parallel()

	visible, hidden := visibleRelations(nil, false)
	require.Empty(t, visible)
	require.Zero(t, hidden)
}

// The CLI renders a diagnostic itself, because it is an API client and may not
// import the analyzer's formatter. The rendering has to match what the server
// records on the lineage version, or the same gap would read two ways.
func TestDiagnosticTextMatchesTheAnalyzersWording(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		diagnostic *v1pb.AnalyzeSQLDiagnostic
		want       string
	}{
		{
			name: "a statement shape the analyzer does not model",
			diagnostic: &v1pb.AnalyzeSQLDiagnostic{
				Category: v1pb.DiagnosticCategory_DIAGNOSTIC_CATEGORY_NOT_MODELLED,
				Subject:  "MERGE",
			},
			want: "not modelled: MERGE",
		},
		{
			name: "a shape with the parser defect behind it",
			diagnostic: &v1pb.AnalyzeSQLDiagnostic{
				Category: v1pb.DiagnosticCategory_DIAGNOSTIC_CATEGORY_NOT_MODELLED,
				Subject:  "WITH before INSERT",
				Detail:   "the parser drops the CTE",
			},
			want: "not modelled: WITH before INSERT: the parser drops the CTE",
		},
		{
			name: "a reference that resolved to nothing",
			diagnostic: &v1pb.AnalyzeSQLDiagnostic{
				Category:  v1pb.DiagnosticCategory_DIAGNOSTIC_CATEGORY_UNRESOLVED_REFERENCE,
				Subject:   "a CTE body",
				Reference: "stage.id",
			},
			want: "unresolved reference in a CTE body: stage.id",
		},
		{
			name: "a reference several relations own",
			diagnostic: &v1pb.AnalyzeSQLDiagnostic{
				Category:  v1pb.DiagnosticCategory_DIAGNOSTIC_CATEGORY_AMBIGUOUS_REFERENCE,
				Subject:   "an assignment target",
				Reference: "a",
			},
			want: "ambiguous reference in an assignment target: a",
		},
		{
			name: "a catalog lookup that failed",
			diagnostic: &v1pb.AnalyzeSQLDiagnostic{
				Category:  v1pb.DiagnosticCategory_DIAGNOSTIC_CATEGORY_CATALOG_UNAVAILABLE,
				Reference: "t",
				Detail:    "connection reset",
			},
			want: "catalog lookup failed for t: connection reset",
		},
		{
			name:       "a category this build does not know",
			diagnostic: &v1pb.AnalyzeSQLDiagnostic{},
			want:       "unclassified diagnostic",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, diagnosticText(tc.diagnostic))
		})
	}
}
