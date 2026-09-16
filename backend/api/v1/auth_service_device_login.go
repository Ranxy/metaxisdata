package v1

import (
	"context"
	"log/slog"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// deviceLoginNamePrefix is the resource name prefix of a device login.
	deviceLoginNamePrefix = "deviceLogins/"
	// devicePagePath is the confirmation page inside the web application.
	devicePagePath = "/device"
	// maxDeviceLoginClientFieldBytes bounds the display-only client fields.
	// They come from an anonymous caller and are held for the request's whole
	// lifetime, so an unbounded value would let one caller decide how much
	// memory the server keeps: with the endpoint's rate limit, a 100 MB name
	// would be tens of gigabytes resident.
	maxDeviceLoginClientFieldBytes = 100
)

// CreateDeviceLogin starts a device login. It is anonymous by design, so it is
// throttled per source address rather than per user.
func (s *AuthService) CreateDeviceLogin(ctx context.Context, req *connect.Request[v1pb.CreateDeviceLoginRequest]) (*connect.Response[v1pb.CreateDeviceLoginResponse], error) {
	if err := validateDeviceLoginClientField("client_name", req.Msg.GetClientName()); err != nil {
		return nil, err
	}
	if err := validateDeviceLoginClientField("client_version", req.Msg.GetClientVersion()); err != nil {
		return nil, err
	}

	metadata := buildRequestMetadata(req.Header(), req.Peer().Addr, s.profile.TrustedProxies)
	now := time.Now()
	if !s.stateCfg.DeviceLoginLimiter.Allow(metadata.GetIp(), now) {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("too many device login requests, try again later"))
	}

	// Read the setting before allocating anything, so a failing workspace
	// lookup does not leave an orphaned request behind.
	verificationURI, err := s.deviceLoginVerificationURI(ctx)
	if err != nil {
		return nil, err
	}

	login, err := s.stateCfg.DeviceLoginStore.NewDeviceLogin(state.DeviceLogin{
		ClientName:       req.Msg.GetClientName(),
		ClientVersion:    req.Msg.GetClientVersion(),
		RequestIP:        metadata.GetIp(),
		RequestUserAgent: metadata.GetUserAgent(),
	}, now)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create the device login"))
	}

	response := &v1pb.CreateDeviceLoginResponse{
		DeviceCode: login.DeviceCode,
		UserCode:   login.UserCode,
		ExpiresIn:  int32(state.DeviceLoginTTL.Seconds()),
		Interval:   int32(state.DeviceLoginRecommendedPollInterval.Seconds()),
	}
	if verificationURI != "" {
		response.VerificationUri = verificationURI
		// The address was rebuilt without a query, so appending one is safe.
		response.VerificationUriComplete = verificationURI + "?user_code=" + neturl.QueryEscape(login.UserCode)
	}
	return connect.NewResponse(response), nil
}

// GetDeviceLogin returns the request a user code refers to, so the confirmation
// page can show what is about to be approved.
func (s *AuthService) GetDeviceLogin(ctx context.Context, req *connect.Request[v1pb.GetDeviceLoginRequest]) (*connect.Response[v1pb.DeviceLogin], error) {
	userCode, err := parseDeviceLoginName(req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	if !s.stateCfg.DeviceLoginLookupLimiter.Allow(deviceLoginCallerKey(ctx, req.Header(), req.Peer().Addr, s.profile.TrustedProxies), time.Now()) {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("too many device login lookups, try again later"))
	}

	login, err := s.stateCfg.DeviceLoginStore.GetByUserCode(userCode, time.Now())
	if err != nil {
		return nil, deviceLoginStoreError(err)
	}
	converted, err := s.convertDeviceLogin(ctx, login)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(converted), nil
}

