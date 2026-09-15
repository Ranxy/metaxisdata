package v1

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// defaultLineageGraphDepth is how far the graph is expanded when the caller
	// does not say.
	defaultLineageGraphDepth = 3
	// maxLineageGraphDepth bounds the walk.
	maxLineageGraphDepth = 10
	// maxLineageGraphNodes and maxLineageGraphEdges bound one response. A graph
	// can be huge (a whole database referencing itself in a cycle), and the
	// client asked for a picture, not for the entire history of the workspace.
	maxLineageGraphNodes = 500
	maxLineageGraphEdges = 10000
	// lineageGraphBudget bounds the wall clock of one traversal. Hitting it is
	// reported as a truncation, not as a failure: a partial graph is still
	// useful and the client can narrow the request.
	lineageGraphBudget = 15 * time.Second
)

// GetLineageGraph returns the multi-level lineage around one object in a single
// call. GetLineage answers one hop at a time, which forces every client to walk
// the graph itself; this walks it on the server, where the per-object column
// lineage is already fully paginated.
func (s *LineageService) GetLineageGraph(ctx context.Context, req *connect.Request[v1pb.GetLineageGraphRequest]) (*connect.Response[v1pb.GetLineageGraphResponse], error) {
	if req.Msg.GetGuid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}
	// The root has to exist, which is also what rejects a typo before any
	// traversal work happens. External dataset GUIDs are allowed here.
	if _, err := s.getLineageMeta(ctx, req.Msg.GetGuid(), req.Msg.GetMetaType()); err != nil {
		return nil, err
	}

	depth := int(req.Msg.GetDepth())
	if depth == 0 {
		depth = defaultLineageGraphDepth
	}
	if depth < 1 || depth > maxLineageGraphDepth {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("depth must be between 1 and %d", maxLineageGraphDepth))
	}
	includeSource := shouldIncludeSource(req.Msg.GetLineageType())
	includeTarget := shouldIncludeTarget(req.Msg.GetLineageType())
	if !includeSource && !includeTarget {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid lineage type %v", req.Msg.GetLineageType()))
	}

	fetchCtx, cancel := context.WithTimeout(ctx, lineageGraphBudget)
	defer cancel()

	root := req.Msg.GetGuid()
	nodes, edges, depthReached, truncated, err := buildLineageGraph(fetchCtx, root, depth,
		lineageGraphLimits{nodes: maxLineageGraphNodes, edges: maxLineageGraphEdges},
		func(ctx context.Context, guid string) ([]*store.ColumnLineage, error) {
			return s.fetchNodeEdges(ctx, guid, includeSource, includeTarget)
		})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to expand the lineage graph of %q", root))
	}

	response := &v1pb.GetLineageGraphResponse{
		RootGuid:     root,
		Edges:        make([]*v1pb.LineageRelation, 0, len(edges)),
		DepthReached: depthReached,
		Truncated:    truncated,
	}
	for _, edge := range edges {
		response.Edges = append(response.Edges, convertColumnLineage(edge))
	}
	external, err := s.enrichLineageGraph(ctx, nodes, response)
	if err != nil {
		return nil, err
	}
	response.ExternalDatasets = external
	return connect.NewResponse(response), nil
}

// lineageGraphLimits bounds one traversal.
type lineageGraphLimits struct {
	nodes int
	edges int
}

// lineageGraphNode is one object in the traversal, with its signed distance
// from the root: negative upstream, positive downstream.
type lineageGraphNode struct {
	guid     string
	distance int32
}

// lineageGraphFetcher returns the column lineage edges that touch guid, in the
// directions the caller asked for. It is a parameter so the traversal can be
// tested without a database.
type lineageGraphFetcher func(ctx context.Context, guid string) ([]*store.ColumnLineage, error)

