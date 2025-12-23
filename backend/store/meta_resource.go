package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type MetaRegistryResource struct {
	ID         int64
	Guid       string
	ObjectType storepb.MetaType
	Metadata   *storepb.StoredMetadata
	MetaHash   []byte
}

func (m *MetaRegistryResource) GuidKey() MetaGuidKey {
	return MetaGuidKey{
		Guid:       m.Guid,
		ObjectType: m.ObjectType,
	}
}

type MetaGuidKey struct {
	Guid       string
	ObjectType storepb.MetaType
}

type FindMetaRegistryResourceMessage struct {
	ID                *int64
	IDList            *[]int64
	Guid              *string
	GuidPrefix        *string
	ObjectType        *storepb.MetaType
	ExcludeObjectType *[]storepb.MetaType
	Limit             *int
	Offset            *int
}

type FindSubLevelMetaRegistryResourceMessage struct {
	ParentGuid         string
	ObjectType         storepb.MetaType
	LimitPreObjectType int
}

type CreateMetaRegistryResourceMessage struct {
	MetaRegistryResource
	MetadataBytes []byte
}

func (s *Store) GetMetaRegistry(ctx context.Context, find *FindMetaRegistryResourceMessage) (*MetaRegistryResource, error) {
	if find.ID != nil {
		if v, ok := s.metaRegistryCache.Get(*find.ID); ok && s.enableCache {
			return v, nil
		}
	}
	if find.Guid != nil {
		if v, ok := s.metaRegistryGuidCache.Get(*find.Guid); ok && s.enableCache {
			return v, nil
		}
	}

	list, err := s.ListMetaRegistry(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, fmt.Errorf("found multiple meta registry with the same criteria")
	}
	metaRegistry := list[0]

	if isMetaTypeCached(metaRegistry.ObjectType) {
		s.metaRegistryCache.Add(metaRegistry.ID, metaRegistry)
		s.metaRegistryGuidCache.Add(metaRegistry.Guid, metaRegistry)
	}
	return metaRegistry, nil
}

func (s *Store) ListMetaRegistry(ctx context.Context, find *FindMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	list, err := s.listMetaRegistryResource(ctx, tx, find, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, metaRegistry := range list {
		if isMetaTypeCached(metaRegistry.ObjectType) {
			s.metaRegistryCache.Add(metaRegistry.ID, metaRegistry)
			s.metaRegistryGuidCache.Add(metaRegistry.Guid, metaRegistry)
		}
	}
	return list, nil
}

func (s *Store) ListMetaRegistryResource(ctx context.Context, find *FindMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	list, err := s.listMetaRegistryResource(ctx, tx, find, true)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, metaRegistry := range list {
		if isMetaTypeCached(metaRegistry.ObjectType) {
			reg := &MetaRegistryResource{
				ID:         metaRegistry.ID,
				Guid:       metaRegistry.Guid,
				ObjectType: metaRegistry.ObjectType,
				Metadata:   nil,
				MetaHash:   metaRegistry.MetaHash,
			}
			s.metaRegistryCache.Add(metaRegistry.ID, reg)
		}
	}
	return list, nil
}

func (s *Store) listMetaRegistryResource(ctx context.Context, txn *sql.Tx, find *FindMetaRegistryResourceMessage, withMetadata bool) ([]*MetaRegistryResource, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource.id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.IDList; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource.id = ANY($%d)", len(args)+1)), append(args, *v)
	}
	if v := find.Guid; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource.guid = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.GuidPrefix; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource.guid LIKE $%d", len(args)+1)), append(args, *v+"%")
	}
	if v := find.ObjectType; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource.object_type = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ExcludeObjectType; v != nil && len(*v) > 0 {
		where, args = append(where, fmt.Sprintf("meta_registry_resource.object_type != ALL($%d)", len(args)+1)), append(args, *v)
	}

	var query string
	if withMetadata {
		query = `
		SELECT
			meta_registry_resource.id,
			meta_registry_resource.guid,
			meta_registry_resource.object_type,
			meta_registry_resource.metadata,
			meta_registry_resource.meta_hash
		FROM meta_registry_resource
		WHERE %s
		ORDER BY guid`
	} else {
		query = `
		SELECT
			meta_registry_resource.id,
			meta_registry_resource.guid,
			meta_registry_resource.object_type,
			NULL AS metadata,
			meta_registry_resource.meta_hash
		FROM meta_registry_resource
		WHERE %s
		ORDER BY guid`
	}

	query = fmt.Sprintf(query, strings.Join(where, " AND "))
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	var metaRegistryMessages []*MetaRegistryResource
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var metadata []byte
		var metaRegistryMessage MetaRegistryResource
		if err := rows.Scan(
			&metaRegistryMessage.ID,
			&metaRegistryMessage.Guid,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := protojson.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, " failed to unmarshal stored metadata")
			}
			metaRegistryMessage.Metadata = m
		}

		metaRegistryMessages = append(metaRegistryMessages, &metaRegistryMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metaRegistryMessages, nil
}