// ApproveDeviceLogin records the signed-in caller's decision. The caller becomes
// the identity the CLI's token is issued for, so their account state is checked
// again here.
func (s *AuthService) ApproveDeviceLogin(ctx context.Context, req *connect.Request[v1pb.ApproveDeviceLoginRequest]) (*connect.Response[emptypb.Empty], error) {
	approver, ok := GetUserFromContext(ctx)
	if !ok || approver == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	userCode, err := parseDeviceLoginName(req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	if err := s.validateDeviceLoginApprover(ctx, approver); err != nil {
		return nil, err
	}
	if !s.stateCfg.DeviceLoginLookupLimiter.Allow(deviceLoginCallerKey(ctx, req.Header(), req.Peer().Addr, s.profile.TrustedProxies), time.Now()) {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("too many device login lookups, try again later"))
	}

	if _, err := s.stateCfg.DeviceLoginStore.Approve(userCode, approver.ID, req.Msg.GetApprove(), time.Now()); err != nil {
		return nil, deviceLoginStoreError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ExchangeDeviceLogin polls a request. An approved request is consumed and
// answered with the token; every other state is reported as-is so the client
// knows whether to keep waiting, stop, or start over.
func (s *AuthService) ExchangeDeviceLogin(ctx context.Context, req *connect.Request[v1pb.ExchangeDeviceLoginRequest]) (*connect.Response[v1pb.ExchangeDeviceLoginResponse], error) {
	if req.Msg.GetDeviceCode() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("device_code is required"))
	}

	login, err := s.stateCfg.DeviceLoginStore.Exchange(req.Msg.GetDeviceCode(), time.Now())
	if err != nil {
		return nil, deviceLoginStoreError(err)
	}

	response := &v1pb.ExchangeDeviceLoginResponse{State: convertDeviceLoginState(login.State)}
	if login.State != state.DeviceLoginApproved {
		return connect.NewResponse(response), nil
	}

	// The token lifetime is resolved once and reused, so the reported remaining
	// lifetime cannot drift from the one the token was minted with.
	tokenDuration := auth.GetTokenDuration(ctx, s.store)
	user, token, err := s.issueDeviceLoginToken(ctx, login, tokenDuration)
	if err != nil {
		return nil, err
	}
	response.Token = token
	response.User = convertToUser(user)
	response.ExpiresIn = int64(tokenDuration.Seconds())
	return connect.NewResponse(response), nil
}

// deviceLoginVerificationURI is the bare confirmation page address. It is empty
// when the workspace has no usable external URL, in which case the client falls
// back to the address it is already talking to.
//
// The setting is a frontend address an administrator typed, so it is parsed and
// rebuilt rather than concatenated: a value that carries a query, a fragment or
// a trailing path would otherwise produce a broken link. A value that cannot be
// parsed at all is reported as "no address" rather than as an error, because
// failing the login over an unrelated frontend setting would leave a workspace
// unable to sign anyone in.
func (s *AuthService) deviceLoginVerificationURI(ctx context.Context) (string, error) {
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to find workspace setting"))
	}
	raw := strings.TrimSpace(setting.GetExternalUrl())
	if raw == "" {
		return "", nil
	}

	parsed, err := neturl.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		// Falling back is deliberate: an unparseable frontend setting must not
		// stop a workspace from signing anyone in.
		slog.Warn("ignoring the workspace external URL: it cannot be parsed as an absolute address", slog.String("external_url", raw))
		return "", nil //nolint:nilerr // a bad setting falls back to the client's own address
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + devicePagePath
	return parsed.String(), nil
}

// issueDeviceLoginToken mints the access token for the user who approved the
// request, following the same path a web login takes so both produce the same
// token and stamp the same last-login time.
func (s *AuthService) issueDeviceLoginToken(ctx context.Context, login state.DeviceLogin, tokenDuration time.Duration) (*store.UserMessage, string, error) {
	user, err := s.store.GetUserByID(ctx, login.ApprovedByUserID)
	if err != nil {
		return nil, "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get the approving user"))
	}
	if user == nil {
		return nil, "", connect.NewError(connect.CodeNotFound, errors.New("the approving user no longer exists"))
	}
	// The approval may be minutes old, so the account state is checked again
	// before it is turned into a credential.
	if err := s.validateDeviceLoginApprover(ctx, user); err != nil {
		return nil, "", err
	}

	token, err := auth.GenerateAccessToken(user.Name, user.ID, s.profile.Mode, s.secret, tokenDuration)
	if err != nil {
		return nil, "", connect.NewError(connect.CodeInternal, errors.New("failed to generate API access token"))
	}

	updatedUser, err := s.store.UpdateUser(ctx, user, &store.UpdateUserMessage{Profile: profileWithLastLogin(user.Profile)})
	if err != nil {
		slog.Error("failed to update user profile", log.WithError(err), slog.Int("user_id", user.ID))
	} else {
		user = updatedUser
	}
	return user, token, nil
}

