//go:build integration

package runner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

var rateLimitUserSeq atomic.Int64

// TestLogoutIsBudgetedRealServerIntegration pins the one anonymous and audited
// method that had no entry budget: Logout accepts a token this server signed for
// as long as the caller keeps sending it, and each call writes a permanent ledger
// row even though revoking the same token twice changes nothing. A real server
// must refuse the replay past the per-source budget.
func TestLogoutIsBudgetedRealServerIntegration(t *testing.T) {
	t.Parallel()

	// 127.0.0.2 is bindable on Linux but not everywhere, so skip where it is not.
	// Every request in this test comes from that address, which keeps its bucket
	// out of the one the rest of the suite shares.
	probe, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("127.0.0.2 is not bindable here: %v", err)
	}
	require.NoError(t, probe.Close())

	env := sharedPostgresServiceEnvNoReset(t)
	dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}
	httpClient := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext},
	}
	t.Cleanup(httpClient.CloseIdleConnections)

	ctx := context.Background()
	// A user of its own: logging out the shared admin token would revoke it for
	// every other test in the suite.
	email := fmt.Sprintf("rate-limit-logout-%d-%d@example.com", time.Now().UnixNano(), rateLimitUserSeq.Add(1))
	password := "rate-limit-logout-password"
	adminClient := v1connect.NewUserServiceClient(&http.Client{Timeout: 5 * time.Second}, env.BaseURL)
	_, err = adminClient.CreateUser(ctx, withToken(env.AdminToken(), &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Rate Limit Logout",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)

	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)
	login, err := authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{Email: email, Password: password}))
	require.NoError(t, err)
	token := login.Msg.GetToken()
	require.NotEmpty(t, token)

	// The budget is the state package's constant, repeated here so a silent change
	// to it shows up as a failing test rather than passing either way.
	const logoutSourceBudget = 120
	for i := range logoutSourceBudget {
		_, err := authClient.Logout(ctx, withToken(token, &v1pb.LogoutRequest{}))
		require.NoError(t, err, "request %d is inside the budget", i+1)
	}
	_, err = authClient.Logout(ctx, withToken(token, &v1pb.LogoutRequest{}))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err),
		"a replayed logout cannot write ledger rows without bound: %v", err)
}

// TestPrincipalThrottleBoundsExpensiveMethodsRealServerIntegration pins that the
// real server wires the per-principal budget, not merely that the interceptor
// would refuse if it were applied. FetchLLMModels is the method to exhaust:
// it is in principalLimitedProcedures, it names nothing so it fails before any
// provider call, and it is not audited — thousands of cheap requests rather than
// thousands of ledger rows.
func TestPrincipalThrottleBoundsExpensiveMethodsRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	client := v1connect.NewLLMServiceClient(&http.Client{Timeout: 5 * time.Second}, env.BaseURL)
	ctx := context.Background()
	adminToken := env.AdminToken()

	// The budget is the state package's constant, repeated here so a silent change
	// to it shows up as a failing test rather than passing either way.
	const principalMethodBudget = 3000
	for i := range principalMethodBudget {
		_, err := client.FetchLLMModels(ctx, withToken(adminToken, &v1pb.FetchLLMModelsRequest{}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "request %d is inside the budget", i+1)
	}
	_, err := client.FetchLLMModels(ctx, withToken(adminToken, &v1pb.FetchLLMModelsRequest{}))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err),
		"an authenticated caller cannot loop an expensive method without bound: %v", err)
}

// TestRefusedAnonymousDeviceLoginLeavesNoLedgerRowRealServerIntegration is the
// regression for the budget that used to live inside the CreateDeviceLogin
// handler. The audit interceptor wraps the handler, so every refused call still
// wrote a permanent ledger row while nothing bounded how fast an unauthenticated
// caller could produce them — tens of thousands of rows a minute from one client.
// On the interceptor chain the refusal lands before the audit interceptor, so only
// the accepted calls are recorded.
func TestRefusedAnonymousDeviceLoginLeavesNoLedgerRowRealServerIntegration(t *testing.T) {
	t.Parallel()

	// 127.0.0.3 is bindable on Linux but not everywhere, so skip where it is not.
	// It is deliberately not 127.0.0.2: that address already carries another test's
	// device login, and its bucket would decide how many calls this one may make.
	// A bucket of its own is what makes the counts below exact.
	probe, err := net.Listen("tcp", "127.0.0.3:0")
	if err != nil {
		t.Skipf("127.0.0.3 is not bindable here: %v", err)
	}
	require.NoError(t, probe.Close())

	env := sharedPostgresServiceEnvNoReset(t)
	dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.3")}}
	httpClient := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext},
	}
	t.Cleanup(httpClient.CloseIdleConnections)

	ctx := context.Background()
	client := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)

	// A name of its own, so a concurrent test's rows cannot move the count.
	clientName := fmt.Sprintf("rate-limit-probe-%d", time.Now().UnixNano())
	countRows := func() int {
		var rows int
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM audit_log
			WHERE payload->>'method' = $1 AND payload->'request'->>'clientName' = $2`,
			v1connect.AuthServiceCreateDeviceLoginProcedure, clientName).Scan(&rows))
		return rows
	}

	// The budget is the state package's constant, repeated here so a silent change
	// to it shows up as a failing test rather than passing either way.
	const deviceLoginSourceBudget = 10
	const calls = 40
	refused := 0
	for i := range calls {
		_, err := client.CreateDeviceLogin(ctx, connect.NewRequest(&v1pb.CreateDeviceLoginRequest{
			ClientName:    clientName,
			ClientVersion: "1",
		}))
		if connect.CodeOf(err) == connect.CodeResourceExhausted {
			refused++
			continue
		}
		require.NoError(t, err, "call %d is inside the budget", i+1)
	}

	require.Equal(t, calls-deviceLoginSourceBudget, refused, "the budget refuses the rest")
	require.Equal(t, deviceLoginSourceBudget, countRows(),
		"a refused anonymous request must leave no ledger row")
}
