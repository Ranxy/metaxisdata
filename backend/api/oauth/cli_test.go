package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// activeCLIGrant is a usable CLI session for user 7.
func activeCLIGrant(now time.Time) *store.OAuthGrant {
	return &store.OAuthGrant{
		ID:        43,
		TokenHash: auth.HashToken("refresh-1"),
		ClientID:  CLIClientID,
		UserID:    7,
		Kind:      store.OAuthGrantKindCLI,
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now.Add(-time.Hour),
	}
}

func TestRotateCLIGrantRotatesInPlace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		grant:    activeCLIGrant(now),
		user:     &store.UserMessage{ID: 7, Name: "user@example.com"},
		rotateID: 43,
		rotateOK: true,
	}

	grantID, user, refreshToken, expiresAt, err := RotateCLIGrant(context.Background(), fake, nil, "refresh-1", now, time.Hour)
	require.NoError(t, err)
	require.Equal(t, int64(43), grantID)
	require.Equal(t, 7, user.ID)
	require.NotEmpty(t, refreshToken)
	require.NotEqual(t, "refresh-1", refreshToken)
	require.Equal(t, now.Add(time.Hour), expiresAt, "the session slides from now")

	require.Equal(t, 1, fake.getCalls)
	require.Equal(t, CLIClientID, fake.lastGetClientID)
	require.Equal(t, auth.HashToken("refresh-1"), fake.lastRotateOld)
	require.Equal(t, auth.HashToken(refreshToken), fake.lastRotateNew)
	require.Empty(t, fake.revokedIDs, "a healthy session is not revoked")
}

// Every refusal decided from the row must happen before the atomic rotation, so a
// client-correctable request does not burn the credential.
func TestRotateCLIGrantRefusesWithoutBurning(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-time.Minute)
	user := &store.UserMessage{ID: 7, Name: "user@example.com"}

	tests := []struct {
		name  string
		grant *store.OAuthGrant
	}{
		{
			name:  "an unknown token",
			grant: nil,
		},
		{
			name: "an MCP grant is not a CLI session",
			grant: func() *store.OAuthGrant {
				grant := activeCLIGrant(now)
				grant.Kind = store.OAuthGrantKindMCP
				return grant
			}(),
		},
		{
			name: "an expired session",
			grant: func() *store.OAuthGrant {
				grant := activeCLIGrant(now)
				grant.ExpiresAt = now.Add(-time.Second)
				return grant
			}(),
		},
		{
			name: "a revoked session",
			grant: func() *store.OAuthGrant {
				grant := activeCLIGrant(now)
				grant.RevokedAt = &revokedAt
				return grant
			}(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeTokenStore{grant: tc.grant, user: user, rotateOK: true}
			_, _, _, _, err := RotateCLIGrant(context.Background(), fake, nil, "refresh-1", now, time.Hour)
			require.ErrorIs(t, err, ErrGrantNotRefreshable)
			require.Zero(t, fake.rotateCalls, "a refusal the client can correct must not consume the token")
			require.Empty(t, fake.revokedIDs)
		})
	}
}

// A principal that can no longer use the session ends it explicitly: an in-place
// rotation cannot express that by deleting the consumed row.
func TestRotateCLIGrantEndsAnUnusableSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lastChange := now.Add(-time.Minute)
	cases := []struct {
		name string
		user *store.UserMessage
		want error
	}{
		{
			name: "the user no longer exists",
			want: ErrGrantUserMissing,
		},
		{
			name: "the user was deactivated",
			user: &store.UserMessage{ID: 7, MemberDeleted: true},
			want: ErrGrantUserDeactivated,
		},
		{
			name: "the session predates the last password change",
			user: &store.UserMessage{ID: 7, Profile: &storepb.UserProfile{LastChangePasswordTime: timestamppb.New(lastChange)}},
			want: ErrGrantPredatesPasswordChange,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeTokenStore{grant: activeCLIGrant(now), user: tc.user, rotateID: 43, rotateOK: true}
			_, _, _, _, err := RotateCLIGrant(context.Background(), fake, nil, "refresh-1", now, time.Hour)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, []int64{43}, fake.revokedIDs, "the consumed session must not stay listed as active")
		})
	}
}

// A database read failure is not proof the account is gone: the credential is
// already burned, so the session is failed without ending a connection that may
// still be healthy.
func TestRotateCLIGrantSurfacesAReadFailureWithoutRevoking(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		grant:    activeCLIGrant(now),
		userErr:  errors.New("store unavailable"),
		rotateID: 43,
		rotateOK: true,
	}

	_, _, _, _, err := RotateCLIGrant(context.Background(), fake, nil, "refresh-1", now, time.Hour)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrGrantUserMissing)
	require.Empty(t, fake.revokedIDs, "a read failure must not revoke a live session")
}

// The OAuth token endpoint redeems MCP grants only; a CLI session is refreshed
// through the ConnectRPC endpoint. The empty resource already refuses it, so the
// test gives the CLI grant the MCP resource to pin the kind check itself.
func TestCLIGrantIsRefusedAtTheOAuthTokenEndpoint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	grant := activeCLIGrant(now)
	grant.Resource = testResource
	fake := &fakeTokenStore{
		grant:    grant,
		user:     &store.UserMessage{ID: 7},
		rotateID: 43,
		rotateOK: true,
	}
	server, _ := newTokenTestServer(t, fake, now)

	recorder := postToken(t, server, refreshForm(MCPReadScope))
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	var response oauthError
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "invalid_grant", response.Error)
	require.Zero(t, fake.rotateCalls, "the wrong kind must not be rotated")
}
