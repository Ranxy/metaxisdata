package iam

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// PermissionChecker is the workspace-wide check the IAM manager implements. The
// scoped authorizer calls it first and adds grants and derived relations on top.
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
