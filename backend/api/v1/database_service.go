package v1

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	celoperators "github.com/google/cel-go/common/operators"
	celoverloads "github.com/google/cel-go/common/overloads"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/schema"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// DatabaseService implements the database service.
type DatabaseService struct {
	v1connect.UnimplementedDatabaseServiceHandler
	store        *store.Store
	stateCfg     *state.State
	dbFactory    *dbfactory.DBFactory
	schemaSyncer *schemasync.Syncer
}

// NewDatabaseService creates a new DatabaseService.
func NewDatabaseService(store *store.Store, stateCfg *state.State, dbFactory *dbfactory.DBFactory, schemaSyncer *schemasync.Syncer) *DatabaseService {
	return &DatabaseService{
		store:        store,
		stateCfg:     stateCfg,
		dbFactory:    dbFactory,
		schemaSyncer: schemaSyncer,
	}
}

func (*DatabaseService) GetDatabase(_ context.Context, _ *connect.Request[v1pb.GetDatabaseRequest]) (*connect.Response[v1pb.Database], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("metaxisdata.v1pb.DatabaseService.GetDatabase is not implemented"))
}

func (s *DatabaseService) SyncDatabase(ctx context.Context, req *connect.Request[v1pb.SyncDatabaseRequest]) (*connect.Response[v1pb.SyncDatabaseResponse], error) {
	database, err := getDatabaseMessage(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	if database.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("database %q has been deleted", req.Msg.Name))
	}

	if err := s.schemaSyncer.SyncDatabaseSchema(ctx, database); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to sync database"))
	}

	return connect.NewResponse(&v1pb.SyncDatabaseResponse{}), nil
}

func (s *DatabaseService) ListDatabase(ctx context.Context, req *connect.Request[v1pb.ListDatabaseRequest]) (*connect.Response[v1pb.ListDatabasesResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindDatabaseMessage{
		Limit:       &limitPlusOne,
		Offset:      &offset.offset,
		ShowDeleted: req.Msg.ShowDeleted,
	}

	filter, err := getListDatabaseFilter(req.Msg.Filter)
	if err != nil {
		return nil, err
	}
	find.Filter = filter

	switch {
	case strings.HasPrefix(req.Msg.Parent, common.ProjectNamePrefix):
		p, err := common.GetProjectID(req.Msg.Parent)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid parent %q", req.Msg.Parent))
		}
		find.ProjectID = &p
	case strings.HasPrefix(req.Msg.Parent, common.WorkspacePrefix):
	case strings.HasPrefix(req.Msg.Parent, common.InstanceNamePrefix):
		instanceID, err := common.GetInstanceID(req.Msg.Parent)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid parent %q", req.Msg.Parent))
		}
		find.InstanceID = &instanceID
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid parent %q", req.Msg.Parent))
	}

	databaseMessages, err := s.store.ListDatabases(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("%v", err.Error()))
	}

	nextPageToken := ""
	if len(databaseMessages) == limitPlusOne {
		databaseMessages = databaseMessages[:offset.limit]
		if nextPageToken, err = offset.getNextPageToken(); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to marshal next page token, error: %v", err))
		}
	}

	response := &v1pb.ListDatabasesResponse{
		NextPageToken: nextPageToken,
	}
	for _, databaseMessage := range databaseMessages {
		database, err := s.convertToDatabase(ctx, databaseMessage)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to convert database, error: %v", err))
		}
		response.Databases = append(response.Databases, database)
	}
	return connect.NewResponse(response), nil
}

