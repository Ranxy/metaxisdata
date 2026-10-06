package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type IamPolicyMessage struct {
	Policy *storepb.IamPolicy
	Etag   string
}

// ErrPolicyEtagMismatch reports a Set whose etag does not match the stored
// policy. Callers map it to connect.CodeAborted so the client re-fetches.
var ErrPolicyEtagMismatch = errors.New("iam policy etag mismatch")

// allUsersBaselineRole is the only role the allUsers pseudo-member may be bound
// to: the workspaceMember baseline every authenticated principal already holds
// implicitly.
const allUsersBaselineRole = common.RolePrefix + common.WorkspaceMember

// CheckAllUsersBinding enforces the allUsers invariant on a complete workspace
// policy. The v1 write path (validateIamPolicy) calls it, and so do the store
// writers below, so the invariant holds for every writer instead of only for
// the RPC that happens to validate first.
//
// allUsers matches every authenticated principal, including everyone who
// registers after the write, and self-signup is open by default. Binding it to
// any other role would hand that role to every future sign-up, so it may only
// carry the implicit baseline; and that one binding must survive a full
// replace instead of being quietly dropped. Custom roles stay available; they
// are granted to explicit users and groups.
func CheckAllUsersBinding(policy *storepb.IamPolicy) error {
	if err := checkAllUsersRole(policy); err != nil {
		return err
	}
	for _, binding := range policy.GetBindings() {
		if binding.GetRole() == allUsersBaselineRole && slices.Contains(binding.GetMembers(), common.AllUsers) {
			return nil
		}
	}
	return common.Errorf(common.Invalid, "policy must keep its %s binding to %s", allUsersBaselineRole, common.AllUsers)
}

// checkAllUsersRole enforces the half of the invariant that every writer can
// afford: allUsers may only be bound to the baseline. The other half — the
// baseline binding must still be there — is a property of a whole policy, so it
// is checked by CheckAllUsersBinding on the full-replace path; the incremental
// patch below may legitimately run against a policy that predates the binding.
func checkAllUsersRole(policy *storepb.IamPolicy) error {
	for _, binding := range policy.GetBindings() {
		if !slices.Contains(binding.GetMembers(), common.AllUsers) {
			continue
		}
		if binding.GetRole() != allUsersBaselineRole {
			return common.Errorf(common.Invalid,
				"%s may only be bound to %s, not to %s: it matches every authenticated principal, including future sign-ups",
				common.AllUsers, allUsersBaselineRole, binding.GetRole())
		}
	}
	return nil
}

// generateEtag generates etag for the given body.
func generateEtag(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixMilli())
}

// etagMismatch reports whether a Set must be rejected. An empty provided etag
// skips the check (a first write, or a client that did not read first); any
// other value must equal the stored policy's etag.
func etagMismatch(current, provided string) bool {
	return provided != "" && provided != current
}

func (s *Store) GetWorkspaceIamPolicy(ctx context.Context) (*IamPolicyMessage, error) {
	resourceType := storepb.Policy_WORKSPACE
	return s.getIamPolicy(ctx, &FindPolicyMessage{
		ResourceType: &resourceType,
	})
}

