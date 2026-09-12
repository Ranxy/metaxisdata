package v1

import (
	"context"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// DatabaseService implements the database service.
type DatabaseService struct {
	v1connect.UnimplementedDatabaseServiceHandler
	store        *store.Store
	schemaSyncer *schemasync.Syncer
}

// NewDatabaseService creates a new DatabaseService.
func NewDatabaseService(store *store.Store, schemaSyncer *schemasync.Syncer) *DatabaseService {
	return &DatabaseService{
		store:        store,
		schemaSyncer: schemaSyncer,
	}
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

func (s *DatabaseService) ListDatabases(ctx context.Context, req *connect.Request[v1pb.ListDatabasesRequest]) (*connect.Response[v1pb.ListDatabasesResponse], error) {
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

	databaseMessages, nextPageToken, err := paginate(databaseMessages, offset)
	if err != nil {
		return nil, err
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

	getTypedMetadataList := func() (list []*v1pb.MetadataResponse_Metadata, err error) {
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
			if subLevelList, nextPageToken, err = paginate(subLevelList, offset); err != nil {
				return nil, err
			}
			typesStoredMetadataMap := make(map[v1pb.MetaType][]*v1pb.StoredMetadata)
			for _, meta := range subLevelList {
				tp := v1pb.MetaType(meta.ObjectType)
				metaMessage := convertStoredMetadataMessage(meta.Metadata)
				typesStoredMetadataMap[tp] = append(typesStoredMetadataMap[tp], metaMessage)
			}

			list = []*v1pb.MetadataResponse_Metadata{}

			for tp, storeLit := range typesStoredMetadataMap {
				list = append(list, &v1pb.MetadataResponse_Metadata{
					MetaType:      tp,
					List:          storeLit,
					NextPageToken: nextPageToken,
				})
			}

			return list, nil
		}
		subLevelFindMessage := &store.FindSubLevelMetaRegistryResourceMessage{
			ParentGUID:          req.Msg.ParentGuid,
			ObjectType:          parentType,
			LimitPreObjectType:  limitPlusOne,
			OffsetPreObjectType: offset.offset,
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

		list = []*v1pb.MetadataResponse_Metadata{}

		for tp, storeLit := range typesStoredMetadataMap {
			page, nextPageToken, err := paginate(storeLit, offset)
			if err != nil {
				return nil, err
			}
			list = append(list, &v1pb.MetadataResponse_Metadata{
				MetaType:      tp,
				List:          page,
				NextPageToken: nextPageToken,
			})
		}

		slices.SortFunc(list, func(a, b *v1pb.MetadataResponse_Metadata) int {
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

	// Search used to be hard-coded to 50 results; keep that as the default now
	// that the request can page.
	size := int(req.Msg.GetPageSize())
	if size <= 0 {
		size = 50
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   size,
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}

	find := &store.SearchMetaRegistryResourceMessage{
		SearchStr: searchStr,
		Limit:     offset.limit + 1,
		Offset:    offset.offset,
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

	list, nextPageToken, err := paginate(list, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.SearchMetadataResponse{NextPageToken: nextPageToken}
	for _, meta := range list {
		response.Results = append(response.Results, &v1pb.SearchMetadataResult{
			Guid:     meta.GUID,
			MetaType: v1pb.MetaType(meta.ObjectType),
			Metadata: convertStoredMetadataMessage(meta.Metadata),
		})
	}

	return connect.NewResponse(response), nil
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

// DiffMetadata computes the schema diff and migration DDL between two metadata versions.

// buildDatabaseSchemaAtTime reconstructs a full DatabaseSchemaMetadata from history at a given time.

// rebuildDatabaseObjects fetches all objects under a database GUID and returns a SchemaMetadata.

// rebuildSchemaContents fetches all objects under a schema GUID and returns the rebuilt SchemaMetadata.

// buildDiffSummary creates a human-readable summary from a MetadataDiff.

func (s *DatabaseService) convertToDatabase(ctx context.Context, database *store.DatabaseMessage) (*v1pb.Database, error) {
	instance, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{
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