// validateDeviceLoginApprover checks that the approver may still be turned into
// a CLI credential.
//
// The workspace password policy is deliberately not re-evaluated here. It only
// governs password sign-in, so applying it to whoever happens to hold a web
// session would lock SSO-only users out of the CLI over a random password they
// never chose. The bypass it guards against is already closed one layer down:
// the auth interceptor refuses a token restricted to reset_password on this RPC
// because only UpdateUser and Logout are allowed for it.
func (s *AuthService) validateDeviceLoginApprover(ctx context.Context, approver *store.UserMessage) error {
	if approver.Type != storepb.PrincipalType_END_USER {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("only an end user can approve a device login"))
	}
	if approver.MemberDeleted {
		return connect.NewError(connect.CodeUnauthenticated, errors.Errorf("user has been deactivated by administrators"))
	}
	return validateEmailWithDomains(ctx, s.store, approver.Email, false)
}

func (s *AuthService) convertDeviceLogin(ctx context.Context, login state.DeviceLogin) (*v1pb.DeviceLogin, error) {
	converted := &v1pb.DeviceLogin{
		Name:             deviceLoginNamePrefix + login.UserCode,
		State:            convertDeviceLoginState(login.State),
		UserCode:         login.UserCode,
		CreateTime:       timestamppb.New(login.CreateTime),
		ExpireTime:       timestamppb.New(login.ExpireTime),
		ClientName:       login.ClientName,
		ClientVersion:    login.ClientVersion,
		RequestIp:        login.RequestIP,
		RequestUserAgent: login.RequestUserAgent,
	}
	if login.ApprovedByUserID == 0 {
		return converted, nil
	}
	approver, err := s.store.GetUserByID(ctx, login.ApprovedByUserID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get the approving user"))
	}
	if approver != nil {
		converted.ApprovedBy = convertToUser(approver)
	}
	return converted, nil
}

func convertDeviceLoginState(loginState state.DeviceLoginState) v1pb.DeviceLoginState {
	switch loginState {
	case state.DeviceLoginPending:
		return v1pb.DeviceLoginState_PENDING
	case state.DeviceLoginApproved:
		return v1pb.DeviceLoginState_APPROVED
	case state.DeviceLoginDenied:
		return v1pb.DeviceLoginState_DENIED
	case state.DeviceLoginExpired:
		return v1pb.DeviceLoginState_EXPIRED
	default:
		return v1pb.DeviceLoginState_DEVICE_LOGIN_STATE_UNSPECIFIED
	}
}

// deviceLoginStoreError maps the store's lifecycle errors onto Connect codes. A
// consumed or unknown request is deliberately reported as not found: the client
// has to start a new authorization either way.
func deviceLoginStoreError(err error) error {
	switch {
	case errors.Is(err, state.ErrDeviceLoginNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("device login not found"))
	case errors.Is(err, state.ErrDeviceLoginExpired):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("the device login expired"))
	case errors.Is(err, state.ErrDeviceLoginNotPending):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("the device login was already answered"))
	case errors.Is(err, state.ErrDeviceLoginSlowDown):
		return connect.NewError(connect.CodeResourceExhausted, errors.New("the device login was polled too soon"))
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

// validateDeviceLoginClientField rejects a display-only field that is longer
// than the server is willing to hold. It is a clear error rather than a silent
// truncation, because a client that sends one is malformed and a clipped label
// would only be confusing.
func validateDeviceLoginClientField(name, value string) error {
	if len(value) > maxDeviceLoginClientFieldBytes {
		return connect.NewError(connect.CodeInvalidArgument,
			errors.Errorf("%s must be at most %d bytes, got %d", name, maxDeviceLoginClientFieldBytes, len(value)))
	}
	return nil
}

// deviceLoginCallerKey identifies who to count a lookup against. The two
// endpoints are authenticated, so the user is the right bucket; the source
// address is the fallback for a request the auth layer let through without one.
func deviceLoginCallerKey(ctx context.Context, header http.Header, peerAddr string, trustedProxies []string) string {
	if user, ok := GetUserFromContext(ctx); ok && user != nil {
		return "user:" + strconv.Itoa(user.ID)
	}
	return "ip:" + buildRequestMetadata(header, peerAddr, trustedProxies).GetIp()
}

// parseDeviceLoginName reads the user code out of "deviceLogins/{user_code}".
func parseDeviceLoginName(name string) (string, error) {
	invalid := connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid device login name %q", name))
	if !strings.HasPrefix(name, deviceLoginNamePrefix) {
		return "", invalid
	}
	userCode := state.NormalizeUserCode(strings.TrimPrefix(name, deviceLoginNamePrefix))
	if userCode == "" {
		return "", invalid
	}
	return userCode, nil
}
