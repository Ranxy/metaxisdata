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
