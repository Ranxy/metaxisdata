package iam

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// GrantReader is the IAM state the authorizer reads: scoped grants, the derived
// agent relations, role snapshots, groups, and the process-local generation that
// invalidates the per-principal snapshot.
type GrantReader interface {
	ListIamGrants(ctx context.Context, find *store.FindIamGrantMessage) ([]*store.IamGrantMessage, error)
	GetRoleSnapshot(ctx context.Context, resourceID string) (*store.RoleMessage, error)
	GetGroup(ctx context.Context, email string) (*store.GroupMessage, error)
	ListAgentGrantRelations(ctx context.Context, principalID int) ([]*store.AgentGrantRelation, error)
	AuthorizationGeneration() uint64
}

// principalSnapshot is the cached IAM state for one principal: the grants whose
// member names it (directly, through allUsers, or through a group), the derived
// agent relations, and the resolved role permissions. It is keyed on the store's
// authorization generation, so a grant/role/group/agent write rebuilds it.
type principalSnapshot struct {
	generation      uint64
	grants          []*store.IamGrantMessage
	relations       []*store.AgentGrantRelation
	rolePermissions map[string]map[permission.Permission]bool
}

// ScopedAuthorizer answers a resource check from the workspace policy first and
// then from scoped grants and the agent ownership relations. The workspace
// permission is the fast path and keeps the current behavior; the snapshot cache
// makes the second path cheap.
type ScopedAuthorizer struct {
	workspace PermissionChecker
	grants    GrantReader

	mu       sync.RWMutex
	cache    map[int]*principalSnapshot
	cacheGen uint64
}

// NewScopedAuthorizer builds the grant-backed authorizer over the workspace
// checker. The workspace policy is the root: a principal it allows needs no
// relationship lookup.
func NewScopedAuthorizer(workspace PermissionChecker, grants GrantReader) *ScopedAuthorizer {
	return &ScopedAuthorizer{workspace: workspace, grants: grants, cache: make(map[int]*principalSnapshot)}
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
	snapshot, err := a.snapshot(ctx, principal)
	if err != nil {
		return false, err
	}
	chain := chainKeys(ref)
	now := time.Now()
	for _, grant := range snapshot.grants {
		if !grantConditionHolds(grant, now) {
			continue
		}
		if !chain[grantScopeKey(grant.ResourceType, grant.ResourceID)] {
			continue
		}
		if snapshot.rolePermissions[grant.Role][perm] {
			return true, nil
		}
	}
	for _, relation := range snapshot.relations {
		scope, ok := relationScope(relation.DatabaseRef)
		if !ok {
			continue
		}
		if !chain[grantScopeKey(scope.Type.String(), scope.ID)] {
			continue
		}
		if snapshot.rolePermissions[relation.Role][perm] {
			return true, nil
		}
	}
	return false, nil
}

// Scopes returns every scope the principal holds perm in. A workspace-wide
// permission is All; otherwise the applicable grants and derived relations
// contribute their scopes. An empty set means the principal cannot perform the
// action anywhere and a list must return nothing.
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
	snapshot, err := a.snapshot(ctx, principal)
	if err != nil {
		return resource.ScopeSet{}, err
	}
	var set resource.ScopeSet
	now := time.Now()
	for _, grant := range snapshot.grants {
		if !grantConditionHolds(grant, now) {
			continue
		}
		if !snapshot.rolePermissions[grant.Role][perm] {
			continue
		}
		resourceType, ok := resource.ParseType(grant.ResourceType)
		if !ok {
			continue
		}
		set.Add(resource.NewScope(resourceType, grant.ResourceID))
	}
	for _, relation := range snapshot.relations {
		if !snapshot.rolePermissions[relation.Role][perm] {
			continue
		}
		scope, ok := relationScope(relation.DatabaseRef)
		if !ok {
			continue
		}
		set.Add(scope)
	}
	return set.Normalize(), nil
}

