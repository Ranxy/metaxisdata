package v1

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestConnectedAppListScope(t *testing.T) {
	t.Parallel()

	member := &store.UserMessage{ID: 7, Email: "member@example.com"}
	tests := []struct {
		name          string
		checker       PermissionChecker
		requestedUser string
		wantUserID    int
		wantAll       bool
		wantCode      connect.Code
	}{
		{
			name:       "member without the permission sees only their own rows",
			checker:    &fakeChecker{},
			wantUserID: 7,
		},
		{
			name:          "member may name themself",
			checker:       &fakeChecker{},
			requestedUser: "users/7",
			wantUserID:    7,
		},
		{
			name:          "member naming another user is refused",
			checker:       &fakeChecker{},
			requestedUser: "users/8",
			wantCode:      connect.CodePermissionDenied,
		},
		{
			name:    "admin without a user filter sees every connection",
			checker: &fakeChecker{granted: map[permission.Permission]bool{permission.ConnectedAppsList: true}},
			wantAll: true,
		},
		{
			name:          "admin may narrow to one user",
			checker:       &fakeChecker{granted: map[permission.Permission]bool{permission.ConnectedAppsList: true}},
			requestedUser: "users/8",
			wantUserID:    8,
		},
		{
			name:          "a malformed user name is invalid rather than ignored",
			checker:       &fakeChecker{granted: map[permission.Permission]bool{permission.ConnectedAppsList: true}},
			requestedUser: "people/8",
			wantCode:      connect.CodeInvalidArgument,
		},
		{
			name:     "a checker failure is internal, and must not widen the scope",
			checker:  &fakeChecker{err: errors.New("boom")},
			wantCode: connect.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			userID, err := connectedAppListScope(context.Background(), tc.checker, member, tc.requestedUser)
			if tc.wantCode != 0 {
				var connectErr *connect.Error
				require.ErrorAs(t, err, &connectErr)
				require.Equal(t, tc.wantCode, connectErr.Code())
				return
			}
			require.NoError(t, err)
			if tc.wantAll {
				require.Nil(t, userID)
				return
			}
			require.NotNil(t, userID)
			require.Equal(t, tc.wantUserID, *userID)
		})
	}
}

func TestConnectedAppRevocable(t *testing.T) {
	t.Parallel()

	caller := &store.UserMessage{ID: 7}
	own := &store.OAuthGrant{UserID: 7}
	another := &store.OAuthGrant{UserID: 8}
	tests := []struct {
		name    string
		checker PermissionChecker
		grant   *store.OAuthGrant
		want    bool
		wantErr bool
	}{
		{name: "own connection needs no permission", checker: &fakeChecker{}, grant: own, want: true},
		{name: "another user's connection without the permission is not revocable", checker: &fakeChecker{}, grant: another, want: false},
		{
			name:    "another user's connection with the permission is revocable",
			checker: &fakeChecker{granted: map[permission.Permission]bool{permission.ConnectedAppsRevoke: true}},
			grant:   another,
			want:    true,
		},
		{
			name:    "a checker failure fails closed",
			checker: &fakeChecker{err: errors.New("boom")},
			grant:   another,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := connectedAppRevocable(context.Background(), tc.checker, caller, tc.grant)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestConnectedAppStatePrecedence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-time.Hour)
	lastChange := now.Add(time.Hour)
	resource := "https://mx.example.com/mcp"

	tests := []struct {
		name            string
		grant           *store.OAuthGrant
		currentResource string
		want            v1pb.ConnectedApp_State
	}{
		{
			name:  "active",
			grant: &store.OAuthGrant{ExpiresAt: now.Add(time.Hour), Resource: resource},
			want:  v1pb.ConnectedApp_ACTIVE,
		},
		{
			name:  "revoked wins over an expired grant",
			grant: &store.OAuthGrant{ExpiresAt: now.Add(-time.Hour), RevokedAt: &revokedAt},
			want:  v1pb.ConnectedApp_REVOKED,
		},
		{
			name:  "expired",
			grant: &store.OAuthGrant{ExpiresAt: now.Add(-time.Hour)},
			want:  v1pb.ConnectedApp_EXPIRED,
		},
		{
			name:  "expired wins over a deactivated account",
			grant: &store.OAuthGrant{ExpiresAt: now.Add(-time.Hour), UserDeleted: true},
			want:  v1pb.ConnectedApp_EXPIRED,
		},
		{
			name:  "a deactivated account is inactive",
			grant: &store.OAuthGrant{ExpiresAt: now.Add(time.Hour), UserDeleted: true},
			want:  v1pb.ConnectedApp_INACTIVE,
		},
		{
			name:            "a moved external URL requires re-authorization",
			grant:           &store.OAuthGrant{ExpiresAt: now.Add(time.Hour), Resource: "https://old.example.com/mcp"},
			currentResource: resource,
			want:            v1pb.ConnectedApp_REAUTH_REQUIRED,
		},
		{
			name:  "a grant older than the last password change requires re-authorization",
			grant: &store.OAuthGrant{ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-time.Hour), UserLastChangePasswordTime: &lastChange},
			want:  v1pb.ConnectedApp_REAUTH_REQUIRED,
		},
		{
			name:  "an unresolvable surface does not invent a re-authorization state",
			grant: &store.OAuthGrant{ExpiresAt: now.Add(time.Hour), Resource: "https://old.example.com/mcp"},
			want:  v1pb.ConnectedApp_ACTIVE,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, connectedAppState(tc.grant, now, tc.currentResource))
		})
	}
}

