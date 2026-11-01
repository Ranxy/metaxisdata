package schemasync

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// supportsSchemaFingerprint reports whether an object type carries a structure
// fingerprint: the shapes a data-quality check can be written against. A column,
// schema, database, sequence, function or procedure has no shape of its own.
func supportsSchemaFingerprint(metaType storepb.MetaType) bool {
	switch metaType {
	case storepb.MetaType_TABLE,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_VIEW,
		storepb.MetaType_MATERIALIZED_VIEW:
		return true
	case storepb.MetaType_COLUMN,
		storepb.MetaType_SCHEMA,
		storepb.MetaType_DATABASE,
		storepb.MetaType_SEQUENCE,
		storepb.MetaType_FUNCTION,
		storepb.MetaType_PROCEDURE,
		storepb.MetaType_UNSPECIFIED:
		return false
	default:
		return false
	}
}

// schemaFingerprintBatch is the fingerprint write set of one database sync,
// computed before the metadata transaction is opened.
//
// changed holds the objects whose fingerprint moved away from a row that already
// existed. A row written for the first time is not a change: a brand-new object
// has no check written against an older shape, and the first sync after the
// fingerprint table landed is backfill, not news.
type schemaFingerprintBatch struct {
	writes    []*store.CreateMetaRegistrySchemaFingerprintMessage
	changed   []store.MetaGUIDKey
	unchanged int
}

// prepareSchemaFingerprints computes the structure fingerprint of every
// schema-bearing object in the snapshot and diffs it against the stored rows. It
// is pure computation plus one indexed read, so it runs before the metadata
// transaction is opened, like the metadata and DDL diffs.
func prepareSchemaFingerprints(ctx context.Context, s *store.Store, candidates []*store.CreateMetaRegistryResourceMessage) (*schemaFingerprintBatch, error) {
	items, fingerprints, keys := collectSchemaFingerprints(candidates)
	if len(items) == 0 {
		return &schemaFingerprintBatch{}, nil
	}

	stored, err := s.ListMetaRegistrySchemaFingerprints(ctx, keys)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list stored schema fingerprints")
	}

	batch := diffSchemaFingerprints(items, fingerprints, stored)
	slog.Debug("Computed schema fingerprints",
		slog.Int("written", len(batch.writes)),
		slog.Int("unchanged", batch.unchanged),
		slog.Int("changed", len(batch.changed)))
	return batch, nil
}

// collectSchemaFingerprints reduces the snapshot to the objects that have a
// shape and computes each fingerprint. A resource whose type says it has a shape
// but whose metadata does not is skipped rather than failing the database's sync:
// the fingerprint is a read model for data quality, and one malformed object must
// not stop the registry from converging.
func collectSchemaFingerprints(candidates []*store.CreateMetaRegistryResourceMessage) ([]*store.CreateMetaRegistryResourceMessage, map[store.MetaGUIDKey][]byte, []store.MetaGUIDKey) {
	items := make([]*store.CreateMetaRegistryResourceMessage, 0, len(candidates))
	fingerprints := make(map[store.MetaGUIDKey][]byte, len(candidates))
	keys := make([]store.MetaGUIDKey, 0, len(candidates))
	for _, item := range candidates {
		if !supportsSchemaFingerprint(item.ObjectType) {
			continue
		}
		fingerprint, ok := store.CalcSchemaFingerprint(item.Metadata)
		if !ok {
			slog.Warn("Skipping an object whose metadata has no shape",
				slog.String("guid", item.GUID),
				slog.String("objectType", item.ObjectType.String()))
			continue
		}
		key := item.GUIDKey()
		items = append(items, item)
		fingerprints[key] = fingerprint
		keys = append(keys, key)
	}
	return items, fingerprints, keys
}

// diffSchemaFingerprints is the pure write set: only a fingerprint that differs
// from a stored row is written, and only an object that already had a row counts
// as changed. The first write of a row is the backfill.
func diffSchemaFingerprints(items []*store.CreateMetaRegistryResourceMessage, fingerprints map[store.MetaGUIDKey][]byte, stored map[store.MetaGUIDKey][]byte) *schemaFingerprintBatch {
	batch := &schemaFingerprintBatch{}
	for _, item := range items {
		key := item.GUIDKey()
		fingerprint := fingerprints[key]
		previous, exists := stored[key]
		if exists && bytes.Equal(previous, fingerprint) {
			batch.unchanged++
			continue
		}
		batch.writes = append(batch.writes, &store.CreateMetaRegistrySchemaFingerprintMessage{
			GUID:        item.GUID,
			ObjectType:  item.ObjectType,
			Fingerprint: fingerprint,
		})
		if exists {
			batch.changed = append(batch.changed, key)
		}
	}
	return batch
}

