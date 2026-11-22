package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// IamGrantMessage is one resource-scoped grant: a role bound to a member over a
// resource subtree. The workspace IAM policy is the root; a grant row is always
// additive to it.
type IamGrantMessage struct {
	ID           int64
	ResourceType string
	ResourceID   string
	Role         string
	Member       string
	Condition    string
	CreatedBy    *int32
	CreatedAt    time.Time
}

// FindIamGrantMessage filters grants. An empty but non-nil Scopes or Members
// slice matches nothing, so a caller that collected no scope cannot read every
// grant by accident.
type FindIamGrantMessage struct {
	ResourceType *string
	ResourceID   *string
	Members      []string
	// IncludeGroupGrants also matches group members. The store does not expand a
	// group; the caller checks the user's membership, because group expansion is
	// an identity concern rather than a grant query.
	IncludeGroupGrants bool
	Limit              *int
	Offset             *int
}

const iamGrantColumns = `id, resource_type, resource_id, role, member, condition, created_by, created_at`

// CreateIamGrant inserts one grant. The unique index makes a repeated grant a
// conflict rather than a duplicate row.
func (s *Store) CreateIamGrant(ctx context.Context, message *IamGrantMessage) (*IamGrantMessage, error) {
	if message == nil || message.ResourceType == "" || message.Role == "" || message.Member == "" {
		return nil, common.Errorf(common.Invalid, "a grant needs a resource type, a role and a member")
	}
	if message.Member == common.AllUsers {
		return nil, common.Errorf(common.Invalid, "allUsers is the workspace baseline; bind a group instead")
	}
	if message.ResourceType == "workspace" {
		if message.ResourceID != "" {
			return nil, common.Errorf(common.Invalid, "a workspace grant has no resource id")
		}
	} else if message.ResourceID == "" {
		return nil, common.Errorf(common.Invalid, "a grant on a resource needs a resource id")
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO iam_grant
			(resource_type, resource_id, role, member, condition, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+iamGrantColumns,
		message.ResourceType, message.ResourceID, message.Role, message.Member,
		message.Condition, message.CreatedBy, message.CreatedAt)
	created, err := scanIamGrant(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "the grant already exists")
		}
		return nil, errors.Wrap(err, "failed to create the iam grant")
	}
	s.bumpAuthorizationGeneration()
	return created, nil
}

// DeleteIamGrant removes one grant by id. It reports whether a row changed.
func (s *Store) DeleteIamGrant(ctx context.Context, id int64) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `DELETE FROM iam_grant WHERE id = $1`, id)
	if err != nil {
		return false, errors.Wrap(err, "failed to delete the iam grant")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to read the deleted grant count")
	}
	if rows > 0 {
		s.bumpAuthorizationGeneration()
	}
	return rows > 0, nil
}

// ListIamGrants reads the grants matching a filter. The authorizer lists the
// member's grants once per principal snapshot and matches the resource chain in
// memory, so no resource-chain query is needed.
func (s *Store) ListIamGrants(ctx context.Context, find *FindIamGrantMessage) ([]*IamGrantMessage, error) {
	if find == nil {
		find = &FindIamGrantMessage{}
	}
	where, args := buildIamGrantWhere(find)
	query := `SELECT ` + iamGrantColumns + ` FROM iam_grant WHERE ` +
		strings.Join(where, " AND ") + ` ORDER BY id`
	if find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the iam grants")
	}
	defer rows.Close()
	var grants []*IamGrantMessage
	for rows.Next() {
		grant, err := scanIamGrant(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan the iam grant")
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to list the iam grants")
	}
	return grants, nil
}

// GetIamGrant reads one grant by id; nil means absent.
func (s *Store) GetIamGrant(ctx context.Context, id int64) (*IamGrantMessage, error) {
	row := s.GetDB().QueryRowContext(ctx, `SELECT `+iamGrantColumns+` FROM iam_grant WHERE id = $1`, id)
	grant, err := scanIamGrant(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the iam grant")
	}
	return grant, nil
}

// buildIamGrantWhere turns a filter into predicates. A filter that names no
// scope or no member narrows to nothing rather than widening to every grant.
func buildIamGrantWhere(find *FindIamGrantMessage) ([]string, []any) {
	where := []string{"TRUE"}
	args := []any{}
	if v := find.ResourceType; v != nil {
		args = append(args, *v)
		where = append(where, fmt.Sprintf("resource_type = $%d", len(args)))
	}
	if v := find.ResourceID; v != nil {
		args = append(args, *v)
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)))
	}
	if find.Members != nil || find.IncludeGroupGrants {
		conditions := make([]string, 0, 2)
		if len(find.Members) > 0 {
			args = append(args, pq.Array(find.Members))
			conditions = append(conditions, fmt.Sprintf("member = ANY($%d)", len(args)))
		}
		if find.IncludeGroupGrants {
			conditions = append(conditions, "member LIKE 'groups/%'")
		}
		if len(conditions) == 0 {
			where = append(where, "FALSE")
		} else {
			where = append(where, "("+strings.Join(conditions, " OR ")+")")
		}
	}
	return where, args
}

func scanIamGrant(scanner interface{ Scan(dest ...any) error }) (*IamGrantMessage, error) {
	var (
		grant     IamGrantMessage
		createdBy sql.NullInt32
	)
	if err := scanner.Scan(&grant.ID, &grant.ResourceType, &grant.ResourceID, &grant.Role,
		&grant.Member, &grant.Condition, &createdBy, &grant.CreatedAt); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		grant.CreatedBy = &createdBy.Int32
	}
	return &grant, nil
}
