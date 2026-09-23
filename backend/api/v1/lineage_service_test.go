package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
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
