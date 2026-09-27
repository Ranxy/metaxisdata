package v1

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// LineageService implements the instance service.
type LineageService struct {
	v1connect.UnimplementedLineageServiceHandler
	store *store.Store
	// lineage resolves AnalyzeSQL against the analyzers the process assembled.
	lineage *lineage.Analyzer
}

// NewLineageService creates a new LineageService.
func NewLineageService(store *store.Store, lineageAnalyzer *lineage.Analyzer) *LineageService {
	return &LineageService{
		store:   store,
		lineage: lineageAnalyzer,
	}
}

// Lineage page sizes. The lists are column-level edges for one object, so they
// are usually small, but a widely referenced table can have many.
const (
	defaultLineagePageSize = 500
	maxLineagePageSize     = 5000
	// maxLineageCountGuids bounds one counts batch. The caller is a graph
	// labelling the nodes it draws, and the ceiling keeps a single request from
	// becoming an unbounded guid list.
	maxLineageCountGuids = 1000
)

// lineagePageOffset parses the page_size/page_token pair. Unlike the shared
// helper it defaults to defaultLineagePageSize rather than 10, because callers
// (the lineage graph) ask for the whole list at once.
func lineagePageOffset(pageSizeValue int32, pageToken string) (*pageOffset, error) {
	limit := int(pageSizeValue)
	if limit <= 0 && pageToken == "" {
		limit = defaultLineagePageSize
	}
	return parseLimitAndOffset(&pageSize{
		token:   pageToken,
		limit:   limit,
		maximum: maxLineagePageSize,
	})
}

func (s *LineageService) GetLineage(ctx context.Context, req *connect.Request[v1pb.GetLineageRequest]) (*connect.Response[v1pb.GetLineageResponse], error) {
	if req.Msg.GetGuid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}

	_, err := s.getLineageMeta(ctx, req.Msg.Guid, req.Msg.GetMetaType())
	if err != nil {
		return nil, err
	}

	response := &v1pb.GetLineageResponse{}
	if !shouldIncludeSource(req.Msg.GetLineageType()) && !shouldIncludeTarget(req.Msg.GetLineageType()) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid lineage type %v", req.Msg.GetLineageType()))
	}

	offset, err := lineagePageOffset(req.Msg.GetPageSize(), req.Msg.GetPageToken())
	if err != nil {
		return nil, err
	}
	// One extra row tells whether another page follows. The same offset applies
	// to both lists, so an exhausted list simply returns empty later pages.
	probe := offset.limit + 1
	hasMore := false

	if shouldIncludeSource(req.Msg.GetLineageType()) {
		find := &store.FindColumnLineageMessage{
			TargetGUID: &req.Msg.Guid,
			Limit:      &probe,
			Offset:     &offset.offset,
		}
		lineages, err := s.store.ListColumnLineage(ctx, find)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list source lineage for %q: %v", req.Msg.Guid, err))
		}
		if len(lineages) > offset.limit {
			hasMore = true
			lineages = lineages[:offset.limit]
		}
		for _, lineage := range lineages {
			response.RelationsSource = append(response.RelationsSource, convertColumnLineage(lineage))
		}
	}

	if shouldIncludeTarget(req.Msg.GetLineageType()) {
		find := &store.FindColumnLineageMessage{
			SourceGUID: &req.Msg.Guid,
			Limit:      &probe,
			Offset:     &offset.offset,
		}
		lineages, err := s.store.ListColumnLineage(ctx, find)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list target lineage for %q: %v", req.Msg.Guid, err))
		}
		if len(lineages) > offset.limit {
			hasMore = true
			lineages = lineages[:offset.limit]
		}
		for _, lineage := range lineages {
			response.RelationsTarget = append(response.RelationsTarget, convertColumnLineage(lineage))
		}
	}

	// Enrich response with external dataset metadata.
	externalDatasets, err := s.collectExternalDatasets(ctx, response)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to load external datasets"))
	}
	response.ExternalDatasets = externalDatasets

	if hasMore {
		nextPageToken, err := offset.getNextPageToken()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal next page token"))
		}
		response.NextPageToken = nextPageToken
	}

	return connect.NewResponse(response), nil
}

// GetLineageCounts returns how many distinct objects each requested object is
// connected to. A node's degree cannot be read off its neighbours' relations,
// so a graph that labels every node it draws would need one GetLineage per
// node; two aggregates answer the whole set instead.
//
// A GUID with no relations is reported with zero counts rather than refused:
// the caller is labelling objects it already has, and one that has no lineage
// yet is a legitimate node. Unknown and blank GUIDs are treated the same way.
func (s *LineageService) GetLineageCounts(ctx context.Context, req *connect.Request[v1pb.GetLineageCountsRequest]) (*connect.Response[v1pb.GetLineageCountsResponse], error) {
	guids := distinctLineageCountGuids(req.Msg.GetGuids())
	if len(guids) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("at least one guid is required"))
	}
	if len(guids) > maxLineageCountGuids {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("at most %d guids are allowed, got %d", maxLineageCountGuids, len(guids)))
	}

	counts, err := s.store.CountColumnLineageByGUID(ctx, guids)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count lineage"))
	}

	return connect.NewResponse(&v1pb.GetLineageCountsResponse{
		Counts: buildLineageCounts(guids, counts),
	}), nil
}

