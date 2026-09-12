package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// GroupService manages groups. A group is an IAM principal: the workspace IAM
// policy may bind roles to `groups/{email}`, granting every member the role.
// Each RPC is gated by the ACL interceptor with the metaxisdata.groups.*
// permissions.
type GroupService struct {
	v1connect.UnimplementedGroupServiceHandler
	store *store.Store
}

// NewGroupService creates a new GroupService.
func NewGroupService(stores *store.Store) *GroupService {
	return &GroupService{store: stores}
}

func (s *GroupService) GetGroup(ctx context.Context, request *connect.Request[v1pb.GetGroupRequest]) (*connect.Response[v1pb.Group], error) {
	group, err := s.store.GetGroupByName(ctx, request.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if group == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("group %q not found", request.Msg.GetName()))
	}
	return connect.NewResponse(convertToV1Group(group)), nil
}

func (s *GroupService) ListGroups(ctx context.Context, request *connect.Request[v1pb.ListGroupsRequest]) (*connect.Response[v1pb.ListGroupsResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   request.Msg.GetPageToken(),
		limit:   int(request.Msg.GetPageSize()),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1
	groups, err := s.store.ListGroups(ctx, &store.FindGroupMessage{
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list groups"))
	}
	groups, nextPageToken, err := paginate(groups, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListGroupsResponse{NextPageToken: nextPageToken}
	for _, group := range groups {
		response.Groups = append(response.Groups, convertToV1Group(group))
	}
	return connect.NewResponse(response), nil
}

func (s *GroupService) CreateGroup(ctx context.Context, request *connect.Request[v1pb.CreateGroupRequest]) (*connect.Response[v1pb.Group], error) {
	email, err := common.GetGroupEmail(request.Msg.GetGroup().GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	payload, err := convertToGroupPayload(ctx, s.store, request.Msg.GetGroup().GetMembers())
	if err != nil {
		return nil, err
	}
	group, err := s.store.CreateGroup(ctx, &store.GroupMessage{
		Email:       email,
		Title:       request.Msg.GetGroup().GetTitle(),
		Description: request.Msg.GetGroup().GetDescription(),
		Payload:     payload,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertToV1Group(group)), nil
}

func (s *GroupService) UpdateGroup(ctx context.Context, request *connect.Request[v1pb.UpdateGroupRequest]) (*connect.Response[v1pb.Group], error) {
	email, err := common.GetGroupEmail(request.Msg.GetGroup().GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if request.Msg.GetUpdateMask() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}
	existing, err := s.store.GetGroup(ctx, email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get group"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("group %q not found", email))
	}

	patch := &store.UpdateGroupMessage{}
	for _, path := range request.Msg.GetUpdateMask().GetPaths() {
		switch path {
		case "title":
			title := request.Msg.GetGroup().GetTitle()
			patch.Title = &title
		case "description":
			description := request.Msg.GetGroup().GetDescription()
			patch.Description = &description
		case "members":
			payload, err := convertToGroupPayload(ctx, s.store, request.Msg.GetGroup().GetMembers())
			if err != nil {
				return nil, err
			}
			patch.Payload = payload
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update_mask path %q", path))
		}
	}

	group, err := s.store.UpdateGroup(ctx, email, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update group"))
	}
	return connect.NewResponse(convertToV1Group(group)), nil
}

func (s *GroupService) DeleteGroup(ctx context.Context, request *connect.Request[v1pb.DeleteGroupRequest]) (*connect.Response[emptypb.Empty], error) {
	email, err := common.GetGroupEmail(request.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	// Deleting a group that a policy still binds would silently revoke (or
	// strand) that binding, so refuse until the bindings are removed.
	used, err := s.store.GetPoliciesUsingMember(ctx, common.FormatGroupEmail(email))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check group references"))
	}
	if len(used) > 0 {
		names := make([]string, 0, len(used))
		for _, u := range used {
			names = append(names, u.Resource)
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("group %q is still bound in the IAM policy of: %s", email, strings.Join(names, ", ")))
	}
	deleted, err := s.store.DeleteGroup(ctx, email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete group"))
	}
	if !deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("group %q not found", email))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// convertToV1Group converts a stored group into its v1 view.
func convertToV1Group(group *store.GroupMessage) *v1pb.Group {
	out := &v1pb.Group{
		Name:        common.FormatGroupEmail(group.Email),
		Title:       group.Title,
		Description: group.Description,
	}
	if group.Payload != nil {
		for _, member := range group.Payload.GetMembers() {
			out.Members = append(out.Members, &v1pb.GroupMember{
				Member: member.GetMember(),
				Role:   convertToV1GroupMemberRole(member.GetRole()),
			})
		}
	}
	return out
}

func convertToV1GroupMemberRole(role storepb.GroupMember_Role) v1pb.GroupMember_Role {
	switch role {
	case storepb.GroupMember_OWNER:
		return v1pb.GroupMember_OWNER
	case storepb.GroupMember_MEMBER:
		return v1pb.GroupMember_MEMBER
	case storepb.GroupMember_ROLE_UNSPECIFIED:
		return v1pb.GroupMember_ROLE_UNSPECIFIED
	default:
		return v1pb.GroupMember_ROLE_UNSPECIFIED
	}
}

func convertToStoreGroupMemberRole(role v1pb.GroupMember_Role) storepb.GroupMember_Role {
	switch role {
	case v1pb.GroupMember_OWNER:
		return storepb.GroupMember_OWNER
	case v1pb.GroupMember_MEMBER:
		return storepb.GroupMember_MEMBER
	case v1pb.GroupMember_ROLE_UNSPECIFIED:
		return storepb.GroupMember_ROLE_UNSPECIFIED
	default:
		return storepb.GroupMember_ROLE_UNSPECIFIED
	}
}

// convertToGroupPayload converts and validates the v1 member list. Every member
// must name an existing, non-deleted user; a binding to a missing member would
// never match, so it is rejected at write time.
func convertToGroupPayload(ctx context.Context, stores *store.Store, members []*v1pb.GroupMember) (*storepb.GroupPayload, error) {
	payload := &storepb.GroupPayload{}
	seen := map[string]bool{}
	for _, member := range members {
		name := member.GetMember()
		userID, err := common.GetUserID(name)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid member %q", name))
		}
		if seen[name] {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("duplicate member %q", name))
		}
		seen[name] = true
		user, err := stores.GetUserByID(ctx, userID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to look up member %q", name))
		}
		if user == nil || user.MemberDeleted {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("member %q does not exist or is deleted", name))
		}
		payload.Members = append(payload.Members, &storepb.GroupMember{
			Member: name,
			Role:   convertToStoreGroupMemberRole(member.GetRole()),
		})
	}
	return payload, nil
}
