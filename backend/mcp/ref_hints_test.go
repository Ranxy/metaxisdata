package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/tools/metadata"
)

// The frozen tool contract includes the hint: a shared resolver failure must reach
// the model with its next-step hint intact. The hint's text is owned by the shared
// body and tested there; the MCP wrapper is pinned to copy the shared envelope
// through instead of re-deriving a failure of its own.
func TestResolverFailuresKeepTheirHint(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})

	cases := []struct {
		name      string
		tool      string
		arguments string
		// source is the shared tool body the MCP tool wraps.
		source func(context.Context, json.RawMessage) (any, error)
	}{
		{
			name:      "object name",
			tool:      "get_metadata",
			arguments: `{"object":{"instance":"prod","database":"shop"},"meta_type":"TABLE"}`,
			source:    server.catalog.GetMetadata,
		},
		{
			name:      "container reference",
			tool:      "list_metadata",
			arguments: `{"parent":{"instance":"prod"}}`,
			source:    server.catalog.ListMetadata,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := json.RawMessage(testCase.arguments)
			_, sourceErr := testCase.source(context.Background(), raw)
			expected := metadata.AsError(sourceErr)
			require.NotNil(t, expected, "the shared body must report a tool error")
			require.NotEmpty(t, expected.Hint)

			_, err := toolByName(t, server, testCase.tool).Run(context.Background(), testUser(), raw)
			failure := failureOf(t, err)
			require.Equal(t, expected.Code, failure.Code)
			require.Equal(t, expected.Message, failure.Message)
			require.Equal(t, expected.Hint, failure.Hint)
		})
	}
}
