package schemasync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func fingerprintResource(guid string, columns ...string) *store.CreateMetaRegistryResourceMessage {
	metaColumns := make([]*storepb.ColumnMetadata, 0, len(columns))
	for _, column := range columns {
		metaColumns = append(metaColumns, &storepb.ColumnMetadata{Name: column, Type: "text", Nullable: true})
	}
	return &store.CreateMetaRegistryResourceMessage{
		MetaRegistryResource: store.MetaRegistryResource{
			GUID:       guid,
			ObjectType: storepb.MetaType_TABLE,
			Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
				Columns: metaColumns,
			}}},
		},
	}
}

func TestSupportsSchemaFingerprint(t *testing.T) {
	t.Parallel()

	cases := map[storepb.MetaType]bool{
		storepb.MetaType_TABLE:             true,
		storepb.MetaType_EXTERNAL_TABLE:    true,
		storepb.MetaType_VIEW:              true,
		storepb.MetaType_MATERIALIZED_VIEW: true,
		storepb.MetaType_COLUMN:            false,
		storepb.MetaType_SCHEMA:            false,
		storepb.MetaType_DATABASE:          false,
		storepb.MetaType_SEQUENCE:          false,
		storepb.MetaType_FUNCTION:          false,
		storepb.MetaType_PROCEDURE:         false,
		storepb.MetaType_UNSPECIFIED:       false,
	}
	for metaType, want := range cases {
		require.Equal(t, want, supportsSchemaFingerprint(metaType), metaType.String())
	}
}

// The write set is what the sync sends to the database: a stored row that is
// already current is dropped, and only a row that existed before and now differs
// is reported as a change a monitor should hear about.
func TestDiffSchemaFingerprints(t *testing.T) {
	t.Parallel()

	unchanged := fingerprintResource("inst;db;;kept", "id")
	changed := fingerprintResource("inst;db;;changed", "id", "name")
	created := fingerprintResource("inst;db;;new", "id")
	items := []*store.CreateMetaRegistryResourceMessage{unchanged, changed, created}

	fingerprints := make(map[store.MetaGUIDKey][]byte, len(items))
	keys := make([]store.MetaGUIDKey, 0, len(items))
	for _, item := range items {
		fingerprint, ok := store.CalcSchemaFingerprint(item.Metadata)
		require.True(t, ok)
		key := item.GUIDKey()
		fingerprints[key] = fingerprint
		keys = append(keys, key)
	}

	stored := map[store.MetaGUIDKey][]byte{
		keys[0]: fingerprints[keys[0]],
		keys[1]: []byte("an older shape"),
	}
	batch := diffSchemaFingerprints(items, fingerprints, stored)

	require.Equal(t, 1, batch.unchanged)
	require.Len(t, batch.writes, 2, "the changed object and the new one are written")
	// The new object is backfill, not a change: nothing was written against an
	// older shape of it.
	require.Equal(t, []store.MetaGUIDKey{keys[1]}, batch.changed)
}

// A resource the type says has a shape but whose metadata does not is skipped
// instead of failing the database's sync.
func TestCollectSchemaFingerprintsSkipsResourcesWithoutAShape(t *testing.T) {
	t.Parallel()

	table := fingerprintResource("inst;db;;orders", "id")
	column := &store.CreateMetaRegistryResourceMessage{
		MetaRegistryResource: store.MetaRegistryResource{
			GUID:       "inst;db;;orders;id",
			ObjectType: storepb.MetaType_COLUMN,
			Metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ColumnMetadata{ColumnMetadata: &storepb.ColumnMetadata{Name: "id"}}},
		},
	}
	malformed := &store.CreateMetaRegistryResourceMessage{
		MetaRegistryResource: store.MetaRegistryResource{
			GUID:       "inst;db;;broken",
			ObjectType: storepb.MetaType_TABLE,
			Metadata:   &storepb.StoredMetadata{Type: &storepb.StoredMetadata_DatabaseSchemaMetadata{DatabaseSchemaMetadata: &storepb.DatabaseSchemaMetadata{Name: "db"}}},
		},
	}

	items, fingerprints, keys := collectSchemaFingerprints([]*store.CreateMetaRegistryResourceMessage{column, malformed, table})
	require.Len(t, items, 1)
	require.Len(t, keys, 1)
	require.Contains(t, fingerprints, table.GUIDKey())
}

// A monitored object whose structure moved produces one message for every check
// owner; the target's checks but no owner produce one for the administrators; an
// object nobody checks produces nothing at all.
func TestBuildSchemaChangeNotifications(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 10, 30, 0, 0, time.UTC)
	owned := store.MetaGUIDKey{GUID: "inst;db;;orders", ObjectType: storepb.MetaType_TABLE}
	unowned := store.MetaGUIDKey{GUID: "inst;db;;payments", ObjectType: storepb.MetaType_TABLE}
	unmonitored := store.MetaGUIDKey{GUID: "inst;db;;audit", ObjectType: storepb.MetaType_TABLE}
	targets := map[store.MetaGUIDKey]store.QualitySchemaChangeTarget{
		owned:       {RuleCount: 2, OwnerIDs: []int32{7, 9}},
		unowned:     {RuleCount: 1},
		unmonitored: {RuleCount: 0},
	}

	messages := buildSchemaChangeNotifications([]store.MetaGUIDKey{owned, unowned, unmonitored}, targets, now)
	require.Len(t, messages, 2, "the unmonitored object is not news to anybody")

	require.Equal(t, []int32{7, 9}, messages[0].recipients)
	require.False(t, messages[0].toAdmins)
	require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY, messages[0].message.GetType())
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, messages[0].message.GetSeverity())
	detail := messages[0].message.GetDataQuality()
	require.Equal(t, storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_TARGET_SCHEMA_CHANGED, detail.GetKind())
	require.Equal(t, owned.GUID, detail.GetTargetGuid())
	require.Equal(t, "inst/db/orders", detail.GetTargetPath())
	require.NotEmpty(t, messages[0].message.GetDedupeKey())

	require.True(t, messages[1].toAdmins, "checks with no owner are the administrator's to review")
	require.Empty(t, messages[1].recipients)

	// One event per occurrence window: two syncs of the same change inside the
	// hour share a key, and the next hour or another target is a new event.
	require.Equal(t, messages[0].message.GetDedupeKey(), buildSchemaChangeNotifications(
		[]store.MetaGUIDKey{owned}, targets, now.Add(10*time.Minute))[0].message.GetDedupeKey())
	require.NotEqual(t, messages[0].message.GetDedupeKey(), buildSchemaChangeNotifications(
		[]store.MetaGUIDKey{owned}, targets, now.Add(time.Hour+time.Minute))[0].message.GetDedupeKey())
	require.NotEqual(t, messages[0].message.GetDedupeKey(), buildSchemaChangeNotifications(
		[]store.MetaGUIDKey{unowned}, targets, now)[0].message.GetDedupeKey())
}

func TestSchemaFingerprintTargetPath(t *testing.T) {
	t.Parallel()

	require.Equal(t, "inst-1/app/orders", schemaFingerprintTargetPath("inst-1;app;;orders"))
	require.Equal(t, "inst-1/app/public/orders", schemaFingerprintTargetPath("inst-1;app;public;orders"))
	require.Equal(t, "", schemaFingerprintTargetPath(""))
}
