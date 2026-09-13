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

// buildOpenMetaRegistryHistoryByKeyQuery returns the lookup for the open
// history row of each requested key. The predicate pairs guid with object_type:
// `guid = ANY($1) AND object_type = ANY($2)` also matches every cross
// combination, so it can return unrequested resources.
func buildOpenMetaRegistryHistoryByKeyQuery() string {
	return `
		SELECT id, guid, object_type, meta_hash, valid_from, valid_to
		FROM meta_registry_resource_history
		WHERE valid_to IS NULL
			AND (guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))
	`
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

	rows, err := tx.QueryContext(ctx, buildOpenMetaRegistryHistoryByKeyQuery(), pq.Array(guids), pq.Array(objectTypes))
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

// buildCloseOpenMetaRegistryHistoryQuery closes the open history row of each
// given key. The key pairs guid with object_type, so one statement replaces the
// per-key round trip without closing a cross combination.
func buildCloseOpenMetaRegistryHistoryQuery() string {
	return `
		UPDATE meta_registry_resource_history AS history
		SET valid_to = $3
		FROM (SELECT * FROM unnest($1::text[], $2::int[]) AS key(guid, object_type)) AS keys
		WHERE history.guid = keys.guid
			AND history.object_type = keys.object_type
			AND history.valid_to IS NULL
	`
}

func (*Store) closeOpenMetaRegistryHistory(ctx context.Context, tx *sql.Tx, list []*MetaRegistryHistory, observedAt time.Time) error {
	if len(list) == 0 {
		return nil
	}

	guids := make([]string, 0, len(list))
	objectTypes := make([]storepb.MetaType, 0, len(list))
	for _, history := range list {
		guids = append(guids, history.GUID)
		objectTypes = append(objectTypes, history.ObjectType)
	}

	if _, err := tx.ExecContext(ctx, buildCloseOpenMetaRegistryHistoryQuery(), pq.Array(guids), pq.Array(objectTypes), observedAt); err != nil {
		return err
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
