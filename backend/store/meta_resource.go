package store

import (
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
	ParentGUID          string
	ObjectType          storepb.MetaType
	LimitPreObjectType  int
	OffsetPreObjectType int
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

// metaRegistryGUIDCacheKey returns the cache key for a GUID-scoped lookup. It
// reports false when the lookup is not narrowed to a single object type, since
// a GUID may exist under several object types.
func metaRegistryGUIDCacheKey(find *FindMetaRegistryResourceMessage) (MetaGUIDKey, bool) {
	if find.GUID == nil || find.ObjectType == nil {
		return MetaGUIDKey{}, false
	}
	return MetaGUIDKey{GUID: *find.GUID, ObjectType: *find.ObjectType}, true
}

func (s *Store) GetMetaRegistry(ctx context.Context, find *FindMetaRegistryResourceMessage) (*MetaRegistryResource, error) {
	if find.ID != nil {
		if v, ok := s.metaRegistryCache.Get(*find.ID); ok && !s.cacheDisabled {
			return v, nil
		}
	}
	// The GUID alone is not unique: the table's unique constraint is
	// (guid, object_type). Only consult the GUID cache for a lookup that also
	// narrowed the object type.
	if key, ok := metaRegistryGUIDCacheKey(find); ok {
		if v, ok := s.metaRegistryGUIDCache.Get(key); ok && !s.cacheDisabled {
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
		s.metaRegistryGUIDCache.Add(metaRegistry.GUIDKey(), metaRegistry)
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
			s.metaRegistryGUIDCache.Add(metaRegistry.GUIDKey(), metaRegistry)
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
			s.metaRegistryGUIDCache.Add(reg.GUIDKey(), reg)
		}
	}
	return list, nil
}

// ListMetaRegistryResourceDigest lists meta registry rows without loading or
// unmarshalling their metadata: only ID, GUID, ObjectType and MetaHash are
// populated. The lineage analyzer scans every analyzable object hourly just to
// compare hashes, and the full listing made it parse every view definition.
//
// It deliberately does not touch the metadata caches — a cached entry with a
// nil Metadata would corrupt later reads.
func (s *Store) ListMetaRegistryResourceDigest(ctx context.Context, find *FindMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	list, err := s.listMetaRegistryResourceImpl(ctx, tx, find, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
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
	list, err := s.listMetaRegistryResourceHistoryImpl(ctx, tx, find, asOf)
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
	SearchStr string
	// GUIDPrefix restricts the search to a GUID subtree. A nil or empty prefix
	// searches every instance; an empty prefix used to compile to
	// `guid = '' OR guid LIKE ';%'`, which matched nothing, so the
	// explain-SQL search tool silently returned no rows when no instance was
	// selected.
	GUIDPrefix *string
	ObjectType *storepb.MetaType
	Limit      int
	Offset     int
}

// SearchMetaRegistryResource searches metadata by name and comment within the JSONB column.
// It reads the generated search_text column, which is exactly the concatenation
// of the inner object's name, title, comment and userComment fields: the matched
// rows are the same, but the predicate is answered by the trigram index instead
// of a sequential LATERAL jsonb_each scan.
func (s *Store) SearchMetaRegistryResource(ctx context.Context, find *SearchMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	if find.SearchStr == "" {
		return nil, common.Errorf(common.Invalid, "search string is required")
	}

	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	where, args := []string{}, []any{}

	if v := find.GUIDPrefix; v != nil && *v != "" {
		where, args = appendGUIDSubtreeCondition(where, args, "r.guid", *v)
	}
	if v := find.ObjectType; v != nil {
		where, args = append(where, fmt.Sprintf("r.object_type = $%d", len(args)+1)), append(args, *v)
	}

	// Escape LIKE metacharacters in user input.
	args = append(args, "%"+likePatternEscaper.Replace(find.SearchStr)+"%")
	where = append(where, fmt.Sprintf("r.search_text ILIKE $%d", len(args)))

	query := fmt.Sprintf(`
		SELECT
			r.id,
			r.guid,
			r.object_type,
			r.metadata,
			r.meta_hash
		FROM meta_registry_resource r
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

func (s *Store) ListSublevelMetaRegistryResource(ctx context.Context, find *FindSubLevelMetaRegistryResourceMessage) ([]*MetaRegistryResource, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if find.LimitPreObjectType == 0 {
		find.LimitPreObjectType = common.DefaultMetaSubLevelLimit
	}

	list, err := s.listSublevelMetaRegistryResourceImpl(ctx, tx, find.ParentGUID, find.ObjectType, find.LimitPreObjectType, find.OffsetPreObjectType)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, metaRegistry := range list {
		if isMetaTypeCached(metaRegistry.ObjectType) {
			s.metaRegistryCache.Add(metaRegistry.ID, metaRegistry)
			s.metaRegistryGUIDCache.Add(metaRegistry.GUIDKey(), metaRegistry)
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

	list, err := s.listSublevelMetaRegistryResourceHistoryImpl(ctx, tx, find.ParentGUID, find.ObjectType, find.LimitPreObjectType, find.OffsetPreObjectType, asOf)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return list, nil
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

	// PostgreSQL does not promise the row order of RETURNING, so the returned
	// id has to be matched back by (guid, object_type) rather than by position.
	type resourceKey struct {
		guid       string
		objectType storepb.MetaType
	}
	index := make(map[resourceKey]int, len(creates))
	for i, create := range creates {
		index[resourceKey{guid: create.GUID, objectType: create.ObjectType}] = i
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
			RETURNING id, guid, object_type
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

	seen := 0
	for rows.Next() {
		var (
			id         int64
			guid       string
			objectType int32
		)
		if err := rows.Scan(&id, &guid, &objectType); err != nil {
			slog.Error("InsertReturningFailed", slog.Int("count", len(creates)))
			return nil, errors.Wrap(err, "InsertReturningFailed")
		}
		i, ok := index[resourceKey{guid: guid, objectType: storepb.MetaType(objectType)}]
		if !ok {
			return nil, errors.Errorf("upsert returned an unknown resource %q/%d", guid, objectType)
		}
		creates[i].ID = id
		seen++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if seen != len(creates) {
		return nil, errors.Errorf("upsert returned %d rows for %d resources", seen, len(creates))
	}

	if err := s.upsertMetaRegistryHistory(ctx, tx, creates, observedAt); err != nil {
		return nil, err
	}

	// The cache is intentionally not touched here: this runs inside a
	// caller-owned transaction that may still roll back. Callers invalidate the
	// affected keys with InvalidateMetaRegistryCache after committing.

	resp := make([]*MetaRegistryResource, 0, len(creates))
	for _, create := range creates {
		resp = append(resp, &create.MetaRegistryResource)
	}

	return resp, nil
}

// InvalidateMetaRegistryCache drops the current-snapshot cache entries for the
// given rows. Call it after the transaction that wrote them has committed, so a
// rolled-back write can never leave a stale or uncommitted entry behind.
func (s *Store) InvalidateMetaRegistryCache(list []*MetaRegistryResource) {
	for _, registry := range list {
		if !isMetaTypeCached(registry.ObjectType) {
			continue
		}
		s.metaRegistryCache.Remove(registry.ID)
		s.metaRegistryGUIDCache.Remove(registry.GUIDKey())
	}
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

	// See BatchCreateMetaRegistryResourceAt: invalidation happens after the
	// caller commits, via InvalidateMetaRegistryCache.
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
		}
	case storepb.MetaType_TABLE:
		return []storepb.MetaType{storepb.MetaType_COLUMN}
	case storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW, storepb.MetaType_EXTERNAL_TABLE:
		return []storepb.MetaType{storepb.MetaType_COLUMN}
	default:
		return []storepb.MetaType{}
	}
}
