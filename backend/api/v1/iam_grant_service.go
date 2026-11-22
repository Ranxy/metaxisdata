package v1

import (
	"context"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	exprpb "google.golang.org/genproto/googleapis/type/expr"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// iamGrantPrefix names one scoped grant. Grants are workspace-level objects
	// (the metadata tables are single-tenant), like roles and groups.
	iamGrantPrefix = "iamGrants/"
	// iamGrantDefaultPageSize and iamGrantMaximumPageSize bound one grant list.
	iamGrantDefaultPageSize = 50
	iamGrantMaximumPageSize = 1000
)

// ListIamGrants lists the scoped grants, optionally narrowed to one resource.
func (s *IamService) ListIamGrants(ctx context.Context, req *connect.Request[v1pb.ListIamGrantsRequest]) (*connect.Response[v1pb.ListIamGrantsResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      iamGrantMaximumPageSize,
		defaultLimit: iamGrantDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	find := &store.FindIamGrantMessage{
		Limit:  intPointer(offset.limit + 1),
		Offset: intPointer(offset.offset),
	}
	if raw := strings.TrimSpace(req.Msg.GetResource()); raw != "" {
		resourceType, resourceID, err := parseIamGrantResource(raw)
		if err != nil {
			return nil, err
		}
		find.ResourceType = &resourceType
		find.ResourceID = &resourceID
	}
	grants, err := s.store.ListIamGrants(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the iam grants")
	}
	grants, nextPageToken, err := paginate(grants, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListIamGrantsResponse{NextPageToken: nextPageToken}
	for _, grant := range grants {
		response.Grants = append(response.Grants, convertIamGrant(grant))
	}
	return connect.NewResponse(response), nil
}

// CreateIamGrant adds one scoped grant. The role, member, scope and condition
// are validated before the row is written; a duplicate is a conflict.
func (s *IamService) CreateIamGrant(ctx context.Context, req *connect.Request[v1pb.CreateIamGrantRequest]) (*connect.Response[v1pb.IamGrant], error) {
	grant := req.Msg.GetGrant()
	if grant == nil {
		return nil, connectErrorInvalid("a grant is required")
	}
	resourceType, resourceID, err := parseIamGrantResource(grant.GetResource())
	if err != nil {
		return nil, err
	}
	if err := s.validateIamGrantResource(ctx, resourceType, resourceID); err != nil {
		return nil, err
	}
	roleID, err := common.GetRoleID(grant.GetRole())
	if err != nil {
		return nil, connectErrorInvalid("invalid role %q", grant.GetRole())
	}
	role, err := s.store.GetRoleSnapshot(ctx, roleID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the role"))
	}
	if role == nil {
		return nil, connectErrorInvalid("unknown role %q", grant.GetRole())
	}
	switch roleID {
	case store.WorkspaceAdminRole, store.WorkspaceMemberRole, store.QualityAgentRole:
		return nil, connectErrorInvalid(
			"role %q is workspace-scoped; grant qualityOwner or a custom role at a scope", grant.GetRole())
	}
	// allUsers is the workspace read baseline. A scoped write grant to every
	// principal would turn the baseline into a write grant; bind a group instead.
	if grant.GetMember() == common.AllUsers {
		return nil, connectErrorInvalid("allUsers is the workspace baseline; bind a group instead")
	}
	if err := validateIamMember(ctx, s.store, grant.GetMember()); err != nil {
		return nil, err
	}
	condition := strings.TrimSpace(grant.GetCondition().GetExpression())
	if _, err := common.EvalBindingCondition(condition, time.Now()); err != nil {
		return nil, connectErrorInvalid("invalid condition: %v", err)
	}
	created, err := s.store.CreateIamGrant(ctx, &store.IamGrantMessage{
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Role:         grant.GetRole(),
		Member:       grant.GetMember(),
		Condition:    condition,
		CreatedBy:    principalIDPointer(currentUserID(ctx)),
	})
	if err != nil {
		if common.ErrorCode(err) == common.Conflict {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, errors.Wrap(err, "failed to create the iam grant")
	}
	return connect.NewResponse(convertIamGrant(created)), nil
}

// DeleteIamGrant removes one scoped grant.
func (s *IamService) DeleteIamGrant(ctx context.Context, req *connect.Request[v1pb.DeleteIamGrantRequest]) (*connect.Response[emptypb.Empty], error) {
	token, err := common.GetOpenLineageToken(req.Msg.GetName(), iamGrantPrefix)
	if err != nil {
		return nil, connectErrorInvalid("invalid grant name %q", req.Msg.GetName())
	}
	id, err := strconv.ParseInt(token, 10, 64)
	if err != nil || id <= 0 {
		return nil, connectErrorInvalid("invalid grant name %q", req.Msg.GetName())
	}
	deleted, err := s.store.DeleteIamGrant(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "failed to delete the iam grant")
	}
	if !deleted {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("the iam grant %q does not exist", req.Msg.GetName()))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// parseIamGrantResource maps the API's scope name to the type and id the grant
// ledger stores. The first API supports an instance or one database; a scope the
// picker cannot name is refused rather than stored as an unreachable grant.
func parseIamGrantResource(resourceName string) (string, string, error) {
	resourceName = strings.TrimSpace(resourceName)
	if resourceName == "" {
		return "", "", connectErrorInvalid("a scope resource is required")
	}
	if strings.HasPrefix(resourceName, common.InstanceNamePrefix) && strings.Contains(resourceName, "/databases/") {
		instanceID, databaseName, err := common.GetInstanceDatabaseID(resourceName)
		if err != nil {
			return "", "", connectErrorInvalid("invalid scope %q", resourceName)
		}
		return resource.TypeDatabase.String(), common.BuildMetaGUID(instanceID, databaseName), nil
	}
	instanceID, err := common.GetInstanceID(resourceName)
	if err != nil {
		return "", "", connectErrorInvalid(
			"invalid scope %q; use instances/{instance} or instances/{instance}/databases/{database}", resourceName)
	}
	return resource.TypeInstance.String(), instanceID, nil
}

// validateIamGrantResource proves the scope names a live resource, so a typo is
// rejected instead of stored as a grant that can never match.
func (s *IamService) validateIamGrantResource(ctx context.Context, resourceType, resourceID string) error {
	switch resourceType {
	case resource.TypeInstance.String():
		instance, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &resourceID})
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the instance"))
		}
		if instance == nil || instance.Deleted {
			return connectErrorInvalid("the instance %q does not exist", resourceID)
		}
	case resource.TypeDatabase.String():
		parts := common.SplitMetaGUID(resourceID)
		if len(parts) != 2 {
			return connectErrorInvalid("invalid database scope")
		}
		instanceID, databaseName := parts[0], parts[1]
		database, err := s.store.GetDatabase(ctx, &store.FindDatabaseMessage{
			InstanceID:   &instanceID,
			DatabaseName: &databaseName,
		})
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the database"))
		}
		if database == nil || database.Deleted {
			return connectErrorInvalid("the database %q does not exist", resourceID)
		}
	default:
		return connectErrorInvalid("unsupported scope type %q", resourceType)
	}
	return nil
}