func (s *DatabaseService) ListMetadata(ctx context.Context, req *connect.Request[v1pb.ListMetadataRequest]) (*connect.Response[v1pb.MetadataResponse], error) {
	var parentType storepb.MetaType
	if strings.Contains(req.Msg.GetParentGuid(), common.MetaGUIDSplit) {
		parentMeta, err := s.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &req.Msg.ParentGuid})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get parent meta registry %q: %v", req.Msg.ParentGuid, err))
		}
		if parentMeta == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("meta registry %q not found", req.Msg.ParentGuid))
		}
		parentType = parentMeta.ObjectType
	} else {
		parentType = storepb.MetaType_INSTANCE
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	getTypedMetadataList := func() (list []*v1pb.MetadataResponse_MetadataList, err error) {
		if req.Msg.MetaType != nil {
			findMessage := &store.FindMetaRegistryResourceMessage{
				GUIDPrefix: &req.Msg.ParentGuid,
				Limit:      &limitPlusOne,
				Offset:     &offset.offset,
				ObjectType: (*storepb.MetaType)(req.Msg.MetaType),
			}
			subLevelList, err := s.store.ListMetaRegistryResource(ctx, findMessage)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list meta registry resources under %q: %v", req.Msg.ParentGuid, err))
			}
			nextPageToken := ""
			if len(subLevelList) == limitPlusOne {
				subLevelList = subLevelList[:offset.limit]
				if nextPageToken, err = offset.getNextPageToken(); err != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to marshal next page token"))
				}
			}
			typesStoredMetadataMap := make(map[v1pb.MetaType][]*v1pb.StoredMetadata)
			for _, meta := range subLevelList {
				tp := v1pb.MetaType(meta.ObjectType)
				metaMessage := convertStoredMetadataMessage(meta.Metadata)
				typesStoredMetadataMap[tp] = append(typesStoredMetadataMap[tp], metaMessage)
			}

			list = []*v1pb.MetadataResponse_MetadataList{}

			for tp, storeLit := range typesStoredMetadataMap {
				list = append(list, &v1pb.MetadataResponse_MetadataList{
					MetaType:      tp,
					List:          storeLit,
					NextPageToken: nextPageToken,
				})
			}

			return list, nil
		}
		subLevelFindMessage := &store.FindSubLevelMetaRegistryResourceMessage{
			ParentGUID:         req.Msg.ParentGuid,
			ObjectType:         parentType,
			LimitPreObjectType: limitPlusOne,
		}
		subLevelList, err := s.store.ListSublevelMetaRegistryResource(ctx, subLevelFindMessage)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list sublevel meta registry resources under %q: %v", req.Msg.ParentGuid, err))
		}

		typesStoredMetadataMap := make(map[v1pb.MetaType][]*v1pb.StoredMetadata)
		for _, meta := range subLevelList {
			tp := v1pb.MetaType(meta.ObjectType)
			metaMessage := convertStoredMetadataMessage(meta.Metadata)
			typesStoredMetadataMap[tp] = append(typesStoredMetadataMap[tp], metaMessage)
		}

		list = []*v1pb.MetadataResponse_MetadataList{}

		for tp, storeLit := range typesStoredMetadataMap {
			nextPageToken := ""
			if len(storeLit) == limitPlusOne {
				storeLit = storeLit[:offset.limit]
				if nextPageToken, err = offset.getNextPageToken(); err != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to marshal next page token"))
				}
			}
			list = append(list, &v1pb.MetadataResponse_MetadataList{
				MetaType:      tp,
				List:          storeLit,
				NextPageToken: nextPageToken,
			})
		}

		slices.SortFunc(list, func(a, b *v1pb.MetadataResponse_MetadataList) int {
			return int(a.MetaType.Number() - b.MetaType.Number())
		})

		return list, nil
	}

	typeddMetadataList, err := getTypedMetadataList()
	if err != nil {
		return nil, err
	}

	response := &v1pb.MetadataResponse{TypesStoredMetadata: typeddMetadataList}

	return connect.NewResponse(response), nil
}

func (s *DatabaseService) GetMetadata(ctx context.Context, req *connect.Request[v1pb.GetMetadataRequest]) (*connect.Response[v1pb.GetMetadataResponse], error) {
	meta, err := s.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &req.Msg.Guid, ObjectType: (*storepb.MetaType)(&req.Msg.MetaType)})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get meta registry %q: %v", req.Msg.Guid, err))
	}
	if meta == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("meta registry %q not found", req.Msg.Guid))
	}

	response := &v1pb.GetMetadataResponse{
		Metadata: convertStoredMetadataMessage(meta.Metadata),
	}

	return connect.NewResponse(response), nil
}

