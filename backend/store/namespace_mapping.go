package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// NamespaceMappingMessage is the store representation of a namespace mapping.
type NamespaceMappingMessage struct {
	ID                 int64
	Namespace          string
	InstanceResourceID string
	DatabaseName       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// FindNamespaceMappingMessage is the query filter for namespace mappings.
type FindNamespaceMappingMessage struct {
	ID        *int64
	Namespace *string
}

// CreateNamespaceMapping creates a new namespace mapping.
func (s *Store) CreateNamespaceMapping(ctx context.Context, msg *NamespaceMappingMessage) (*NamespaceMappingMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	var result NamespaceMappingMessage
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO namespace_mapping (namespace, instance_resource_id, database_name)
		VALUES ($1, $2, $3)
		RETURNING id, namespace, instance_resource_id, database_name, created_at, updated_at
	`, msg.Namespace, msg.InstanceResourceID, msg.DatabaseName).Scan(
		&result.ID, &result.Namespace, &result.InstanceResourceID, &result.DatabaseName,
		&result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		return nil, errors.Wrap(err, "failed to create namespace mapping")
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}
	return &result, nil
}

// GetNamespaceMapping returns a single namespace mapping.
func (s *Store) GetNamespaceMapping(ctx context.Context, find *FindNamespaceMappingMessage) (*NamespaceMappingMessage, error) {
	list, err := s.ListNamespaceMapping(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// ListNamespaceMapping returns namespace mappings matching the filter.
func (s *Store) ListNamespaceMapping(ctx context.Context, find *FindNamespaceMappingMessage) ([]*NamespaceMappingMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if find.ID != nil {
		where, args = append(where, fmt.Sprintf("id = $%d", len(args)+1)), append(args, *find.ID)
	}
	if find.Namespace != nil {
		where, args = append(where, fmt.Sprintf("namespace = $%d", len(args)+1)), append(args, *find.Namespace)
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, namespace, instance_resource_id, database_name, created_at, updated_at
		FROM namespace_mapping
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY id ASC
	`, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query namespace mappings")
	}
	defer rows.Close()

	var result []*NamespaceMappingMessage
	for rows.Next() {
		var msg NamespaceMappingMessage
		if err := rows.Scan(
			&msg.ID, &msg.Namespace, &msg.InstanceResourceID, &msg.DatabaseName,
			&msg.CreatedAt, &msg.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan namespace mapping")
		}
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}

// UpdateNamespaceMapping updates an existing namespace mapping.
func (s *Store) UpdateNamespaceMapping(ctx context.Context, id int64, msg *NamespaceMappingMessage) (*NamespaceMappingMessage, error) {
	sets, args := []string{}, []any{}

	if msg.Namespace != "" {
		sets, args = append(sets, fmt.Sprintf("namespace = $%d", len(args)+1)), append(args, msg.Namespace)
	}
	if msg.InstanceResourceID != "" {
		sets, args = append(sets, fmt.Sprintf("instance_resource_id = $%d", len(args)+1)), append(args, msg.InstanceResourceID)
	}
	sets, args = append(sets, fmt.Sprintf("database_name = $%d", len(args)+1)), append(args, msg.DatabaseName)
	sets = append(sets, "updated_at = NOW()")

	args = append(args, id)

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	var result NamespaceMappingMessage
	if err := tx.QueryRowContext(ctx, `
		UPDATE namespace_mapping
		SET `+strings.Join(sets, ", ")+`
		WHERE id = $`+fmt.Sprintf("%d", len(args))+`
		RETURNING id, namespace, instance_resource_id, database_name, created_at, updated_at
	`, args...).Scan(
		&result.ID, &result.Namespace, &result.InstanceResourceID, &result.DatabaseName,
		&result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.Errorf("namespace mapping %d not found", id)
		}
		return nil, errors.Wrap(err, "failed to update namespace mapping")
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}
	return &result, nil
}

// DeleteNamespaceMapping deletes a namespace mapping by ID.
func (s *Store) DeleteNamespaceMapping(ctx context.Context, id int64) error {
	result, err := s.GetDB().ExecContext(ctx, `DELETE FROM namespace_mapping WHERE id = $1`, id)
	if err != nil {
		return errors.Wrap(err, "failed to delete namespace mapping")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if n == 0 {
		return errors.Errorf("namespace mapping %d not found", id)
	}
	return nil
}
