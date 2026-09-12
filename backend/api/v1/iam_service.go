package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// IamService exposes the workspace IAM policy. Get reads the whole policy; Set
// replaces it whole, guarded by the etag returned by Get. Both RPCs are gated
// by the ACL interceptor with metaxisdata.iam.getPolicy / setPolicy. Set
// validates every binding before writing and refuses a policy that would leave
// the workspace without an active admin.
type IamService struct {
	v1connect.UnimplementedIamServiceHandler
	store *store.Store
}

// NewIamService creates a new IamService.
func NewIamService(stores *store.Store) *IamService {
	return &IamService{store: stores}
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
	// write; otherwise a single Set could permanently lock everyone out.
	ok, err := hasActiveWorkspaceAdmin(ctx, s.store, policy, 0)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check workspace admin"))
	}
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workspace must have at least one active admin"))
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
func validateIamPolicy(ctx context.Context, stores *store.Store, policy *storepb.IamPolicy) error {
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
// principal: allUsers, a non-deleted user, or an existing group.
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
