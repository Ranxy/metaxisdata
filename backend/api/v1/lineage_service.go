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

	findMessages, err := buildLineageFindMessages(req.Msg.Guid, req.Msg.GetLineageType())
	if err != nil {
		return nil, err
	}

	response := &v1pb.GetLineageResponse{}
	seen := make(map[int64]struct{})
	for _, find := range findMessages {
		lineages, err := s.store.ListColumnLineage(ctx, find)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list column lineage for %q: %v", req.Msg.Guid, err))
		}
		for _, lineage := range lineages {
			if _, ok := seen[lineage.ID]; ok {
				continue
			}
			relation, err := convertColumnLineage(lineage)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to convert lineage relation %d: %v", lineage.ID, err))
			}
			seen[lineage.ID] = struct{}{}
			response.Relations = append(response.Relations, relation)
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

func buildLineageFindMessages(guid string, lineageType v1pb.LineageType) ([]*store.FindColumnLineageMessage, error) {
	switch lineageType {
	case v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED:
		return []*store.FindColumnLineageMessage{
			{TargetGUID: &guid},
			{SourceGUID: &guid},
		}, nil
	case v1pb.LineageType_SOURCE:
		return []*store.FindColumnLineageMessage{{TargetGUID: &guid}}, nil
	case v1pb.LineageType_TARGET:
		return []*store.FindColumnLineageMessage{{SourceGUID: &guid}}, nil
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid lineage type %v", lineageType))
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
