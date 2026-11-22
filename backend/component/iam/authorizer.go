package iam

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// PermissionChecker is the workspace-wide check the IAM manager implements. It is
// the only decision the compatibility authorizer has until scoped grants land.
type PermissionChecker interface {
	CheckPermission(ctx context.Context, perm permission.Permission, user *store.UserMessage) (bool, error)
}

// Authorizer answers the resource-scoped authorization question: may this
// principal perform this action on this resource, and which scopes does it hold
// the action in for a list that must filter at the query.
type Authorizer interface {
	Check(ctx context.Context, principal *store.UserMessage, perm permission.Permission, ref resource.Ref) (bool, error)
	Scopes(ctx context.Context, principal *store.UserMessage, perm permission.Permission) (resource.ScopeSet, error)
}

// WorkspaceAuthorizer is the compatibility implementation: the workspace-wide
// permission decides every resource, and a principal that holds the permission
// holds it in every scope. It changes no decision until the scoped-grant
// authorizer replaces it.
type WorkspaceAuthorizer struct {
	checker PermissionChecker
}

// NewWorkspaceAuthorizer wraps a workspace-wide checker.
func NewWorkspaceAuthorizer(checker PermissionChecker) *WorkspaceAuthorizer {
	return &WorkspaceAuthorizer{checker: checker}
}

// Check reports whether the principal holds the permission. The resource is not
// consulted: the compatibility model has no resource dimension yet.
func (a *WorkspaceAuthorizer) Check(ctx context.Context, principal *store.UserMessage, perm permission.Permission, _ resource.Ref) (bool, error) {
	if principal == nil {
		return false, nil
	}
	return a.checker.CheckPermission(ctx, perm, principal)
}

// Scopes returns All when the principal holds the permission, and an empty set
// otherwise, so a list keeps today's unfiltered behavior for an allowed caller.
func (a *WorkspaceAuthorizer) Scopes(ctx context.Context, principal *store.UserMessage, perm permission.Permission) (resource.ScopeSet, error) {
	allowed, err := a.Check(ctx, principal, perm, resource.WorkspaceRef())
	if err != nil || !allowed {
		return resource.ScopeSet{}, err
	}
	return resource.ScopeSet{All: true}, nil
}

var _ Authorizer = (*WorkspaceAuthorizer)(nil)

// ScopedPermissionChecker adapts Authorizer to the flat PermissionChecker the
// in-process tool pipeline uses. It answers the capability question — does the
// principal hold the permission in at least one scope? — because a tool
// declaration carries no resource; the product then scopes the tool body to one
// of those scopes (the quality agent's session target is one of its databases).
type ScopedPermissionChecker struct {
	authorizer Authorizer
}

// NewScopedPermissionChecker wraps a resource authorizer as the flat checker.
func NewScopedPermissionChecker(authorizer Authorizer) *ScopedPermissionChecker {
	return &ScopedPermissionChecker{authorizer: authorizer}
}

// CheckPermission reports whether the principal holds perm in any scope.
func (c *ScopedPermissionChecker) CheckPermission(ctx context.Context, perm permission.Permission, principal *store.UserMessage) (bool, error) {
	if principal == nil {
		return false, nil
	}
	scopes, err := c.authorizer.Scopes(ctx, principal, perm)
	if err != nil {
		return false, err
	}
	return !scopes.IsEmpty(), nil
}

// CheckResource answers the permission on one resource. The in-process agent's
// target session binds its tool checks to the session target through this.
func (c *ScopedPermissionChecker) CheckResource(ctx context.Context, perm permission.Permission, principal *store.UserMessage, ref resource.Ref) (bool, error) {
	if principal == nil {
		return false, nil
	}
	return c.authorizer.Check(ctx, principal, perm, ref)
}

var _ PermissionChecker = (*ScopedPermissionChecker)(nil)
