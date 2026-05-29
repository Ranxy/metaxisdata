package store

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type MetaRegistryResource struct {
	ID         int64
	GUID       string
	ObjectType storepb.MetaType
	Metadata   *storepb.StoredMetadata
	MetaHash   []byte
}

func (m *MetaRegistryResource) GUIDKey() MetaGUIDKey {
	return MetaGUIDKey{
		GUID:       m.GUID,
		ObjectType: m.ObjectType,
	}
}

type MetaGUIDKey struct {
	GUID       string
	ObjectType storepb.MetaType
}

type FindMetaRegistryResourceMessage struct {
	ID                *int64
	IDList            *[]int64
	GUID              *string
	GUIDPrefix        *string
	ObjectType        *storepb.MetaType
	ExcludeObjectType *[]storepb.MetaType
	Limit             *int
	Offset            *int
	ExtraArgs         []ExtraArgs
}

type FindSubLevelMetaRegistryResourceMessage struct {
	ParentGUID         string
	ObjectType         storepb.MetaType
	LimitPreObjectType int
}

type CreateMetaRegistryResourceMessage struct {
	MetaRegistryResource
	MetadataBytes []byte
}

type MetaRegistryHistory struct {
	ID         int64
	GUID       string
	ObjectType storepb.MetaType
	Metadata   *storepb.StoredMetadata
	MetaHash   []byte
	ValidFrom  time.Time
	ValidTo    *time.Time
}

type FindMetaRegistryHistoryMessage struct {
	GUID           *string
	GUIDPrefix     *string
	ObjectType     *storepb.MetaType
	Limit          *int
	Offset         *int
	ValidFrom      *time.Time
	TransitionTime *time.Time
	OrderDesc      bool
}

var likePatternEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func appendGUIDSubtreeCondition(where []string, args []any, column string, guidPrefix string) ([]string, []any) {
	exactArgIndex := len(args) + 1
	args = append(args, guidPrefix)

	descendantArgIndex := len(args) + 1
	descendantPattern := likePatternEscaper.Replace(guidPrefix+common.MetaGUIDSplit) + "%"
	args = append(args, descendantPattern)

	where = append(where, fmt.Sprintf(`(%s = $%d OR %s LIKE $%d ESCAPE E'\\')`, column, exactArgIndex, column, descendantArgIndex))
	return where, args
}

func (s *Store) GetMetaRegistry(ctx context.Context, find *FindMetaRegistryResourceMessage) (*MetaRegistryResource, error) {
	if find.ID != nil {
		if v, ok := s.metaRegistryCache.Get(*find.ID); ok && s.enableCache {
			return v, nil
		}
	}
	if find.GUID != nil {
		if v, ok := s.metaRegistryGUIDCache.Get(*find.GUID); ok && s.enableCache {
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
		return nil, errors.Errorf("found multiple meta registry with the same criteria")
	}
	metaRegistry := list[0]

	if isMetaTypeCached(metaRegistry.ObjectType) {
		s.metaRegistryCache.Add(metaRegistry.ID, metaRegistry)
		s.metaRegistryGUIDCache.Add(metaRegistry.GUID, metaRegistry)
	}
	return metaRegistry, nil
}

func (s *Store) ListMetaRegistry(ctx context.Context, find *FindMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	list, err := s.listMetaRegistryResourceImpl(ctx, tx, find, true)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, metaRegistry := range list {
		if isMetaTypeCached(metaRegistry.ObjectType) {
			s.metaRegistryCache.Add(metaRegistry.ID, metaRegistry)
			s.metaRegistryGUIDCache.Add(metaRegistry.GUID, metaRegistry)
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
	list, err := s.listMetaRegistryResourceImpl(ctx, tx, find, true)
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
				GUID:       metaRegistry.GUID,
				ObjectType: metaRegistry.ObjectType,
				Metadata:   metaRegistry.Metadata,
				MetaHash:   metaRegistry.MetaHash,
			}
			s.metaRegistryCache.Add(metaRegistry.ID, reg)
			s.metaRegistryGUIDCache.Add(metaRegistry.GUID, reg)
		}
	}
	return list, nil
}

