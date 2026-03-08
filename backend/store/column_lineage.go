package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// ColumnLineage is a single column-level lineage edge.
type ColumnLineage struct {
	ID             int64
	MetaGUID       string
	MetaType       storepb.MetaType
	SourceGUID     string
	SourceColumn   string
	TargetColumn   string
	RelationType   model.RelationType
	Transformation []model.Transformation
	UpdatedAt      time.Time
}

// FindColumnLineageMessage is the query filter for column lineage.
type FindColumnLineageMessage struct {
	MetaGUID     *string
	MetaType     *storepb.MetaType
	SourceGUID   *string
	SourceColumn *string
	TargetColumn *string
	Limit        *int
	Offset       *int
}

// ColumnLineageVersion tracks the analysis state per object.
type ColumnLineageVersion struct {
	MetaGUID     string
	MetaType     storepb.MetaType
	MetaHash     []byte
	AnalyzedAt   time.Time
	ErrorMessage *string
}

// BatchReplaceColumnLineage deletes any existing lineage edges for the given
// object and inserts the new ones, all within a single transaction.
func (s *Store) BatchReplaceColumnLineage(ctx context.Context, metaGUID string, metaType storepb.MetaType, lineages []*ColumnLineage) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM column_lineage WHERE meta_guid = $1 AND meta_type = $2`,
		metaGUID, metaType,
	); err != nil {
		return err
	}

	if len(lineages) > 0 {
		if err := insertColumnLineage(ctx, tx, metaGUID, metaType, lineages); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func insertColumnLineage(ctx context.Context, tx *sql.Tx, metaGUID string, metaType storepb.MetaType, lineages []*ColumnLineage) error {
	metaGUIDs := make([]string, len(lineages))
	metaTypes := make([]storepb.MetaType, len(lineages))
	sourceGUIDs := make([]string, len(lineages))
	sourceColumns := make([]string, len(lineages))
	targetColumns := make([]string, len(lineages))
	relationTypes := make([]int32, len(lineages))
	transformations := make([]string, len(lineages))

	for i, l := range lineages {
		metaGUIDs[i] = metaGUID
		metaTypes[i] = metaType
		sourceGUIDs[i] = l.SourceGUID
		sourceColumns[i] = l.SourceColumn
		targetColumns[i] = l.TargetColumn
		relationTypes[i] = int32(l.RelationType)
		b, err := json.Marshal(l.Transformation)
		if err != nil {
			return errors.Wrap(err, "failed to marshal transformation")
		}
		transformations[i] = string(b)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO column_lineage (meta_guid, meta_type, source_guid, source_column, target_column, relation_type, transformation)
		SELECT * FROM UNNEST($1::text[], $2::int[], $3::text[], $4::text[], $5::text[], $6::int[], $7::jsonb[])
	`,
		pq.Array(metaGUIDs),
		pq.Array(metaTypes),
		pq.Array(sourceGUIDs),
		pq.Array(sourceColumns),
		pq.Array(targetColumns),
		pq.Array(relationTypes),
		pq.Array(transformations),
	)
	return err
}

// ListColumnLineage returns column lineage edges matching the given filter.
func (s *Store) ListColumnLineage(ctx context.Context, find *FindColumnLineageMessage) ([]*ColumnLineage, error) {
	where, args := []string{"TRUE"}, []any{}

	if v := find.MetaGUID; v != nil {
		where = append(where, fmt.Sprintf("meta_guid = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.MetaType; v != nil {
		where = append(where, fmt.Sprintf("meta_type = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.SourceGUID; v != nil {
		where = append(where, fmt.Sprintf("source_guid = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.SourceColumn; v != nil {
		where = append(where, fmt.Sprintf("source_column = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.TargetColumn; v != nil {
		where = append(where, fmt.Sprintf("target_column = $%d", len(args)+1))
		args = append(args, *v)
	}

	query := fmt.Sprintf(`
		SELECT id, meta_guid, meta_type, source_guid, source_column, target_column, relation_type, transformation, updated_at
		FROM column_lineage
		WHERE %s
		ORDER BY id`, strings.Join(where, " AND "))

	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*ColumnLineage
	for rows.Next() {
		var cl ColumnLineage
		var transformRaw []byte
		if err := rows.Scan(
			&cl.ID,
			&cl.MetaGUID,
			&cl.MetaType,
			&cl.SourceGUID,
			&cl.SourceColumn,
			&cl.TargetColumn,
			&cl.RelationType,
			&transformRaw,
			&cl.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(transformRaw) > 0 {
			if err := json.Unmarshal(transformRaw, &cl.Transformation); err != nil {
				return nil, errors.Wrap(err, "failed to unmarshal transformation")
			}
		}
		result = append(result, &cl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// UpsertColumnLineageVersion inserts or updates the analysis version record for an object.
func (s *Store) UpsertColumnLineageVersion(ctx context.Context, v *ColumnLineageVersion) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO column_lineage_version (meta_guid, meta_type, meta_hash, analyzed_at, error_message)
		VALUES ($1, $2, $3, NOW(), $4)
		ON CONFLICT (meta_guid, meta_type) DO UPDATE SET
			meta_hash = EXCLUDED.meta_hash,
			analyzed_at = EXCLUDED.analyzed_at,
			error_message = EXCLUDED.error_message
	`, v.MetaGUID, v.MetaType, v.MetaHash, v.ErrorMessage)
	return err
}

// GetColumnLineageVersion returns the analysis version record for an object, or nil if not found.
func (s *Store) GetColumnLineageVersion(ctx context.Context, metaGUID string, metaType storepb.MetaType) (*ColumnLineageVersion, error) {
	row := s.GetDB().QueryRowContext(ctx, `
		SELECT meta_guid, meta_type, meta_hash, analyzed_at, error_message
		FROM column_lineage_version
		WHERE meta_guid = $1 AND meta_type = $2
	`, metaGUID, metaType)

	var v ColumnLineageVersion
	err := row.Scan(&v.MetaGUID, &v.MetaType, &v.MetaHash, &v.AnalyzedAt, &v.ErrorMessage)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// DeleteColumnLineageByMeta removes all lineage data for the given object.
func (s *Store) DeleteColumnLineageByMeta(ctx context.Context, metaGUID string, metaType storepb.MetaType) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM column_lineage WHERE meta_guid = $1 AND meta_type = $2`,
		metaGUID, metaType,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM column_lineage_version WHERE meta_guid = $1 AND meta_type = $2`,
		metaGUID, metaType,
	); err != nil {
		return err
	}

	return tx.Commit()
}
