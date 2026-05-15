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