func (s *Store) GetMetaRegistryAsOf(ctx context.Context, find *FindMetaRegistryResourceMessage, asOf time.Time) (*MetaRegistryResource, error) {
	list, err := s.ListMetaRegistryResourceAsOf(ctx, find, asOf)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, errors.Errorf("found multiple meta registry history rows with the same criteria")
	}
	return list[0], nil
}

func (s *Store) ListMetaRegistryResourceAsOf(ctx context.Context, find *FindMetaRegistryResourceMessage, asOf time.Time) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	list, err := s.listMetaRegistryResourceHistoryImpl(ctx, tx, find, asOf, true)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) ListMetaRegistryHistory(ctx context.Context, find *FindMetaRegistryHistoryMessage) ([]*MetaRegistryHistory, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	list, err := s.listMetaRegistryHistoryImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return list, nil
}

// SearchMetaRegistryResourceMessage is the message to search meta registry resources
// by matching name/comment fields inside the metadata JSONB.
type SearchMetaRegistryResourceMessage struct {
	SearchStr  string
	GUIDPrefix *string
	ObjectType *storepb.MetaType
	Limit      int
	Offset     int
}

// SearchMetaRegistryResource searches metadata by name and comment within the JSONB column.
// It uses a LATERAL join to extract the single inner metadata object (e.g. tableMetadata, schemaMetadata)
// and searches the name, comment, and userComment text fields.
func (s *Store) SearchMetaRegistryResource(ctx context.Context, find *SearchMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	where, args := []string{}, []any{}

	if v := find.GUIDPrefix; v != nil {
		where, args = appendGUIDSubtreeCondition(where, args, "r.guid", *v)
	}
	if v := find.ObjectType; v != nil {
		where, args = append(where, fmt.Sprintf("r.object_type = $%d", len(args)+1)), append(args, *v)
	}

	// Escape LIKE metacharacters in user input.
	escaped := likePatternEscaper.Replace(find.SearchStr)
	searchPattern := "%" + escaped + "%"
	args = append(args, searchPattern)
	searchIdx := len(args)
	where = append(where, fmt.Sprintf(`(m.inner_meta->>'name' ILIKE $%d OR m.inner_meta->>'title' ILIKE $%d OR m.inner_meta->>'comment' ILIKE $%d OR m.inner_meta->>'userComment' ILIKE $%d)`, searchIdx, searchIdx, searchIdx, searchIdx))

	query := fmt.Sprintf(`
		SELECT
			r.id,
			r.guid,
			r.object_type,
			r.metadata,
			r.meta_hash
		FROM meta_registry_resource r,
			LATERAL (SELECT value AS inner_meta FROM jsonb_each(r.metadata) LIMIT 1) AS m
		WHERE %s
		ORDER BY r.guid`, strings.Join(where, " AND "))

	if find.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", find.Limit)
	}
	if find.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", find.Offset)
	}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*MetaRegistryResource
	for rows.Next() {
		var metadata []byte
		var msg MetaRegistryResource
		if err := rows.Scan(&msg.ID, &msg.GUID, &msg.ObjectType, &metadata, &msg.MetaHash); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, "failed to unmarshal stored metadata")
			}
			msg.Metadata = m
		}
		list = append(list, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return list, nil
}

func buildMetaRegistryWhereClause(tableName string, find *FindMetaRegistryResourceMessage) ([]string, []any) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("%s.id = $%d", tableName, len(args)+1)), append(args, *v)
	}
	if v := find.IDList; v != nil {
		where, args = append(where, fmt.Sprintf("%s.id = ANY($%d)", tableName, len(args)+1)), append(args, *v)
	}
	if v := find.GUID; v != nil {
		where, args = append(where, fmt.Sprintf("%s.guid = $%d", tableName, len(args)+1)), append(args, *v)
	}
	if v := find.GUIDPrefix; v != nil {
		where, args = appendGUIDSubtreeCondition(where, args, tableName+".guid", *v)
	}
	if v := find.ObjectType; v != nil {
		where, args = append(where, fmt.Sprintf("%s.object_type = $%d", tableName, len(args)+1)), append(args, *v)
	}
	if v := find.ExcludeObjectType; v != nil && len(*v) > 0 {
		where, args = append(where, fmt.Sprintf("%s.object_type != ALL($%d)", tableName, len(args)+1)), append(args, *v)
	}
	if v := find.ExtraArgs; len(v) > 0 {
		for _, extraArg := range v {
			where, args = append(where, fmt.Sprintf("%s %s $%d", extraArg.Left, extraArg.Op, len(args)+1)), append(args, extraArg.Right)
		}
	}
	return where, args
}

