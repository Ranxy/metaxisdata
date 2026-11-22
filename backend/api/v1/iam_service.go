package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/component/iam"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// IamService exposes the workspace IAM policy, the scoped grants, and the
// self-service permission check. Get/Set are gated by metaxisdata.iam.getPolicy
// / setPolicy; Set validates every binding before writing and refuses a policy
// that would leave the workspace without an active admin. CheckPermissions is
// unannotated: the caller's identity is the only input and the handler
// authenticates before answering.
type IamService struct {
	v1connect.UnimplementedIamServiceHandler
	store      *store.Store
	authorizer iam.Authorizer
}

// NewIamService creates a new IamService. The authorizer answers the
// self-service permission check; nil keeps the policy management RPCs usable in
// focused tests while CheckPermissions fails closed.
func NewIamService(stores *store.Store, authorizer iam.Authorizer) *IamService {
	return &IamService{store: stores, authorizer: authorizer}
}

func (s *IamService) GetWorkspaceIamPolicy(ctx context.Context, _ *connect.Request[v1pb.GetWorkspaceIamPolicyRequest]) (*connect.Response[v1pb.IamPolicyView], error) {
	policy, err := s.store.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace iam policy"))
	}
	return connect.NewResponse(toIamPolicyView(policy)), nil
}

func (s *IamService) SetWorkspaceIamPolicy(ctx context.Context, request *connect.Request[v1pb.SetWorkspaceIamPolicyRequest]) (*connect.Response[v1pb.IamPolicyView], error) {
	policy := convertToStoreIamPolicy(request.Msg.GetPolicy())
	if err := validateIamPolicy(ctx, s.store, policy); err != nil {
		return nil, err
	}
	// The workspace must keep at least one active end-user admin after the
	// write, bound by a condition that holds at check time and is not merely
	// time-boxed; otherwise a single Set could permanently lock everyone out.
	ok, err := hasActiveWorkspaceAdmin(ctx, s.store, policy, 0)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check workspace admin"))
	}
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workspace must keep at least one active admin whose binding has no condition"))
	}

	updated, err := s.store.SetWorkspaceIamPolicy(ctx, policy, request.Msg.GetEtag())
	if err != nil {
		if errors.Is(err, store.ErrPolicyEtagMismatch) {
			return nil, connect.NewError(connect.CodeAborted, errors.New("iam policy changed since last read; re-fetch and retry"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to set workspace iam policy"))
	}
	return connect.NewResponse(toIamPolicyView(updated)), nil
}

// toIamPolicyView wraps a stored policy plus its etag into the v1 view.
func toIamPolicyView(p *store.IamPolicyMessage) *v1pb.IamPolicyView {
	if p == nil || p.Policy == nil {
		return &v1pb.IamPolicyView{Policy: &v1pb.IamPolicy{}, Etag: ""}
	}
	return &v1pb.IamPolicyView{Policy: convertToV1IamPolicy(p.Policy), Etag: p.Etag}
}

func convertToV1IamPolicy(p *storepb.IamPolicy) *v1pb.IamPolicy {
	if p == nil {
		return &v1pb.IamPolicy{}
	}
	out := &v1pb.IamPolicy{Bindings: make([]*v1pb.Binding, 0, len(p.GetBindings()))}
	for _, binding := range p.GetBindings() {
		out.Bindings = append(out.Bindings, &v1pb.Binding{
			Role:      binding.GetRole(),
			Members:   binding.GetMembers(),
			Condition: binding.GetCondition(),
		})
	}
	return out
}

func convertToStoreIamPolicy(p *v1pb.IamPolicy) *storepb.IamPolicy {
	if p == nil {
		return &storepb.IamPolicy{}
	}
	out := &storepb.IamPolicy{Bindings: make([]*storepb.Binding, 0, len(p.GetBindings()))}
	for _, binding := range p.GetBindings() {
		out.Bindings = append(out.Bindings, &storepb.Binding{
			Role:      binding.GetRole(),
			Members:   binding.GetMembers(),
			Condition: binding.GetCondition(),
		})
	}
	return out
}

// validateIamPolicy checks every binding before a write: roles must resolve to
// a known role, conditions must be evaluable from the attributes this
// deployment binds, and members must name a real, active principal. A binding
// to a missing role or principal would silently never match, so it is rejected
// rather than stored.
//
// It also pins the allUsers invariant, which a single full replace could
// otherwise widen for every principal who ever signs up. The decision lives in
// the store so that every writer, not just this RPC, enforces it.
func validateIamPolicy(ctx context.Context, stores *store.Store, policy *storepb.IamPolicy) error {
	if err := store.CheckAllUsersBinding(policy); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	for _, binding := range policy.GetBindings() {
		resourceID, err := common.GetRoleID(binding.GetRole())
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid role %q", binding.GetRole()))
		}
		role, err := stores.GetRoleSnapshot(ctx, resourceID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to resolve role %q", binding.GetRole()))
		}
		if role == nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown role %q", binding.GetRole()))
		}
		// Metaxisdata only binds request.time, so a condition referencing other
		// attributes cannot be evaluated and would be dropped at check time.
		// Reject it at write time instead of storing a binding that never
		// applies.
		if _, err := common.EvalBindingCondition(binding.GetCondition().GetExpression(), time.Now()); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid condition for role %q", binding.GetRole()))
		}
		for _, member := range binding.GetMembers() {
			if err := validateIamMember(ctx, stores, member); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateIamMember reports whether a binding member names a real, active
// principal: allUsers, a non-deleted user, or an existing group. Which role
// allUsers may be bound to is checked by store.CheckAllUsersBinding.
func validateIamMember(ctx context.Context, stores *store.Store, member string) error {
	switch {
	case member == common.AllUsers:
		return nil
	case strings.HasPrefix(member, common.UserNamePrefix):
		userID, err := common.GetUserID(member)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid member %q", member))
		}
		user, err := stores.GetUserByID(ctx, userID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to look up member %q", member))
		}
		if user == nil || user.MemberDeleted {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("member %q does not exist or is deleted", member))
		}
	case strings.HasPrefix(member, common.GroupPrefix):
		group, err := stores.GetGroupByName(ctx, member)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to look up member %q", member))
		}
		if group == nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("member %q does not exist", member))
		}
	default:
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid member %q (want users/{uid}, groups/{email}, or allUsers)", member))
	}
	return nil
}

