//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// refreshUserSeq namespaces the accounts this file creates, so two runs in the
// same nanosecond cannot collide on an address.
var refreshUserSeq atomic.Int64

// TestWebSessionRefreshRealServerIntegration pins the SPA session's rotation
// rules against a real server: a web login hands out both HttpOnly cookies,
// Refresh consumes the refresh cookie and rotates the pair, a replayed token is
// refused, and Logout ends the session rather than merely clearing the cookie.
func TestWebSessionRefreshRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	ctx := context.Background()

	const password = "test-password-for-refresh-session"
	email := fmt.Sprintf("auth-refresh-%d-%d@example.com", time.Now().UnixNano(), refreshUserSeq.Add(1))
	_, err := userClient.CreateUser(ctx, withToken(env.AdminToken(), &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Refresh Session Test",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)

	login := func(t *testing.T) (*http.Cookie, *http.Cookie) {
		t.Helper()

		resp, err := authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{Email: email, Password: password, Web: true}))
		require.NoError(t, err)
		require.Empty(t, resp.Msg.GetToken(), "a web login never echoes the token in the body")
		return sessionCookies(t, resp.Header())
	}

	t.Run("a web login sets both HttpOnly cookies", func(t *testing.T) {
		t.Parallel()

		access, refresh := login(t)
		require.NotEmpty(t, access.Value)
		require.True(t, access.HttpOnly, "page JavaScript must not read the access token")
		require.NotEmpty(t, refresh.Value)
		require.True(t, refresh.HttpOnly, "page JavaScript must not read the refresh token")
	})

	t.Run("refresh rotates the pair and consumes the old token", func(t *testing.T) {
		t.Parallel()

		_, refresh := login(t)

		rotated, err := authClient.Refresh(ctx, withCookies(&v1pb.RefreshRequest{}, refresh))
		require.NoError(t, err)
		newAccess, newRefresh := sessionCookies(t, rotated.Header())
		require.NotEmpty(t, newAccess.Value)
		require.NotEqual(t, refresh.Value, newRefresh.Value, "rotation must not hand the same credential back")

		// A replay of the consumed token is refused, because the row is gone.
		_, err = authClient.Refresh(ctx, withCookies(&v1pb.RefreshRequest{}, refresh))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

		// The rotated cookie works, which proves the replacement was stored
		// rather than only echoed to the cookie.
		_, err = authClient.Refresh(ctx, withCookies(&v1pb.RefreshRequest{}, newRefresh))
		require.NoError(t, err)
	})

	t.Run("logout deletes the row the cookie stands for", func(t *testing.T) {
		t.Parallel()

		access, refresh := login(t)

		_, err := authClient.Logout(ctx, withCookies(&v1pb.LogoutRequest{}, access, refresh))
		require.NoError(t, err)

		// The cookie the server just cleared must also be dead if a client kept
		// its own copy, or logout would only be a browser-side gesture.
		_, err = authClient.Refresh(ctx, withCookies(&v1pb.RefreshRequest{}, refresh))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("refresh without a cookie is refused", func(t *testing.T) {
		t.Parallel()

		_, err := authClient.Refresh(ctx, connect.NewRequest(&v1pb.RefreshRequest{}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})
}

// sessionCookies pulls the two session cookies out of a response's headers.
func sessionCookies(t *testing.T, header http.Header) (*http.Cookie, *http.Cookie) {
	t.Helper()

	response := http.Response{Header: header}
	byName := map[string]*http.Cookie{}
	for _, cookie := range response.Cookies() {
		byName[cookie.Name] = cookie
	}
	access, ok := byName["access-token"]
	require.True(t, ok, "the response sets an access-token cookie")
	refresh, ok := byName["refresh-token"]
	require.True(t, ok, "the response sets a refresh-token cookie")
	return access, refresh
}

// withCookies attaches the given cookies to a request.
func withCookies[T any](msg *T, cookies ...*http.Cookie) *connect.Request[T] {
	req := connect.NewRequest(msg)
	for _, cookie := range cookies {
		req.Header().Add("Cookie", cookie.String())
	}
	return req
}