// formatIamGrantResource renders a stored scope as the API's resource name.
func formatIamGrantResource(grant *store.IamGrantMessage) string {
	switch grant.ResourceType {
	case resource.TypeInstance.String():
		return common.FormatInstance(grant.ResourceID)
	case resource.TypeDatabase.String():
		parts := common.SplitMetaGUID(grant.ResourceID)
		if len(parts) == 2 {
			return common.FormatDatabase(parts[0], parts[1])
		}
		return grant.ResourceID
	default:
		return grant.ResourceID
	}
}

// convertIamGrant renders one stored grant.
func convertIamGrant(grant *store.IamGrantMessage) *v1pb.IamGrant {
	if grant == nil {
		return nil
	}
	view := &v1pb.IamGrant{
		Name:       iamGrantPrefix + strconv.FormatInt(grant.ID, 10),
		Resource:   formatIamGrantResource(grant),
		Role:       grant.Role,
		Member:     grant.Member,
		CreateTime: timestamppb.New(grant.CreatedAt),
	}
	if grant.Condition != "" {
		view.Condition = &exprpb.Expr{Expression: grant.Condition}
	}
	if grant.CreatedBy != nil {
		view.CreatedBy = common.FormatUserUID(int(*grant.CreatedBy))
	}
	return view
}
