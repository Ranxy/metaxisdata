package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The frozen tool contract includes the hint: a shared resolver failure must reach
// the model through the MCP envelope with its next-step hint intact.
func TestResolverFailuresKeepTheirHintInTheMCPEnvelope(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})

	// object.name missing: the hint explains how to address the object.
	_, err := toolByName(t, server, "get_metadata").Run(context.Background(), testUser(),
		json.RawMessage(`{"object":{"instance":"prod","database":"shop"},"meta_type":"TABLE"}`))
	failure := failureOf(t, err)
	require.Equal(t, codeInvalidArgument, failure.Code)
	require.Equal(t, "address the object as {instance, database, schema?, name}, or pass a guid from an earlier result", failure.Hint)

	// A container reference with no name: the hint explains how to address it.
	_, err = toolByName(t, server, "list_metadata").Run(context.Background(), testUser(),
		json.RawMessage(`{"parent":{"instance":"prod"}}`))
	failure = failureOf(t, err)
	require.Equal(t, codeInvalidArgument, failure.Code)
	require.Equal(t, "address it as {instance, database, schema?}, or pass a guid from an earlier result", failure.Hint)
}
