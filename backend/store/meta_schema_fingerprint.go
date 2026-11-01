package store

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CreateMetaRegistrySchemaFingerprintMessage is a fingerprint row to write. The
// fingerprint itself is produced by CalcSchemaFingerprint; the store never
// derives it, so the write path and the metadata it describes cannot drift.
type CreateMetaRegistrySchemaFingerprintMessage struct {
	GUID        string
	ObjectType  storepb.MetaType
	Fingerprint []byte
}

// MetaRegistrySchemaSignal is what a data-quality check compares its saved
// snapshot with.
type MetaRegistrySchemaSignal struct {
	// Fingerprint is the object's current structure fingerprint. It is empty when
	// the object has no row yet: a database that has not been synced since the
	// fingerprint table landed, or an object type that carries no shape.
	Fingerprint []byte
	// LegacyDDLHash is the engine's DDL hash (meta_registry_resource_schema),
	// written only by the engines whose DDL is captured. A snapshot saved before
	// fingerprints existed carries that hash, so a match here also means
	// "unchanged": the compatibility branch keeps an upgrade from reporting every
	// pre-existing StarRocks/Doris check as a schema change.
	LegacyDDLHash []byte
}

// buildUpsertMetaRegistrySchemaFingerprintQuery writes the current fingerprint of
// each object. The hash guard keeps the periodic full refresh from rewriting rows
// whose shape did not change, so an unchanged object produces no dead tuple and
// keeps its updated_at.
func buildUpsertMetaRegistrySchemaFingerprintQuery() string {
	return `
		INSERT INTO meta_registry_schema_fingerprint (
			guid,
			object_type,
			fingerprint
		) SELECT * FROM UNNEST ($1::text[], $2::int[], $3::bytea[])
		ON CONFLICT (guid, object_type) DO UPDATE SET
			fingerprint = EXCLUDED.fingerprint,
			updated_at = now()
		WHERE meta_registry_schema_fingerprint.fingerprint IS DISTINCT FROM EXCLUDED.fingerprint
	`
}

// buildDeleteMetaRegistrySchemaFingerprintByKeyQuery deletes by key. The
// predicate pairs guid with object_type: `guid = ANY($1) AND object_type =
// ANY($2)` also matches every cross combination, so it could delete unrequested
// resources.
func buildDeleteMetaRegistrySchemaFingerprintByKeyQuery() string {
	return `
		DELETE FROM meta_registry_schema_fingerprint
		WHERE (guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))
	`
}

// buildListMetaRegistrySchemaFingerprintQuery reads the stored fingerprints for a
// set of keys. It reads only the hash, never the metadata: the syncer already has
// the fresh snapshot and needs this result just to decide what to write.
func buildListMetaRegistrySchemaFingerprintQuery() string {
	return `
		SELECT guid, object_type, fingerprint
		FROM meta_registry_schema_fingerprint
		WHERE (guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))
	`
}

// buildListMetaRegistrySchemaSignalQuery reads the fingerprint and the legacy DDL
// hash of each requested object in one round trip. The request is the driving
// side of the join, so an object that has neither row still costs nothing and a
// missing row is distinguishable from an empty value.
func buildListMetaRegistrySchemaSignalQuery() string {
	return `
		SELECT requested.guid, requested.object_type, f.fingerprint, s.schema_hash
		FROM (SELECT * FROM unnest($1::text[], $2::int[])) AS requested(guid, object_type)
		LEFT JOIN meta_registry_schema_fingerprint f
			ON f.guid = requested.guid AND f.object_type::int = requested.object_type
		LEFT JOIN meta_registry_resource_schema s
			ON s.guid = requested.guid AND s.object_type::int = requested.object_type
	`
}

// metaGUIDKeyArrays splits keys into the parallel arrays the paired predicates
// take.
func metaGUIDKeyArrays(keys []MetaGUIDKey) (guids []string, objectTypes []storepb.MetaType) {
	guids = make([]string, 0, len(keys))
	objectTypes = make([]storepb.MetaType, 0, len(keys))
	for _, key := range keys {
		guids = append(guids, key.GUID)
		objectTypes = append(objectTypes, key.ObjectType)
	}
	return guids, objectTypes
}

