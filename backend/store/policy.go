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

// generateEtag generates etag for the given body.
func generateEtag(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixMilli())
}

func (s *Store) GetWorkspaceIamPolicy(ctx context.Context) (*IamPolicyMessage, error) {
	resourceType := storepb.Policy_WORKSPACE
	return s.getIamPolicy(ctx, &FindPolicyMessage{
		ResourceType: &resourceType,
	})
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

	roleMap := map[string]bool{}
	for _, role := range patch.Roles {
		roleMap[role] = true
	}

	for _, binding := range workspaceIamPolicy.Bindings {
		index := slices.Index(binding.Members, patch.Member)
		if !roleMap[binding.Role] {
			if index >= 0 {
				binding.Members = slices.Delete(binding.Members, index, index+1)
			}
		} else {
			if index < 0 {
				binding.Members = append(binding.Members, patch.Member)
			}
		}

		delete(roleMap, binding.Role)
	}

	for role := range roleMap {
		workspaceIamPolicy.Bindings = append(workspaceIamPolicy.Bindings, &storepb.Binding{
			Role: role,
			Members: []string{
				patch.Member,
			},
		})
	}

	policyPayload, err := protojson.Marshal(workspaceIamPolicy)
	if err != nil {
		return err
	}

	if _, err := upsertPolicyImpl(ctx, txn, &PolicyMessage{
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
		if v, ok := s.policyCache.Get(getPolicyCacheKey(*find.ResourceType, *find.Resource, *find.Type)); ok {
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

func upsertPolicyImpl(ctx context.Context, txn *sql.Tx, create *PolicyMessage) (*PolicyMessage, error) {
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
		return nil, err
	}
	return create, nil
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
