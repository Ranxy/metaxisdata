package v1

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

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
	s.schemaSyncer.QueueLineageAnalysis(created.GUID, storepb.MetaType_MANUAL_SQL)
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

func (s *DatabaseService) ListManualSQLs(ctx context.Context, req *connect.Request[v1pb.ListManualSQLsRequest]) (*connect.Response[v1pb.ListManualSQLsResponse], error) {
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
		ShowDeleted: req.Msg.GetShowDeleted(),
		Limit:       &limitPlusOne,
		Offset:      &offset.offset,
	}
	if instanceID != "-" {
		find.InstanceResourceID = &instanceID
	}
	if databaseName != "-" {
		find.DatabaseName = &databaseName
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

	list, nextPageToken, err := paginate(list, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListManualSQLsResponse{NextPageToken: nextPageToken}
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
		Query:  &query,
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	}
	if instanceID != "-" {
		find.InstanceResourceID = &instanceID
	}
	if databaseName != "-" {
		find.DatabaseName = &databaseName
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
	list, nextPageToken, err := paginate(list, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.SearchManualSQLResponse{NextPageToken: nextPageToken}
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
	s.schemaSyncer.QueueLineageAnalysis(updated.GUID, storepb.MetaType_MANUAL_SQL)
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