func (s *DatabaseService) SearchMetadata(ctx context.Context, req *connect.Request[v1pb.SearchMetadataRequest]) (*connect.Response[v1pb.SearchMetadataResponse], error) {
	searchStr := strings.TrimSpace(req.Msg.GetSearchStr())
	if searchStr == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("search_str is required"))
	}

	const searchLimit = 50

	find := &store.SearchMetaRegistryResourceMessage{
		SearchStr: searchStr,
		Limit:     searchLimit + 1,
	}
	if req.Msg.ParentGuidPrefix != nil {
		find.GUIDPrefix = req.Msg.ParentGuidPrefix
	}
	if req.Msg.MetaType != nil {
		metaType := storepb.MetaType(*req.Msg.MetaType)
		find.ObjectType = &metaType
	}

	list, err := s.store.SearchMetaRegistryResource(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to search metadata: %v", err))
	}

	response := &v1pb.SearchMetadataResponse{}
	if len(list) > searchLimit {
		list = list[:searchLimit]
	}
	for _, meta := range list {
		response.Results = append(response.Results, &v1pb.SearchMetadataResult{
			Guid:     meta.GUID,
			MetaType: v1pb.MetaType(meta.ObjectType),
			Metadata: convertStoredMetadataMessage(meta.Metadata),
		})
	}

	return connect.NewResponse(response), nil
}

func (s *DatabaseService) GetSchemaString(ctx context.Context, req *connect.Request[v1pb.GetSchemaStringRequest]) (*connect.Response[v1pb.MetadataSchemaString], error) {
	instanceGUID, ok := common.GetInstaceFromGUID(req.Msg.Guid)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid guid %q", req.Msg.Guid))
	}

	schemaName, ok := common.GetSchemaFromGUID(req.Msg.Guid)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid guid %q", req.Msg.Guid))
	}

	instance, err := s.store.GetInstanceV2(ctx, &store.FindInstanceMessage{ResourceID: &instanceGUID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get instance %q: %v", instanceGUID, err))
	}

	if instance == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q not found", instanceGUID))
	}

	meta, err := s.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &req.Msg.Guid, ObjectType: (*storepb.MetaType)(&req.Msg.MetaType)})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get meta registry %q: %v", req.Msg.Guid, err))
	}
	if meta == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("meta registry %q not found", req.Msg.Guid))
	}

	engine := instance.Metadata.GetEngine()

	switch req.Msg.MetaType {
	case v1pb.MetaType_TABLE:
		tableMeta := meta.Metadata.GetTableMetadata()
		if tableMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("table metadata is nil"))
		}

		// Get sequences that own this table from the same schema
		schemaPrefix := common.GUIDPrefix(req.Msg.Guid)
		sequences, err := s.getTableSequences(ctx, schemaPrefix, tableMeta.Name)
		if err != nil {
			return nil, err
		}

		schemaStr, err := schema.GetTableDefinition(engine, schemaName, tableMeta, sequences)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate table definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_VIEW:
		viewMeta := meta.Metadata.GetViewMetadata()
		if viewMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("view metadata is nil"))
		}

		schemaStr, err := schema.GetViewDefinition(engine, schemaName, viewMeta)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate view definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_MATERIALIZED_VIEW:
		mvMeta := meta.Metadata.GetMaterializedViewMetadata()
		if mvMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("materialized view metadata is nil"))
		}

		schemaStr, err := schema.GetMaterializedViewDefinition(engine, schemaName, mvMeta)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate materialized view definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_FUNCTION:
		funcMeta := meta.Metadata.GetFunctionMetadata()
		if funcMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("function metadata is nil"))
		}

		schemaStr, err := schema.GetFunctionDefinition(engine, schemaName, funcMeta)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate function definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_PROCEDURE:
		procMeta := meta.Metadata.GetProcedureMetadata()
		if procMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("procedure metadata is nil"))
		}

		schemaStr, err := schema.GetProcedureDefinition(engine, schemaName, procMeta)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate procedure definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	default:
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("GetMetadataSchema is not implemented for this meta type"))
	}
}

func (s *DatabaseService) CreateManualSQL(ctx context.Context, req *connect.Request[v1pb.CreateManualSQLRequest]) (*connect.Response[v1pb.ManualSQL], error) {
	instanceID, databaseName, err := common.GetInstanceDatabaseID(req.Msg.GetParent())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid parent"))
	}
	if req.Msg.GetManualSqlId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("manual_sql_id is required"))
	}
	if req.Msg.GetManualSql() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("manual_sql is required"))
	}

	msg, err := buildManualSQLMessageFromV1(instanceID, databaseName, req.Msg.GetManualSqlId(), req.Msg.GetManualSql())
	if err != nil {
		return nil, err
	}
	created, err := s.store.CreateManualSQL(ctx, msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create manual SQL"))
	}
	return connect.NewResponse(convertManualSQLResource(created)), nil
}

