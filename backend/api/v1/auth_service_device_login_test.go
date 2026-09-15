package v1

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/state"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// These cover the parts of the device login surface that do not need a
// database: the code format, the resource-name parsing, the state mapping and
// the pending read path. The approve and exchange paths reach the user store,
// so they are exercised by the integration suite instead.

func TestNormalizeDeviceUserCodeAcceptsWhatAPersonTypes(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"7Q2X-9M4K",
		"7q2x-9m4k",
		"7Q2X9M4K",
		" 7q2x 9m4k ",
		"7Q2X_9M4K",
	} {
		require.Equal(t, "7Q2X-9M4K", state.NormalizeUserCode(input), "input %q", input)
	}

	// Crockford decodes the confusables to a digit, so a canonical code never
	// contains O, I or L.
	require.Equal(t, "0011-1100", state.NormalizeUserCode("OOII-LIOO"))
	require.Equal(t, "0000-0000", state.NormalizeUserCode("OOOO-OOOO"))
	require.Equal(t, "1111-1111", state.NormalizeUserCode("IIII-IIII"))

	for _, input := range []string{"", "7Q2X-9M4", "7Q2X-9M4KK", "7Q2X-9M4!", "UUUU-UUUU", "7Q2X=9M4K"} {
		require.Empty(t, state.NormalizeUserCode(input), "input %q must be rejected", input)
	}
}

func TestParseDeviceLoginName(t *testing.T) {
	t.Parallel()

	userCode, err := parseDeviceLoginName("deviceLogins/7q2x-9m4k")
	require.NoError(t, err)
	require.Equal(t, "7Q2X-9M4K", userCode)

	for _, name := range []string{"", "7Q2X-9M4K", "deviceLogins/", "deviceLogins/short", "users/7Q2X-9M4K"} {
		_, err := parseDeviceLoginName(name)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "name %q", name)
	}
}

func TestConvertDeviceLoginState(t *testing.T) {
	t.Parallel()

	for stored, want := range map[state.DeviceLoginState]v1pb.DeviceLoginState{
		state.DeviceLoginPending:  v1pb.DeviceLoginState_PENDING,
		state.DeviceLoginApproved: v1pb.DeviceLoginState_APPROVED,
		state.DeviceLoginDenied:   v1pb.DeviceLoginState_DENIED,
		state.DeviceLoginExpired:  v1pb.DeviceLoginState_EXPIRED,
	} {
		require.Equal(t, want, convertDeviceLoginState(stored))
	}
}

func TestDeviceLoginStoreErrorMapping(t *testing.T) {
	t.Parallel()

	for err, want := range map[error]connect.Code{
		state.ErrDeviceLoginNotFound:   connect.CodeNotFound,
		state.ErrDeviceLoginExpired:    connect.CodeFailedPrecondition,
		state.ErrDeviceLoginNotPending: connect.CodeFailedPrecondition,
		state.ErrDeviceLoginSlowDown:   connect.CodeResourceExhausted,
	} {
		require.Equal(t, want, connect.CodeOf(deviceLoginStoreError(err)))
	}
}

// A pending request is readable without touching the user store, which is what
// the confirmation page does before anyone approves anything.
func TestGetDeviceLoginReturnsAPendingRequest(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	svc := &AuthService{stateCfg: stateCfg}

	now := time.Now()
	created, err := stateCfg.DeviceLoginStore.Create(state.DeviceLogin{
		DeviceCode:       "device-secret",
		UserCode:         "7Q2X-9M4K",
		ClientName:       "mxd",
		ClientVersion:    "0.1.0",
		RequestIP:        "203.0.113.7",
		RequestUserAgent: "mxd/0.1.0",
	}, now)
	require.NoError(t, err)

	resp, err := svc.GetDeviceLogin(context.Background(), connect.NewRequest(&v1pb.GetDeviceLoginRequest{
		Name: "deviceLogins/" + created.UserCode,
	}))
	require.NoError(t, err)

	msg := resp.Msg
	require.Equal(t, "deviceLogins/7Q2X-9M4K", msg.GetName())
	require.Equal(t, v1pb.DeviceLoginState_PENDING, msg.GetState())
	require.Equal(t, "7Q2X-9M4K", msg.GetUserCode())
	require.Equal(t, "mxd", msg.GetClientName())
	require.Equal(t, "0.1.0", msg.GetClientVersion())
	require.Equal(t, "203.0.113.7", msg.GetRequestIp())
	require.Equal(t, "mxd/0.1.0", msg.GetRequestUserAgent())
	require.Nil(t, msg.GetApprovedBy())
	require.Equal(t, created.ExpireTime.UTC(), msg.GetExpireTime().AsTime().UTC())
}

func TestGetDeviceLoginRejectsUnknownCodes(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	svc := &AuthService{stateCfg: stateCfg}
	ctx := context.Background()

	_, err = svc.GetDeviceLogin(ctx, connect.NewRequest(&v1pb.GetDeviceLoginRequest{
		Name: "deviceLogins/7Q2X-9M4K",
	}))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// A well-formed but unknown code is not found; a malformed one is rejected
	// before the store is consulted. "UUUU-UUUU" uses a character outside the
	// Crockford alphabet, and "123" is too short.
	for _, name := range []string{"deviceLogins/UUUU-UUUU", "deviceLogins/123"} {
		_, err = svc.GetDeviceLogin(ctx, connect.NewRequest(&v1pb.GetDeviceLoginRequest{Name: name}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "name %q", name)
	}
}

func TestExchangeDeviceLoginRequiresADeviceCode(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	svc := &AuthService{stateCfg: stateCfg}

	_, err = svc.ExchangeDeviceLogin(context.Background(), connect.NewRequest(&v1pb.ExchangeDeviceLoginRequest{}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// Polling a pending request reports that it is still pending, and polling it
// again immediately is refused as a slow-down.
func TestExchangeDeviceLoginReportsPendingAndThrottles(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	svc := &AuthService{stateCfg: stateCfg}
	ctx := context.Background()

	created, err := stateCfg.DeviceLoginStore.Create(state.DeviceLogin{
		DeviceCode: "device-secret",
		UserCode:   "7Q2X-9M4K",
	}, time.Now())
	require.NoError(t, err)

	resp, err := svc.ExchangeDeviceLogin(ctx, connect.NewRequest(&v1pb.ExchangeDeviceLoginRequest{
		DeviceCode: created.DeviceCode,
	}))
	require.NoError(t, err)
	require.Equal(t, v1pb.DeviceLoginState_PENDING, resp.Msg.GetState())
	require.Empty(t, resp.Msg.GetToken())

	_, err = svc.ExchangeDeviceLogin(ctx, connect.NewRequest(&v1pb.ExchangeDeviceLoginRequest{
		DeviceCode: created.DeviceCode,
	}))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
}

// Approving requires a signed-in caller; the token restriction and the ACL
// layer sit in front of this, but the handler must not panic without a user.
func TestApproveDeviceLoginRequiresACaller(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	svc := &AuthService{stateCfg: stateCfg}

	_, err = svc.ApproveDeviceLogin(context.Background(), connect.NewRequest(&v1pb.ApproveDeviceLoginRequest{
		Name:    "deviceLogins/7Q2X-9M4K",
		Approve: true,
	}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}