func (*Store) listMetaRegistryResourceImpl(ctx context.Context, txn *sql.Tx, find *FindMetaRegistryResourceMessage, withMetadata bool) ([]*MetaRegistryResource, error) {
	where, args := buildMetaRegistryWhereClause("meta_registry_resource", find)

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
			&metaRegistryMessage.GUID,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
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

func (*Store) listMetaRegistryResourceHistoryImpl(ctx context.Context, txn *sql.Tx, find *FindMetaRegistryResourceMessage, asOf time.Time, withMetadata bool) ([]*MetaRegistryResource, error) {
	where, args := buildMetaRegistryWhereClause("meta_registry_resource_history", find)
	args = append(args, asOf)
	asOfArg := len(args)
	where = append(where, fmt.Sprintf("meta_registry_resource_history.valid_from <= $%d", asOfArg))
	where = append(where, fmt.Sprintf("(meta_registry_resource_history.valid_to IS NULL OR meta_registry_resource_history.valid_to > $%d)", asOfArg))

	var query string
	if withMetadata {
		query = `
		SELECT
			meta_registry_resource_history.id,
			meta_registry_resource_history.guid,
			meta_registry_resource_history.object_type,
			meta_registry_resource_history.metadata,
			meta_registry_resource_history.meta_hash
		FROM meta_registry_resource_history
		WHERE %s
		ORDER BY guid`
	} else {
		query = `
		SELECT
			meta_registry_resource_history.id,
			meta_registry_resource_history.guid,
			meta_registry_resource_history.object_type,
			NULL AS metadata,
			meta_registry_resource_history.meta_hash
		FROM meta_registry_resource_history
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
			&metaRegistryMessage.GUID,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
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

func (*Store) listMetaRegistryHistoryImpl(ctx context.Context, txn *sql.Tx, find *FindMetaRegistryHistoryMessage) ([]*MetaRegistryHistory, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.GUID; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource_history.guid = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.GUIDPrefix; v != nil {
		where, args = appendGUIDSubtreeCondition(where, args, "meta_registry_resource_history.guid", *v)
	}
	if v := find.ObjectType; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource_history.object_type = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ValidFrom; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource_history.valid_from = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.TransitionTime; v != nil {
		args = append(args, *v)
		argIndex := len(args)
		where = append(where, fmt.Sprintf("(meta_registry_resource_history.valid_from = $%d OR meta_registry_resource_history.valid_to = $%d)", argIndex, argIndex))
	}

	query := `
		SELECT
			meta_registry_resource_history.id,
			meta_registry_resource_history.guid,
			meta_registry_resource_history.object_type,
			meta_registry_resource_history.metadata,
			meta_registry_resource_history.meta_hash,
			meta_registry_resource_history.valid_from,
			meta_registry_resource_history.valid_to
		FROM meta_registry_resource_history
		WHERE %s
		ORDER BY meta_registry_resource_history.valid_from %s, meta_registry_resource_history.guid`
	order := "ASC"
	if find.OrderDesc {
		order = "DESC"
	}
	query = fmt.Sprintf(query, strings.Join(where, " AND "), order)
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*MetaRegistryHistory
	for rows.Next() {
		var metadata []byte
		var history MetaRegistryHistory
		var validTo sql.NullTime
		if err := rows.Scan(
			&history.ID,
			&history.GUID,
			&history.ObjectType,
			&metadata,
			&history.MetaHash,
			&history.ValidFrom,
			&validTo,
		); err != nil {
			return nil, err
		}
		if validTo.Valid {
			value := validTo.Time
			history.ValidTo = &value
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, "failed to unmarshal stored metadata history")
			}
			history.Metadata = m
		}
		list = append(list, &history)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

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

func (s *Store) ListSublevelMetaRegistryResource(ctx context.Context, find *FindSubLevelMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if find.LimitPreObjectType == 0 {
		find.LimitPreObjectType = common.DefaultMetaSubLevelLimit
	}

	list, err := s.listSublevelMetaRegistryResourceImpl(ctx, tx, find.ParentGUID, find.ObjectType, find.LimitPreObjectType)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, metaRegistry := range list {
		if isMetaTypeCached(metaRegistry.ObjectType) {
			s.metaRegistryCache.Add(metaRegistry.ID, metaRegistry)
			s.metaRegistryGUIDCache.Add(metaRegistry.GUID, metaRegistry)
		}
	}
	return list, nil
}

func (s *Store) ListSublevelMetaRegistryResourceAsOf(ctx context.Context, find *FindSubLevelMetaRegistryResourceMessage, asOf time.Time) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if find.LimitPreObjectType == 0 {
		find.LimitPreObjectType = common.DefaultMetaSubLevelLimit
	}

	list, err := s.listSublevelMetaRegistryResourceHistoryImpl(ctx, tx, find.ParentGUID, find.ObjectType, find.LimitPreObjectType, asOf)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return list, nil
}

func (*Store) listSublevelMetaRegistryResourceImpl(ctx context.Context, txn *sql.Tx, parentGUID string, objectType storepb.MetaType, limitPreObjectType int) ([]*MetaRegistryResource, error) {
	// Whether using Lateral Join or Windows, for large datasets,
	// PG's query optimizer seems unable to select the correct index.
	// Therefore, we directly use the UNION ALL method here.

	nextTypes := getNextLevelObjectType(objectType)
	if len(nextTypes) == 0 {
		return []*MetaRegistryResource{}, nil
	}

	args := []any{}

	qb := strings.Builder{}

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
		WHERE (meta_registry_resource.guid = $%d OR meta_registry_resource.guid LIKE $%d ESCAPE E'\\') AND meta_registry_resource.object_type = $%d
		ORDER BY guid limit %d)
		`, unionStr, len(args)+1, len(args)+2, len(args)+3, limitPreObjectType)
		args = append(
			args,
			parentGUID,
			likePatternEscaper.Replace(parentGUID+common.MetaGUIDSplit)+"%",
			nextType,
		)
		//nolint:revive
		if _, err := qb.WriteString(nextQuery); err != nil {
			return nil, err
		}
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
			&metaRegistryMessage.GUID,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
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

func (*Store) listSublevelMetaRegistryResourceHistoryImpl(ctx context.Context, txn *sql.Tx, parentGUID string, objectType storepb.MetaType, limitPreObjectType int, asOf time.Time) ([]*MetaRegistryResource, error) {
	nextTypes := getNextLevelObjectType(objectType)
	if len(nextTypes) == 0 {
		return []*MetaRegistryResource{}, nil
	}

	args := []any{}
	qb := strings.Builder{}

	for idx, nextType := range nextTypes {
		unionStr := ""
		if idx != 0 {
			unionStr = "UNION ALL "
		}
		nextQuery := fmt.Sprintf(`%s
		SELECT * FROM(
		SELECT
			meta_registry_resource_history.id,
			meta_registry_resource_history.guid,
			meta_registry_resource_history.object_type,
			meta_registry_resource_history.metadata,
			meta_registry_resource_history.meta_hash
		FROM meta_registry_resource_history
		WHERE (meta_registry_resource_history.guid = $%d OR meta_registry_resource_history.guid LIKE $%d ESCAPE E'\\')
			AND meta_registry_resource_history.object_type = $%d
			AND meta_registry_resource_history.valid_from <= $%d
			AND (meta_registry_resource_history.valid_to IS NULL OR meta_registry_resource_history.valid_to > $%d)
		ORDER BY guid limit %d)
		`, unionStr, len(args)+1, len(args)+2, len(args)+3, len(args)+4, len(args)+4, limitPreObjectType)
		args = append(
			args,
			parentGUID,
			likePatternEscaper.Replace(parentGUID+common.MetaGUIDSplit)+"%",
			nextType,
			asOf,
		)
		if _, err := qb.WriteString(nextQuery); err != nil {
			return nil, err
		}
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
			&metaRegistryMessage.GUID,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
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

// BatchCreateMetaRegistryResource creates or updates the current meta registry snapshot.
func (s *Store) BatchCreateMetaRegistryResource(ctx context.Context, tx *sql.Tx, creates []*CreateMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	return s.BatchCreateMetaRegistryResourceAt(ctx, tx, creates, time.Now().UTC())
}

// BatchCreateMetaRegistryResourceAt creates or updates the current meta registry snapshot at a specific observed time.
func (s *Store) BatchCreateMetaRegistryResourceAt(ctx context.Context, tx *sql.Tx, creates []*CreateMetaRegistryResourceMessage, observedAt time.Time) ([]*MetaRegistryResource, error) {
	if len(creates) == 0 {
		return nil, nil
	}

	guids := make([]string, 0, len(creates))
	objectTypes := make([]storepb.MetaType, 0, len(creates))
	metadata := make([]string, 0, len(creates))
	metaHashes := make([][]byte, 0, len(creates))
	for _, create := range creates {
		guids = append(guids, create.GUID)
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
			slog.Error("InsertReturningFailed", slog.String("guid", creates[i].GUID), "object_type", creates[i].ObjectType.String())
			return nil, errors.Wrap(err, "InsertReturningFailed")
		}
		i++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.upsertMetaRegistryHistory(ctx, tx, creates, observedAt); err != nil {
		return nil, err
	}

	for _, create := range creates {
		metaRegistory := &MetaRegistryResource{
			ID:         create.ID,
			GUID:       create.GUID,
			ObjectType: create.ObjectType,
			Metadata:   create.Metadata,
			MetaHash:   create.MetaHash,
		}
		if isMetaTypeCached(metaRegistory.ObjectType) {
			s.metaRegistryCache.Add(metaRegistory.ID, metaRegistory)
			s.metaRegistryGUIDCache.Add(metaRegistory.GUID, metaRegistory)
		}
	}

	resp := make([]*MetaRegistryResource, 0, len(creates))
	for _, create := range creates {
		resp = append(resp, &create.MetaRegistryResource)
	}

	return resp, nil
}

// BatchDeleteMetaRegistry deletes current meta registry rows and closes their open history records.
func (s *Store) BatchDeleteMetaRegistry(ctx context.Context, tx *sql.Tx, list []*MetaRegistryResource) error {
	return s.BatchDeleteMetaRegistryAt(ctx, tx, list, time.Now().UTC())
}

// BatchDeleteMetaRegistryAt deletes current meta registry rows and closes their open history records at a specific observed time.
func (s *Store) BatchDeleteMetaRegistryAt(ctx context.Context, tx *sql.Tx, list []*MetaRegistryResource, observedAt time.Time) error {
	if len(list) == 0 {
		return nil
	}

	historyToClose := make([]*MetaRegistryHistory, 0, len(list))
	ids := make([]int64, 0, len(list))
	for _, registry := range list {
		ids = append(ids, registry.ID)
		historyToClose = append(historyToClose, &MetaRegistryHistory{GUID: registry.GUID, ObjectType: registry.ObjectType})
	}

	if err := s.closeOpenMetaRegistryHistory(ctx, tx, historyToClose, observedAt); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM meta_registry_resource WHERE id = ANY($1)`, pq.Array(ids)); err != nil {
		return err
	}

	for _, registry := range list {
		s.metaRegistryCache.Remove(registry.ID)
		s.metaRegistryGUIDCache.Remove(registry.GUID)
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
		return []storepb.MetaType{storepb.MetaType_SCHEMA, storepb.MetaType_MANUAL_SQL}
	case storepb.MetaType_SCHEMA:
		return []storepb.MetaType{
			storepb.MetaType_TABLE,
			storepb.MetaType_EXTERNAL_TABLE,
			storepb.MetaType_VIEW,
			storepb.MetaType_MATERIALIZED_VIEW,
			storepb.MetaType_FUNCTION,
			storepb.MetaType_PROCEDURE,
			storepb.MetaType_SEQUENCE,
			storepb.MetaType_MANUAL_SQL,
			storepb.MetaType_PACKAGE,
			storepb.MetaType_STREAM,
		}
	case storepb.MetaType_TABLE:
		return []storepb.MetaType{storepb.MetaType_COLUMN}
	case storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW, storepb.MetaType_EXTERNAL_TABLE:
		return []storepb.MetaType{storepb.MetaType_COLUMN}
	default:
		return []storepb.MetaType{}
	}
}
