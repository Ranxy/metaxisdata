package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
)

// Every tool call writes a row into a ledger that is never pruned, so a tool's
// arguments go through the same bound the ConnectRPC interceptor applies. A
// revert to redaction alone would leave a megabyte of SQL in a permanent row.
func TestAuditArgumentsAreBounded(t *testing.T) {
	t.Parallel()

	require.Nil(t, auditArguments(nil))

	// A multi-byte argument is cut on a rune boundary: a half rune is invalid
	// UTF-8, which protobuf rejects, and the whole row would be lost.
	arguments, err := json.Marshal(map[string]any{"sql": strings.Repeat("名", 3000)})
	require.NoError(t, err)
	sql := auditArguments(arguments).GetFields()["arguments"].GetStructValue().GetFields()["sql"].GetStringValue()
	require.True(t, utf8.ValidString(sql))
	require.True(t, strings.HasSuffix(sql, "...(truncated)"))
	require.LessOrEqual(t, len(sql), audit.MaxAuditFieldBytes+len("...(truncated)"))

	// The endpoint accepts 2 MiB and a call the budget refused keeps its arguments,
	// so the tree is bounded before it is rendered rather than copied into a row.
	large, err := json.Marshal(map[string]any{"arguments": map[string]any{
		"sql": strings.Repeat("s", 256<<10),
	}})
	require.NoError(t, err)
	notRecorded := auditArguments(large)
	require.Contains(t, notRecorded.GetFields()["truncated"].GetStringValue(), "exceeded")
	require.NotContains(t, notRecorded.GetFields(), "arguments")
}