// distinctLineageCountGuids drops blank and repeated GUIDs, keeping the order
// the caller first asked for. The response mirrors that order, so a caller can
// pair its request with the result position by position.
func distinctLineageCountGuids(guids []string) []string {
	seen := make(map[string]struct{}, len(guids))
	distinct := make([]string, 0, len(guids))
	for _, guid := range guids {
		if guid == "" {
			continue
		}
		if _, ok := seen[guid]; ok {
			continue
		}
		seen[guid] = struct{}{}
		distinct = append(distinct, guid)
	}
	return distinct
}

// buildLineageCounts reports one entry per requested GUID, in request order,
// zero-filled when the store had nothing for it.
func buildLineageCounts(guids []string, counts map[string]*store.ColumnLineageCount) []*v1pb.LineageCount {
	result := make([]*v1pb.LineageCount, 0, len(guids))
	for _, guid := range guids {
		entry := &v1pb.LineageCount{Guid: guid}
		if count, ok := counts[guid]; ok {
			entry.UpstreamCount = count.Upstream
			entry.DownstreamCount = count.Downstream
		}
		result = append(result, entry)
	}
	return result
}

// collectExternalDatasets finds all external GUIDs in the lineage response and fetches their metadata.
// A store failure is returned rather than downgraded to an empty list, which
// the UI could not tell apart from "no external datasets".
func (s *LineageService) collectExternalDatasets(ctx context.Context, resp *v1pb.GetLineageResponse) ([]*v1pb.ExternalDatasetInfo, error) {
	guidSet := make(map[string]struct{})
	for _, r := range resp.RelationsSource {
		if openlineage.IsExternalGUID(r.SourceGuid) {
			guidSet[r.SourceGuid] = struct{}{}
		}
		if openlineage.IsExternalGUID(r.TargetGuid) {
			guidSet[r.TargetGuid] = struct{}{}
		}
	}
	for _, r := range resp.RelationsTarget {
		if openlineage.IsExternalGUID(r.SourceGuid) {
			guidSet[r.SourceGuid] = struct{}{}
		}
		if openlineage.IsExternalGUID(r.TargetGuid) {
			guidSet[r.TargetGuid] = struct{}{}
		}
	}

	if len(guidSet) == 0 {
		return nil, nil
	}

	guids := make([]string, 0, len(guidSet))
	for g := range guidSet {
		guids = append(guids, g)
	}

	datasets, err := s.store.FindExternalDatasetByGUIDs(ctx, guids)
	if err != nil {
		return nil, err
	}

	result := make([]*v1pb.ExternalDatasetInfo, 0, len(datasets))
	for _, d := range datasets {
		result = append(result, &v1pb.ExternalDatasetInfo{
			Guid:        d.GUID,
			Namespace:   d.Namespace,
			Name:        d.Name,
			DatasetType: d.DatasetType,
		})
	}
	return result, nil
}

func (s *LineageService) GetLineageForContext(ctx context.Context, req *connect.Request[v1pb.GetLineageForContextRequest]) (*connect.Response[v1pb.GetLineageForContextResponse], error) {
	if req.Msg.GetGuid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}

	meta, err := s.getLineageMeta(ctx, req.Msg.Guid, req.Msg.GetMetaType())
	if err != nil {
		return nil, err
	}

	offset, err := lineagePageOffset(req.Msg.GetPageSize(), req.Msg.GetPageToken())
	if err != nil {
		return nil, err
	}
	probe := offset.limit + 1

	find := &store.FindColumnLineageMessage{
		MetaGUID: &req.Msg.Guid,
		MetaType: &meta.ObjectType,
		Limit:    &probe,
		Offset:   &offset.offset,
	}
	lineages, err := s.store.ListColumnLineage(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list context lineage for %q: %v", req.Msg.Guid, err))
	}

	page, nextPageToken, err := paginate(lineages, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.GetLineageForContextResponse{NextPageToken: nextPageToken}
	for _, lineage := range page {
		response.Relations = append(response.Relations, convertColumnLineage(lineage))
	}

	return connect.NewResponse(response), nil
}