// CheckPermissions answers the caller's own permissions on one resource. It is
// self-service: the handler authenticates, so the request can name a resource
// the caller is not allowed to read, and the answer is still only about the
// caller.
func (s *IamService) CheckPermissions(ctx context.Context, req *connect.Request[v1pb.CheckPermissionsRequest]) (*connect.Response[v1pb.CheckPermissionsResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	if s.authorizer == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("the resource authorizer is not configured"))
	}
	resourceType, ok := resource.ParseType(strings.TrimSpace(req.Msg.GetResourceType()))
	if !ok {
		return nil, connectErrorInvalid("invalid resource type %q", req.Msg.GetResourceType())
	}
	if len(req.Msg.GetPermissions()) > maxCheckPermissions {
		return nil, connectErrorInvalid("at most %d permissions may be checked at once", maxCheckPermissions)
	}
	ref := resource.NewRef(resourceType, normalizeCheckPermissionsID(resourceType, strings.TrimSpace(req.Msg.GetResourceId())))
	if !ref.Valid() {
		return nil, connectErrorInvalid("a valid resource id is required")
	}
	allowed := make([]bool, 0, len(req.Msg.GetPermissions()))
	checked := make(map[permission.Permission]bool)
	for _, raw := range req.Msg.GetPermissions() {
		perm := permission.Permission(raw)
		if !permission.Exist(perm) {
			return nil, connectErrorInvalid("unknown permission %q", raw)
		}
		if ok, seen := checked[perm]; seen {
			allowed = append(allowed, ok)
			continue
		}
		// A workspace-only permission has no resource answer: a scoped grant
		// cannot make it true here even if a stored row names it.
		if !permission.Scopeable(perm) {
			checked[perm] = false
			allowed = append(allowed, false)
			continue
		}
		ok, err := s.authorizer.Check(ctx, user, perm, ref)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the permission"))
		}
		checked[perm] = ok
		allowed = append(allowed, ok)
	}
	return connect.NewResponse(&v1pb.CheckPermissionsResponse{Allowed: allowed}), nil
}

const maxCheckPermissions = 64

// normalizeCheckPermissionsID accepts the resource name or the raw id for the
// two name-addressed resource kinds. Metadata resources keep their GUID.
func normalizeCheckPermissionsID(resourceType resource.Type, id string) string {
	switch resourceType {
	case resource.TypeInstance:
		if parsed, err := common.GetInstanceID(id); err == nil {
			return parsed
		}
	case resource.TypeDatabase:
		if instance, database, err := common.GetInstanceDatabaseID(id); err == nil {
			return common.BuildMetaGUID(instance, database)
		}
	default:
	}
	return id
}
