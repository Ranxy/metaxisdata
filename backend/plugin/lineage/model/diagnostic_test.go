package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDiagnosticRendersTheAnalyzersWording pins the sentences an error carries.
// The golden corpus asserts fragments of them and a stored lineage version
// records them, so the wording is part of the contract rather than a formatting
// detail: a category that starts rendering differently would silently stop
// matching the cases that pin it.
func TestDiagnosticRendersTheAnalyzersWording(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		diagnostic Diagnostic
		want       string
	}{
		{
			name:       "a statement shape the analyzer does not model",
			diagnostic: Diagnostic{Category: DiagnosticNotModelled, Subject: "MERGE"},
			want:       "not modelled: MERGE",
		},
		{
			name: "a shape with the parser defect behind it",
			diagnostic: Diagnostic{
				Category: DiagnosticNotModelled,
				Subject:  "WITH before INSERT",
				Detail:   "the parser drops the CTE, so its sources cannot be resolved",
			},
			want: "not modelled: WITH before INSERT: the parser drops the CTE, so its sources cannot be resolved",
		},
		{
			name:       "a reference that resolved to nothing",
			diagnostic: Diagnostic{Category: DiagnosticUnresolved, Subject: "a CTE body", Reference: "stage.id"},
			want:       "unresolved reference in a CTE body: stage.id",
		},
		{
			name:       "a reference several relations own",
			diagnostic: Diagnostic{Category: DiagnosticAmbiguous, Subject: "an assignment target", Reference: "a"},
			want:       "ambiguous reference in an assignment target: a",
		},
		{
			name:       "a catalog lookup that failed",
			diagnostic: Diagnostic{Category: DiagnosticCatalogUnavailable, Reference: "t", Detail: "connection reset"},
			want:       "catalog lookup failed for t: connection reset",
		},
		{
			name:       "a category the renderer does not know",
			diagnostic: Diagnostic{},
			want:       "unclassified diagnostic",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.diagnostic.String())
		})
	}
}

// TestFormatDiagnosticsNamesWhatTheLimitLeftOut pins that a truncated list still
// says it was truncated: a caller that only sees the notes it was given must not
// read a bounded list as a complete one.
func TestFormatDiagnosticsNamesWhatTheLimitLeftOut(t *testing.T) {
	t.Parallel()

	notes := []Diagnostic{
		{Category: DiagnosticNotModelled, Subject: "MERGE"},
		{Category: DiagnosticUnresolved, Subject: "a CTE body", Reference: "stage.id"},
	}
	require.Equal(t, "not modelled: MERGE; unresolved reference in a CTE body: stage.id",
		FormatDiagnostics(notes, 0))
	require.Equal(t,
		"not modelled: MERGE; unresolved reference in a CTE body: stage.id; 3 more not listed",
		FormatDiagnostics(notes, 3))
	require.Empty(t, FormatDiagnostics(nil, 0))
}