func (s *LineageService) getLineageMeta(ctx context.Context, guid string, metaType v1pb.MetaType) (*store.MetaRegistryResource, error) {
	// External datasets won't have a meta_registry entry.
	if openlineage.IsExternalGUID(guid) {
		return &store.MetaRegistryResource{
			GUID:       guid,
			ObjectType: storepb.MetaType_EXTERNAL_DATASET,
		}, nil
	}

	findMeta := &store.FindMetaRegistryResourceMessage{GUID: &guid}
	if metaType != v1pb.MetaType_UNSPECIFIED {
		storeMetaType := storepb.MetaType(metaType)
		findMeta.ObjectType = &storeMetaType
	}

	meta, err := s.store.GetMetaRegistry(ctx, findMeta)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get meta registry %q: %v", guid, err))
	}
	if meta == nil {
		// A lineage edge can name a relation the registry does not have: the writer
		// has not synced its schema yet, or the reference is to an object the sync
		// excludes. The edge is real either way, so the node is reported as an
		// unresolved relation of the type the edge claims instead of failing the
		// whole request. Reconciling those edges once the relation does appear is
		// the revalidation pass's job; until then the node simply carries no
		// metadata.
		slog.Debug("lineage node has no metadata registry entry", "guid", guid, "objectType", metaType.String())
		return lineageNodeWithMissingMeta(guid, metaType), nil
	}

	return meta, nil
}

// lineageNodeWithMissingMeta is the node a lineage edge's endpoint is reported as
// when the metadata registry has no row for it. The type the edge claims is kept
// so the graph can still group and render the node, and no metadata is invented.
func lineageNodeWithMissingMeta(guid string, metaType v1pb.MetaType) *store.MetaRegistryResource {
	objectType := storepb.MetaType_TABLE
	if metaType != v1pb.MetaType_UNSPECIFIED {
		objectType = storepb.MetaType(metaType)
	}
	return &store.MetaRegistryResource{GUID: guid, ObjectType: objectType}
}

func shouldIncludeSource(lineageType v1pb.LineageType) bool {
	switch lineageType {
	case v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED:
		return true
	case v1pb.LineageType_SOURCE:
		return true
	case v1pb.LineageType_TARGET:
		return false
	default:
		return false
	}
}

func shouldIncludeTarget(lineageType v1pb.LineageType) bool {
	switch lineageType {
	case v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED:
		return true
	case v1pb.LineageType_SOURCE:
		return false
	case v1pb.LineageType_TARGET:
		return true
	default:
		return false
	}
}

func convertColumnLineage(lineage *store.ColumnLineage) *v1pb.LineageRelation {
	return &v1pb.LineageRelation{
		Id:              lineage.ID,
		MetaGuid:        lineage.MetaGUID,
		MetaType:        v1pb.MetaType(lineage.MetaType),
		SourceGuid:      lineage.SourceGUID,
		SourceColumn:    lineage.SourceColumn,
		SourceType:      v1pb.MetaType(lineage.SourceType),
		TargetGuid:      lineage.TargetGUID,
		TargetColumn:    lineage.TargetColumn,
		TargetType:      v1pb.MetaType(lineage.TargetType),
		RelationType:    convertRelationType(lineage.RelationType),
		Transformations: convertTransformations(lineage.Transformation),
		UpdatedAt:       timestamppb.New(lineage.UpdatedAt),
	}
}

func convertTransformations(transformations []model.Transformation) []*v1pb.Transformation {
	result := make([]*v1pb.Transformation, 0, len(transformations))
	for _, transformation := range transformations {
		result = append(result, &v1pb.Transformation{
			Operation:    string(transformation.Operation),
			Expression:   transformation.Expression,
			FunctionName: transformation.FunctionName,
			Arguments:    transformation.Arguments,
			GroupKeys:    transformation.GroupKeys,
			PartitionBy:  transformation.PartitionBy,
			OrderBy:      transformation.OrderBy,
			OpType:       transformation.OpType,
			Condition:    transformation.Condition,
			All:          transformation.All,
		})
	}
	return result
}

// convertRelationType reports the stored relation type as it is. Collapsing every
// non-direct relation to INDIRECT would hide what the analyzers now distinguish —
// a join key, an aggregation and a set operation are different relations between
// a source column and its target.
func convertRelationType(relationType model.RelationType) v1pb.RelationType {
	switch relationType {
	case model.RelationTypeDirect:
		return v1pb.RelationType_DIRECT
	case model.RelationTypeIndirect:
		return v1pb.RelationType_INDIRECT
	case model.RelationTypeJoin:
		return v1pb.RelationType_JOIN
	case model.RelationTypeGroup:
		return v1pb.RelationType_GROUP
	case model.RelationTypeUnion:
		return v1pb.RelationType_UNION
	case model.RelationTypeIntersect:
		return v1pb.RelationType_INTERSECT
	case model.RelationTypeExcept:
		return v1pb.RelationType_EXCEPT
	case model.RelationTypeUnknown:
		return v1pb.RelationType_UNKNOWN
	default:
		return v1pb.RelationType_RELATION_TYPE_UNSPECIFIED
	}
}
