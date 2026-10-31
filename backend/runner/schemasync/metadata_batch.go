package schemasync

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type batchMetaCreate struct {
	exist    []*store.MetaRegistryResource
	guidList []*store.CreateMetaRegistryResourceMessage
	// storedStatistics is what the statistics of the stored rows are, so the diff can tell a
	// stats-only change from an unchanged row without loading their metadata.
	storedStatistics map[store.MetaGUIDKey]store.MetaRegistryStatistics
	updates          []*store.CreateMetaRegistryResourceMessage // populated by prepare()
	deletes          []*store.MetaRegistryResource              // populated by prepare()
	// statsUpdates are rows whose non-statistics content is unchanged but whose statistics
	// moved. They are written to the metadata column alone, so they never enter history or
	// the lineage queue. Populated by prepare().
	statsUpdates []*store.CreateMetaRegistryResourceMessage
}

func (b *batchMetaCreate) StoreMetaResource(_ context.Context, prefixName string, objectType storepb.MetaType, data *storepb.StoredMetadata) error {
	guid, err := convertMetadataToGUID(prefixName, objectType, data)
	if err != nil {
		return err
	}

	for _, child := range getChildMetadataResources(guid, objectType, data) {
		b.add(child.GUID, child.ObjectType, child.Metadata)
	}

	b.add(guid, objectType, data)
	return nil
}
func (b *batchMetaCreate) add(guid string, mt storepb.MetaType, data *storepb.StoredMetadata) {
	registry := &store.CreateMetaRegistryResourceMessage{
		MetaRegistryResource: store.MetaRegistryResource{
			GUID:       guid,
			ObjectType: mt,
			Metadata:   data,
		},
	}
	b.guidList = append(b.guidList, registry)
}

// prepare computes the diff between the snapshot and the stored rows. It is
// pure computation and must run before the write transaction is opened, so no
// row lock is held while the target instance is queried.
func (b *batchMetaCreate) prepare() error {
	updates, deletes, err := b.diff()
	if err != nil {
		return errors.Wrap(err, "batchMetaCreateRunDiff")
	}
	b.updates = updates
	b.deletes = deletes
	return nil
}

// write persists a prepared diff inside the caller's transaction.
func (b *batchMetaCreate) write(ctx context.Context, s *store.Store, tx *sql.Tx) error {
	observedAt := time.Now().UTC()

	if len(b.deletes) > 0 {
		if err := s.BatchDeleteMetaRegistryAt(ctx, tx, b.deletes, observedAt); err != nil {
			return errors.Wrap(err, "BatchDeleteMetaRegistryResourceByID")
		}
	}
	if len(b.updates) > 0 {
		if _, err := s.BatchCreateMetaRegistryResourceAt(ctx, tx, b.updates, observedAt); err != nil {
			return errors.Wrap(err, "BatchCreateMetaRegistryResource")
		}
	}
	return nil
}
func (b *batchMetaCreate) diff() (updates []*store.CreateMetaRegistryResourceMessage, deletes []*store.MetaRegistryResource, err error) {
	existMap := make(map[store.MetaGUIDKey]*store.MetaRegistryResource)
	for _, item := range b.exist {
		if !isSchemaSyncManagedMetaType(item.ObjectType) {
			continue
		}
		existMap[item.GUIDKey()] = item
	}

	// A snapshot can name the same (GUID, object type) twice — two columns with
	// the same name, say. Writing both would make the batch upsert fail with
	// "cannot affect row a second time" and take the whole database's sync with
	// it, so the duplicate is dropped and logged. The first entry is kept, which
	// is deterministic because the snapshot order is.
	seen := make(map[store.MetaGUIDKey]struct{}, len(b.guidList))
	guidList := make([]*store.CreateMetaRegistryResourceMessage, 0, len(b.guidList))
	for _, item := range b.guidList {
		key := item.GUIDKey()
		if _, duplicate := seen[key]; duplicate {
			slog.Warn("Skipping a duplicate metadata resource in a schema sync batch",
				slog.String("guid", item.GUID),
				slog.String("objectType", item.ObjectType.String()))
			continue
		}
		seen[key] = struct{}{}
		guidList = append(guidList, item)
	}

	for _, item := range guidList {
		metadataBytes, hash, err := store.CalcStoreMetaHash(item.Metadata)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to calculate metadata hash for guid %q", item.GUID)
		}

		item.MetaHash = hash
		item.MetadataBytes = metadataBytes

		existing, ok := existMap[item.GUIDKey()]
		if !ok {
			// new item
			updates = append(updates, item)
			continue
		}

		if existing.MetaHash == nil || item.MetaHash == nil || !bytes.Equal(existing.MetaHash, item.MetaHash) {
			updates = append(updates, item)
		} else if stored, ok := b.storedStatistics[item.GUIDKey()]; ok && stored != store.MetadataStatistics(item.Metadata) {
			// The schema did not change but the statistics moved: refresh only what the API
			// shows. Treating this as an update would write a history row and re-queue
			// lineage analysis for a row count.
			b.statsUpdates = append(b.statsUpdates, item)
		}
		delete(existMap, item.GUIDKey())
	}
	for _, item := range existMap {
		deletes = append(deletes, item)
	}

	return updates, deletes, nil
}
func isSchemaSyncManagedMetaType(metaType storepb.MetaType) bool {
	switch metaType {
	case storepb.MetaType_DATABASE,
		storepb.MetaType_SCHEMA,
		storepb.MetaType_TABLE,
		storepb.MetaType_COLUMN,
		storepb.MetaType_VIEW,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_FUNCTION,
		storepb.MetaType_PROCEDURE,
		storepb.MetaType_MATERIALIZED_VIEW,
		storepb.MetaType_SEQUENCE:
		return true
	default:
		return false
	}
}
func convertMetadataToGUID(prefix string, objectType storepb.MetaType, data *storepb.StoredMetadata) (string, error) {
	name, err := metaObjectName(objectType, data)
	if err != nil {
		return "", err
	}
	return buildGUID(prefix, name), nil
}
func getChildMetadataResources(parentGUID string, objectType storepb.MetaType, data *storepb.StoredMetadata) []*store.CreateMetaRegistryResourceMessage {
	switch objectType {
	case storepb.MetaType_TABLE:
		return buildColumnMetadataResources(parentGUID, data.GetTableMetadata().Columns)
	default:
		return nil
	}
}