// ListMetaRegistrySchemaFingerprints returns the stored fingerprint of every key
// that has a row. The schema syncer uses it to diff a fresh snapshot against what
// it wrote last time, so only real changes reach the database.
func (s *Store) ListMetaRegistrySchemaFingerprints(ctx context.Context, keys []MetaGUIDKey) (map[MetaGUIDKey][]byte, error) {
	result := make(map[MetaGUIDKey][]byte, len(keys))
	if len(keys) == 0 {
		return result, nil
	}

	guids, objectTypes := metaGUIDKeyArrays(keys)
	rows, err := s.GetDB().QueryContext(ctx, buildListMetaRegistrySchemaFingerprintQuery(),
		pq.Array(guids), pq.Array(objectTypes),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var guid string
		var objectType int32
		var fingerprint []byte
		if err := rows.Scan(&guid, &objectType, &fingerprint); err != nil {
			return nil, err
		}
		result[MetaGUIDKey{GUID: guid, ObjectType: storepb.MetaType(objectType)}] = fingerprint
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ListMetaRegistrySchemaSignals returns, per requested key, the current
// structure fingerprint and the legacy DDL hash. It is the data-quality read
// path's single query: one round trip for a page of targets instead of one per
// target.
func (s *Store) ListMetaRegistrySchemaSignals(ctx context.Context, keys []MetaGUIDKey) (map[MetaGUIDKey]MetaRegistrySchemaSignal, error) {
	result := make(map[MetaGUIDKey]MetaRegistrySchemaSignal, len(keys))
	if len(keys) == 0 {
		return result, nil
	}

	guids, objectTypes := metaGUIDKeyArrays(keys)
	rows, err := s.GetDB().QueryContext(ctx, buildListMetaRegistrySchemaSignalQuery(),
		pq.Array(guids), pq.Array(objectTypes),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var guid string
		var objectType int32
		var fingerprint, legacyDDLHash []byte
		if err := rows.Scan(&guid, &objectType, &fingerprint, &legacyDDLHash); err != nil {
			return nil, err
		}
		result[MetaGUIDKey{GUID: guid, ObjectType: storepb.MetaType(objectType)}] = MetaRegistrySchemaSignal{
			Fingerprint:   fingerprint,
			LegacyDDLHash: legacyDDLHash,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// BatchUpsertMetaRegistrySchemaFingerprints writes fingerprints inside the
// caller's transaction, so they commit with the metadata rows they describe.
func (*Store) BatchUpsertMetaRegistrySchemaFingerprints(ctx context.Context, tx *sql.Tx, creates []*CreateMetaRegistrySchemaFingerprintMessage) error {
	if len(creates) == 0 {
		return nil
	}

	guids := make([]string, 0, len(creates))
	objectTypes := make([]storepb.MetaType, 0, len(creates))
	fingerprints := make([][]byte, 0, len(creates))
	for _, create := range creates {
		guids = append(guids, create.GUID)
		objectTypes = append(objectTypes, create.ObjectType)
		fingerprints = append(fingerprints, create.Fingerprint)
	}

	_, err := tx.ExecContext(ctx, buildUpsertMetaRegistrySchemaFingerprintQuery(),
		pq.Array(guids), pq.Array(objectTypes), pq.Array(fingerprints))
	if err != nil {
		return err
	}
	return nil
}

// BatchDeleteMetaRegistrySchemaFingerprints removes fingerprints by key inside
// the caller's transaction. Deleting a key that has no row is a no-op.
func (*Store) BatchDeleteMetaRegistrySchemaFingerprints(ctx context.Context, tx *sql.Tx, keys []MetaGUIDKey) error {
	if len(keys) == 0 {
		return nil
	}

	guids, objectTypes := metaGUIDKeyArrays(keys)
	_, err := tx.ExecContext(ctx, buildDeleteMetaRegistrySchemaFingerprintByKeyQuery(),
		pq.Array(guids), pq.Array(objectTypes))
	if err != nil {
		return err
	}
	return nil
}
