package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The API reports the relation type the analyzers recorded. Collapsing every kind
// to INDIRECT, as it used to, hid the difference between a join key, an
// aggregation and a set operation.
func TestConvertRelationTypeKeepsEveryKind(t *testing.T) {
	require.Equal(t, v1pb.RelationType_DIRECT, convertRelationType(model.RelationTypeDirect))
	require.Equal(t, v1pb.RelationType_INDIRECT, convertRelationType(model.RelationTypeIndirect))
	require.Equal(t, v1pb.RelationType_JOIN, convertRelationType(model.RelationTypeJoin))
	require.Equal(t, v1pb.RelationType_GROUP, convertRelationType(model.RelationTypeGroup))
	require.Equal(t, v1pb.RelationType_UNION, convertRelationType(model.RelationTypeUnion))
	require.Equal(t, v1pb.RelationType_INTERSECT, convertRelationType(model.RelationTypeIntersect))
	require.Equal(t, v1pb.RelationType_EXCEPT, convertRelationType(model.RelationTypeExcept))
	require.Equal(t, v1pb.RelationType_UNKNOWN, convertRelationType(model.RelationTypeUnknown))

	// A stored value outside the enum is not a relation kind, so it is reported as
	// unspecified rather than as one of them.
	require.Equal(t, v1pb.RelationType_RELATION_TYPE_UNSPECIFIED, convertRelationType(model.RelationType(0)))
}

// The counts response is positional: a caller pairs its request list with the
// result list, so the order is the request order with duplicates and blanks
// removed once, not the store's map order.
func TestDistinctLineageCountGuidsKeepsFirstRequestOrder(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		[]string{"b", "a", "c"},
		distinctLineageCountGuids([]string{"b", "", "a", "b", "", "c", "a"}),
	)
	require.Empty(t, distinctLineageCountGuids(nil))
	require.Empty(t, distinctLineageCountGuids([]string{"", ""}))
}

// An object with no relations is still a node the caller asked about, so it is
// reported with zero counts rather than dropped from the response.
func TestBuildLineageCountsZeroFillsEveryRequestedGuid(t *testing.T) {
	t.Parallel()

	counts := map[string]*store.ColumnLineageCount{
		"a": {Upstream: 5, Downstream: 2},
		"c": {Downstream: 7},
		"d": {}, // present but with no edges in either direction
	}

	result := buildLineageCounts([]string{"a", "b", "c", "d"}, counts)
	require.Len(t, result, 4)
	require.Equal(t, &v1pb.LineageCount{Guid: "a", UpstreamCount: 5, DownstreamCount: 2}, result[0])
	require.Equal(t, &v1pb.LineageCount{Guid: "b"}, result[1])
	require.Equal(t, &v1pb.LineageCount{Guid: "c", DownstreamCount: 7}, result[2])
	require.Equal(t, &v1pb.LineageCount{Guid: "d"}, result[3])
}

// A lineage edge can name a relation the registry has no row for - the writer has
// not synced yet, or the reference is to an object the sync excludes - and the
// graph has to render that node rather than fail the request. The node keeps the
// type the edge claims and invents no metadata.
func TestLineageNodeWithMissingMeta(t *testing.T) {
	node := lineageNodeWithMissingMeta("mysql-dev-1;e2e_ods;;base", v1pb.MetaType_TABLE)
	assert.Equal(t, "mysql-dev-1;e2e_ods;;base", node.GUID)
	assert.Equal(t, storepb.MetaType_TABLE, node.ObjectType)
	assert.Nil(t, node.Metadata, "an unresolved node must not invent metadata")

	fallback := lineageNodeWithMissingMeta("mysql-dev-1;information_schema;;COLUMNS", v1pb.MetaType_UNSPECIFIED)
	assert.Equal(t, storepb.MetaType_TABLE, fallback.ObjectType, "an untyped edge still yields a renderable node")

	view := lineageNodeWithMissingMeta("test-pg-1;e2e;e2e_dwd;v_order_base", v1pb.MetaType_VIEW)
	assert.Equal(t, storepb.MetaType_VIEW, view.ObjectType, "the type the edge claims is kept")
}
