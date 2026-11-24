package lineage

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func TestParseDirectionMapsTheTransportWords(t *testing.T) {
	t.Parallel()

	upstream, err := ParseDirection("up")
	require.NoError(t, err)
	require.Equal(t, v1pb.LineageType_SOURCE, upstream)

	downstream, err := ParseDirection(" downstream ")
	require.NoError(t, err)
	require.Equal(t, v1pb.LineageType_TARGET, downstream)

	both, err := ParseDirection("")
	require.NoError(t, err)
	require.Equal(t, v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED, both)

	_, err = ParseDirection("sideways")
	require.Error(t, err)
}

func TestFilterGraphByColumnKeepsTheEdgesAndTheirNodes(t *testing.T) {
	t.Parallel()

	graph := &v1pb.GetLineageGraphResponse{
		RootGuid: "1;shop;;orders",
		Nodes: []*v1pb.LineageNode{
			{Guid: "1;shop;;orders"},
			{Guid: "1;shop;;users"},
			{Guid: "1;shop;;unrelated"},
		},
		Edges: []*v1pb.LineageRelation{
			{SourceGuid: "1;shop;;users", TargetGuid: "1;shop;;orders", SourceColumn: "id", TargetColumn: "user_id"},
			{SourceGuid: "1;shop;;unrelated", TargetGuid: "1;shop;;orders", SourceColumn: "x", TargetColumn: "y"},
		},
	}

	filtered := filterGraphByColumn(graph, "user_id")
	require.Len(t, filtered.GetEdges(), 1)
	require.Equal(t, "1;shop;;users", filtered.GetEdges()[0].GetSourceGuid())
	require.Len(t, filtered.GetNodes(), 2)
}
