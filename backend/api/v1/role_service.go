package v1

import (
	"context"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// RoleService manages custom roles. Predefined roles are read-only over this
// API: create/update/delete refuse a resource ID that collides with a
// predefined role. Each RPC is gated by the ACL interceptor with the
// metaxisdata.roles.* permissions.
type RoleService struct {
	v1connect.UnimplementedRoleServiceHandler
	store *store.Store
}

// NewRoleService creates a new RoleService.
func NewRoleService(stores *store.Store) *RoleService {
	return &RoleService{store: stores}
}

func (s *RoleService) GetRole(ctx context.Context, request *connect.Request[v1pb.GetRoleRequest]) (*connect.Response[v1pb.Role], error) {
	roleID, err := common.GetRoleID(request.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	role, err := s.store.GetRole(ctx, &store.FindRoleMessage{ResourceID: &roleID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get role"))
	}
	if role == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("role %q not found", request.Msg.GetName()))
	}
	return connect.NewResponse(toV1Role(role)), nil
}

func (s *RoleService) ListRoles(ctx context.Context, request *connect.Request[v1pb.ListRolesRequest]) (*connect.Response[v1pb.ListRolesResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   request.Msg.GetPageToken(),
		limit:   int(request.Msg.GetPageSize()),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	roles, err := s.store.ListRoles(ctx, &store.FindRoleMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list roles"))
	}
	// The list mixes Go-defined predefined roles with DB rows, so it is paged
	// in memory after assembly.
	roles, nextPageToken, err := paginateInMemory(roles, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListRolesResponse{NextPageToken: nextPageToken}
	for _, role := range roles {
		response.Roles = append(response.Roles, toV1Role(role))
	}
	return connect.NewResponse(response), nil
}

func (s *RoleService) CreateRole(ctx context.Context, request *connect.Request[v1pb.CreateRoleRequest]) (*connect.Response[v1pb.Role], error) {
	roleID, err := common.GetRoleID(request.Msg.GetRole().GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if store.IsPredefinedRole(roleID) {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("role %q is predefined and read-only", roleID))
	}
	permissions, err := toPermissionMap(request.Msg.GetRole().GetPermissions())
	if err != nil {
		return nil, err
	}
	role, err := s.store.CreateRole(ctx, &store.RoleMessage{
		ResourceID:  roleID,
		Name:        request.Msg.GetRole().GetTitle(),
		Description: request.Msg.GetRole().GetDescription(),
		Permissions: permissions,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create role"))
	}
	return connect.NewResponse(toV1Role(role)), nil
}

func (s *RoleService) UpdateRole(ctx context.Context, request *connect.Request[v1pb.UpdateRoleRequest]) (*connect.Response[v1pb.Role], error) {
	roleID, err := common.GetRoleID(request.Msg.GetRole().GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if store.IsPredefinedRole(roleID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("role %q is predefined and read-only", roleID))
	}
	if request.Msg.GetUpdateMask() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}

	patch := &store.UpdateRoleMessage{ResourceID: roleID}
	for _, path := range request.Msg.GetUpdateMask().GetPaths() {
		switch path {
		case "title":
			title := request.Msg.GetRole().GetTitle()
			patch.Name = &title
		case "description":
			description := request.Msg.GetRole().GetDescription()
			patch.Description = &description
		case "permissions":
			permissions, err := toPermissionMap(request.Msg.GetRole().GetPermissions())
			if err != nil {
				return nil, err
			}
			patch.Permissions = &permissions
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update_mask path %q", path))
		}
	}

	role, err := s.store.UpdateRole(ctx, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update role"))
	}
	if role == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("role %q not found", roleID))
	}
	return connect.NewResponse(toV1Role(role)), nil
}

func (s *RoleService) DeleteRole(ctx context.Context, request *connect.Request[v1pb.DeleteRoleRequest]) (*connect.Response[emptypb.Empty], error) {
	roleID, err := common.GetRoleID(request.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if store.IsPredefinedRole(roleID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("role %q is predefined and read-only", roleID))
	}
	role, err := s.store.GetRole(ctx, &store.FindRoleMessage{ResourceID: &roleID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get role"))
	}
	if role == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("role %q not found", roleID))
	}
	// Deleting a role that is still bound would silently turn that binding into
	// a no-op, so refuse until the bindings are removed.
	used, err := s.store.GetResourcesUsedByRole(ctx, common.FormatRole(roleID))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check role references"))
	}
	if len(used) > 0 {
		names := make([]string, 0, len(used))
		for _, u := range used {
			names = append(names, u.Resource)
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("role %q is still bound in: %s", roleID, strings.Join(names, ", ")))
	}
	if err := s.store.DeleteRole(ctx, roleID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete role"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// toV1Role converts a stored role into its v1 view. Predefined roles are
// flagged so the client can render them read-only.
func toV1Role(role *store.RoleMessage) *v1pb.Role {
	permissions := make([]string, 0, len(role.Permissions))
	for p := range role.Permissions {
		permissions = append(permissions, p)
	}
	slices.Sort(permissions)
	return &v1pb.Role{
		Name:        common.FormatRole(role.ResourceID),
		Title:       role.Name,
		Description: role.Description,
		Permissions: permissions,
		Predefined:  store.IsPredefinedRole(role.ResourceID),
	}
}

// toPermissionMap validates a permission list against the catalog and turns it
// into the set the store persists. An unknown permission is rejected rather
// than stored, so a role can never carry a capability the engine cannot grant.
func toPermissionMap(permissions []string) (map[permission.Permission]bool, error) {
	result := make(map[permission.Permission]bool, len(permissions))
	for _, p := range permissions {
		if !permission.Exist(p) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown permission %q", p))
		}
		result[p] = true
	}
	return result, nil
}
