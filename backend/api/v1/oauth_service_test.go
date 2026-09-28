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

func TestParseOAuthAuthorizationRequestName(t *testing.T) {
	t.Parallel()

	requestID, err := parseOAuthAuthorizationRequestName("oauthAuthorizationRequests/abc123")
	require.NoError(t, err)
	require.Equal(t, "abc123", requestID)

	for _, name := range []string{"", "abc123", "oauthAuthorizationRequests/", "deviceLogins/abc123", "oauthAuthorizationRequests"} {
		_, err := parseOAuthAuthorizationRequestName(name)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "name %q", name)
	}
}

// The conversion is the only place a stored request becomes visible to the
// browser, so every field the page shows is mapped and the authorization code
// the record may carry afterwards is dropped.
func TestConvertOAuthAuthorizationRequest(t *testing.T) {
	t.Parallel()

	stateCfg := mustState(t)
	now := time.Now().Truncate(time.Millisecond)
	created, err := stateCfg.OAuthAuthorizationRequestStore.Create(state.OAuthAuthorizationRequest{
		ClientID:            "client-1",
		ClientName:          "Claude",
		RedirectURI:         "http://127.0.0.1:8123/callback",
		Resource:            "https://metaxisdata.example/mcp",
		Scopes:              []string{"metaxisdata.mcp.read"},
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		ClientState:         "opaque-client-state",
		UserID:              7,
		RequestIP:           "203.0.113.7",
	}, now)
	require.NoError(t, err)

	// An approval mints the code onto the same record, which is exactly the
	// state the consent page reads when it comes back after a decision.
	require.NoError(t, stateCfg.OAuthAuthorizationRequestStore.Approve(created.RequestID, 7, true, now))
	completed, err := stateCfg.OAuthAuthorizationRequestStore.Complete(created.RequestID, 7, now)
	require.NoError(t, err)
	require.NotEmpty(t, completed.Code)

	msg := convertOAuthAuthorizationRequest(completed)
	require.Equal(t, "oauthAuthorizationRequests/"+created.RequestID, msg.GetName())
	require.Equal(t, "Claude", msg.GetClientName())
	require.Equal(t, "http://127.0.0.1:8123/callback", msg.GetRedirectUri())
	require.Equal(t, "https://metaxisdata.example/mcp", msg.GetResource())
	require.Equal(t, []string{"metaxisdata.mcp.read"}, msg.GetScopes())
	require.Equal(t, "203.0.113.7", msg.GetRequestIp())
	require.Equal(t, completed.CreateTime.UTC(), msg.GetCreateTime().AsTime().UTC())
	require.Equal(t, completed.ExpireTime.UTC(), msg.GetExpireTime().AsTime().UTC())

	// The proto has no code-shaped field at all, and the code's value cannot
	// appear anywhere in the serialized message.
	fields := msg.ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		require.NotContains(t, string(fields.Get(i).Name()), "code", "field %q", fields.Get(i).Name())
	}
	encoded, err := proto.Marshal(msg)
	require.NoError(t, err)
	require.False(t, bytes.Contains(encoded, []byte(completed.Code)), "the authorization code leaked into the response")
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

	_, err := svc.GetOAuthAuthorizationRequest(oauthCallerContext(7), connect.NewRequest(&v1pb.GetOAuthAuthorizationRequestRequest{
		Name: "oauthAuthorizationRequests/",
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestGetOAuthAuthorizationRequestReturnsTheRequest(t *testing.T) {
	t.Parallel()

	stateCfg := mustState(t)
	svc := &OAuthService{stateCfg: stateCfg}
	created, err := stateCfg.OAuthAuthorizationRequestStore.Create(state.OAuthAuthorizationRequest{
		ClientName:  "Claude",
		RedirectURI: "https://claude.example/callback",
		Resource:    "https://metaxisdata.example/mcp",
		Scopes:      []string{"metaxisdata.mcp.read"},
		UserID:      7,
		RequestIP:   "203.0.113.7",
	}, time.Now())
	require.NoError(t, err)

	resp, err := svc.GetOAuthAuthorizationRequest(oauthCallerContext(7), connect.NewRequest(&v1pb.GetOAuthAuthorizationRequestRequest{
		Name: "oauthAuthorizationRequests/" + created.RequestID,
	}))
	require.NoError(t, err)
	require.Equal(t, "oauthAuthorizationRequests/"+created.RequestID, resp.Msg.GetName())
	require.Equal(t, "Claude", resp.Msg.GetClientName())
	require.Equal(t, "https://metaxisdata.example/mcp", resp.Msg.GetResource())
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

// A request accepts exactly one decision. The handler is not called here
// because the approver eligibility check reads the workspace setting from the
// database, so this drives the store the handler delegates to and maps the
// error the handler would return.
func TestApproveOAuthAuthorizationRequestNonPendingIsFailedPrecondition(t *testing.T) {
	t.Parallel()

	stateCfg := mustState(t)
	now := time.Now()
	created, err := stateCfg.OAuthAuthorizationRequestStore.Create(state.OAuthAuthorizationRequest{
		ClientName: "Claude",
		UserID:     7,
	}, now)
	require.NoError(t, err)
	require.NoError(t, stateCfg.OAuthAuthorizationRequestStore.Approve(created.RequestID, 7, true, now))

	err = stateCfg.OAuthAuthorizationRequestStore.Approve(created.RequestID, 7, false, now)
	require.ErrorIs(t, err, state.ErrOAuthRequestNotPending)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(oauthAuthorizationRequestStoreError(err)))
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