// snapshot returns the principal's cached IAM snapshot, rebuilding it when the
// store's generation has moved.
func (a *ScopedAuthorizer) snapshot(ctx context.Context, principal *store.UserMessage) (*principalSnapshot, error) {
	generation := a.grants.AuthorizationGeneration()
	a.mu.RLock()
	cached := a.cache[principal.ID]
	a.mu.RUnlock()
	if cached != nil && cached.generation == generation {
		return cached, nil
	}
	grants, err := a.grants.ListIamGrants(ctx, &store.FindIamGrantMessage{
		Members:            directMembers(principal),
		IncludeGroupGrants: true,
	})
	if err != nil {
		return nil, err
	}
	snapshot := &principalSnapshot{
		generation:      generation,
		rolePermissions: make(map[string]map[permission.Permission]bool),
	}
	for _, grant := range grants {
		member, err := a.grantMemberMatches(ctx, principal, grant.Member)
		if err != nil {
			return nil, err
		}
		if !member {
			continue
		}
		snapshot.grants = append(snapshot.grants, grant)
		if err := a.resolveRole(ctx, snapshot, grant.Role); err != nil {
			return nil, err
		}
	}
	relations, err := a.grants.ListAgentGrantRelations(ctx, principal.ID)
	if err != nil {
		return nil, err
	}
	snapshot.relations = relations
	for _, relation := range relations {
		if err := a.resolveRole(ctx, snapshot, relation.Role); err != nil {
			return nil, err
		}
	}
	a.mu.Lock()
	if a.cacheGen != generation {
		a.cache = make(map[int]*principalSnapshot)
		a.cacheGen = generation
	}
	a.cache[principal.ID] = snapshot
	a.mu.Unlock()
	return snapshot, nil
}

// resolveRole stores a role's permission set in the snapshot once. An unknown or
// malformed role grants nothing; an unreadable snapshot fails closed.
func (a *ScopedAuthorizer) resolveRole(ctx context.Context, snapshot *principalSnapshot, role string) error {
	if _, ok := snapshot.rolePermissions[role]; ok {
		return nil
	}
	resourceID, err := common.GetRoleID(role)
	if err != nil {
		return nil //nolint:nilerr // a malformed role name fails closed
	}
	roleSnapshot, err := a.grants.GetRoleSnapshot(ctx, resourceID)
	if err != nil {
		return err
	}
	if roleSnapshot != nil {
		snapshot.rolePermissions[role] = roleSnapshot.Permissions
	}
	return nil
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

// grantConditionHolds evaluates a grant's time condition. An unevaluable
// condition fails closed.
func grantConditionHolds(grant *store.IamGrantMessage, now time.Time) bool {
	if strings.TrimSpace(grant.Condition) == "" {
		return true
	}
	holds, err := common.EvalBindingCondition(grant.Condition, now)
	if err != nil {
		slog.Warn("failed to evaluate a scoped grant condition",
			slog.String("role", grant.Role), slog.Any("error", err))
		return false
	}
	return holds
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

// chainKeys renders a resource chain as lookup keys for grant scopes.
func chainKeys(ref resource.Ref) map[string]bool {
	chain := chainFor(ref)
	keys := make(map[string]bool, len(chain))
	for _, item := range chain {
		keys[grantScopeKey(item.Type.String(), item.ID)] = true
	}
	return keys
}

// grantScopeKey is the lookup key shared by grants, relations and the chain.
func grantScopeKey(resourceType, resourceID string) string {
	return resourceType + "\x00" + resourceID
}

// relationScope maps an agent database reference to the database scope a grant
// would use.
func relationScope(databaseRef string) (resource.Scope, bool) {
	instanceID, databaseName, err := common.GetInstanceDatabaseID(databaseRef)
	if err != nil {
		return resource.Scope{}, false
	}
	return resource.NewScope(resource.TypeDatabase, common.BuildMetaGUID(instanceID, databaseName)), true
}

// directMembers is the member set that names the principal without group
// expansion. Group grants are matched separately by IncludeGroupGrants.
func directMembers(principal *store.UserMessage) []string {
	return []string{common.FormatUserUID(principal.ID), common.AllUsers}
}

var _ Authorizer = (*ScopedAuthorizer)(nil)

// ScopedPermissions returns every permission the principal holds on any scope
// through a grant or a derived relation. It is what the current-user permission
// union adds to the workspace permissions, so the SPA can gate a page on a
// scoped grant the workspace policy does not include.
func (a *ScopedAuthorizer) ScopedPermissions(ctx context.Context, principal *store.UserMessage) ([]permission.Permission, error) {
	if principal == nil {
		return nil, nil
	}
	snapshot, err := a.snapshot(ctx, principal)
	if err != nil {
		return nil, err
	}
	set := map[permission.Permission]bool{}
	now := time.Now()
	for _, grant := range snapshot.grants {
		if !grantConditionHolds(grant, now) {
			continue
		}
		for perm := range snapshot.rolePermissions[grant.Role] {
			set[perm] = true
		}
	}
	for _, relation := range snapshot.relations {
		for perm := range snapshot.rolePermissions[relation.Role] {
			set[perm] = true
		}
	}
	out := make([]permission.Permission, 0, len(set))
	for perm := range set {
		out = append(out, perm)
	}
	slices.Sort(out)
	return out, nil
}
