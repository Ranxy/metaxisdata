package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

func TestAppendGUIDSubtreeCondition(t *testing.T) {
	t.Parallel()

	where, args := appendGUIDSubtreeCondition(nil, nil, "meta_registry_resource.guid", "inst;db1")
	require.Len(t, where, 1)
	require.Equal(t, "(meta_registry_resource.guid = $1 OR meta_registry_resource.guid LIKE $2 ESCAPE '\\\\')", where[0])
	require.Equal(t, []any{"inst;db1", "inst;db1" + common.MetaGUIDSplit + "%"}, args)
}

func TestAppendGUIDSubtreeConditionEscapesLikeWildcards(t *testing.T) {
	t.Parallel()

	_, args := appendGUIDSubtreeCondition(nil, nil, "r.guid", `inst;db_1%prod`)
	require.Len(t, args, 2)
	require.Equal(t, `inst;db_1%prod`, args[0])
	require.Equal(t, `inst;db\_1\%prod`+common.MetaGUIDSplit+`%`, args[1])
}
