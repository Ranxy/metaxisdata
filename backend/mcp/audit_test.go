package mcp

import (
	"encoding/json"
	"fmt"
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

	// Bounded fields are not enough on their own: enough of them still serialize
	// over the whole-payload cap, and the row keeps the marker instead.
	fields := make(map[string]any, 64)
	for i := range 64 {
		fields[fmt.Sprintf("field%d", i)] = strings.Repeat("s", audit.MaxAuditFieldBytes)
	}
	oversized, err := json.Marshal(map[string]any{"arguments": fields})
	require.NoError(t, err)
	require.Contains(t,
		auditArguments(oversized).GetFields()["truncated"].GetStringValue(), "exceeded")
}
