package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The section marker must not survive into the title: the UI renders the title
// as a heading itself, so a retained "## " showed as "## ## ...".
func TestParseStructuredResponseStripsHeadingMarkers(t *testing.T) {
	t.Parallel()

	summary, sectionsJSON := parseStructuredResponse("A short summary.\n## Execution\nIt scans a table.\n## Notes\nNothing else.")
	require.Equal(t, "A short summary.", summary)
	require.JSONEq(t, `[
		{"title":"Execution","content":"It scans a table."},
		{"title":"Notes","content":"Nothing else."}
	]`, sectionsJSON)
}

// When the model opens with a heading there is no summary line, and the whole
// answer used to be kept as the summary with an empty section.
func TestParseStructuredResponseHandlesALeadingHeading(t *testing.T) {
	t.Parallel()

	summary, sectionsJSON := parseStructuredResponse("## Execution\nIt scans a table.")
	require.Equal(t, "SQL Explanation", summary)

	var sections []explainSection
	require.NoError(t, json.Unmarshal([]byte(sectionsJSON), &sections))
	require.Len(t, sections, 1)
	require.Equal(t, "Execution", sections[0].Title)
	require.Equal(t, "It scans a table.", sections[0].Content)
}

func TestParseStructuredResponseFallsBackToPlainText(t *testing.T) {
	t.Parallel()

	summary, sectionsJSON := parseStructuredResponse("no headings here")
	require.Equal(t, "no headings here", summary)

	var sections []explainSection
	require.NoError(t, json.Unmarshal([]byte(sectionsJSON), &sections))
	require.Len(t, sections, 1)
	require.Equal(t, "Explanation", sections[0].Title)
	require.Equal(t, "no headings here", sections[0].Content)
}
