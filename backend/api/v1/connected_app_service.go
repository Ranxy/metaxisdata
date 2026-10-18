package v1

import (
	"context"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/api/oauth"
	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/iam"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ConnectedAppService manages the MCP OAuth connections a user established by
// authorizing a client against the deployment's MCP endpoint.
//
// Neither method carries a permission annotation: the same call serves a
// workspace administrator listing every connection and an ordinary user managing
// their own, so each handler scopes its rows and authorizes the widen itself
// (the pattern UserService's self-service paths and NotificationService use). The
// catalog still defines connectedApps.list and connectedApps.revoke, which are
// what the widen checks below resolve.
type ConnectedAppService struct {
	v1connect.UnimplementedConnectedAppServiceHandler
	stores   *store.Store
	iam      PermissionChecker
	stateCfg *state.State
}

// NewConnectedAppService creates a new ConnectedAppService.
func NewConnectedAppService(stores *store.Store, iamManager *iam.Manager, stateCfg *state.State) *ConnectedAppService {
	return &ConnectedAppService{stores: stores, iam: iamManager, stateCfg: stateCfg}
}

// ListConnectedApps returns the caller's connections, or the whole workspace's
// when the caller holds metaxisdata.connectedApps.list.
func (s *ConnectedAppService) ListConnectedApps(ctx context.Context, req *connect.Request[v1pb.ListConnectedAppsRequest]) (*connect.Response[v1pb.ListConnectedAppsResponse], error) {
	caller, ok := GetUserFromContext(ctx)
	if !ok || caller == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	// `user` selects whose connections to list; the scope rule lives in
	// connectedAppListScope so it is pinned by a hermetic matrix test.
	userID, err := connectedAppListScope(ctx, s.iam, caller, req.Msg.GetUser())
	if err != nil {
		return nil, err
	}

	limit := int(req.Msg.GetPageSize())
	if limit == 0 && req.Msg.GetPageToken() == "" {
		// The shared pager defaults to 10; this list is a settings table, where a
		// small first page reads like a bug. A follow-up request keeps the page
		// size the token was issued with.
		limit = 50
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   limit,
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	now := time.Now()
	// The derived state and the state filter need the current MCP resource. An
	// unresolvable surface (disabled, or external_url missing) leaves it empty,
	// which drops the re-authorization clause instead of failing the page: the
	// management view is also how a stale surface gets cleaned up.
	currentResource := s.currentMCPResource(ctx)
	filter, err := parseConnectedAppFilter(req.Msg.GetFilter(), now, currentResource)
	if err != nil {
		return nil, err
	}
	canReadUsers, err := s.iam.CheckPermission(ctx, permission.UsersGet, caller)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the users read permission"))
	}

	grants, err := s.stores.ListConnectedApps(ctx, &store.FindOAuthGrantMessage{
		UserID: userID,
		Filter: filter,
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list connected apps"))
	}
	grants, nextPageToken, err := paginate(grants, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListConnectedAppsResponse{NextPageToken: nextPageToken}
	for _, grant := range grants {
		response.ConnectedApps = append(response.ConnectedApps, convertConnectedApp(grant, now, currentResource, canReadUsers || grant.UserID == caller.ID))
	}
	return connect.NewResponse(response), nil
}

// RevokeConnectedApp revokes one connection. A user may revoke their own; another
// user's connection requires metaxisdata.connectedApps.revoke, and the refusal is
// NotFound so the call cannot be used to discover which connections exist.
func (s *ConnectedAppService) RevokeConnectedApp(ctx context.Context, req *connect.Request[v1pb.RevokeConnectedAppRequest]) (*connect.Response[v1pb.ConnectedApp], error) {
	caller, ok := GetUserFromContext(ctx)
	if !ok || caller == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	id, err := common.GetConnectedAppID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid connected app name %q", req.Msg.GetName()))
	}

	grant, err := s.stores.GetConnectedApp(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to load the connected app"))
	}
	if grant == nil {
		return nil, connectedAppNotFound(id)
	}
	revocable, err := connectedAppRevocable(ctx, s.iam, caller, grant)
	if err != nil {
		return nil, err
	}
	if !revocable {
		return nil, connectedAppNotFound(id)
	}

	now := time.Now()
	revokedBy := caller.ID
	if _, err := s.stores.RevokeOAuthGrant(ctx, id, now, &revokedBy); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to revoke the connected app"))
	}
	// Marked after the write commits, so this replica refuses the connection's
	// tokens immediately; a repeated revocation costs nothing but still refreshes
	// the cache decision.
	s.stateCfg.GrantRevocationCache.Revoke(strconv.FormatInt(id, 10), now)

	// Return the stored snapshot, not the pre-revocation read: a repeated revoke
	// keeps the first revoker's row, while the response describes the current
	// state either way.
	revoked, err := s.stores.GetConnectedApp(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to reload the connected app"))
	}
	if revoked == nil {
		return nil, connectedAppNotFound(id)
	}
	canReadUsers, err := s.iam.CheckPermission(ctx, permission.UsersGet, caller)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the users read permission"))
	}
	return connect.NewResponse(convertConnectedApp(revoked, now, s.currentMCPResource(ctx), canReadUsers || revoked.UserID == caller.ID)), nil
}

