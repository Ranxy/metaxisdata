package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"

	"github.com/Ranxy/metaxisdata/backend/common"
)

func TestAppendGUIDSubtreeConditionEscapesLikeWildcards(t *testing.T) {
	t.Parallel()

	_, args := appendGUIDSubtreeCondition(nil, nil, "r.guid", `inst;db_1%prod`)
	require.Len(t, args, 2)
	require.Equal(t, `inst;db_1%prod`, args[0])
	require.Equal(t, `inst;db\_1\%prod`+common.MetaGUIDSplit+`%`, args[1])
}

func TestGetNextLevelObjectTypeIncludesManualSQLUnderSchema(t *testing.T) {
	t.Parallel()

	require.Contains(
		t,
		getNextLevelObjectType(storepb.MetaType_SCHEMA),
		storepb.MetaType_MANUAL_SQL,
	)
}

func TestMetaRegistryGUIDCacheKeyIncludesObjectType(t *testing.T) {
	t.Parallel()

	guid := "inst;db;public;orders"
	tableType, viewType := storepb.MetaType_TABLE, storepb.MetaType_VIEW
	// A GUID may exist under several object types, so the cache key must not be
	// the bare GUID: two different types must not share an entry.
	tableFind := &FindMetaRegistryResourceMessage{GUID: &guid, ObjectType: &tableType}
	viewFind := &FindMetaRegistryResourceMessage{GUID: &guid, ObjectType: &viewType}

	tableKey, tableOK := metaRegistryGUIDCacheKey(tableFind)
	viewKey, viewOK := metaRegistryGUIDCacheKey(viewFind)
	require.True(t, tableOK)
	require.True(t, viewOK)
	require.NotEqual(t, tableKey, viewKey)
	require.Equal(t, storepb.MetaType_TABLE, tableKey.ObjectType)
	require.Equal(t, storepb.MetaType_VIEW, viewKey.ObjectType)

	// A GUID-only lookup is not cacheable: it could match any object type.
	_, ok := metaRegistryGUIDCacheKey(&FindMetaRegistryResourceMessage{GUID: &guid})
	require.False(t, ok)
}

func TestBuildMetaRegistryHistoryMutations(t *testing.T) {
	t.Parallel()

	newItem := &CreateMetaRegistryResourceMessage{MetaRegistryResource: MetaRegistryResource{GUID: "inst;db;public;orders", ObjectType: storepb.MetaType_TABLE, MetaHash: []byte("new")}}
	unchangedItem := &CreateMetaRegistryResourceMessage{MetaRegistryResource: MetaRegistryResource{GUID: "inst;db;public;users", ObjectType: storepb.MetaType_TABLE, MetaHash: []byte("same")}}
	changedItem := &CreateMetaRegistryResourceMessage{MetaRegistryResource: MetaRegistryResource{GUID: "inst;db;public;items", ObjectType: storepb.MetaType_TABLE, MetaHash: []byte("after")}}

	toClose, toOpen := buildMetaRegistryHistoryMutations(map[MetaGUIDKey]*MetaRegistryHistory{
		{GUID: unchangedItem.GUID, ObjectType: unchangedItem.ObjectType}: {GUID: unchangedItem.GUID, ObjectType: unchangedItem.ObjectType, MetaHash: []byte("same")},
		{GUID: changedItem.GUID, ObjectType: changedItem.ObjectType}:     {GUID: changedItem.GUID, ObjectType: changedItem.ObjectType, MetaHash: []byte("before")},
	}, []*CreateMetaRegistryResourceMessage{newItem, unchangedItem, changedItem})

	require.Len(t, toClose, 1)
	require.Equal(t, changedItem.GUID, toClose[0].GUID)
	require.Len(t, toOpen, 2)
	require.Equal(t, []string{newItem.GUID, changedItem.GUID}, []string{toOpen[0].GUID, toOpen[1].GUID})
}