// buildLineageGraph walks the graph breadth first from root. It reports the
// nodes and edges it may have collected plus whether a ceiling or the context
// cut the walk short. A cancelled or expired context is a truncation, not an
// error: the caller asked for a bounded picture and that is what it gets.
func buildLineageGraph(ctx context.Context, root string, depth int, limits lineageGraphLimits, fetch lineageGraphFetcher) ([]*lineageGraphNode, []*store.ColumnLineage, int32, bool, error) {
	nodes := []*lineageGraphNode{{guid: root}}
	edges := make([]*store.ColumnLineage, 0)

	visited := map[string]struct{}{root: {}}
	seenEdges := map[string]struct{}{}
	frontier := nodes
	depthReached := int32(0)

	for level := 0; level < depth && len(frontier) > 0; level++ {
		next := make([]*lineageGraphNode, 0, len(frontier))
		for _, node := range frontier {
			if ctx.Err() != nil {
				return nodes, edges, depthReached, true, nil //nolint:nilerr // an exhausted budget is reported as truncation
			}

			fetched, err := fetch(ctx, node.guid)
			if err != nil {
				// A read that fails because the budget ran out is a truncation,
				// not a failure: the caller asked for a bounded picture and a
				// partial one is still useful.
				if ctx.Err() != nil {
					return nodes, edges, depthReached, true, nil //nolint:nilerr // an exhausted budget is reported as truncation
				}
				return nil, nil, 0, false, err
			}

			for _, edge := range fetched {
				if len(edges) >= limits.edges {
					return nodes, edges, depthReached, true, nil
				}
				if key := columnLineageKey(edge); key != "" {
					if _, ok := seenEdges[key]; ok {
						continue
					}
					seenEdges[key] = struct{}{}
				}
				edges = append(edges, edge)

				// An edge only says which object is on the other end; which
				// direction that is depends on where the walk came from.
				var peer string
				var distance int32
				switch {
				case edge.TargetGUID == node.guid && edge.SourceGUID != node.guid:
					peer, distance = edge.SourceGUID, node.distance-1
				case edge.SourceGUID == node.guid && edge.TargetGUID != node.guid:
					peer, distance = edge.TargetGUID, node.distance+1
				default:
					// A self edge adds nothing to the graph.
					continue
				}
				if _, ok := visited[peer]; ok {
					continue
				}
				if len(nodes) >= limits.nodes {
					return nodes, edges, depthReached, true, nil
				}
				visited[peer] = struct{}{}
				discovered := &lineageGraphNode{guid: peer, distance: distance}
				nodes = append(nodes, discovered)
				next = append(next, discovered)
			}
		}
		frontier = next
		depthReached = int32(level + 1)
	}

	return nodes, edges, depthReached, false, nil
}

// columnLineageKey identifies an edge by its endpoints rather than by its row
// id, so the same relation fetched from both ends is collapsed even when it is
// stored as two rows for two analyzed objects.
func columnLineageKey(edge *store.ColumnLineage) string {
	if edge.SourceGUID == "" || edge.TargetGUID == "" {
		return ""
	}
	return edge.SourceGUID + "\x00" + edge.SourceColumn + "\x00" + edge.TargetGUID + "\x00" + edge.TargetColumn
}

// fetchNodeEdges walks every page of one object's column lineage in the
// requested directions. The store puts no ceiling on how many edges a single
// object may have, so the traversal's own limits are what bounds the work.
func (s *LineageService) fetchNodeEdges(ctx context.Context, guid string, includeSource, includeTarget bool) ([]*store.ColumnLineage, error) {
	var edges []*store.ColumnLineage
	if includeSource {
		// Upstream means the object is the target of the edge.
		found, err := s.listAllColumnLineage(ctx, &store.FindColumnLineageMessage{TargetGUID: &guid})
		if err != nil {
			return nil, err
		}
		edges = append(edges, found...)
	}
	if includeTarget {
		found, err := s.listAllColumnLineage(ctx, &store.FindColumnLineageMessage{SourceGUID: &guid})
		if err != nil {
			return nil, err
		}
		edges = append(edges, found...)
	}
	return edges, nil
}

