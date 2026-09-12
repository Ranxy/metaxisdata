package v1

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/component/state"
)

// CreateSSOState is what binds an OAuth2 callback to the client that started
// the flow; a state must be unique, single-use and short-lived.
func TestCreateSSOStateIssuesSingleUseNonces(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	svc := &AuthService{stateCfg: stateCfg}
	ctx := context.Background()

	first, err := svc.CreateSSOState(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	second, err := svc.CreateSSOState(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	require.NotEmpty(t, first.Msg.GetState())
	require.NotEqual(t, first.Msg.GetState(), second.Msg.GetState())

	require.True(t, svc.consumeSSOState(first.Msg.GetState()), "a fresh state is accepted")
	require.False(t, svc.consumeSSOState(first.Msg.GetState()), "a state can be used only once")

	require.False(t, svc.consumeSSOState(""))
	require.False(t, svc.consumeSSOState("never-issued"))

	expired, err := svc.CreateSSOState(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	stateCfg.SSOStateCache.Add(expired.Msg.GetState(), time.Now().Add(-2*state.SSOStateTTL))
	require.False(t, svc.consumeSSOState(expired.Msg.GetState()), "an expired state is rejected")
}
