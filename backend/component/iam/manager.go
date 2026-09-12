// Package iam implements the metaxisdata permission-check engine.
//
// It resolves a caller's effective permission set from the IAM model: a
// permission is a fine-grained string (backend/common/permission), a role is a
// named bundle of permissions (backend/store/predefined_roles.go for built-in
// roles, the role table for custom roles), and the workspace IAM policy binds
// principals to roles (backend/store/policy.go). This mirrors laelia's
// backend/component/iam, adapted to metaxisdata's single-workspace model.
package iam

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/store"
	"github.com/Ranxy/metaxisdata/backend/utils"
)

// Store is the subset of the metadata store the engine reads. Narrowing it
// keeps permission resolution testable without a database.
type Store interface {
	utils.MemberStore

	GetWorkspaceIamPolicy(ctx context.Context) (*store.IamPolicyMessage, error)
	GetRoleSnapshot(ctx context.Context, resourceID string) (*store.RoleMessage, error)
}

// Manager resolves permissions against the IAM model.
type Manager struct {
	store Store
}

// NewManager builds an IAM manager backed by the given store.
func NewManager(stores *store.Store) *Manager {
	return &Manager{store: stores}
}

// CheckPermission reports whether the caller holds the permission.
//
// Every authenticated principal receives the roles/workspaceMember baseline
// (the implicit allUsers->workspaceMember binding of the single-workspace
// model); user callers additionally receive the permissions of every role they
// hold in the workspace IAM policy, directly, through a group, or via
// allUsers. The check short-circuits on the first granting set, so the common
// case costs one map lookup and no allocation.
func (m *Manager) CheckPermission(ctx context.Context, perm permission.Permission, user *store.UserMessage) (bool, error) {
	if user == nil {
		return false, nil
	}
	if baseline := store.GetPredefinedRole(store.WorkspaceMemberRole); baseline != nil && baseline.Permissions[perm] {
		return true, nil
	}

	workspacePolicy, err := m.store.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return false, err
	}
	for _, role := range utils.GetUserRolesInIamPolicy(ctx, m.store, user, workspacePolicy.Policy) {
		if rolePerms := m.rolePermissions(ctx, role); rolePerms[perm] {
			return true, nil
		}
	}
	return false, nil
}

// EffectivePermissions returns the caller's whole workspace permission set: the
// workspaceMember baseline unioned with the permissions of every role the user
// holds in the workspace IAM policy. Used by GetCurrentUser to populate
// User.permissions for frontend gating.
func (m *Manager) EffectivePermissions(ctx context.Context, user *store.UserMessage) ([]permission.Permission, error) {
	perms := map[permission.Permission]bool{}
	if baseline := store.GetPredefinedRole(store.WorkspaceMemberRole); baseline != nil {
		for p := range baseline.Permissions {
			perms[p] = true
		}
	}
	if user != nil {
		workspacePolicy, err := m.store.GetWorkspaceIamPolicy(ctx)
		if err != nil {
			return nil, err
		}
		for _, role := range utils.GetUserRolesInIamPolicy(ctx, m.store, user, workspacePolicy.Policy) {
			for p := range m.rolePermissions(ctx, role) {
				perms[p] = true
			}
		}
	}
	out := make([]permission.Permission, 0, len(perms))
	for p := range perms {
		out = append(out, p)
	}
	slices.Sort(out)
	return out, nil
}

// rolePermissions returns the permission set for a role named roles/{id},
// resolving predefined roles in-memory first and custom roles from the DB
// (cached) via GetRoleSnapshot. An unknown role resolves to no permissions; a
// DB error is logged and also treated as no permissions (fail-closed).
func (m *Manager) rolePermissions(ctx context.Context, role string) map[permission.Permission]bool {
	resourceID := strings.TrimPrefix(role, common.RolePrefix)
	roleMessage, err := m.store.GetRoleSnapshot(ctx, resourceID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to resolve role permissions",
			slog.String("role", role),
			slog.Any("err", err))
		return nil
	}
	if roleMessage == nil {
		return nil
	}
	return roleMessage.Permissions
}