// listAllColumnLineage pages through one column lineage filter. It mirrors what
// the web client already does in TypeScript, so both see the same graph.
func (s *LineageService) listAllColumnLineage(ctx context.Context, find *store.FindColumnLineageMessage) ([]*store.ColumnLineage, error) {
	var all []*store.ColumnLineage
	offset := 0
	for {
		limit := maxLineagePageSize
		probe := limit + 1
		page, err := s.store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{
			MetaGUID:     find.MetaGUID,
			MetaType:     find.MetaType,
			SourceGUID:   find.SourceGUID,
			SourceColumn: find.SourceColumn,
			TargetGUID:   find.TargetGUID,
			TargetColumn: find.TargetColumn,
			Limit:        &probe,
			Offset:       &offset,
		})
		if err != nil {
			return nil, err
		}
		if len(page) <= limit {
			return append(all, page...), nil
		}
		all = append(all, page[:limit]...)
		offset += limit
	}
}

// enrichLineageGraph renders the nodes and collects the external dataset
// metadata the edges reference. The object type comes from one batched registry
// lookup; a node the registry does not know (an object that was never synced)
// still appears, described by the segments of its GUID.
func (s *LineageService) enrichLineageGraph(ctx context.Context, nodes []*lineageGraphNode, response *v1pb.GetLineageGraphResponse) ([]*v1pb.ExternalDatasetInfo, error) {
	guids := make([]string, 0, len(nodes))
	externalGUIDs := make([]string, 0)
	for _, node := range nodes {
		guids = append(guids, node.guid)
		if openlineage.IsExternalGUID(node.guid) {
			externalGUIDs = append(externalGUIDs, node.guid)
		}
	}

	types := map[string]v1pb.MetaType{}
	if len(guids) > 0 {
		metas, err := s.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{GUIDs: &guids})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the lineage graph nodes"))
		}
		for _, meta := range metas {
			types[meta.GUID] = v1pb.MetaType(meta.ObjectType)
		}
	}

	externalByGUID := map[string]*v1pb.ExternalDatasetInfo{}
	var external []*v1pb.ExternalDatasetInfo
	if len(externalGUIDs) > 0 {
		datasets, err := s.store.FindExternalDatasetByGUIDs(ctx, externalGUIDs)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to load external datasets"))
		}
		for _, dataset := range datasets {
			info := &v1pb.ExternalDatasetInfo{
				Guid:        dataset.GUID,
				Namespace:   dataset.Namespace,
				Name:        dataset.Name,
				DatasetType: dataset.DatasetType,
			}
			externalByGUID[dataset.GUID] = info
			external = append(external, info)
		}
	}

	for _, node := range nodes {
		response.Nodes = append(response.Nodes, convertLineageGraphNode(node, types[node.guid], externalByGUID[node.guid]))
	}
	return external, nil
}

// convertLineageGraphNode describes one node. Every GUID is
// "instance[;database[;schema[;name]]]", so its own segments name the object
// even when the registry has never seen it.
func convertLineageGraphNode(node *lineageGraphNode, metaType v1pb.MetaType, external *v1pb.ExternalDatasetInfo) *v1pb.LineageNode {
	converted := &v1pb.LineageNode{
		Guid:     node.guid,
		MetaType: metaType,
		Distance: node.distance,
	}
	if external != nil {
		converted.Name = external.GetName()
	}
	parts := common.SplitMetaGUID(node.guid)
	for i, part := range parts {
		switch i {
		case 0:
			converted.InstanceId = part
		case 1:
			converted.Database = part
		case 2:
			converted.Schema = part
		default:
		}
	}
	if converted.Name == "" && len(parts) > 0 {
		converted.Name = parts[len(parts)-1]
	}
	return converted
}