func TestConvertConnectedApp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	created := now.Add(-48 * time.Hour)
	expires := now.Add(240 * time.Hour)
	lastToolCall := now.Add(-time.Minute)
	grant := &store.OAuthGrant{
		ID:             42,
		ClientID:       "client-1",
		ClientName:     "Agent",
		UserID:         7,
		UserEmail:      "member@example.com",
		Resource:       "https://mx.example.com/mcp",
		Scope:          "metaxisdata.mcp.read",
		CreatedAt:      created,
		ExpiresAt:      expires,
		LastToolCallAt: &lastToolCall,
	}

	app := convertConnectedApp(grant, now, grant.Resource, true)
	require.Equal(t, "connectedApps/42", app.GetName())
	require.Equal(t, "client-1", app.GetClientId())
	require.Equal(t, "Agent", app.GetClientName())
	require.Equal(t, "users/7", app.GetUser())
	require.Equal(t, "member@example.com", app.GetUserEmail())
	require.Equal(t, []string{"metaxisdata.mcp.read"}, app.GetScopes())
	require.Equal(t, created, app.GetCreateTime().AsTime())
	require.Equal(t, expires, app.GetExpireTime().AsTime())
	require.Equal(t, lastToolCall, app.GetLastToolCallTime().AsTime())
	require.Nil(t, app.GetRevokeTime())
	require.Equal(t, v1pb.ConnectedApp_ACTIVE, app.GetState())

	hidden := convertConnectedApp(grant, now, grant.Resource, false)
	require.Empty(t, hidden.GetUserEmail(), "reading connected apps does not grant reading accounts")
	require.Equal(t, "users/7", hidden.GetUser())
	require.Equal(t, common.FormatUserUID(7), hidden.GetUser())
}

func TestParseConnectedAppStateFilter(t *testing.T) {
	t.Parallel()

	resource := "https://mx.example.com/mcp"
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	filter, err := parseConnectedAppFilter(`state == "REVOKED"`, now, resource)
	require.NoError(t, err)
	require.Contains(t, filter.Where, "CASE")
	require.Contains(t, filter.Where, "WHEN g.revoked_at IS NOT NULL THEN 'REVOKED'")
	require.Contains(t, filter.Where, "g.resource <> $")
	require.Contains(t, filter.Args, now)
	require.Contains(t, filter.Args, resource)
	require.Contains(t, filter.Args, "REVOKED")

	filter, err = parseConnectedAppFilter(`state in ["ACTIVE", "EXPIRED"]`, now, "")
	require.NoError(t, err)
	require.Contains(t, filter.Where, "IN (")
	require.NotContains(t, filter.Where, "g.resource <>", "a disabled surface cannot judge a moved URL")
	require.Contains(t, filter.Args, "ACTIVE")
	require.Contains(t, filter.Args, "EXPIRED")

	_, err = parseConnectedAppFilter(`state == "STATE_UNSPECIFIED"`, now, resource)
	require.Equal(t, connect.CodeInvalidArgument, connectCode(t, err))

	_, err = parseConnectedAppFilter(`state == "BROKEN"`, now, resource)
	require.Equal(t, connect.CodeInvalidArgument, connectCode(t, err))
}

func TestParseConnectedAppFilterFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	filter, err := parseConnectedAppFilter(`user == "users/7" && client.matches("Agent")`, now, "")
	require.NoError(t, err)
	require.Contains(t, filter.Where, "g.user_id = $1")
	require.Contains(t, filter.Where, "LOWER(c.client_name) LIKE $2")
	require.Equal(t, []any{7, "%agent%"}, filter.Args)

	filter, err = parseConnectedAppFilter(`user_email.matches("member@")`, now, "")
	require.NoError(t, err)
	require.Contains(t, filter.Where, "LOWER(p.email) LIKE $1")

	filter, err = parseConnectedAppFilter(`create_time >= "2026-01-01T00:00:00Z"`, now, "")
	require.NoError(t, err)
	require.Contains(t, filter.Where, "g.created_at >=")

	_, err = parseConnectedAppFilter(`create_time == "2026-01-01T00:00:00Z"`, now, "")
	require.Equal(t, connect.CodeInvalidArgument, connectCode(t, err))
}

// connectCode extracts the Connect code from an error a test expects to be an
// InvalidArgument.
func connectCode(t *testing.T, err error) connect.Code {
	t.Helper()
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	return connectErr.Code()
}
