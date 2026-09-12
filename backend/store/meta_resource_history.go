package store

import (
	"bytes"
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func buildMetaRegistryHistoryMutations(existing map[MetaGUIDKey]*MetaRegistryHistory, creates []*CreateMetaRegistryResourceMessage) ([]*MetaRegistryHistory, []*CreateMetaRegistryResourceMessage) {
	toClose := make([]*MetaRegistryHistory, 0, len(creates))
	toOpen := make([]*CreateMetaRegistryResourceMessage, 0, len(creates))
	for _, create := range creates {
		existingHistory, ok := existing[create.GUIDKey()]
		if !ok {
			toOpen = append(toOpen, create)
			continue
		}
		if bytes.Equal(existingHistory.MetaHash, create.MetaHash) {
			continue
		}
		toClose = append(toClose, existingHistory)
		toOpen = append(toOpen, create)
	}
	return toClose, toOpen
}

func (*Store) listOpenMetaRegistryHistoryByKey(ctx context.Context, tx *sql.Tx, keys []MetaGUIDKey) (map[MetaGUIDKey]*MetaRegistryHistory, error) {
	result := make(map[MetaGUIDKey]*MetaRegistryHistory)
	if len(keys) == 0 {
		return result, nil
	}

	guids := make([]string, 0, len(keys))
	objectTypes := make([]storepb.MetaType, 0, len(keys))
	for _, key := range keys {
		guids = append(guids, key.GUID)
		objectTypes = append(objectTypes, key.ObjectType)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, guid, object_type, meta_hash, valid_from, valid_to
		FROM meta_registry_resource_history
		WHERE valid_to IS NULL
			AND guid = ANY($1)
			AND object_type = ANY($2)
	`, pq.Array(guids), pq.Array(objectTypes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var history MetaRegistryHistory
		var validTo sql.NullTime
		if err := rows.Scan(&history.ID, &history.GUID, &history.ObjectType, &history.MetaHash, &history.ValidFrom, &validTo); err != nil {
			return nil, err
		}
		if validTo.Valid {
			t := validTo.Time
			history.ValidTo = &t
		}
		result[MetaGUIDKey{GUID: history.GUID, ObjectType: history.ObjectType}] = &history
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (*Store) closeOpenMetaRegistryHistory(ctx context.Context, tx *sql.Tx, list []*MetaRegistryHistory, observedAt time.Time) error {
	for _, history := range list {
		if _, err := tx.ExecContext(ctx, `
			UPDATE meta_registry_resource_history
			SET valid_to = $3
			WHERE guid = $1 AND object_type = $2 AND valid_to IS NULL
		`, history.GUID, history.ObjectType, observedAt); err != nil {
			return err
		}
	}
	return nil
}

func (*Store) insertMetaRegistryHistory(ctx context.Context, tx *sql.Tx, creates []*CreateMetaRegistryResourceMessage, observedAt time.Time) error {
	if len(creates) == 0 {
		return nil
	}

	guids := make([]string, 0, len(creates))
	objectTypes := make([]storepb.MetaType, 0, len(creates))
	metadata := make([]string, 0, len(creates))
	metaHashes := make([][]byte, 0, len(creates))
	validFrom := make([]time.Time, 0, len(creates))
	for _, create := range creates {
		guids = append(guids, create.GUID)
		objectTypes = append(objectTypes, create.ObjectType)
		metadata = append(metadata, string(create.MetadataBytes))
		metaHashes = append(metaHashes, create.MetaHash)
		validFrom = append(validFrom, observedAt)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO meta_registry_resource_history (
			guid,
			object_type,
			metadata,
			meta_hash,
			valid_from
		) SELECT * FROM UNNEST ($1::text[], $2::int[], $3::jsonb[], $4::bytea[], $5::timestamptz[])
	`, pq.Array(guids), pq.Array(objectTypes), pq.Array(metadata), pq.Array(metaHashes), pq.Array(validFrom)); err != nil {
		return err
	}

	return nil
}

func (s *Store) upsertMetaRegistryHistory(ctx context.Context, tx *sql.Tx, creates []*CreateMetaRegistryResourceMessage, observedAt time.Time) error {
	keys := make([]MetaGUIDKey, 0, len(creates))
	for _, create := range creates {
		keys = append(keys, create.GUIDKey())
	}

	existing, err := s.listOpenMetaRegistryHistoryByKey(ctx, tx, keys)
	if err != nil {
		return err
	}

	toClose, toOpen := buildMetaRegistryHistoryMutations(existing, creates)
	if err := s.closeOpenMetaRegistryHistory(ctx, tx, toClose, observedAt); err != nil {
		return err
	}
	return s.insertMetaRegistryHistory(ctx, tx, toOpen, observedAt)
}
