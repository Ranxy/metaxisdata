package schemasync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// definitionFetchConcurrency bounds how many SHOW CREATE queries run at once
	// against one database, so a full fetch cannot occupy the whole connection
	// pool of the target instance.
	definitionFetchConcurrency = 8
	// fullDefinitionObjectLimit is the number of schema-bearing objects above
	// which a full DDL refresh degrades to a rotating batch: one sync re-reads
	// that many objects and the cursor advances, so every object is refreshed
	// eventually instead of never. A full fetch is the default because a
	// StarRocks property change does not move our metadata hash, so a
	// change-only fetch would leave the stored DDL stale forever; the limit is a
	// safety valve for pathologically large databases.
	fullDefinitionObjectLimit = 2000
	// objectDefinitionFullRefreshInterval is how often a database re-reads the
	// DDL of objects the metadata diff did not change. Between refreshes a sync
	// reads definitions only for objects it already re-fetched metadata for, so
	// the per-sync DDL cost is proportional to the change, not to the database.
	// A property or comment change that leaves the metadata hash alone is picked
	// up within this interval instead of every sync.
	objectDefinitionFullRefreshInterval = 1 * time.Hour
)

// definitionFetchPlan is the set of objects whose DDL one sync reads, computed
// before the sync's transaction. nextCursor is where the rotation over a
// database above fullDefinitionObjectLimit stands after this plan; full records
// whether this plan was the periodic full refresh.
type definitionFetchPlan struct {
	items      []*store.CreateMetaRegistryResourceMessage
	full       bool
	rotating   bool
	nextCursor int
}

// planDefinitionFetch decides which definitions a sync reads. full asks for the
// periodic refresh of objects the metadata diff did not change; a large database
// refreshes one rotating window per such refresh rather than every object, and
// changed objects are always included so a real DDL change never waits for the
// rotation to reach it.
func (b *batchMetaCreate) planDefinitionFetch(full bool, cursor int) definitionFetchPlan {
	schemaBearing := b.schemaBearingResources()
	changed := b.changedDefinitionResources()

	plan := definitionFetchPlan{full: full}
	if !full {
		plan.items = changed
		return plan
	}
	if len(schemaBearing) <= fullDefinitionObjectLimit {
		plan.items = schemaBearing
		return plan
	}

	// Sort before windowing: the rotation cursor has to point at the same place
	// across syncs, and the snapshot's map iteration order is not stable.
	slices.SortFunc(schemaBearing, func(a, bx *store.CreateMetaRegistryResourceMessage) int {
		return strings.Compare(a.GUID, bx.GUID)
	})
	plan.rotating = true
	start := cursor % len(schemaBearing)
	if start < 0 {
		start += len(schemaBearing)
	}
	window := make([]*store.CreateMetaRegistryResourceMessage, 0, fullDefinitionObjectLimit)
	for i := 0; i < fullDefinitionObjectLimit && i < len(schemaBearing); i++ {
		window = append(window, schemaBearing[(start+i)%len(schemaBearing)])
	}
	plan.nextCursor = (start + len(window)) % len(schemaBearing)
	plan.items = mergeDefinitionResources(window, changed)
	slog.Info("Database exceeds the full definition fetch limit; refreshing a rotating batch",
		slog.Int("objects", len(schemaBearing)),
		slog.Int("limit", fullDefinitionObjectLimit),
		slog.Int("batch", len(window)))
	return plan
}

// schemaBearingResources returns every snapshot resource that can have its own DDL.
func (b *batchMetaCreate) schemaBearingResources() []*store.CreateMetaRegistryResourceMessage {
	schemaBearing := make([]*store.CreateMetaRegistryResourceMessage, 0, len(b.guidList))
	for _, item := range b.guidList {
		if supportsObjectDefinition(item.ObjectType) {
			schemaBearing = append(schemaBearing, item)
		}
	}
	return schemaBearing
}

// changedDefinitionResources returns the objects whose metadata changed in this sync.
func (b *batchMetaCreate) changedDefinitionResources() []*store.CreateMetaRegistryResourceMessage {
	changed := make([]*store.CreateMetaRegistryResourceMessage, 0, len(b.updates))
	for _, item := range b.updates {
		if supportsObjectDefinition(item.ObjectType) {
			changed = append(changed, item)
		}
	}
	return changed
}