// SetWorkspaceIamPolicy replaces the workspace IAM policy whole. etag guards
// optimistic concurrency: an empty etag skips the check (a first write), any
// other value must equal the etag returned by GetWorkspaceIamPolicy. The
// allUsers invariant is enforced here as well as at the API boundary, so a
// caller that skips validateIamPolicy cannot store a policy that violates it.
func (s *Store) SetWorkspaceIamPolicy(ctx context.Context, policy *storepb.IamPolicy, etag string) (*IamPolicyMessage, error) {
	if err := CheckAllUsersBinding(policy); err != nil {
		return nil, err
	}
	// Read the current policy with a strong read: a cached etag could be stale
	// after a write through another connection, which would let this Set
	// overwrite that change even though the caller read an older policy.
	s.policyCache.Remove(getPolicyCacheKey(storepb.Policy_WORKSPACE, "", storepb.Policy_IAM))
	existing, err := s.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if etagMismatch(existing.Etag, etag) {
		return nil, ErrPolicyEtagMismatch
	}

	payload, err := protojson.Marshal(policy)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal workspace iam policy")
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := upsertPolicyImpl(ctx, tx, &PolicyMessage{
		ResourceType:      storepb.Policy_WORKSPACE,
		Payload:           string(payload),
		Type:              storepb.Policy_IAM,
		InheritFromParent: false,
		// Enforce cannot be false while creating a policy.
		Enforce: true,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.policyCache.Remove(getPolicyCacheKey(storepb.Policy_WORKSPACE, "", storepb.Policy_IAM))

	return s.GetWorkspaceIamPolicy(ctx)
}

type PatchIamPolicyMessage struct {
	Member string
	Roles  []string
}

// PatchWorkspaceIamPolicy will set or remove the member for the workspace role.
func (s *Store) PatchWorkspaceIamPolicy(ctx context.Context, patch *PatchIamPolicyMessage) (*IamPolicyMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := s.patchWorkspaceIamPolicyImpl(ctx, tx, patch); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.policyCache.Remove(getPolicyCacheKey(storepb.Policy_WORKSPACE, "", storepb.Policy_IAM))

	return s.GetWorkspaceIamPolicy(ctx)
}

// patchWorkspaceIamPolicyImpl sets or removes the member for the workspace role
// within the caller's transaction.
func (s *Store) patchWorkspaceIamPolicyImpl(ctx context.Context, txn *sql.Tx, patch *PatchIamPolicyMessage) error {
	resourceType := storepb.Policy_WORKSPACE
	pType := storepb.Policy_IAM
	policies, err := s.listPolicyImpl(ctx, txn, &FindPolicyMessage{
		ResourceType: &resourceType,
		Type:         &pType,
		ShowAll:      true,
	})
	if err != nil {
		return err
	}
	if len(policies) > 1 {
		return errors.Errorf("found %d workspace iam policies, expect at most 1", len(policies))
	}

	workspaceIamPolicy := &storepb.IamPolicy{}
	if len(policies) == 1 && policies[0].Payload != "" {
		if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(policies[0].Payload), workspaceIamPolicy); err != nil {
			return errors.Wrapf(err, "failed to unmarshal workspace iam policy")
		}
	}

	patchIamPolicyBindings(workspaceIamPolicy, patch.Member, patch.Roles)

	// The patch can drop members and append bindings, so the result is checked
	// as a whole: a caller passing allUsers would otherwise move it off the
	// baseline (see checkAllUsersRole).
	if err := checkAllUsersRole(workspaceIamPolicy); err != nil {
		return err
	}

	policyPayload, err := protojson.Marshal(workspaceIamPolicy)
	if err != nil {
		return err
	}

	if err := upsertPolicyImpl(ctx, txn, &PolicyMessage{
		ResourceType:      storepb.Policy_WORKSPACE,
		Payload:           string(policyPayload),
		Type:              storepb.Policy_IAM,
		InheritFromParent: false,
		// Enforce cannot be false while creating a policy.
		Enforce: true,
	}); err != nil {
		return err
	}

	return nil
}

// patchIamPolicyBindings grants member every role in roles and revokes it from
// every other binding. Missing roles are appended in request order so the stored
// payload is deterministic.
func patchIamPolicyBindings(policy *storepb.IamPolicy, member string, roles []string) {
	pending := map[string]bool{}
	for _, role := range roles {
		pending[role] = true
	}

	for _, binding := range policy.Bindings {
		index := slices.Index(binding.Members, member)
		if !pending[binding.Role] {
			if index >= 0 {
				binding.Members = slices.Delete(binding.Members, index, index+1)
			}
		} else if index < 0 {
			binding.Members = append(binding.Members, member)
		}
		delete(pending, binding.Role)
	}

	for _, role := range roles {
		if !pending[role] {
			continue
		}
		delete(pending, role)
		policy.Bindings = append(policy.Bindings, &storepb.Binding{
			Role:    role,
			Members: []string{member},
		})
	}
}

func (s *Store) getIamPolicy(ctx context.Context, find *FindPolicyMessage) (*IamPolicyMessage, error) {
	pType := storepb.Policy_IAM
	find.Type = &pType
	policy, err := s.GetPolicy(ctx, find)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return &IamPolicyMessage{
			Policy: &storepb.IamPolicy{},
		}, nil
	}

	p := &storepb.IamPolicy{}
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(policy.Payload), p); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal iam policy for %v", policy.Resource)
	}

	return &IamPolicyMessage{
		Policy: p,
		Etag:   generateEtag(policy.UpdatedAt),
	}, nil
}