func (s *Store) ListSublevelMetaRegistryResource(ctx context.Context, find *FindSubLevelMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if find.LimitPreObjectType == 0 {
		find.LimitPreObjectType = common.DefaultMetaSubLevelLimit
	}

	list, err := s.listSublevelMetaRegistryResource(ctx, tx, find.ParentGuid, find.ObjectType, find.LimitPreObjectType)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, metaRegistry := range list {
		if isMetaTypeCached(metaRegistry.ObjectType) {
			s.metaRegistryCache.Add(metaRegistry.ID, metaRegistry)
			s.metaRegistryGuidCache.Add(metaRegistry.Guid, metaRegistry)
		}
	}
	return list, nil
}

func (s *Store) listSublevelMetaRegistryResource(ctx context.Context, txn *sql.Tx, parentGuid string, objectType storepb.MetaType, limitPreObjectType int) ([]*MetaRegistryResource, error) {
	// Whether using Lateral Join or Windows, for large datasets,
	// PG's query optimizer seems unable to select the correct index.
	// Therefore, we directly use the UNION ALL method here.

	nextPrefix := parentGuid + common.MetaGuidSplit + "%"
	args := []any{}

	qb := strings.Builder{}

	nextTypes := getNextLevelObjectType(objectType)
	for idx, nextType := range nextTypes {
		unionStr := ""
		if idx != 0 {
			unionStr = "UNION ALL "
		}
		nextQuery := fmt.Sprintf(`%s
		SELECT * FROM(
		SELECT
			meta_registry_resource.id,
			meta_registry_resource.guid,
			meta_registry_resource.object_type,
			meta_registry_resource.metadata,
			meta_registry_resource.meta_hash
		FROM meta_registry_resource
		WHERE meta_registry_resource.guid LIKE $%d AND meta_registry_resource.object_type = $%d
		ORDER BY guid limit %d)
		`, unionStr, len(args)+1, len(args)+2, limitPreObjectType)
		args = append(args, nextPrefix, nextType)
		qb.WriteString(nextQuery)
	}
	var metaRegistryMessages []*MetaRegistryResource
	rows, err := txn.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var metadata []byte
		var metaRegistryMessage MetaRegistryResource
		if err := rows.Scan(
			&metaRegistryMessage.ID,
			&metaRegistryMessage.Guid,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := protojson.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, " failed to unmarshal stored metadata")
			}
			metaRegistryMessage.Metadata = m
		}

		metaRegistryMessages = append(metaRegistryMessages, &metaRegistryMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metaRegistryMessages, nil
}