// mergeDefinitionResources appends the items of extra that first is missing, so a
// changed object outside the rotation window is still read this sync.
func mergeDefinitionResources(first, extra []*store.CreateMetaRegistryResourceMessage) []*store.CreateMetaRegistryResourceMessage {
	seen := make(map[store.MetaGUIDKey]struct{}, len(first)+len(extra))
	merged := make([]*store.CreateMetaRegistryResourceMessage, 0, len(first)+len(extra))
	for _, group := range [][]*store.CreateMetaRegistryResourceMessage{first, extra} {
		for _, item := range group {
			key := item.GUIDKey()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, item)
		}
	}
	return merged
}

// objectDefinitionBatch is the DDL write set computed before the metadata
// transaction is opened: the definitions are fetched over the network, and that
// must not happen while the transaction holds row locks.
type objectDefinitionBatch struct {
	upserts   []*store.CreateMetaRegistrySchemaMessage
	deletes   []store.MetaGUIDKey
	unchanged int
	failed    int
}

// prepareObjectDefinitions captures the engine's own DDL for the candidates and
// computes the rows to write. It runs before the caller's transaction because
// the fetch issues up to one network round trip per object. It returns a nil
// batch when there is nothing to fetch.
func prepareObjectDefinitions(ctx context.Context, s *store.Store, reader db.ObjectDefinitionReader, candidates []*store.CreateMetaRegistryResourceMessage) (*objectDefinitionBatch, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	// The stored hashes are read first so the freshly fetched definitions can be
	// diffed against them: a full fetch re-reads every object on every sync, and
	// shipping every definition back would be pure waste when nothing changed.
	existing, err := s.ListMetaRegistrySchemaHashes(ctx, registryKeys(candidates))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list stored object definitions")
	}

	fetched, failed := fetchObjectDefinitions(ctx, reader, candidates)
	upserts, deletes, unchanged := diffDefinitions(fetched, existing)

	if failed > 0 {
		// The stored definitions of the failed objects are left untouched: a
		// transient fetch failure must not erase a good definition.
		slog.Warn("Failed to fetch some object definitions at this sync",
			slog.Int("failed", failed), slog.Int("total", len(candidates)))
	}
	return &objectDefinitionBatch{upserts: upserts, deletes: deletes, unchanged: unchanged, failed: failed}, nil
}

// write persists the fetched definitions inside the caller's transaction, after
// their metadata rows so the DDL rows stay a strict subset of them. It is a
// no-op when the engine has no definitions to fetch.
func (b *objectDefinitionBatch) write(ctx context.Context, s *store.Store, tx *sql.Tx) error {
	if b == nil {
		return nil
	}
	if err := s.BatchUpsertMetaRegistrySchema(ctx, tx, b.upserts); err != nil {
		return errors.Wrap(err, "failed to upsert object definitions")
	}
	if err := s.BatchDeleteMetaRegistrySchema(ctx, tx, b.deletes); err != nil {
		return errors.Wrap(err, "failed to delete object definitions")
	}
	slog.Debug("Synced object definitions",
		slog.Int("changed", len(b.upserts)),
		slog.Int("unchanged", b.unchanged),
		slog.Int("deleted", len(b.deletes)),
		slog.Int("failed", b.failed))
	return nil
}

// supportsObjectDefinition reports whether an object type can have its own DDL.
// COLUMN is excluded because a column has no definition of its own, and
// DATABASE/SCHEMA/SEQUENCE for the same reason. FUNCTION and PROCEDURE are
// included so a MySQL-family driver can opt in later; the driver answers
// ok=false for types it cannot produce.
func supportsObjectDefinition(metaType storepb.MetaType) bool {
	switch metaType {
	case storepb.MetaType_TABLE,
		storepb.MetaType_VIEW,
		storepb.MetaType_MATERIALIZED_VIEW,
		storepb.MetaType_FUNCTION,
		storepb.MetaType_PROCEDURE:
		return true
	default:
		return false
	}
}