// PolicyMessage is the mssage for policy.
type PolicyMessage struct {
	Resource          string
	ResourceType      storepb.Policy_Resource
	Payload           string
	InheritFromParent bool
	Type              storepb.Policy_Type
	Enforce           bool

	UpdatedAt time.Time
}

// FindPolicyMessage is the message for finding policies.
type FindPolicyMessage struct {
	ResourceType *storepb.Policy_Resource
	Resource     *string
	Type         *storepb.Policy_Type
	// ShowAll will show all policies regardless of the enforce status.
	ShowAll bool
}

// GetPolicy gets a policy.
func (s *Store) GetPolicy(ctx context.Context, find *FindPolicyMessage) (*PolicyMessage, error) {
	if find.ResourceType != nil && find.Resource != nil && find.Type != nil {
		if v, ok := s.policyCache.Get(getPolicyCacheKey(*find.ResourceType, *find.Resource, *find.Type)); ok && !s.cacheDisabled {
			return v, nil
		}
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// We will always return the resource regardless of its deleted state.
	find.ShowAll = true
	policies, err := s.listPolicyImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		// Cache the policy for not found as well to reduce the look up latency.
		if find.ResourceType != nil && find.Resource != nil && find.Type != nil {
			s.policyCache.Add(getPolicyCacheKey(*find.ResourceType, *find.Resource, *find.Type), nil)
		}
		return nil, nil
	}
	if len(policies) > 1 {
		return nil, &common.Error{Code: common.Conflict, Err: errors.Errorf("found %d policies with filter %+v, expect 1", len(policies), find)}
	}
	policy := policies[0]

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.policyCache.Add(getPolicyCacheKey(policy.ResourceType, policy.Resource, policy.Type), policy)

	return policy, nil
}

func upsertPolicyImpl(ctx context.Context, txn *sql.Tx, create *PolicyMessage) error {
	create.UpdatedAt = time.Now()
	if _, err := txn.ExecContext(ctx, `
		INSERT INTO policy (
			resource_type,
			resource,
			inherit_from_parent,
			type,
			payload,
			enforce,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT(resource_type, resource, type) DO UPDATE SET
			inherit_from_parent = EXCLUDED.inherit_from_parent,
			payload = EXCLUDED.payload,
			enforce = EXCLUDED.enforce,
			updated_at = EXCLUDED.updated_at
		`,
		create.ResourceType.String(),
		create.Resource,
		create.InheritFromParent,
		create.Type.String(),
		create.Payload,
		create.Enforce,
		create.UpdatedAt,
	); err != nil {
		return err
	}
	return nil
}

func (*Store) listPolicyImpl(ctx context.Context, txn *sql.Tx, find *FindPolicyMessage) ([]*PolicyMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.ResourceType; v != nil {
		where, args = append(where, fmt.Sprintf("resource_type = $%d", len(args)+1)), append(args, v.String())
	}
	if v := find.Resource; v != nil {
		where, args = append(where, fmt.Sprintf("resource = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.Type; v != nil {
		where, args = append(where, fmt.Sprintf("type = $%d", len(args)+1)), append(args, v.String())
	}
	if !find.ShowAll {
		where, args = append(where, fmt.Sprintf("enforce = $%d", len(args)+1)), append(args, true)
	}

	rows, err := txn.QueryContext(ctx, `
		SELECT
			updated_at,
			resource_type,
			resource,
			inherit_from_parent,
			type,
			payload,
			enforce
		FROM policy
		WHERE `+strings.Join(where, " AND "),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policyList []*PolicyMessage
	for rows.Next() {
		var policyMessage PolicyMessage
		var resourceTypeString, typeString string
		if err := rows.Scan(
			&policyMessage.UpdatedAt,
			&resourceTypeString,
			&policyMessage.Resource,
			&policyMessage.InheritFromParent,
			&typeString,
			&policyMessage.Payload,
			&policyMessage.Enforce,
		); err != nil {
			return nil, err
		}
		resourceTypeValue, ok := storepb.Policy_Resource_value[resourceTypeString]
		if !ok {
			return nil, errors.Errorf("invalid policy resource type string: %s", resourceTypeString)
		}
		policyMessage.ResourceType = storepb.Policy_Resource(resourceTypeValue)
		value, ok := storepb.Policy_Type_value[typeString]
		if !ok {
			return nil, errors.Errorf("invalid policy type string: %s", typeString)
		}
		policyMessage.Type = storepb.Policy_Type(value)
		policyList = append(policyList, &policyMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return policyList, nil
}