func (s *DatabaseService) GetManualSQL(ctx context.Context, req *connect.Request[v1pb.GetManualSQLRequest]) (*connect.Response[v1pb.ManualSQL], error) {
	instanceID, databaseName, manualSQLID, err := parseManualSQLName(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	manualSQL, err := s.store.GetManualSQL(ctx, &store.FindManualSQLMessage{
		InstanceResourceID: &instanceID,
		DatabaseName:       &databaseName,
		ManualSQLID:        &manualSQLID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get manual SQL"))
	}
	if manualSQL == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("manual SQL %q not found", req.Msg.GetName()))
	}
	return connect.NewResponse(convertManualSQLResource(manualSQL)), nil
}

func (s *DatabaseService) ListManualSQL(ctx context.Context, req *connect.Request[v1pb.ListManualSQLRequest]) (*connect.Response[v1pb.ListManualSQLResponse], error) {
	instanceID, databaseName, err := common.GetInstanceDatabaseID(req.Msg.GetParent())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid parent"))
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   int(req.Msg.GetPageSize()),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindManualSQLMessage{
		InstanceResourceID: &instanceID,
		DatabaseName:       &databaseName,
		ShowDeleted:        req.Msg.GetShowDeleted(),
		Limit:              &limitPlusOne,
		Offset:             &offset.offset,
	}
	if schemaName := strings.TrimSpace(req.Msg.GetSchemaName()); schemaName != "" {
		find.SchemaName = &schemaName
	}
	if len(req.Msg.GetTags()) > 0 {
		tags := req.Msg.GetTags()
		find.Tags = &tags
	}

	list, err := s.store.ListManualSQL(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list manual SQL"))
	}

	response := &v1pb.ListManualSQLResponse{}
	if len(list) == limitPlusOne {
		list = list[:offset.limit]
		response.NextPageToken, err = offset.getNextPageToken()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal next page token"))
		}
	}
	for _, item := range list {
		response.ManualSqls = append(response.ManualSqls, convertManualSQLResource(item))
	}
	return connect.NewResponse(response), nil
}

func (s *DatabaseService) SearchManualSQL(ctx context.Context, req *connect.Request[v1pb.SearchManualSQLRequest]) (*connect.Response[v1pb.SearchManualSQLResponse], error) {
	instanceID, databaseName, err := common.GetInstanceDatabaseID(req.Msg.GetParent())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid parent"))
	}
	query := strings.TrimSpace(req.Msg.GetQuery())
	if query == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("query is required"))
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   int(req.Msg.GetPageSize()),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindManualSQLMessage{
		InstanceResourceID: &instanceID,
		DatabaseName:       &databaseName,
		Query:              &query,
		Limit:              &limitPlusOne,
		Offset:             &offset.offset,
	}
	if schemaName := strings.TrimSpace(req.Msg.GetSchemaName()); schemaName != "" {
		find.SchemaName = &schemaName
	}
	if len(req.Msg.GetTags()) > 0 {
		tags := req.Msg.GetTags()
		find.Tags = &tags
	}

	list, err := s.store.ListManualSQL(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to search manual SQL"))
	}
	response := &v1pb.SearchManualSQLResponse{}
	if len(list) == limitPlusOne {
		list = list[:offset.limit]
		response.NextPageToken, err = offset.getNextPageToken()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal next page token"))
		}
	}
	for _, item := range list {
		response.ManualSqls = append(response.ManualSqls, convertManualSQLResource(item))
	}
	return connect.NewResponse(response), nil
}

