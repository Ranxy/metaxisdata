//go:build integration

package runner

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// M17: a pending device login holds the caller's User-Agent for ten minutes and
// the store keeps up to 10000 of them, so an unbounded header would let one
// anonymous caller decide how much memory the server keeps. The same value is
// copied into the audit row of every audited call, where it would be permanent.
// Both are bounded at the source, in BuildRequestMetadata.
func TestDeviceLoginStoresABoundedUserAgentRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	anonymous := newAPIClients(env, "")
	signedIn := newAPIClients(env, env.AdminToken())

	// Small enough to stay under the HTTP server's own header ceiling, far larger
	// than anything the request needs.
	create := connect.NewRequest(&v1pb.CreateDeviceLoginRequest{ClientName: "mxd", ClientVersion: "0.1.0"})
	create.Header().Set("User-Agent", strings.Repeat("u", 512<<10))
	created, err := anonymous.auth.CreateDeviceLogin(ctx, create)
	require.NoError(t, err)

	detail, err := signedIn.auth.GetDeviceLogin(ctx, connect.NewRequest(&v1pb.GetDeviceLoginRequest{
		Name: "deviceLogins/" + created.Msg.GetUserCode(),
	}))
	require.NoError(t, err)

	stored := detail.Msg.GetRequestUserAgent()
	require.LessOrEqual(t, len(stored), audit.MaxUserAgentBytes+len("...(truncated)"),
		"an anonymous caller must not decide how much memory a pending request holds")
	require.True(t, strings.HasSuffix(stored, "...(truncated)"), "the value is cut short, not dropped")
	require.Equal(t, "mxd", detail.Msg.GetClientName(), "the bounded fields are unaffected")
}
