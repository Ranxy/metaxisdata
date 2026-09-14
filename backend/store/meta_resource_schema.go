package store

import (
	"context"
	"crypto/sha256"
	"database/sql"

	"github.com/lib/pq"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CreateMetaRegistrySchemaMessage is a DDL row to write.
type CreateMetaRegistrySchemaMessage struct {
	GUID       string
	ObjectType storepb.MetaType
	Schema     string
}

// buildUpsertMetaRegistrySchemaQuery writes the current DDL of each object. The
// hash guard keeps a full-fetch sync from rewriting rows whose DDL did not
// change, so an unchanged object produces no dead tuple and keeps its
// updated_at.
func buildUpsertMetaRegistrySchemaQuery() string {
	return `
		INSERT INTO meta_registry_resource_schema (
			guid,
			object_type,
			schema,
			schema_hash
		) SELECT * FROM UNNEST ($1::text[], $2::int[], $3::text[], $4::bytea[])
		ON CONFLICT (guid, object_type) DO UPDATE SET
			schema = EXCLUDED.schema,
			schema_hash = EXCLUDED.schema_hash,
			updated_at = now()
		WHERE meta_registry_resource_schema.schema_hash IS DISTINCT FROM EXCLUDED.schema_hash
	`
}

// buildDeleteMetaRegistrySchemaByKeyQuery deletes by key. The predicate pairs
// guid with object_type: `guid = ANY($1) AND object_type = ANY($2)` also matches
// every cross combination, so it could delete unrequested resources.
func buildDeleteMetaRegistrySchemaByKeyQuery() string {
	return `
		DELETE FROM meta_registry_resource_schema
		WHERE (guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))
	`
}

// buildListMetaRegistrySchemaHashQuery reads the stored hashes for a set of
// keys, using the same paired predicate as the delete.
func buildListMetaRegistrySchemaHashQuery() string {
	return `
		SELECT guid, object_type, schema_hash
		FROM meta_registry_resource_schema
		WHERE (guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))
	`
}

// ListMetaRegistrySchemaHashes returns the stored DDL hash of every key that has
// a row. A syncer that re-reads every object's DDL can compare against these
// hashes and write only the definitions that actually changed, instead of
// shipping every definition back to the database on every sync.
func (s *Store) ListMetaRegistrySchemaHashes(ctx context.Context, keys []MetaGUIDKey) (map[MetaGUIDKey][]byte, error) {
	result := make(map[MetaGUIDKey][]byte, len(keys))
	if len(keys) == 0 {
		return result, nil
	}

	guids := make([]string, 0, len(keys))
	objectTypes := make([]storepb.MetaType, 0, len(keys))
	for _, key := range keys {
		guids = append(guids, key.GUID)
		objectTypes = append(objectTypes, key.ObjectType)
	}

	rows, err := s.GetDB().QueryContext(ctx, buildListMetaRegistrySchemaHashQuery(),
		pq.Array(guids), pq.Array(objectTypes),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var guid string
		var objectType int32
		var schemaHash []byte
		if err := rows.Scan(&guid, &objectType, &schemaHash); err != nil {
			return nil, err
		}
		result[MetaGUIDKey{GUID: guid, ObjectType: storepb.MetaType(objectType)}] = schemaHash
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetMetaRegistrySchema returns the stored DDL of one object. ok is false when
// no row exists, which is the normal state before the first sync and for every
// engine whose DDL is not fetched; the caller then falls back to reconstructing
// the definition from metadata.
func (s *Store) GetMetaRegistrySchema(ctx context.Context, guid string, objectType storepb.MetaType) (string, bool, error) {
	var schema string
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT schema FROM meta_registry_resource_schema WHERE guid = $1 AND object_type = $2`,
		guid, objectType,
	).Scan(&schema); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return schema, true, nil
}

// BatchUpsertMetaRegistrySchema writes DDL rows inside the caller's transaction.
func (*Store) BatchUpsertMetaRegistrySchema(ctx context.Context, tx *sql.Tx, creates []*CreateMetaRegistrySchemaMessage) error {
	if len(creates) == 0 {
		return nil
	}

	guids := make([]string, 0, len(creates))
	objectTypes := make([]storepb.MetaType, 0, len(creates))
	schemas := make([]string, 0, len(creates))
	schemaHashes := make([][]byte, 0, len(creates))
	for _, create := range creates {
		hash := sha256.Sum256([]byte(create.Schema))
		guids = append(guids, create.GUID)
		objectTypes = append(objectTypes, create.ObjectType)
		schemas = append(schemas, create.Schema)
		schemaHashes = append(schemaHashes, hash[:])
	}

	if _, err := tx.ExecContext(ctx, buildUpsertMetaRegistrySchemaQuery(),
		pq.Array(guids), pq.Array(objectTypes), pq.Array(schemas), pq.Array(schemaHashes),
	); err != nil {
		return err
	}
	return nil
}

// BatchDeleteMetaRegistrySchema removes DDL rows by key inside the caller's
// transaction. Deleting a key that has no row is a no-op.
func (*Store) BatchDeleteMetaRegistrySchema(ctx context.Context, tx *sql.Tx, keys []MetaGUIDKey) error {
	if len(keys) == 0 {
		return nil
	}

	guids := make([]string, 0, len(keys))
	objectTypes := make([]storepb.MetaType, 0, len(keys))
	for _, key := range keys {
		guids = append(guids, key.GUID)
		objectTypes = append(objectTypes, key.ObjectType)
	}

	if _, err := tx.ExecContext(ctx, buildDeleteMetaRegistrySchemaByKeyQuery(),
		pq.Array(guids), pq.Array(objectTypes),
	); err != nil {
		return err
	}
	return nil
}
