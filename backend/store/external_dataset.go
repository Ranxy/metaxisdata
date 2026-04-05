package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ExternalDatasetMessage is the store representation of an external dataset.
type ExternalDatasetMessage struct {
	ID           int64
	GUID         string
	Namespace    string
	Name         string
	DatasetType  string
	SchemaFields []*storepb.SchemaField
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
		return existing, nil
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
		RETURNING id, guid, namespace, name, dataset_type, schema_fields, created_at, updated_at
	`, guid, namespace, name, datasetType).Scan(
		&msg.ID, &msg.GUID, &msg.Namespace, &msg.Name, &msg.DatasetType,
		&schemaFieldsScanner{fields: &msg.SchemaFields},
		&msg.CreatedAt, &msg.UpdatedAt,
	); err != nil {
		return nil, errors.Wrap(err, "failed to upsert external dataset")
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
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
		SELECT id, guid, namespace, name, dataset_type, schema_fields, created_at, updated_at
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
			&schemaFieldsScanner{fields: &msg.SchemaFields},
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

// schemaFieldsScanner implements sql.Scanner for []*storepb.SchemaField JSON.
type schemaFieldsScanner struct {
	fields *[]*storepb.SchemaField
}

func (s *schemaFieldsScanner) Scan(src any) error {
	if src == nil {
		*s.fields = nil
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return errors.Errorf("unsupported type for schema_fields: %T", src)
	}

	type field struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description,omitempty"`
	}
	var fields []field
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	result := make([]*storepb.SchemaField, len(fields))
	for i, f := range fields {
		result[i] = &storepb.SchemaField{
			Name:        f.Name,
			Type:        f.Type,
			Description: f.Description,
		}
	}
	*s.fields = result
	return nil
}
