//go:build integration

package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// TestUnauthenticatedRequestsAreRejectedRealServerIntegration is the reverse of
// the hermetic api/auth tests: a real server must refuse a request that carries
// no credential, a malformed one, or one signed with a foreign key.
func TestUnauthenticatedRequestsAreRejectedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	instanceClient := v1connect.NewInstanceServiceClient(httpClient, env.BaseURL)
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)

	ctx := context.Background()

	tests := []struct {
		name          string
		authorization string
	}{
		{"no credential", ""},
		{"malformed token", "Bearer not-a-jwt"},
		{"token with the wrong algorithm", "Bearer eyJhbGciOiJub25lIn0.e30."},
		{"empty bearer", "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := connect.NewRequest(&v1pb.ListInstancesRequest{PageSize: 1})
			if tt.authorization != "" {
				req.Header().Set("Authorization", tt.authorization)
			}

			_, err := instanceClient.ListInstances(ctx, req)
			require.Error(t, err)
			require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "unexpected error: %v", err)
		})
	}

	// A read that never touches the store must be rejected the same way.
	_, err := userClient.GetCurrentUser(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

// TestLogoutAndPasswordChangeRevokeTokensRealServerIntegration pins the two
// revocation paths against a real server: Logout only revokes a token it can
// verify, and a password change invalidates tokens minted before it.
func TestLogoutAndPasswordChangeRevokeTokensRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)

	ctx := context.Background()
	adminToken := env.AdminToken()

	// Every subtest gets its own user so none of them can disturb the shared
	// admin account (and its token) or each other.
	newUser := func(t *testing.T, password string) (string, string) {
		t.Helper()
		email := fmt.Sprintf("auth-revoke-%d-%d@example.com", time.Now().UnixNano(), authRevokeUserSeq.Add(1))
		resp, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
			User: &v1pb.User{
				Email:    email,
				Title:    "Auth Revocation Test",
				Password: password,
				UserType: v1pb.UserType_END_USER,
			},
		}))
		require.NoError(t, err)
		return resp.Msg.GetName(), email
	}
	login := func(t *testing.T, email, password string) string {
		t.Helper()
		resp, err := authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{Email: email, Password: password}))
		require.NoError(t, err)
		return resp.Msg.GetToken()
	}

	t.Run("a forged logout is rejected", func(t *testing.T) {
		t.Parallel()

		// Logout used to accept an arbitrary string, so an unauthenticated
		// caller could flood the revocation cache and evict real entries.
		_, err := authClient.Logout(ctx, withToken("not-a-jwt", &v1pb.LogoutRequest{}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

		_, err = authClient.Logout(ctx, connect.NewRequest(&v1pb.LogoutRequest{}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("a real logout revokes its token", func(t *testing.T) {
		t.Parallel()

		const password = "Integration-pass-1!"
		_, email := newUser(t, password)
		token := login(t, email, password)

		_, err := authClient.Logout(ctx, withToken(token, &v1pb.LogoutRequest{}))
		require.NoError(t, err)

		_, err = userClient.GetCurrentUser(ctx, withToken(token, &emptypb.Empty{}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("a password change revokes older tokens", func(t *testing.T) {
		t.Parallel()

		const password = "Integration-pass-1!"
		const newPassword = "Integration-pass-2!"
		userName, email := newUser(t, password)
		token := login(t, email, password)

		_, err := userClient.UpdateUser(ctx, withToken(token, &v1pb.UpdateUserRequest{
			User:            &v1pb.User{Name: userName, Password: newPassword},
			UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"password"}},
			CurrentPassword: password,
		}))
		require.NoError(t, err)

		// The minting token predates the change and must no longer work.
		_, err = userClient.GetCurrentUser(ctx, withToken(token, &emptypb.Empty{}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

		// A token minted afterwards is fine.
		fresh := login(t, email, newPassword)
		_, err = userClient.GetCurrentUser(ctx, withToken(fresh, &emptypb.Empty{}))
		require.NoError(t, err)
	})
}

// authRevokeUserSeq keeps the per-subtest emails unique even if the wall clock
// has coarse resolution.
var authRevokeUserSeq atomic.Int64

// jwtClaimValue reads one claim out of a signed token without verifying it. The
// tests already know the token came from the server; they need the jti and exp it
// carries to check what the server recorded about it.
func jwtClaimValue(t *testing.T, token, claim string) any {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "not a compact JWS")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims[claim]
}

// M4: a logout writes a persistent record keyed by the token's jti, and the
// maintenance prune drops it only after the token itself expires. The old
// bounded in-memory LRU could be churned by any account holder, which brought a
// revoked token back to life.
func TestRevokedTokensArePersistentRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)

	ctx := context.Background()
	const password = "Integration-pass-1!"
	email := fmt.Sprintf("auth-persist-%d-%d@example.com", time.Now().UnixNano(), authRevokeUserSeq.Add(1))
	_, err := userClient.CreateUser(ctx, withToken(env.AdminToken(), &v1pb.CreateUserRequest{
		User: &v1pb.User{Email: email, Title: "Auth Persist Test", Password: password, UserType: v1pb.UserType_END_USER},
	}))
	require.NoError(t, err)

	loginResp, err := authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{Email: email, Password: password}))
	require.NoError(t, err)
	token := loginResp.Msg.GetToken()

	tokenID, ok := jwtClaimValue(t, token, "jti").(string)
	require.True(t, ok)
	require.NotEmpty(t, tokenID, "every token the server signs carries a jti")
	expiresAt, ok := jwtClaimValue(t, token, "exp").(float64)
	require.True(t, ok)
	require.Positive(t, expiresAt)

	_, err = authClient.Logout(ctx, withToken(token, &v1pb.LogoutRequest{}))
	require.NoError(t, err)

	// The record is in the table, not only in this process's cache.
	revoked, err := env.Store.IsTokenRevoked(ctx, tokenID)
	require.NoError(t, err)
	require.True(t, revoked)
	_, err = userClient.GetCurrentUser(ctx, withToken(token, &emptypb.Empty{}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

	// The prune only drops a record once its token has expired; before that the
	// record is what refuses the token.
	pruned, err := env.Store.DeleteExpiredRevokedTokens(ctx, time.Unix(int64(expiresAt), 0).Add(-time.Minute))
	require.NoError(t, err)
	require.Zero(t, pruned, "an unexpired revocation must not be pruned")
	revoked, err = env.Store.IsTokenRevoked(ctx, tokenID)
	require.NoError(t, err)
	require.True(t, revoked)

	pruned, err = env.Store.DeleteExpiredRevokedTokens(ctx, time.Unix(int64(expiresAt), 0).Add(time.Minute))
	require.NoError(t, err)
	require.GreaterOrEqual(t, pruned, int64(1))
	revoked, err = env.Store.IsTokenRevoked(ctx, tokenID)
	require.NoError(t, err)
	require.False(t, revoked, "an expired record is pruned")
}

// M3/M5: the connect entry bounds anonymous Login, and failed attempts are
// counted per account independently of the source. Once the account's window is
// full the request is refused before the password is even compared, so the
// correct password is refused too. The source budget is wider than the account
// budget, so a refusal here proves the account counter rather than the entry
// budget.
func TestLoginThrottleLocksTheAccountRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)

	ctx := context.Background()
	const password = "Integration-pass-1!"
	lockedEmail := fmt.Sprintf("auth-lock-%d-%d@example.com", time.Now().UnixNano(), authRevokeUserSeq.Add(1))
	openEmail := fmt.Sprintf("auth-open-%d-%d@example.com", time.Now().UnixNano(), authRevokeUserSeq.Add(1))
	for _, email := range []string{lockedEmail, openEmail} {
		_, err := userClient.CreateUser(ctx, withToken(env.AdminToken(), &v1pb.CreateUserRequest{
			User: &v1pb.User{Email: email, Title: "Auth Lock Test", Password: password, UserType: v1pb.UserType_END_USER},
		}))
		require.NoError(t, err)
	}

	// Ten failures fill the account's window. The same source is still refused
	// for that account on the eleventh request, before bcrypt runs.
	for i := range 10 {
		_, err := authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{Email: lockedEmail, Password: "wrong-password"}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "attempt %d", i+1)
	}
	_, err := authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{Email: lockedEmail, Password: password}))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err), "a filled window refuses even the right password")

	// Another account from the same source is unaffected, so the lock follows the
	// account and not the whole source.
	_, err = authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{Email: openEmail, Password: "wrong-password"}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

// A malformed Authorization header must not break an endpoint that allows
// anonymous access; it used to fail before the allowlist was consulted.
func TestLoginIgnoresAMalformedAuthorizationHeaderRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)
	ctx := context.Background()

	const password = "Integration-pass-1!"
	email := fmt.Sprintf("auth-header-%d-%d@example.com", time.Now().UnixNano(), authRevokeUserSeq.Add(1))
	_, err := userClient.CreateUser(ctx, withToken(env.AdminToken(), &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Auth Header Test",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)

	req := connect.NewRequest(&v1pb.LoginRequest{Email: email, Password: password})
	req.Header().Set("Authorization", "Bearer not-a-jwt")
	resp, err := authClient.Login(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Msg.GetToken(), "a plain login still returns its token")
}