// metaObjectSchema returns the schema segment of an object's GUID. A GUID is
// instance;database;schema;name, so the third segment is the container the
// definition reader qualifies the name with; the MySQL family stores "" there.
func metaObjectSchema(guid string) string {
	parts := common.SplitMetaGUID(guid)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// metaObjectName returns the plain object name carried by a registry row. It is
// the single place that maps an object type to its name field, shared by GUID
// construction and definition fetching.
func metaObjectName(objectType storepb.MetaType, data *storepb.StoredMetadata) (string, error) {
	switch objectType {
	case storepb.MetaType_DATABASE:
		return data.GetDatabaseSchemaMetadata().GetName(), nil
	case storepb.MetaType_SCHEMA:
		return data.GetSchemaMetadata().GetName(), nil
	case storepb.MetaType_TABLE:
		return data.GetTableMetadata().GetName(), nil
	case storepb.MetaType_VIEW:
		return data.GetViewMetadata().GetName(), nil
	case storepb.MetaType_EXTERNAL_TABLE:
		return data.GetExternalTableMetadata().GetName(), nil
	case storepb.MetaType_FUNCTION:
		return data.GetFunctionMetadata().GetName(), nil
	case storepb.MetaType_PROCEDURE:
		return data.GetProcedureMetadata().GetName(), nil
	case storepb.MetaType_MATERIALIZED_VIEW:
		return data.GetMaterializedViewMetadata().GetName(), nil
	case storepb.MetaType_SEQUENCE:
		return data.GetSequenceMetadata().GetName(), nil
	default:
		return "", errors.Errorf("unsupported meta type %v", objectType)
	}
}

func registryKeys(items []*store.CreateMetaRegistryResourceMessage) []store.MetaGUIDKey {
	keys := make([]store.MetaGUIDKey, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.GUIDKey())
	}
	return keys
}

// fetchedDefinition is one object's fresh DDL. ok is false when the engine
// reports no definition for the object.
type fetchedDefinition struct {
	item       *store.CreateMetaRegistryResourceMessage
	definition string
	ok         bool
}

// fetchObjectDefinitions reads the DDL of every candidate concurrently. Objects
// whose name cannot be mapped, whose fetch failed, or whose definition is not
// valid UTF-8 are excluded from the result and counted instead: a PostgreSQL
// text column rejects invalid byte sequences, and one bad row would abort the
// whole metadata transaction.
func fetchObjectDefinitions(ctx context.Context, reader db.ObjectDefinitionReader, items []*store.CreateMetaRegistryResourceMessage) ([]fetchedDefinition, int) {
	var (
		mu      sync.Mutex
		fetched = make([]fetchedDefinition, 0, len(items))
		failed  int
	)

	p := pool.New().WithMaxGoroutines(definitionFetchConcurrency)
	for _, item := range items {
		p.Go(func() {
			name, err := metaObjectName(item.ObjectType, item.Metadata)
			if err != nil {
				slog.Debug("Skipping object definition fetch", slog.String("guid", item.GUID), log.WithError(err))
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}

			definition, ok, err := reader.GetObjectDefinition(ctx, item.ObjectType, metaObjectSchema(item.GUID), name)
			if err != nil {
				slog.Debug("Failed to fetch object definition", slog.String("guid", item.GUID), log.WithError(err))
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			if ok && !utf8.ValidString(definition) {
				slog.Debug("Skipping non-UTF-8 object definition", slog.String("guid", item.GUID))
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}

			mu.Lock()
			fetched = append(fetched, fetchedDefinition{item: item, definition: definition, ok: ok})
			mu.Unlock()
		})
	}
	p.Wait()

	return fetched, failed
}

// diffDefinitions compares the freshly fetched definitions against the stored
// hashes and returns only what actually needs writing. Unchanged objects are
// dropped here rather than being sent to the database to be discarded by the
// upsert's hash guard, which keeps the per-sync payload proportional to the
// number of real DDL changes instead of the size of the database.
//
// An object the engine no longer has a definition for loses its row only when
// one exists. A failed fetch never reaches this function, so a transient failure
// cannot erase a good definition.
func diffDefinitions(fetched []fetchedDefinition, existing map[store.MetaGUIDKey][]byte) (upserts []*store.CreateMetaRegistrySchemaMessage, deletes []store.MetaGUIDKey, unchanged int) {
	for _, f := range fetched {
		key := f.item.GUIDKey()
		stored, hasStored := existing[key]

		if !f.ok || f.definition == "" {
			if hasStored {
				deletes = append(deletes, key)
			}
			continue
		}

		hash := sha256.Sum256([]byte(f.definition))
		if hasStored && bytes.Equal(stored, hash[:]) {
			unchanged++
			continue
		}
		upserts = append(upserts, &store.CreateMetaRegistrySchemaMessage{
			GUID:       f.item.GUID,
			ObjectType: f.item.ObjectType,
			Schema:     f.definition,
		})
	}
	return upserts, deletes, unchanged
}
