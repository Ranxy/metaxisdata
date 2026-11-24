package v1

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// These cover the consent surface without a database: the resource-name
// parsing, the display-only conversion and the states a request can be read or
// approved in. The approver eligibility check reads the workspace setting, so
// the happy path of Approve is exercised by the integration suite instead.

// oauthCallerContext puts a signed-in user in the context the way the auth
// interceptor does.
func oauthCallerContext(userID int) context.Context {
	return context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{ID: userID})
}

func TestGetOAuthAuthorizationRequestRequiresACaller(t *testing.T) {
	t.Parallel()

	svc := &OAuthService{stateCfg: mustState(t)}

	_, err := svc.GetOAuthAuthorizationRequest(context.Background(), connect.NewRequest(&v1pb.GetOAuthAuthorizationRequestRequest{
		Name: "oauthAuthorizationRequests/abc123",
	}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestGetOAuthAuthorizationRequestRejectsAMalformedName(t *testing.T) {
	t.Parallel()

	svc := &OAuthService{stateCfg: mustState(t)}

	for _, name := range []string{"", "abc123", "oauthAuthorizationRequests/", "deviceLogins/abc123", "oauthAuthorizationRequests"} {
		_, err := svc.GetOAuthAuthorizationRequest(oauthCallerContext(7), connect.NewRequest(&v1pb.GetOAuthAuthorizationRequestRequest{
			Name: name,
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "name %q", name)
	}
}

// The consent page shows the stored record's display fields, and the
// authorization code an approval mints onto it is never among them: the proto
// has no code-shaped field and the secret cannot appear in the serialized
// response.
func TestGetOAuthAuthorizationRequestReturnsTheRequest(t *testing.T) {
	t.Parallel()

	stateCfg := mustState(t)
	svc := &OAuthService{stateCfg: stateCfg}
	now := time.Now().Truncate(time.Millisecond)
	created, err := stateCfg.OAuthAuthorizationRequestStore.Create(state.OAuthAuthorizationRequest{
		ClientID:      "client-1",
		ClientName:    "Claude",
		RedirectURI:   "https://claude.example/callback",
		Resource:      "https://metaxisdata.example/mcp",
		Scopes:        []string{"metaxisdata.mcp.read"},
		CodeChallenge: "challenge",
		ClientState:   "opaque-client-state",
		UserID:        7,
		RequestIP:     "203.0.113.7",
	}, now)
	require.NoError(t, err)

	// An approval mints the code onto the same record, which is exactly the
	// state the consent page reads when it comes back after a decision.
	require.NoError(t, stateCfg.OAuthAuthorizationRequestStore.Approve(created.RequestID, 7, true, now))
	completed, err := stateCfg.OAuthAuthorizationRequestStore.Complete(created.RequestID, 7, now)
	require.NoError(t, err)
	require.NotEmpty(t, completed.Code)

	resp, err := svc.GetOAuthAuthorizationRequest(oauthCallerContext(7), connect.NewRequest(&v1pb.GetOAuthAuthorizationRequestRequest{
		Name: "oauthAuthorizationRequests/" + completed.RequestID,
	}))
	require.NoError(t, err)

	msg := resp.Msg
	require.Equal(t, "oauthAuthorizationRequests/"+completed.RequestID, msg.GetName())
	require.Equal(t, "Claude", msg.GetClientName())
	require.Equal(t, "https://claude.example/callback", msg.GetRedirectUri())
	require.Equal(t, "https://metaxisdata.example/mcp", msg.GetResource())
	require.Equal(t, []string{"metaxisdata.mcp.read"}, msg.GetScopes())
	require.Equal(t, "203.0.113.7", msg.GetRequestIp())
	require.Equal(t, completed.CreateTime.UTC(), msg.GetCreateTime().AsTime().UTC())
	require.Equal(t, completed.ExpireTime.UTC(), msg.GetExpireTime().AsTime().UTC())

	fields := msg.ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		require.NotContains(t, string(fields.Get(i).Name()), "code", "field %q", fields.Get(i).Name())
	}
	encoded, err := proto.Marshal(msg)
	require.NoError(t, err)
	for _, secret := range []string{completed.Code, completed.CodeChallenge, completed.ClientState} {
		require.False(t, bytes.Contains(encoded, []byte(secret)), "internal material leaked into the response")
	}
}

// A request is visible only to the user it was created for: the caller's id is
// part of the lookup, so a foreign request is refused rather than disclosed.
func TestGetOAuthAuthorizationRequestRefusesAnotherUsersRequest(t *testing.T) {
	t.Parallel()

	stateCfg := mustState(t)
	svc := &OAuthService{stateCfg: stateCfg}
	created, err := stateCfg.OAuthAuthorizationRequestStore.Create(state.OAuthAuthorizationRequest{
		ClientName: "Claude",
		UserID:     1,
	}, time.Now())
	require.NoError(t, err)

	_, err = svc.GetOAuthAuthorizationRequest(oauthCallerContext(2), connect.NewRequest(&v1pb.GetOAuthAuthorizationRequestRequest{
		Name: "oauthAuthorizationRequests/" + created.RequestID,
	}))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestGetOAuthAuthorizationRequestReportsUnknownRequests(t *testing.T) {
	t.Parallel()

	svc := &OAuthService{stateCfg: mustState(t)}

	_, err := svc.GetOAuthAuthorizationRequest(oauthCallerContext(7), connect.NewRequest(&v1pb.GetOAuthAuthorizationRequestRequest{
		Name: "oauthAuthorizationRequests/does-not-exist",
	}))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestApproveOAuthAuthorizationRequestRequiresACaller(t *testing.T) {
	t.Parallel()

	svc := &OAuthService{stateCfg: mustState(t)}

	_, err := svc.ApproveOAuthAuthorizationRequest(context.Background(), connect.NewRequest(&v1pb.ApproveOAuthAuthorizationRequestRequest{
		Name:    "oauthAuthorizationRequests/abc123",
		Approve: true,
	}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestApproveOAuthAuthorizationRequestRejectsAMalformedName(t *testing.T) {
	t.Parallel()

	svc := &OAuthService{stateCfg: mustState(t)}

	_, err := svc.ApproveOAuthAuthorizationRequest(oauthCallerContext(7), connect.NewRequest(&v1pb.ApproveOAuthAuthorizationRequestRequest{
		Name:    "users/7",
		Approve: true,
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestOAuthAuthorizationRequestStoreErrorMapping(t *testing.T) {
	t.Parallel()

	for err, want := range map[error]connect.Code{
		state.ErrOAuthRequestNotFound:    connect.CodeNotFound,
		state.ErrOAuthCodeNotFound:       connect.CodeNotFound,
		state.ErrOAuthRequestExpired:     connect.CodeFailedPrecondition,
		state.ErrOAuthRequestNotPending:  connect.CodeFailedPrecondition,
		state.ErrOAuthRequestNotApproved: connect.CodeFailedPrecondition,
		state.ErrOAuthCodeExpired:        connect.CodeFailedPrecondition,
		state.ErrOAuthRequestWrongUser:   connect.CodePermissionDenied,
	} {
		require.Equal(t, want, connect.CodeOf(oauthAuthorizationRequestStoreError(err)))
	}
	require.Equal(t, connect.CodeInternal, connect.CodeOf(oauthAuthorizationRequestStoreError(errors.New("boom"))))
}