// BatchCreateMetaRegistryResource creates a meta registry.
func (s *Store) BatchCreateMetaRegistryResource(ctx context.Context, tx *sql.Tx, creates []*CreateMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {

	guids := make([]string, 0, len(creates))
	objectTypes := make([]storepb.MetaType, 0, len(creates))
	metadata := make([]string, 0, len(creates))
	metaHashes := make([][]byte, 0, len(creates))
	for _, create := range creates {
		guids = append(guids, create.Guid)
		objectTypes = append(objectTypes, create.ObjectType)
		metaHashes = append(metaHashes, create.MetaHash)
		metadata = append(metadata, string(create.MetadataBytes))
	}

	query := `
			INSERT INTO meta_registry_resource (
				guid,
				object_type,
				metadata,
				meta_hash
			) SELECT * FROM UNNEST ($1::text[], $2::int[], $3::jsonb[], $4::BYTEA[])
			ON CONFLICT(guid, object_type) DO UPDATE SET 
				metadata = EXCLUDED.metadata,
				meta_hash = EXCLUDED.meta_hash
			RETURNING id
		`

	rows, err := tx.QueryContext(ctx, query,
		pq.Array(guids),
		pq.Array(objectTypes),
		pq.Array(metadata),
		pq.Array(metaHashes),
	)
	if err != nil {
		slog.Error("InsertReturningFailed", slog.Int("count", len(creates)))
		return nil, errors.Wrap(err, "InsertReturningFailed")
	}
	defer rows.Close()

	i := 0
	for rows.Next() {
		if err := rows.Scan(&creates[i].ID); err != nil {
			slog.Error("InsertReturningFailed", slog.String("guid", creates[i].Guid), "object_type", creates[i].ObjectType.String())
			return nil, errors.Wrap(err, "InsertReturningFailed")
		}
		i++
	}

	for _, create := range creates {
		metaRegistory := &MetaRegistryResource{
			ID:         create.ID,
			Guid:       create.Guid,
			ObjectType: create.ObjectType,
			Metadata:   create.Metadata,
			MetaHash:   create.MetaHash,
		}
		if isMetaTypeCached(metaRegistory.ObjectType) {
			s.metaRegistryCache.Add(metaRegistory.ID, metaRegistory)
			s.metaRegistryGuidCache.Add(metaRegistory.Guid, metaRegistory)
		}
	}

	resp := make([]*MetaRegistryResource, 0, len(creates))
	for _, create := range creates {
		resp = append(resp, &create.MetaRegistryResource)
	}

	return resp, nil
}

// DeleteMetaRegistry deletes a meta registry by ids.
func (s *Store) BatchDeleteMetaRegistry(ctx context.Context, tx *sql.Tx, list []*MetaRegistryResource) error {
	ids := make([]int64, 0, len(list))
	for _, registry := range list {
		ids = append(ids, registry.ID)
	}

	fmt.Println("DELETE !!! ", len(ids))

	if _, err := tx.ExecContext(ctx, `DELETE FROM meta_registry_resource WHERE id = ANY($1)`, pq.Array(ids)); err != nil {
		return err
	}

	for _, registry := range list {
		s.metaRegistryCache.Remove(registry.ID)
		s.metaRegistryGuidCache.Remove(registry.Guid)
	}
	return nil

}

func isMetaTypeCached(metaType storepb.MetaType) bool {
	switch metaType {
	case
		storepb.MetaType_SCHEMA,
		storepb.MetaType_DATABASE,
		storepb.MetaType_TABLE,
		storepb.MetaType_VIEW:
		return true
	default:
		return false
	}
}

func getNextLevelObjectType(metaType storepb.MetaType) []storepb.MetaType {
	switch metaType {
	case storepb.MetaType_INSTANCE:
		return []storepb.MetaType{storepb.MetaType_DATABASE}
	case storepb.MetaType_DATABASE:
		return []storepb.MetaType{storepb.MetaType_SCHEMA}
	case storepb.MetaType_SCHEMA:
		return []storepb.MetaType{storepb.MetaType_TABLE, storepb.MetaType_VIEW}
	case storepb.MetaType_TABLE:
		return []storepb.MetaType{storepb.MetaType_COLUMN}
	default:
		return []storepb.MetaType{}
	}
}
