package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// ExternalDatasetMessage is the store representation of an external dataset.
type ExternalDatasetMessage struct {
	ID          int64
	GUID        string
	Namespace   string
	Name        string
	DatasetType string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FindExternalDatasetMessage is the query filter for external datasets.
type FindExternalDatasetMessage struct {
	GUID      *string
	Namespace *string
	Name      *string
}

// GetOrCreateExternalDataset returns an existing external dataset or creates a new one.
func (s *Store) GetOrCreateExternalDataset(ctx context.Context, namespace, name, datasetType string) (*ExternalDatasetMessage, error) {
	guid := fmt.Sprintf("external:%s:%s", namespace, name)

	existing, err := s.GetExternalDataset(ctx, &FindExternalDatasetMessage{GUID: &guid})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.DatasetType == datasetType {
			// The row already is what the resolver would write. Every event used
			// to rewrite it, which made ingestion write-amplifying.
			return existing, nil
		}
		return s.updateExternalDatasetType(ctx, guid, datasetType)
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	var msg ExternalDatasetMessage
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO external_dataset (guid, namespace, name, dataset_type)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (guid) DO UPDATE SET updated_at = NOW()
		RETURNING id, guid, namespace, name, dataset_type, created_at, updated_at
	`, guid, namespace, name, datasetType).Scan(
		&msg.ID, &msg.GUID, &msg.Namespace, &msg.Name, &msg.DatasetType,
		&msg.CreatedAt, &msg.UpdatedAt,
	); err != nil {
		return nil, errors.Wrap(err, "failed to upsert external dataset")
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}
	return &msg, nil
}

// updateExternalDatasetType records a dataset type the resolver knows better than
// the row that already exists.
func (s *Store) updateExternalDatasetType(ctx context.Context, guid, datasetType string) (*ExternalDatasetMessage, error) {
	var msg ExternalDatasetMessage
	if err := s.GetDB().QueryRowContext(ctx, `
		UPDATE external_dataset
		SET dataset_type = $2, updated_at = NOW()
		WHERE guid = $1
		RETURNING id, guid, namespace, name, dataset_type, created_at, updated_at
	`, guid, datasetType).Scan(
		&msg.ID, &msg.GUID, &msg.Namespace, &msg.Name, &msg.DatasetType,
		&msg.CreatedAt, &msg.UpdatedAt,
	); err != nil {
		return nil, errors.Wrap(err, "failed to update external dataset type")
	}
	return &msg, nil
}

// GetExternalDataset returns a single external dataset matching the filter.
func (s *Store) GetExternalDataset(ctx context.Context, find *FindExternalDatasetMessage) (*ExternalDatasetMessage, error) {
	list, err := s.ListExternalDataset(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, common.Errorf(common.Conflict, "found %d external datasets with filter %+v, expect 1", len(list), find)
	}
	return list[0], nil
}

// ListExternalDataset returns external datasets matching the filter.
func (s *Store) ListExternalDataset(ctx context.Context, find *FindExternalDatasetMessage) ([]*ExternalDatasetMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if find.GUID != nil {
		where, args = append(where, fmt.Sprintf("guid = $%d", len(args)+1)), append(args, *find.GUID)
	}
	if find.Namespace != nil {
		where, args = append(where, fmt.Sprintf("namespace = $%d", len(args)+1)), append(args, *find.Namespace)
	}
	if find.Name != nil {
		where, args = append(where, fmt.Sprintf("name = $%d", len(args)+1)), append(args, *find.Name)
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, guid, namespace, name, dataset_type, created_at, updated_at
		FROM external_dataset
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY id ASC
	`, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query external datasets")
	}
	defer rows.Close()

	var result []*ExternalDatasetMessage
	for rows.Next() {
		var msg ExternalDatasetMessage
		if err := rows.Scan(
			&msg.ID, &msg.GUID, &msg.Namespace, &msg.Name, &msg.DatasetType,
			&msg.CreatedAt, &msg.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan external dataset")
		}
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}

// FindExternalDatasetByGUIDs returns external datasets matching the given GUIDs.
func (s *Store) FindExternalDatasetByGUIDs(ctx context.Context, guids []string) ([]*ExternalDatasetMessage, error) {
	if len(guids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(guids))
	args := make([]any, len(guids))
	for i, g := range guids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = g
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, guid, namespace, name, dataset_type, created_at, updated_at
		FROM external_dataset
		WHERE guid IN (`+strings.Join(placeholders, ", ")+`)
		ORDER BY id ASC
	`, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query external datasets by guids")
	}
	defer rows.Close()

	var result []*ExternalDatasetMessage
	for rows.Next() {
		var msg ExternalDatasetMessage
		if err := rows.Scan(
			&msg.ID, &msg.GUID, &msg.Namespace, &msg.Name, &msg.DatasetType,
			&msg.CreatedAt, &msg.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan external dataset")
		}
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}
