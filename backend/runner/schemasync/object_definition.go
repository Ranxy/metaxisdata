package schemasync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"log/slog"
	"sync"
	"unicode/utf8"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"

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
	// which a database degrades to fetching definitions only for new/changed
	// objects. A full fetch is the default because a StarRocks property change
	// does not move our metadata hash, so a change-only fetch would leave the
	// stored DDL stale forever; the limit is a safety valve for pathologically
	// large databases.
	fullDefinitionObjectLimit = 2000
)

// syncObjectDefinitions captures the engine's own DDL for the objects in the
// current snapshot and writes it inside the caller's transaction, so a
// rolled-back sync cannot leave a definition behind that its metadata row does
// not have. It is a no-op for engines that do not implement
// db.ObjectDefinitionReader.
func syncObjectDefinitions(ctx context.Context, s *store.Store, tx *sql.Tx, reader db.ObjectDefinitionReader, bmc *batchMetaCreate) error {
	candidates := bmc.definitionCandidates()
	if len(candidates) == 0 {
		return nil
	}

	// The stored hashes are read first so the freshly fetched definitions can be
	// diffed against them: a full fetch re-reads every object on every sync, and
	// shipping every definition back would be pure waste when nothing changed.
	existing, err := s.ListMetaRegistrySchemaHashes(ctx, registryKeys(candidates))
	if err != nil {
		return errors.Wrap(err, "failed to list stored object definitions")
	}

	fetched, failed := fetchObjectDefinitions(ctx, reader, candidates)
	upserts, deletes, unchanged := diffDefinitions(fetched, existing)

	if failed > 0 {
		// The stored definitions of the failed objects are left untouched: a
		// transient fetch failure must not erase a good definition.
		slog.Warn("Failed to fetch some object definitions at this sync",
			slog.Int("failed", failed), slog.Int("total", len(candidates)))
	}
	if err := s.BatchUpsertMetaRegistrySchema(ctx, tx, upserts); err != nil {
		return errors.Wrap(err, "failed to upsert object definitions")
	}
	if err := s.BatchDeleteMetaRegistrySchema(ctx, tx, deletes); err != nil {
		return errors.Wrap(err, "failed to delete object definitions")
	}
	slog.Debug("Synced object definitions",
		slog.Int("changed", len(upserts)),
		slog.Int("unchanged", unchanged),
		slog.Int("deleted", len(deletes)),
		slog.Int("failed", failed))
	return nil
}

// definitionCandidates returns the objects eligible for a DDL fetch: every
// schema-bearing resource in the snapshot, or only the changed ones when the
// snapshot exceeds fullDefinitionObjectLimit.
func (b *batchMetaCreate) definitionCandidates() []*store.CreateMetaRegistryResourceMessage {
	schemaBearing := make([]*store.CreateMetaRegistryResourceMessage, 0, len(b.guidList))
	for _, item := range b.guidList {
		if supportsObjectDefinition(item.ObjectType) {
			schemaBearing = append(schemaBearing, item)
		}
	}
	if len(schemaBearing) <= fullDefinitionObjectLimit {
		return schemaBearing
	}

	changed := make([]*store.CreateMetaRegistryResourceMessage, 0, len(b.updates))
	for _, item := range b.updates {
		if supportsObjectDefinition(item.ObjectType) {
			changed = append(changed, item)
		}
	}
	slog.Warn("Database exceeds the full definition fetch limit; only changed objects will have their DDL refreshed",
		slog.Int("objects", len(schemaBearing)),
		slog.Int("limit", fullDefinitionObjectLimit),
		slog.Int("changed", len(changed)))
	return changed
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

			definition, ok, err := reader.GetObjectDefinition(ctx, item.ObjectType, name)
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
