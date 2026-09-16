package v1

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeGraph is a column lineage graph the traversal can walk without a
// database. Edges are stored once and returned from whichever end is asked for,
// which is how the real store behaves.
type fakeGraph struct {
	edges []*store.ColumnLineage
	// fetched counts how many times each object was expanded, so a test can
	// prove the traversal does not revisit an object.
	fetched map[string]int
	// err fails the expansion of one object.
	err map[string]error
}

// fetcher mirrors what the service does: the direction and the budget are
// applied here, not by the traversal, and it returns at most budget+1 so the
// traversal can tell "no more edges" from "the ceiling cut it short".
func (g *fakeGraph) fetcher(includeSource, includeTarget bool) lineageGraphFetcher {
	return func(_ context.Context, guid string, budget int) ([]*store.ColumnLineage, error) {
		if g.fetched == nil {
			g.fetched = map[string]int{}
		}
		g.fetched[guid]++
		if err := g.err[guid]; err != nil {
			return nil, err
		}
		var found []*store.ColumnLineage
		for _, edge := range g.edges {
			if len(found) >= budget+1 {
				break
			}
			// Upstream: the object is the target of the edge.
			if includeSource && edge.TargetGUID == guid {
				found = append(found, edge)
			}
			if includeTarget && edge.SourceGUID == guid {
				found = append(found, edge)
			}
		}
		return found, nil
	}
}

func edge(id int64, source, target string) *store.ColumnLineage {
	return &store.ColumnLineage{
		ID:           id,
		SourceGUID:   source,
		SourceColumn: "c",
		TargetGUID:   target,
		TargetColumn: "c",
	}
}

func graphGUIDs(nodes []*lineageGraphNode) map[string]int32 {
	result := make(map[string]int32, len(nodes))
	for _, node := range nodes {
		result[node.guid] = node.distance
	}
	return result
}

func TestBuildLineageGraphWalksBothDirections(t *testing.T) {
	t.Parallel()

	// d -> a -> b -> c
	graph := &fakeGraph{edges: []*store.ColumnLineage{
		edge(1, "d", "a"),
		edge(2, "a", "b"),
		edge(3, "b", "c"),
	}}

	nodes, edges, depthReached, truncated, err := buildLineageGraph(context.Background(), "a", 5,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(true, true))
	require.NoError(t, err)
	require.False(t, truncated)
	require.Equal(t, int32(2), depthReached, "the deepest node is two hops from the root")
	require.Len(t, edges, 3)
	require.Equal(t, map[string]int32{"a": 0, "d": -1, "b": 1, "c": 2}, graphGUIDs(nodes))
}

func TestBuildLineageGraphHonoursTheDirection(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{edges: []*store.ColumnLineage{
		edge(1, "d", "a"),
		edge(2, "a", "b"),
	}}

	upstream, _, _, _, err := buildLineageGraph(context.Background(), "a", 5,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(true, false))
	require.NoError(t, err)
	require.Equal(t, map[string]int32{"a": 0, "d": -1}, graphGUIDs(upstream))

	downstream, _, _, _, err := buildLineageGraph(context.Background(), "a", 5,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(false, true))
	require.NoError(t, err)
	require.Equal(t, map[string]int32{"a": 0, "b": 1}, graphGUIDs(downstream))
}

func TestBuildLineageGraphStopsAtTheRequestedDepth(t *testing.T) {
	t.Parallel()

	// a -> b -> c -> d
	graph := &fakeGraph{edges: []*store.ColumnLineage{
		edge(1, "a", "b"),
		edge(2, "b", "c"),
		edge(3, "c", "d"),
	}}

	nodes, edges, depthReached, truncated, err := buildLineageGraph(context.Background(), "a", 2,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(false, true))
	require.NoError(t, err)
	require.False(t, truncated, "stopping at the requested depth is not truncation")
	require.Equal(t, int32(2), depthReached, "the requested depth was reached")
	require.Equal(t, map[string]int32{"a": 0, "b": 1, "c": 2}, graphGUIDs(nodes))
	require.Len(t, edges, 2)
}

// A cycle must terminate, and the same edge must not be listed twice just
// because both of its ends were expanded.
func TestBuildLineageGraphTerminatesOnCyclesAndDeduplicatesEdges(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{edges: []*store.ColumnLineage{
		edge(1, "a", "b"),
		edge(2, "b", "a"),
	}}

	nodes, edges, _, truncated, err := buildLineageGraph(context.Background(), "a", 10,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(true, true))
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, nodes, 2)
	require.Len(t, edges, 2, "each edge is reported once")
	require.Equal(t, 1, graph.fetched["a"])
	require.Equal(t, 1, graph.fetched["b"])
}

// A root with nothing attached has reached no depth at all; reporting 1 would
// claim the graph has a level it does not.
func TestBuildLineageGraphReportsZeroDepthForAnIsolatedRoot(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{}
	nodes, edges, depthReached, truncated, err := buildLineageGraph(context.Background(), "a", 5,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(true, true))

	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, nodes, 1)
	require.Empty(t, edges)
	require.Zero(t, depthReached)
}

