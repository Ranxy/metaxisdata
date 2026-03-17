package v1

import (
	"context"
	"encoding/json"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// LineageService implements the instance service.
type LineageService struct {
	v1connect.UnimplementedLineageServiceHandler
	store        *store.Store
	stateCfg     *state.State
	dbFactory    *dbfactory.DBFactory
	schemaSyncer *schemasync.Syncer
}

// NewLineageService creates a new LineageService.
func NewLineageService(store *store.Store, stateCfg *state.State, dbFactory *dbfactory.DBFactory, schemaSyncer *schemasync.Syncer) *LineageService {
	return &LineageService{
		store:        store,
		stateCfg:     stateCfg,
		dbFactory:    dbFactory,
		schemaSyncer: schemaSyncer,
	}
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

	if shouldIncludeSource(req.Msg.GetLineageType()) {
		find := &store.FindColumnLineageMessage{TargetGUID: &req.Msg.Guid}
		lineages, err := s.store.ListColumnLineage(ctx, find)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list source lineage for %q: %v", req.Msg.Guid, err))
		}
		for _, lineage := range lineages {
			relation, err := convertColumnLineage(lineage)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to convert source lineage relation %d: %v", lineage.ID, err))
			}
			response.RelationsSource = append(response.RelationsSource, relation)
		}
	}

	if shouldIncludeTarget(req.Msg.GetLineageType()) {
		find := &store.FindColumnLineageMessage{SourceGUID: &req.Msg.Guid}
		lineages, err := s.store.ListColumnLineage(ctx, find)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list target lineage for %q: %v", req.Msg.Guid, err))
		}
		for _, lineage := range lineages {
			relation, err := convertColumnLineage(lineage)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to convert target lineage relation %d: %v", lineage.ID, err))
			}
			response.RelationsTarget = append(response.RelationsTarget, relation)
		}
	}

	return connect.NewResponse(response), nil
}

func (s *LineageService) GetLineageForContext(ctx context.Context, req *connect.Request[v1pb.GetLineageForContextRequest]) (*connect.Response[v1pb.GetLineageForContextResponse], error) {
	if req.Msg.GetGuid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}

	meta, err := s.getLineageMeta(ctx, req.Msg.Guid, req.Msg.GetMetaType())
	if err != nil {
		return nil, err
	}

	find := &store.FindColumnLineageMessage{
		MetaGUID: &req.Msg.Guid,
		MetaType: &meta.ObjectType,
	}
	lineages, err := s.store.ListColumnLineage(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list context lineage for %q: %v", req.Msg.Guid, err))
	}

	response := &v1pb.GetLineageForContextResponse{}
	for _, lineage := range lineages {
		relation, err := convertColumnLineage(lineage)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to convert lineage relation %d: %v", lineage.ID, err))
		}
		response.Relations = append(response.Relations, relation)
	}

	return connect.NewResponse(response), nil
}

func (s *LineageService) getLineageMeta(ctx context.Context, guid string, metaType v1pb.MetaType) (*store.MetaRegistryResource, error) {
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
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("meta registry %q not found", guid))
	}

	return meta, nil
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

func convertColumnLineage(lineage *store.ColumnLineage) (*v1pb.LineageRelation, error) {
	transformation, err := marshalTransformations(lineage.Transformation)
	if err != nil {
		return nil, err
	}

	return &v1pb.LineageRelation{
		Id:             lineage.ID,
		MetaGuid:       lineage.MetaGUID,
		MetaType:       v1pb.MetaType(lineage.MetaType),
		SourceGuid:     lineage.SourceGUID,
		SourceColumn:   lineage.SourceColumn,
		TargetGuid:     lineage.TargetGUID,
		TargetColumn:   lineage.TargetColumn,
		RelationType:   convertRelationType(lineage.RelationType),
		Transformation: transformation,
		UpdatedAt:      timestamppb.New(lineage.UpdatedAt),
	}, nil
}

func marshalTransformations(transformations []model.Transformation) (string, error) {
	if len(transformations) == 0 {
		return "", nil
	}

	b, err := json.Marshal(transformations)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal transformation")
	}
	return string(b), nil
}

func convertRelationType(relationType model.RelationType) v1pb.RelationType {
	switch relationType {
	case model.RelationTypeDirect:
		return v1pb.RelationType_DIRECT
	default:
		return v1pb.RelationType_INDIRECT
	}
}