// write persists the fingerprints inside the caller's transaction, after the
// metadata rows they describe, so a rolled-back sync cannot leave a fingerprint
// behind that its metadata row does not have.
func (b *schemaFingerprintBatch) write(ctx context.Context, s *store.Store, tx *sql.Tx) error {
	if b == nil {
		return nil
	}
	if err := s.BatchUpsertMetaRegistrySchemaFingerprints(ctx, tx, b.writes); err != nil {
		return errors.Wrap(err, "failed to upsert schema fingerprints")
	}
	return nil
}

// schemaChangeNotification is one message a sync sends after it committed: who
// receives it, and whether it is addressed to the workspace administrators
// because the target has checks but none of them has an owner.
type schemaChangeNotification struct {
	message    *storepb.Notification
	recipients []int32
	toAdmins   bool
}

// buildSchemaChangeNotifications maps the changed objects to the messages their
// monitors receive. An object with no live check produces nothing: nobody watches
// it, so a structure change there is not an event anyone has to review.
func buildSchemaChangeNotifications(changed []store.MetaGUIDKey, targets map[store.MetaGUIDKey]store.QualitySchemaChangeTarget, now time.Time) []schemaChangeNotification {
	notifications := make([]schemaChangeNotification, 0, len(changed))
	for _, key := range changed {
		target, ok := targets[key]
		if !ok || target.RuleCount == 0 {
			continue
		}
		detail := &storepb.DataQualityDetail{
			Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_TARGET_SCHEMA_CHANGED,
			TargetGuid: key.GUID,
			TargetPath: schemaFingerprintTargetPath(key.GUID),
		}
		message := notification.DataQualityMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, detail)
		message.DedupeKey = notification.DataQualityDedupeKey(detail.GetKind(), key.GUID, now)
		notifications = append(notifications, schemaChangeNotification{
			message:    message,
			recipients: target.OwnerIDs,
			toAdmins:   len(target.OwnerIDs) == 0,
		})
	}
	return notifications
}

// reportSchemaFingerprintChanges tells the owners of the checks watching an
// object that its structure moved. It runs after the commit: a rolled-back sync
// changed nothing, so it must not page anyone.
//
// Delivery is best effort, like every other notification a runner sends: the
// sync's own result stands, and the next change reports itself.
func (s *Syncer) reportSchemaFingerprintChanges(ctx context.Context, batch *schemaFingerprintBatch) {
	if s.notifier == nil || batch == nil || len(batch.changed) == 0 {
		return
	}

	targets, err := s.store.ListQualitySchemaChangeTargets(ctx, batch.changed)
	if err != nil {
		slog.Warn("Failed to resolve the owners of changed schema targets", log.WithError(err))
		return
	}

	for _, item := range buildSchemaChangeNotifications(batch.changed, targets, time.Now()) {
		if item.toAdmins {
			// The checks exist but none has an owner: the administrator is the one
			// who can review or assign them.
			if err := s.notifier.SendToWorkspaceAdmins(ctx, item.message); err != nil {
				notification.LogFailure(err, slog.String("quality_event", item.message.GetDataQuality().GetKind().String()))
			}
			continue
		}
		if err := s.notifier.SendToRecipients(ctx, item.message, item.recipients); err != nil {
			notification.LogFailure(err, slog.String("quality_event", item.message.GetDataQuality().GetKind().String()))
		}
	}
}

// schemaFingerprintTargetPath renders a registry GUID as the display path a
// data-quality target carries: instance/database/schema/table, skipping the
// containers an engine does not have. It is display text, not an identity.
func schemaFingerprintTargetPath(guid string) string {
	segments := make([]string, 0, 4)
	for _, segment := range common.SplitMetaGUID(guid) {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return strings.Join(segments, "/")
}
