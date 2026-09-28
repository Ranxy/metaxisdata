package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// oauthAuthorizationRequestNamePrefix is the resource name prefix of a pending
// OAuth 2.1 authorization request.
const oauthAuthorizationRequestNamePrefix = "oauthAuthorizationRequests/"

// OAuthService implements the browser-facing half of the OAuth 2.1
// authorization server: the SPA consent page reads one pending request and
// records the signed-in user's decision. The protocol endpoints themselves are
// plain HTTP and live outside ConnectRPC.
type OAuthService struct {
	v1connect.UnimplementedOAuthServiceHandler
	stores   *store.Store
	profile  *config.Profile
	stateCfg *state.State
}

// NewOAuthService creates a new OAuthService.
func NewOAuthService(stores *store.Store, profile *config.Profile, stateCfg *state.State) *OAuthService {
	return &OAuthService{
		stores:   stores,
		profile:  profile,
		stateCfg: stateCfg,
	}
}

// GetOAuthAuthorizationRequest returns one pending authorization request so the
// consent page can show what is about to be approved.
func (s *OAuthService) GetOAuthAuthorizationRequest(ctx context.Context, req *connect.Request[v1pb.GetOAuthAuthorizationRequestRequest]) (*connect.Response[v1pb.OAuthAuthorizationRequest], error) {
	caller, ok := GetUserFromContext(ctx)
	if !ok || caller == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	requestID, err := parseOAuthAuthorizationRequestName(req.Msg.GetName())
	if err != nil {
		return nil, err
	}

	// The caller's id goes into the lookup, so the store refuses a request that
	// belongs to somebody else before anything is read.
	request, err := s.stateCfg.OAuthAuthorizationRequestStore.Get(requestID, caller.ID, time.Now())
	if err != nil {
		return nil, oauthAuthorizationRequestStoreError(err)
	}
	return connect.NewResponse(convertOAuthAuthorizationRequest(request)), nil
}

// ApproveOAuthAuthorizationRequest records the signed-in caller's decision. The
// caller becomes the identity the authorization code is minted for, so their
// account state is checked again here with the same rule a device login
// approval uses.
func (s *OAuthService) ApproveOAuthAuthorizationRequest(ctx context.Context, req *connect.Request[v1pb.ApproveOAuthAuthorizationRequestRequest]) (*connect.Response[emptypb.Empty], error) {
	caller, ok := GetUserFromContext(ctx)
	if !ok || caller == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	requestID, err := parseOAuthAuthorizationRequestName(req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	if err := validateApprover(ctx, s.stores, caller); err != nil {
		return nil, err
	}

	if err := s.stateCfg.OAuthAuthorizationRequestStore.Approve(requestID, caller.ID, req.Msg.GetApprove(), time.Now()); err != nil {
		return nil, oauthAuthorizationRequestStoreError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// convertOAuthAuthorizationRequest maps a stored request onto the display-only
// message. The record also carries the PKCE challenge, the client's state and,
// once approval is complete, the authorization code; the proto has no field for
// any of them, so none can appear in the response.
func convertOAuthAuthorizationRequest(request state.OAuthAuthorizationRequest) *v1pb.OAuthAuthorizationRequest {
	return &v1pb.OAuthAuthorizationRequest{
		Name:        oauthAuthorizationRequestNamePrefix + request.RequestID,
		ClientName:  request.ClientName,
		RedirectUri: request.RedirectURI,
		Resource:    request.Resource,
		Scopes:      request.Scopes,
		CreateTime:  timestamppb.New(request.CreateTime),
		ExpireTime:  timestamppb.New(request.ExpireTime),
		RequestIp:   request.RequestIP,
	}
}

// oauthAuthorizationRequestStoreError maps the store's lifecycle errors onto
// Connect codes. A consumed or unknown request is deliberately reported as not
// found: the consent page has to start over either way.
func oauthAuthorizationRequestStoreError(err error) error {
	switch {
	case errors.Is(err, state.ErrOAuthRequestNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("oauth authorization request not found"))
	case errors.Is(err, state.ErrOAuthCodeNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("oauth authorization code not found"))
	case errors.Is(err, state.ErrOAuthRequestExpired):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("the oauth authorization request expired"))
	case errors.Is(err, state.ErrOAuthRequestNotPending):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("the oauth authorization request was already answered"))
	case errors.Is(err, state.ErrOAuthRequestNotApproved):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("the oauth authorization request was not approved"))
	case errors.Is(err, state.ErrOAuthCodeExpired):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("the oauth authorization code expired"))
	case errors.Is(err, state.ErrOAuthRequestWrongUser):
		return connect.NewError(connect.CodePermissionDenied, errors.New("the oauth authorization request belongs to another user"))
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

// parseOAuthAuthorizationRequestName reads the request id out of
// "oauthAuthorizationRequests/{request_id}". The id is opaque and assigned by
// the store, so only its presence is checked here.
func parseOAuthAuthorizationRequestName(name string) (string, error) {
	invalid := connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid oauth authorization request name %q", name))
	if !strings.HasPrefix(name, oauthAuthorizationRequestNamePrefix) {
		return "", invalid
	}
	requestID := strings.TrimPrefix(name, oauthAuthorizationRequestNamePrefix)
	if requestID == "" {
		return "", invalid
	}
	return requestID, nil
}
