package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// MetaRegistryStatistics is the volatile part of a resource's metadata: the size estimates
// of a table and the current value of a sequence. normalizeMetadataForHash zeroes exactly
// these fields before hashing, so refreshing them changes what the API shows without moving
// the change fingerprint, writing a history row or re-queueing lineage analysis.
type MetaRegistryStatistics struct {
	RowCount          int64
	DataSize          int64
	IndexSize         int64
	DataFree          int64
	SequenceLastValue string
}

// MetadataStatistics extracts the statistics a stored metadata row carries. A type without
// statistics yields the zero value, which every sync compares equal to itself.
func MetadataStatistics(meta *storepb.StoredMetadata) MetaRegistryStatistics {
	stats := MetaRegistryStatistics{}
	switch {
	case meta.GetTableMetadata() != nil:
		table := meta.GetTableMetadata()
		stats.RowCount = table.GetRowCount()
		stats.DataSize = table.GetDataSize()
		stats.IndexSize = table.GetIndexSize()
		stats.DataFree = table.GetDataFree()
	case meta.GetSequenceMetadata() != nil:
		stats.SequenceLastValue = meta.GetSequenceMetadata().GetLastValue()
	default:
	}
	return stats
}

// metaRegistryStatisticsQuery reads the statistics out of the metadata JSONB instead of
// unmarshalling the row: the sync only needs to know whether they moved. The paths and the
// text-to-bigint casts match protojson's encoding (64-bit integers are strings there); a
// missing key is a zero statistic because protojson omits zero-valued fields.
const metaRegistryStatisticsQuery = `
	SELECT
		meta_registry_resource.guid,
		meta_registry_resource.object_type,
		COALESCE((meta_registry_resource.metadata #>> '{tableMetadata,rowCount}')::bigint, 0),
		COALESCE((meta_registry_resource.metadata #>> '{tableMetadata,dataSize}')::bigint, 0),
		COALESCE((meta_registry_resource.metadata #>> '{tableMetadata,indexSize}')::bigint, 0),
		COALESCE((meta_registry_resource.metadata #>> '{tableMetadata,dataFree}')::bigint, 0),
		COALESCE(meta_registry_resource.metadata #>> '{sequenceMetadata,lastValue}', '')
	FROM meta_registry_resource
	WHERE %s AND meta_registry_resource.object_type = ANY($%d::int[])
`

// ListMetaRegistryStatistics lists the statistics of the resources under a GUID prefix, keyed
// by object. Only the object types that carry statistics are read, and the GUID subtree
// predicate is the one every other prefix-scoped read uses.
func (s *Store) ListMetaRegistryStatistics(ctx context.Context, guidPrefix string) (map[MetaGUIDKey]MetaRegistryStatistics, error) {
	where, args := appendGUIDSubtreeCondition(nil, nil, "meta_registry_resource.guid", guidPrefix)
	args = append(args, pq.Array([]int32{int32(storepb.MetaType_TABLE), int32(storepb.MetaType_SEQUENCE)}))

	query := fmt.Sprintf(metaRegistryStatisticsQuery, strings.Join(where, " AND "), len(args))

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query meta registry statistics")
	}
	defer rows.Close()

	statistics := make(map[MetaGUIDKey]MetaRegistryStatistics)
	for rows.Next() {
		var (
			key       MetaGUIDKey
			objectTyp int16
			stats     MetaRegistryStatistics
		)
		if err := rows.Scan(
			&key.GUID, &objectTyp,
			&stats.RowCount, &stats.DataSize, &stats.IndexSize, &stats.DataFree,
			&stats.SequenceLastValue,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan meta registry statistics")
		}
		key.ObjectType = storepb.MetaType(objectTyp)
		statistics[key] = stats
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for meta registry statistics")
	}
	return statistics, nil
}

// metaRegistryStatisticsUpdateStatement rewrites the metadata of resources whose statistics
// moved. It is deliberately not a history write: the caller passes the current row's full
// metadata, and the meta_hash guard rejects a row whose non-statistics content changed since
// the caller read it. The fingerprint is unchanged by construction (the statistics are
// normalized out of it), so the guard is about concurrent schema syncs, not about statistics.
const metaRegistryStatisticsUpdateStatement = `
	WITH updated AS (
		SELECT guid, object_type::int2 AS object_type, metadata, meta_hash
		FROM UNNEST($1::text[], $2::int[], $3::jsonb[], $4::bytea[])
			AS u(guid, object_type, metadata, meta_hash)
	)
	UPDATE meta_registry_resource AS current
	SET metadata = updated.metadata
	FROM updated
	WHERE current.guid = updated.guid
		AND current.object_type = updated.object_type
		AND current.meta_hash = updated.meta_hash
`

// BatchUpdateMetaRegistryStatistics refreshes the statistics of rows whose non-statistics
// content is unchanged, in its own transaction. It writes no history row and leaves
// meta_hash alone, so a stats refresh is invisible to change detection while the API starts
// returning the new values. A row that changed concurrently is skipped: the skip is not an
// error, because the next sync writes the row's current metadata anyway.
//
// It deliberately has no caller-owned transaction: statistics are a derived estimate, and a
// failure here must never roll back the schema metadata that the same sync committed.
func (s *Store) BatchUpdateMetaRegistryStatistics(ctx context.Context, updates []*CreateMetaRegistryResourceMessage) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}

	guids := make([]string, 0, len(updates))
	objectTypes := make([]int32, 0, len(updates))
	metadata := make([]string, 0, len(updates))
	metaHashes := make([][]byte, 0, len(updates))
	for _, update := range updates {
		guids = append(guids, update.GUID)
		objectTypes = append(objectTypes, int32(update.ObjectType))
		metadata = append(metadata, string(update.MetadataBytes))
		metaHashes = append(metaHashes, update.MetaHash)
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin the statistics transaction")
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, metaRegistryStatisticsUpdateStatement,
		pq.Array(guids), pq.Array(objectTypes), pq.Array(metadata), pq.Array(metaHashes))
	if err != nil {
		return 0, errors.Wrap(err, "failed to update meta registry statistics")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to report the updated meta registry statistics")
	}
	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit the statistics refresh")
	}
	return affected, nil
}
