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

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
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

func (s *DatabaseService) GetDatabase(ctx context.Context, req *connect.Request[v1pb.GetDatabaseRequest]) (*connect.Response[v1pb.Database], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("metaxisdata.v1pb.DatabaseService.GetDatabase is not implemented"))
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
	if strings.Contains(req.Msg.GetParentGuid(), common.MetaGuidSplit) {
		parentMeta, err := s.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{Guid: &req.Msg.ParentGuid})
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
				GuidPrefix: &req.Msg.ParentGuid,
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

		} else {
			subLevelFindMessage := &store.FindSubLevelMetaRegistryResourceMessage{
				ParentGuid:         req.Msg.ParentGuid,
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
	}

	typeddMetadataList, err := getTypedMetadataList()
	if err != nil {
		return nil, err
	}

	response := &v1pb.MetadataResponse{TypesStoredMetadata: typeddMetadataList}

	return connect.NewResponse(response), nil
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

// getDatabaseMessage retrieves a database by parsing the database resource name.
// This is a common utility function to avoid code duplication across services.
func getDatabaseMessage(ctx context.Context, s *store.Store, databaseResourceName string) (*store.DatabaseMessage, error) {
	instanceID, databaseName, err := common.GetInstanceDatabaseID(databaseResourceName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse %q", databaseResourceName)
	}
	instance, err := s.GetInstanceV2(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get instance %s", instanceID)
	}
	if instance == nil {
		return nil, errors.Errorf("instance not found")
	}

	find := &store.FindDatabaseMessage{
		InstanceID:      &instanceID,
		DatabaseName:    &databaseName,
		IsCaseSensitive: store.IsObjectCaseSensitive(instance),
		ShowDeleted:     true,
	}
	database, err := s.GetDatabaseV2(ctx, find)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get database")
	}
	if database == nil {
		return nil, errors.Errorf("database %q not found", databaseResourceName)
	}
	return database, nil
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