func (s *DatabaseService) UpdateManualSQL(ctx context.Context, req *connect.Request[v1pb.UpdateManualSQLRequest]) (*connect.Response[v1pb.ManualSQL], error) {
	manualSQL := req.Msg.GetManualSql()
	if manualSQL == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("manual_sql is required"))
	}
	instanceID, databaseName, manualSQLID, err := parseManualSQLName(manualSQL.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	existing, err := s.store.GetManualSQL(ctx, &store.FindManualSQLMessage{
		InstanceResourceID: &instanceID,
		DatabaseName:       &databaseName,
		ManualSQLID:        &manualSQLID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve manual SQL before update"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("manual SQL %q not found", manualSQL.GetName()))
	}
	patch := &store.UpdateManualSQLMessage{}
	for _, path := range req.Msg.GetUpdateMask().GetPaths() {
		switch path {
		case "title":
			patch.Title = &manualSQL.Title
		case "comment":
			patch.Comment = &manualSQL.Comment
		case "sql_text":
			patch.SQLText = &manualSQL.SqlText
		case "schema_name":
			patch.SchemaName = &manualSQL.SchemaName
		case "tags":
			tags := manualSQL.Tags
			patch.Tags = &tags
		case "attributes":
			attributes := manualSQL.Attributes
			patch.Attributes = &attributes
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update path %q", path))
		}
	}
	if len(req.Msg.GetUpdateMask().GetPaths()) == 0 {
		patch.Title = &manualSQL.Title
		patch.Comment = &manualSQL.Comment
		patch.SQLText = &manualSQL.SqlText
		patch.SchemaName = &manualSQL.SchemaName
		tags := manualSQL.Tags
		patch.Tags = &tags
		attributes := manualSQL.Attributes
		patch.Attributes = &attributes
	}
	updated, err := s.store.UpdateManualSQL(ctx, existing.GUID, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update manual SQL"))
	}
	updated.ManualSQLID = manualSQLID
	return connect.NewResponse(convertManualSQLResource(updated)), nil
}

func (s *DatabaseService) DeleteManualSQL(ctx context.Context, req *connect.Request[v1pb.DeleteManualSQLRequest]) (*connect.Response[emptypb.Empty], error) {
	instanceID, databaseName, manualSQLID, err := parseManualSQLName(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	existing, err := s.store.GetManualSQL(ctx, &store.FindManualSQLMessage{
		InstanceResourceID: &instanceID,
		DatabaseName:       &databaseName,
		ManualSQLID:        &manualSQLID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve manual SQL before delete"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("manual SQL %q not found", req.Msg.GetName()))
	}
	if err := s.store.DeleteManualSQL(ctx, existing.GUID, nil); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete manual SQL"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func parseManualSQLName(name string) (string, string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.InstanceNamePrefix, common.DatabaseIDPrefix, "manualSqls/")
	if err != nil {
		return "", "", "", errors.Wrap(err, "invalid manual SQL name")
	}
	return tokens[0], tokens[1], tokens[2], nil
}

func buildManualSQLMessageFromV1(instanceID, databaseName, manualSQLID string, manualSQL *v1pb.ManualSQL) (*store.ManualSQLMessage, error) {
	if strings.TrimSpace(manualSQL.GetSqlText()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("sql_text is required"))
	}
	name := strings.TrimSpace(manualSQLID)
	if name == "" {
		name = strings.TrimSpace(manualSQL.GetName())
	}
	return &store.ManualSQLMessage{
		ManualSQLID:        manualSQLID,
		InstanceResourceID: instanceID,
		DatabaseName:       databaseName,
		SchemaName:         strings.TrimSpace(manualSQL.GetSchemaName()),
		Name:               name,
		Title:              strings.TrimSpace(manualSQL.GetTitle()),
		Comment:            strings.TrimSpace(manualSQL.GetComment()),
		SQLText:            manualSQL.GetSqlText(),
		Tags:               manualSQL.GetTags(),
		Attributes:         manualSQL.GetAttributes(),
	}, nil
}

func convertManualSQLResource(msg *store.ManualSQLMessage) *v1pb.ManualSQL {
	if msg == nil {
		return nil
	}
	return &v1pb.ManualSQL{
		Name:       fmt.Sprintf("%s/manualSqls/%s", common.FormatDatabase(msg.InstanceResourceID, msg.DatabaseName), msg.ManualSQLID),
		Guid:       msg.GUID,
		Title:      msg.Title,
		SchemaName: msg.SchemaName,
		Comment:    msg.Comment,
		SqlText:    msg.SQLText,
		Tags:       msg.Tags,
		Attributes: msg.Attributes,
		CreatedAt:  timestamppb.New(msg.CreatedAt),
		UpdatedAt:  timestamppb.New(msg.UpdatedAt),
	}
}

// getTableSequences retrieves sequences that belong to a specific table.
func (s *DatabaseService) getTableSequences(ctx context.Context, schemaPrefix, tableName string) ([]*storepb.SequenceMetadata, error) {
	seqType := storepb.MetaType_SEQUENCE
	seqList, err := s.store.ListMetaRegistryResource(ctx, &store.FindMetaRegistryResourceMessage{
		GUIDPrefix: &schemaPrefix,
		ObjectType: &seqType,
		ExtraArgs: []store.ExtraArgs{
			{
				Left:  "metadata->'sequenceMetadata'->>'ownerTable'",
				Op:    "=",
				Right: tableName,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var sequences []*storepb.SequenceMetadata
	for _, seq := range seqList {
		seqMeta := seq.Metadata.GetSequenceMetadata()
		sequences = append(sequences, seqMeta)
	}
	return sequences, nil
}

func parseToEngineSQL(expr celast.Expr, relation string) (string, error) {
	variable, value := getVariableAndValueFromExpr(expr)
	if variable != "engine" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`only "engine" support "engine in [xx]"/"!(engine in [xx])" operator`))
	}

	rawEngineList, ok := value.([]any)
	if !ok {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid engine value %q", value))
	}
	if len(rawEngineList) == 0 {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("empty engine filter"))
	}
	engineList := []string{}
	for _, rawEngine := range rawEngineList {
		v1Engine, ok := v1pb.Engine_value[rawEngine.(string)]
		if !ok {
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid engine filter %q", rawEngine))
		}
		engine := convertEngine(v1pb.Engine(v1Engine))
		engineList = append(engineList, fmt.Sprintf(`'%s'`, engine.String()))
	}

	return fmt.Sprintf("instance.metadata->>'engine' %s (%s)", relation, strings.Join(engineList, ",")), nil
}

func getListDatabaseFilter(filter string) (*store.ListResourceFilter, error) {
	if filter == "" {
		return nil, nil
	}

	e, err := cel.NewEnv()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create cel env"))
	}
	ast, iss := e.Parse(filter)
	if iss != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("failed to parse filter %v, error: %v", filter, iss.String()))
	}

	var getFilter func(expr celast.Expr) (string, error)
	var positionalArgs []any

	parseToSQL := func(variable, value any) (string, error) {
		switch variable {
		case "project":
			projectID, err := common.GetProjectID(value.(string))
			if err != nil {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid project filter %q", value))
			}
			positionalArgs = append(positionalArgs, projectID)
			return fmt.Sprintf("db.project = $%d", len(positionalArgs)), nil
		case "instance":
			instanceID, err := common.GetInstanceID(value.(string))
			if err != nil {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid instance filter %q", value))
			}
			positionalArgs = append(positionalArgs, instanceID)
			return fmt.Sprintf("db.instance = $%d", len(positionalArgs)), nil
		case "environment":
			environmentID, err := common.GetEnvironmentID(value.(string))
			if err != nil {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid environment filter %q", value))
			}
			positionalArgs = append(positionalArgs, environmentID)
			return fmt.Sprintf(`
			COALESCE(
				db.environment,
				instance.environment
			) = $%d`, len(positionalArgs)), nil
		case "engine":
			v1Engine, ok := v1pb.Engine_value[value.(string)]
			if !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid engine filter %q", value))
			}
			engine := convertEngine(v1pb.Engine(v1Engine))
			positionalArgs = append(positionalArgs, engine)
			return fmt.Sprintf("instance.metadata->>'engine' = $%d", len(positionalArgs)), nil
		case "name":
			positionalArgs = append(positionalArgs, value)
			return fmt.Sprintf("db.name = $%d", len(positionalArgs)), nil
		case "label":
			keyVal := strings.Split(value.(string), ":")
			if len(keyVal) != 2 {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`invalid label filter %q, should be in "{label key}:{label value} format"`, value))
			}
			labelKey := keyVal[0]
			labelValues := strings.Split(keyVal[1], ",")
			positionalArgs = append(positionalArgs, labelValues)
			return fmt.Sprintf("db.metadata->'labels'->>'%s' = ANY($%d)", labelKey, len(positionalArgs)), nil
		case "drifted":
			drifted, ok := value.(bool)
			if !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid drifted filter %q", value))
			}
			condition := "IS"
			if !drifted {
				condition = "IS NOT"
			}
			return fmt.Sprintf("(db.metadata->>'drifted')::boolean %s TRUE", condition), nil
		case "exclude_unassigned":
			if excludeUnassigned, ok := value.(bool); excludeUnassigned && ok {
				positionalArgs = append(positionalArgs, common.DefaultProjectID)
				return fmt.Sprintf("db.project != $%d", len(positionalArgs)), nil
			}
			return "TRUE", nil
		case "table":
			positionalArgs = append(positionalArgs, value.(string))
			return fmt.Sprintf(`
				EXISTS (
					SELECT 1
					FROM json_array_elements(ds.metadata->'schemas') AS s,
						 json_array_elements(s->'tables') AS t
					WHERE t->>'name' = $%d
				)
			`, len(positionalArgs)), nil
		default:
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupport variable %q", variable))
		}
	}

	getFilter = func(expr celast.Expr) (string, error) {
		switch expr.Kind() {
		case celast.CallKind:
			functionName := expr.AsCall().FunctionName()
			switch functionName {
			case celoperators.LogicalOr:
				return getSubConditionFromExpr(expr, getFilter, "OR")
			case celoperators.LogicalAnd:
				return getSubConditionFromExpr(expr, getFilter, "AND")
			case celoperators.Equals:
				variable, value := getVariableAndValueFromExpr(expr)
				return parseToSQL(variable, value)
			case celoverloads.Matches:
				variable := expr.AsCall().Target().AsIdent()
				args := expr.AsCall().Args()
				if len(args) != 1 {
					return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`invalid args for %q`, variable))
				}
				value := args[0].AsLiteral().Value()
				strValue, ok := value.(string)
				if !ok {
					return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("expect string, got %T, hint: filter literals should be string", value))
				}
				strValue = strings.ToLower(strValue)

				switch variable {
				case "name":
					return "LOWER(db.name) LIKE '%" + strValue + "%'", nil
				case "table":
					return `EXISTS (
						SELECT 1
						FROM json_array_elements(ds.metadata->'schemas') AS s,
						 	 json_array_elements(s->'tables') AS t
						WHERE t->>'name' LIKE '%` + strValue + `%')`, nil
				default:
					return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`only "name" or "table" support %q operator, but found %q`, celoverloads.Matches, variable))
				}
			case celoperators.In:
				return parseToEngineSQL(expr, "IN")
			case celoperators.LogicalNot:
				args := expr.AsCall().Args()
				if len(args) != 1 {
					return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`only support !(engine in ["{engine1}", "{engine2}"]) format`))
				}
				return parseToEngineSQL(args[0], "NOT IN")
			default:
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unexpected function %v", functionName))
			}
		default:
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unexpected expr kind %v", expr.Kind()))
		}
	}

	where, err := getFilter(ast.NativeRep().Expr())
	if err != nil {
		return nil, err
	}

	return &store.ListResourceFilter{
		Args:  positionalArgs,
		Where: "(" + where + ")",
	}, nil
}