// A self edge is not lineage: recording it would add a row that connects
// nothing.
func TestBuildLineageGraphDropsSelfEdges(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{edges: []*store.ColumnLineage{edge(1, "a", "a"), edge(2, "a", "b")}}
	nodes, edges, _, _, err := buildLineageGraph(context.Background(), "a", 5,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(true, true))

	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Len(t, edges, 1)
	require.Equal(t, "b", edges[0].TargetGUID)
}

func TestBuildLineageGraphTruncatesOnTheNodeCeiling(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{edges: []*store.ColumnLineage{
		edge(1, "a", "b"),
		edge(2, "a", "c"),
		edge(3, "a", "d"),
	}}

	nodes, _, _, truncated, err := buildLineageGraph(context.Background(), "a", 5,
		lineageGraphLimits{nodes: 2, edges: 100}, graph.fetcher(false, true))
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, nodes, 2, "the ceiling is not exceeded")
}

func TestBuildLineageGraphTruncatesOnTheEdgeCeiling(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{edges: []*store.ColumnLineage{
		edge(1, "a", "b"),
		edge(2, "a", "c"),
		edge(3, "a", "d"),
	}}

	_, edges, _, truncated, err := buildLineageGraph(context.Background(), "a", 5,
		lineageGraphLimits{nodes: 100, edges: 2}, graph.fetcher(false, true))
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, edges, 2)
}

// A slow expansion is reported as a partial graph rather than as a failure: the
// caller asked for a bounded picture and that is still useful.
func TestBuildLineageGraphTreatsACancelledContextAsTruncation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	graph := &fakeGraph{edges: []*store.ColumnLineage{edge(1, "a", "b")}}
	nodes, _, _, truncated, err := buildLineageGraph(ctx, "a", 5,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(true, true))

	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, nodes, 1)
	require.Zero(t, graph.fetched["a"], "the walk stopped before expanding anything")
}

func TestBuildLineageGraphSurfacesARealFetchFailure(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{
		edges: []*store.ColumnLineage{edge(1, "a", "b")},
		err:   map[string]error{"a": errors.New("connection reset")},
	}

	_, _, _, _, err := buildLineageGraph(context.Background(), "a", 5,
		lineageGraphLimits{nodes: 100, edges: 100}, graph.fetcher(true, true))
	require.ErrorContains(t, err, "connection reset")
}

func TestColumnLineageKeyIgnoresTheRowIdentity(t *testing.T) {
	t.Parallel()

	first := &store.ColumnLineage{ID: 1, SourceGUID: "a", SourceColumn: "x", TargetGUID: "b", TargetColumn: "y"}
	second := &store.ColumnLineage{ID: 99, SourceGUID: "a", SourceColumn: "x", TargetGUID: "b", TargetColumn: "y"}
	require.Equal(t, columnLineageKey(first), columnLineageKey(second))

	other := &store.ColumnLineage{ID: 1, SourceGUID: "a", SourceColumn: "x", TargetGUID: "b", TargetColumn: "z"}
	require.NotEqual(t, columnLineageKey(first), columnLineageKey(other))

	require.Empty(t, columnLineageKey(&store.ColumnLineage{}), "an edge without endpoints has no identity")
}

func TestConvertLineageGraphNodeDescribesUnsynchronisedObjects(t *testing.T) {
	t.Parallel()

	// A MySQL table: the schema segment is empty, so the name is the last one.
	node := convertLineageGraphNode(&lineageGraphNode{guid: "1;shop;;orders", distance: -2}, v1pb.MetaType_TABLE, nil)
	require.Equal(t, "1;shop;;orders", node.GetGuid())
	require.Equal(t, v1pb.MetaType_TABLE, node.GetMetaType())
	require.Equal(t, "orders", node.GetName())
	require.Equal(t, "1", node.GetInstanceId())
	require.Equal(t, "shop", node.GetDatabase())
	require.Empty(t, node.GetSchema())
	require.Equal(t, int32(-2), node.GetDistance())

	// A database-level object has only two segments.
	database := convertLineageGraphNode(&lineageGraphNode{guid: "1;shop"}, v1pb.MetaType_UNSPECIFIED, nil)
	require.Equal(t, "shop", database.GetName())
	require.Equal(t, "shop", database.GetDatabase())

	// An external dataset is named by its dataset info, not by its GUID.
	external := convertLineageGraphNode(&lineageGraphNode{guid: "openlineage;ns;db.schema.t"}, v1pb.MetaType_EXTERNAL_DATASET, &v1pb.ExternalDatasetInfo{
		Guid: "openlineage;ns;db.schema.t",
		Name: "db.schema.t",
	})
	require.Equal(t, "db.schema.t", external.GetName())
	require.Equal(t, v1pb.MetaType_EXTERNAL_DATASET, external.GetMetaType())
}