// currentMCPResource returns the deployment's MCP resource identifier, or an
// empty string when the surface is disabled or its address cannot be resolved.
func (s *ConnectedAppService) currentMCPResource(ctx context.Context) string {
	resolution, err := oauth.ResolveEndpoints(ctx, s.stores)
	if err != nil || !resolution.Enabled {
		return ""
	}
	return resolution.Endpoints.Resource
}

// connectedAppNotFound is the one refusal for an unknown and for another user's
// connection.
func connectedAppNotFound(id int64) error {
	return connect.NewError(connect.CodeNotFound, errors.Errorf("connected app %d not found", id))
}

// convertConnectedApp maps a stored grant onto the display message. includeEmail
// is false when the caller may neither read users nor is the connection's own
// user: reading connected apps does not grant reading account data.
func convertConnectedApp(grant *store.OAuthGrant, now time.Time, currentResource string, includeEmail bool) *v1pb.ConnectedApp {
	app := &v1pb.ConnectedApp{
		Name:       common.ConnectedAppNamePrefix + strconv.FormatInt(grant.ID, 10),
		ClientId:   grant.ClientID,
		ClientName: grant.ClientName,
		User:       common.FormatUserUID(grant.UserID),
		Resource:   grant.Resource,
		Scopes:     scopesOf(grant.Scope),
		CreateTime: timestamppb.New(grant.CreatedAt),
		ExpireTime: timestamppb.New(grant.ExpiresAt),
		State:      connectedAppState(grant, now, currentResource),
	}
	if includeEmail {
		app.UserEmail = grant.UserEmail
	}
	if grant.LastToolCallAt != nil {
		app.LastToolCallTime = timestamppb.New(*grant.LastToolCallAt)
	}
	if grant.RevokedAt != nil {
		app.RevokeTime = timestamppb.New(*grant.RevokedAt)
	}
	return app
}

// connectedAppState derives the state with the same precedence the list filter's
// CASE expression encodes.
func connectedAppState(grant *store.OAuthGrant, now time.Time, currentResource string) v1pb.ConnectedApp_State {
	switch {
	case grant.RevokedAt != nil:
		return v1pb.ConnectedApp_REVOKED
	case !now.Before(grant.ExpiresAt):
		return v1pb.ConnectedApp_EXPIRED
	case grant.UserDeleted:
		return v1pb.ConnectedApp_INACTIVE
	case currentResource != "" && grant.Resource != currentResource:
		return v1pb.ConnectedApp_REAUTH_REQUIRED
	case grant.UserLastChangePasswordTime != nil && grant.CreatedAt.Before(*grant.UserLastChangePasswordTime):
		return v1pb.ConnectedApp_REAUTH_REQUIRED
	default:
		return v1pb.ConnectedApp_ACTIVE
	}
}

// scopesOf splits the stored space-delimited consent into the API's list.
func scopesOf(scope string) []string {
	fields := strings.Fields(scope)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// connectedAppListScope resolves whose connections a list call may read. A caller
// holding connectedApps.list may read the whole workspace, and naming a user
// narrows it; every other caller is forced to their own rows. Naming another user
// without the permission is refused rather than silently narrowed, so the call
// cannot be used to learn whose connections exist.
func connectedAppListScope(ctx context.Context, checker PermissionChecker, caller *store.UserMessage, requestedUser string) (*int, error) {
	canListAll, err := checker.CheckPermission(ctx, permission.ConnectedAppsList, caller)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the connected-apps permission"))
	}
	if requestedUser != "" {
		id, err := common.GetUserID(requestedUser)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid user %q", requestedUser))
		}
		if !canListAll && id != caller.ID {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("listing another user's connected apps requires metaxisdata.connectedApps.list"))
		}
		return &id, nil
	}
	if canListAll {
		return nil, nil
	}
	id := caller.ID
	return &id, nil
}

// connectedAppRevocable reports whether the caller may revoke the connection. A
// user always owns their own; another user's needs connectedApps.revoke.
func connectedAppRevocable(ctx context.Context, checker PermissionChecker, caller *store.UserMessage, grant *store.OAuthGrant) (bool, error) {
	if grant.UserID == caller.ID {
		return true, nil
	}
	allowed, err := checker.CheckPermission(ctx, permission.ConnectedAppsRevoke, caller)
	if err != nil {
		return false, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the connected-apps revoke permission"))
	}
	return allowed, nil
}