func (s *DatabaseService) convertToDatabase(ctx context.Context, database *store.DatabaseMessage) (*v1pb.Database, error) {
	instance, err := s.store.GetInstanceV2(ctx, &store.FindInstanceMessage{
		ResourceID: &database.InstanceID,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to find instance")
	}

	environment, effectiveEnvironment := "", ""
	if database.EnvironmentID != "" {
		environment = common.FormatEnvironment(database.EnvironmentID)
	}
	if database.EffectiveEnvironmentID != "" {
		effectiveEnvironment = common.FormatEnvironment(database.EffectiveEnvironmentID)
	}
	instanceResource := convertInstanceMessageToInstanceResource(instance)
	return &v1pb.Database{
		Name:                 common.FormatDatabase(database.InstanceID, database.DatabaseName),
		State:                convertDeletedToState(database.Deleted),
		SuccessfulSyncTime:   database.Metadata.GetLastSyncTime(),
		Project:              common.FormatProject(database.ProjectID),
		Environment:          environment,
		EffectiveEnvironment: effectiveEnvironment,
		SchemaVersion:        database.Metadata.GetVersion(),
		Labels:               database.Metadata.Labels,
		InstanceResource:     instanceResource,
		Drifted:              database.Metadata.GetDrifted(),
	}, nil
}