// buildGUID appends new segments to a prefix. The first argument is an opaque
// prefix (an instance ID or an already-built GUID) and is passed through; the
// remaining arguments are names that get the separator escaped.
func buildGUID(prefix string, names ...string) string {
	return prefix + common.MetaGUIDSplit + common.BuildMetaGUID(names...)
}
func buildColumnMetadataResources(prefix string, cols []*storepb.ColumnMetadata) []*store.CreateMetaRegistryResourceMessage {
	resources := make([]*store.CreateMetaRegistryResourceMessage, 0, len(cols))
	for _, col := range cols {
		if col == nil {
			continue
		}
		columnMetadata, ok := proto.Clone(col).(*storepb.ColumnMetadata)
		if !ok {
			continue
		}
		resources = append(resources, &store.CreateMetaRegistryResourceMessage{
			MetaRegistryResource: store.MetaRegistryResource{
				GUID:       buildGUID(prefix, columnMetadata.Name),
				ObjectType: storepb.MetaType_COLUMN,
				Metadata: &storepb.StoredMetadata{
					Type: &storepb.StoredMetadata_ColumnMetadata{
						ColumnMetadata: columnMetadata,
					},
				},
			},
		})
	}
	return resources
}

// maxLoggedDeletedGUIDs bounds how many deleted GUIDs a single log record names.
const maxLoggedDeletedGUIDs = 20

// logSchemaSyncDeletion records the metadata resources a committed sync deleted.
// The driver snapshot is treated as the source of truth, so an empty or partial
// snapshot silently removes rows — MySQL's information_schema only lists objects
// the connecting user may see, with no error. This log is the only trace of that
// happening, so it is written after the commit: a rolled-back sync deleted
// nothing.
func logSchemaSyncDeletion(database string, deletes []*store.MetaRegistryResource, m *metrics.Metrics) {
	if len(deletes) == 0 {
		return
	}
	counts := map[storepb.MetaType]int{}
	guids := make([]string, 0, min(len(deletes), maxLoggedDeletedGUIDs))
	for _, item := range deletes {
		counts[item.ObjectType]++
		if len(guids) < maxLoggedDeletedGUIDs {
			guids = append(guids, item.GUID)
		}
	}
	// The caller runs this only after the commit, so these deletions are what the
	// destructive sync actually removed.
	for metaType, count := range counts {
		m.AddMetaResourcesDeleted(metaType, count)
	}
	slog.Warn("Deleting metadata resources missing from the synced snapshot",
		slog.String("database", database),
		slog.Int("count", len(deletes)),
		slog.Int("omittedGUIDs", len(deletes)-len(guids)),
		slog.Any("countByObjectType", counts),
		slog.Any("sampleGUIDs", guids))
}
