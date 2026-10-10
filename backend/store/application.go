package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// applicationIDLength is the length of a generated application resource id. The id is
// public (it names the resource), so it is random rather than sequential.
const applicationIDLength = 12

// ApplicationMessage is the store representation of an application that reports its SQL.
type ApplicationMessage struct {
	ID            int64
	ApplicationID string
	Name          string
	Description   string
	Disabled      bool
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// FindApplicationMessage is the query filter for applications.
type FindApplicationMessage struct {
	ApplicationID *string
	Name          *string
	// IncludeDisabled keeps stopped applications in the result. The default excludes
	// them, because every caller but the management page is looking for one to use.
	IncludeDisabled bool
	Limit           *int
	Offset          *int
}

// UpdateApplicationMessage contains the mutable fields of an application.
type UpdateApplicationMessage struct {
	Name        *string
	Description *string
	Disabled    *bool
}

const applicationColumns = "id, application_id, name, description, disabled, created_by, created_at, updated_at"

func scanApplication(row interface{ Scan(...any) error }) (*ApplicationMessage, error) {
	msg := &ApplicationMessage{}
	if err := row.Scan(
		&msg.ID, &msg.ApplicationID, &msg.Name, &msg.Description,
		&msg.Disabled, &msg.CreatedBy, &msg.CreatedAt, &msg.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return msg, nil
}

// CreateApplication registers an application. The caller supplies no identifier: the
// store generates one, so every path that creates an application gets the same shape.
func (s *Store) CreateApplication(ctx context.Context, name, description, createdBy string) (*ApplicationMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, common.Errorf(common.Invalid, "application name is required")
	}
	applicationID, err := common.RandomString(applicationIDLength)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate an application id")
	}

	msg, err := scanApplication(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO application (application_id, name, description, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING `+applicationColumns,
		applicationID, name, strings.TrimSpace(description), strings.TrimSpace(createdBy)))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "an application named %q already exists", name)
		}
		return nil, errors.Wrap(err, "failed to create application")
	}
	return msg, nil
}

// GetApplication returns one application matching the filter, or nil.
func (s *Store) GetApplication(ctx context.Context, find *FindApplicationMessage) (*ApplicationMessage, error) {
	list, err := s.ListApplications(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, errors.Errorf("found %d applications with filter %+v, expect 1", len(list), find)
	}
	return list[0], nil
}

// ListApplications lists applications matching the filter.
func (s *Store) ListApplications(ctx context.Context, find *FindApplicationMessage) ([]*ApplicationMessage, error) {
	if find == nil {
		find = &FindApplicationMessage{}
	}
	where, args := []string{"TRUE"}, []any{}
	if find.ApplicationID != nil {
		where = append(where, fmt.Sprintf("application_id = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*find.ApplicationID))
	}
	if find.Name != nil {
		where = append(where, fmt.Sprintf("name = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*find.Name))
	}
	if !find.IncludeDisabled {
		where = append(where, "disabled = FALSE")
	}

	query := `SELECT ` + applicationColumns + ` FROM application WHERE ` + strings.Join(where, " AND ") + ` ORDER BY name ASC, id ASC`
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
		return nil, errors.Wrap(err, "failed to query applications")
	}
	defer rows.Close()

	var result []*ApplicationMessage
	for rows.Next() {
		msg, err := scanApplication(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan application")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for applications")
	}
	return result, nil
}

// UpdateApplication patches one application by its public id.
func (s *Store) UpdateApplication(ctx context.Context, applicationID string, patch *UpdateApplicationMessage) (*ApplicationMessage, error) {
	if patch == nil {
		return nil, common.Errorf(common.Invalid, "an update patch is required")
	}
	sets, args := []string{"updated_at = NOW()"}, []any{}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" {
			return nil, common.Errorf(common.Invalid, "application name is required")
		}
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)+1))
		args = append(args, name)
	}
	if patch.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*patch.Description))
	}
	if patch.Disabled != nil {
		sets = append(sets, fmt.Sprintf("disabled = $%d", len(args)+1))
		args = append(args, *patch.Disabled)
	}
	args = append(args, strings.TrimSpace(applicationID))

	msg, err := scanApplication(s.GetDB().QueryRowContext(ctx, `
		UPDATE application SET `+strings.Join(sets, ", ")+`
		WHERE application_id = $`+fmt.Sprintf("%d", len(args))+`
		RETURNING `+applicationColumns, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, common.Errorf(common.NotFound, "application %q not found", applicationID)
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "an application named %q already exists", strings.TrimSpace(derefString(patch.Name)))
		}
		return nil, errors.Wrap(err, "failed to update application")
	}
	return msg, nil
}

// DeleteApplication removes an application, everything reported with its keys, and every
// statement that no other application has reported.
//
// The order matters. Deleting the application cascades its keys and its observations; the
// statements that are left without any observation are then orphans, and their registry
// mirror, lineage and analysis version have to go with them or the graph would keep nodes
// nothing can reach.
func (s *Store) DeleteApplication(ctx context.Context, applicationID string) error {
	observedAt := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `DELETE FROM application WHERE application_id = $1`, strings.TrimSpace(applicationID))
	if err != nil {
		return errors.Wrap(err, "failed to delete application")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected for the application delete")
	}
	if affected == 0 {
		return common.Errorf(common.NotFound, "application %q not found", applicationID)
	}

	guids, err := deleteOrphanApplicationSQLs(ctx, tx)
	if err != nil {
		return err
	}
	for _, guid := range guids {
		if err := s.deleteApplicationSQLMetaRegistryTx(ctx, tx, guid, observedAt); err != nil {
			return err
		}
		if err := deleteColumnLineageByMetaTx(ctx, tx, guid); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}
	return nil
}

// deleteOrphanApplicationSQLs removes the statements left without any observation and
// returns their GUIDs, so the caller can clean what hangs off them.
func deleteOrphanApplicationSQLs(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, applicationSQLDeleteOrphans)
	if err != nil {
		return nil, errors.Wrap(err, "failed to delete orphan application SQL")
	}
	defer rows.Close()

	var guids []string
	for rows.Next() {
		var guid string
		if err := rows.Scan(&guid); err != nil {
			return nil, errors.Wrap(err, "failed to scan an orphan application SQL guid")
		}
		guids = append(guids, guid)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for orphan application SQL")
	}
	return guids, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
