package iam

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// GrantReader is the scoped-grant, role and group subset the authorizer reads.
// *store.Store is the production implementation; the interface keeps the
// engine testable without a database.
type GrantReader interface {
	ListIamGrants(ctx context.Context, find *store.FindIamGrantMessage) ([]*store.IamGrantMessage, error)
	GetRoleSnapshot(ctx context.Context, resourceID string) (*store.RoleMessage, error)
	GetGroup(ctx context.Context, email string) (*store.GroupMessage, error)
}

// ScopedAuthorizer answers a resource check from the workspace policy first and
// then from scoped grants. The workspace permission is the fast path and keeps
// the current behavior; a grant at the resource or one of its ancestors adds
// authority for a principal the workspace policy does not cover.
type ScopedAuthorizer struct {
	workspace PermissionChecker
	grants    GrantReader
}

// NewScopedAuthorizer builds the grant-backed authorizer over the workspace
// checker. The workspace policy is the root: a principal it allows needs no
// grant lookup.
func NewScopedAuthorizer(workspace PermissionChecker, grants GrantReader) *ScopedAuthorizer {
	return &ScopedAuthorizer{workspace: workspace, grants: grants}
}

// Check reports whether the principal holds perm on ref or one of its ancestors.
func (a *ScopedAuthorizer) Check(ctx context.Context, principal *store.UserMessage, perm permission.Permission, ref resource.Ref) (bool, error) {
	if principal == nil {
		return false, nil
	}
	allowed, err := a.workspace.CheckPermission(ctx, perm, principal)
	if err != nil {
		return false, err
	}
	if allowed {
		return true, nil
	}
	chain := chainFor(ref)
	grants, err := a.grants.ListIamGrants(ctx, &store.FindIamGrantMessage{
		Scopes:             scopePairs(chain),
		Members:            directMembers(principal),
		IncludeGroupGrants: true,
	})
	if err != nil {
		return false, err
	}
	return a.anyGrantAllows(ctx, principal, grants, perm)
}

// Scopes returns every scope the principal holds perm in. A workspace-wide
// permission is All; otherwise the grants whose role contains perm contribute
// their scopes. An empty set means the principal cannot perform the action
// anywhere and a list must return nothing.
func (a *ScopedAuthorizer) Scopes(ctx context.Context, principal *store.UserMessage, perm permission.Permission) (resource.ScopeSet, error) {
	if principal == nil {
		return resource.ScopeSet{}, nil
	}
	allowed, err := a.workspace.CheckPermission(ctx, perm, principal)
	if err != nil {
		return resource.ScopeSet{}, err
	}
	if allowed {
		return resource.ScopeSet{All: true}, nil
	}
	grants, err := a.grants.ListIamGrants(ctx, &store.FindIamGrantMessage{
		Members:            directMembers(principal),
		IncludeGroupGrants: true,
	})
	if err != nil {
		return resource.ScopeSet{}, err
	}
	var set resource.ScopeSet
	for _, grant := range grants {
		applies, err := a.grantApplies(ctx, principal, grant, perm)
		if err != nil {
			return resource.ScopeSet{}, err
		}
		if !applies {
			continue
		}
		resourceType, ok := resource.ParseType(grant.ResourceType)
		if !ok {
			continue
		}
		set.Add(resource.NewScope(resourceType, grant.ResourceID))
	}
	return set.Normalize(), nil
}

// anyGrantAllows reports whether one grant applies to the principal and grants
// the permission.
func (a *ScopedAuthorizer) anyGrantAllows(ctx context.Context, principal *store.UserMessage, grants []*store.IamGrantMessage, perm permission.Permission) (bool, error) {
	for _, grant := range grants {
		applies, err := a.grantApplies(ctx, principal, grant, perm)
		if err != nil {
			return false, err
		}
		if applies {
			return true, nil
		}
	}
	return false, nil
}

// grantApplies evaluates one grant: its condition holds, its member names the
// principal (directly, through allUsers, or through a group), and its role
// contains the permission. An unevaluable condition fails closed.
func (a *ScopedAuthorizer) grantApplies(ctx context.Context, principal *store.UserMessage, grant *store.IamGrantMessage, perm permission.Permission) (bool, error) {
	if grant == nil {
		return false, nil
	}
	if strings.TrimSpace(grant.Condition) != "" {
		holds, err := common.EvalBindingCondition(grant.Condition, time.Now())
		if err != nil {
			slog.Warn("failed to evaluate a scoped grant condition",
				slog.String("role", grant.Role), slog.Any("error", err))
			return false, nil //nolint:nilerr // an unevaluable condition fails closed
		}
		if !holds {
			return false, nil
		}
	}
	member, err := a.grantMemberMatches(ctx, principal, grant.Member)
	if err != nil {
		return false, err
	}
	if !member {
		return false, nil
	}
	return a.roleHasPermission(ctx, grant.Role, perm)
}

// grantMemberMatches expands the three member forms a grant can name. Groups are
// resolved through the cached group read, so a group edit takes effect without
// rewriting every grant.
func (a *ScopedAuthorizer) grantMemberMatches(ctx context.Context, principal *store.UserMessage, member string) (bool, error) {
	switch {
	case member == common.AllUsers:
		return true, nil
	case member == common.FormatUserUID(principal.ID):
		return true, nil
	case strings.HasPrefix(member, common.GroupPrefix):
		email, err := common.GetGroupEmail(member)
		if err != nil {
			return false, nil //nolint:nilerr // a malformed grant member fails closed
		}
		group, err := a.grants.GetGroup(ctx, email)
		if err != nil {
			return false, err
		}
		if group == nil {
			return false, nil
		}
		target := common.FormatUserUID(principal.ID)
		for _, groupMember := range group.Payload.GetMembers() {
			if groupMember.GetMember() == target {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, nil
	}
}

// roleHasPermission resolves a role id to its permission set. An unknown role or
// an unreadable snapshot fails closed.
func (a *ScopedAuthorizer) roleHasPermission(ctx context.Context, role string, perm permission.Permission) (bool, error) {
	resourceID, err := common.GetRoleID(role)
	if err != nil {
		return false, nil //nolint:nilerr // a malformed role name fails closed
	}
	snapshot, err := a.grants.GetRoleSnapshot(ctx, resourceID)
	if err != nil {
		return false, err
	}
	if snapshot == nil {
		return false, nil
	}
	return snapshot.Permissions[perm], nil
}

// chainFor returns the resource and its ancestors. A metadata GUID carries its
// chain; a non-metadata resource is its own scope and needs a resolver once a
// derived resource is checked directly.
func chainFor(ref resource.Ref) []resource.Ref {
	if resource.IsMetadataType(ref.Type) {
		if chain := resource.MetadataAncestors(ref); chain != nil {
			return chain
		}
	}
	return []resource.Ref{ref}
}

// scopePairs renders a chain as grant scopes.
func scopePairs(chain []resource.Ref) []store.IamGrantScope {
	scopes := make([]store.IamGrantScope, 0, len(chain))
	for _, ref := range chain {
		scopes = append(scopes, store.IamGrantScope{ResourceType: ref.Type.String(), ResourceID: ref.ID})
	}
	return scopes
}

// directMembers is the member set that names the principal without group
// expansion. Group grants are matched separately by IncludeGroupGrants.
func directMembers(principal *store.UserMessage) []string {
	return []string{common.FormatUserUID(principal.ID), common.AllUsers}
}

var _ Authorizer = (*ScopedAuthorizer)(nil)
