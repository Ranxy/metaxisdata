package v1

import (
	"context"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ApplicationService manages the applications that report their SQL and the statements they
// have reported. See .agents/docs/application-sql-ingestion.md.
type ApplicationService struct {
	store *store.Store
}

// NewApplicationService creates the service.
func NewApplicationService(s *store.Store) *ApplicationService {
	return &ApplicationService{store: s}
}

func (s *ApplicationService) CreateApplication(ctx context.Context, req *connect.Request[v1pb.CreateApplicationRequest]) (*connect.Response[v1pb.CreateApplicationResponse], error) {
	application := req.Msg.GetApplication()
	title := strings.TrimSpace(application.GetDisplayName())
	if title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("display_name is required"))
	}

	created, err := s.store.CreateApplication(ctx, title, application.GetDescription(), callerEmail(ctx))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the application")
	}

	// Creating an application issues its first key: the credential is what makes the
	// reporting endpoint reachable, and an application without one cannot do anything.
	plainKey, key, err := s.store.CreateApplicationSQLAPIKey(ctx, created.ApplicationID,
		strings.TrimSpace(req.Msg.GetApiKeyDescription()), callerEmail(ctx))
	if err != nil {
		return nil, errors.Wrap(err, "failed to issue the application's first API key")
	}

	return connect.NewResponse(&v1pb.CreateApplicationResponse{
		Application: convertApplication(created),
		ApiKey:      plainKey,
		Key:         convertApplicationAPIKey(created.ApplicationID, key),
	}), nil
}

func (s *ApplicationService) ListApplications(ctx context.Context, req *connect.Request[v1pb.ListApplicationsRequest]) (*connect.Response[v1pb.ListApplicationsResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   int(req.Msg.GetPageSize()),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	list, err := s.store.ListApplications(ctx, &store.FindApplicationMessage{
		IncludeDisabled: req.Msg.GetShowDisabled(),
		Limit:           &limitPlusOne,
		Offset:          &offset.offset,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the applications")
	}
	list, nextPageToken, err := paginate(list, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListApplicationsResponse{NextPageToken: nextPageToken}
	for _, item := range list {
		response.Applications = append(response.Applications, convertApplication(item))
	}
	return connect.NewResponse(response), nil
}

func (s *ApplicationService) UpdateApplication(ctx context.Context, req *connect.Request[v1pb.UpdateApplicationRequest]) (*connect.Response[v1pb.Application], error) {
	applicationID, err := parseApplicationName(req.Msg.GetApplication().GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	patch := &store.UpdateApplicationMessage{}
	for _, path := range req.Msg.GetUpdateMask().GetPaths() {
		switch path {
		case "display_name":
			title := strings.TrimSpace(req.Msg.GetApplication().GetDisplayName())
			patch.Name = &title
		case "description":
			description := req.Msg.GetApplication().GetDescription()
			patch.Description = &description
		case "disabled":
			disabled := req.Msg.GetApplication().GetDisabled()
			patch.Disabled = &disabled
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update path %q", path))
		}
	}

	updated, err := s.store.UpdateApplication(ctx, applicationID, patch)
	if err != nil {
		return nil, errors.Wrap(err, "failed to update the application")
	}
	return connect.NewResponse(convertApplication(updated)), nil
}

func (s *ApplicationService) DeleteApplication(ctx context.Context, req *connect.Request[v1pb.DeleteApplicationRequest]) (*connect.Response[emptypb.Empty], error) {
	applicationID, err := parseApplicationName(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.store.DeleteApplication(ctx, applicationID); err != nil {
		return nil, errors.Wrap(err, "failed to delete the application")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *ApplicationService) CreateApplicationAPIKey(ctx context.Context, req *connect.Request[v1pb.CreateApplicationAPIKeyRequest]) (*connect.Response[v1pb.CreateApplicationAPIKeyResponse], error) {
	applicationID, err := parseApplicationName(req.Msg.GetParent())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	plainKey, key, err := s.store.CreateApplicationSQLAPIKey(ctx, applicationID,
		strings.TrimSpace(req.Msg.GetDescription()), callerEmail(ctx))
	if err != nil {
		return nil, errors.Wrap(err, "failed to issue the API key")
	}
	return connect.NewResponse(&v1pb.CreateApplicationAPIKeyResponse{
		ApiKey: plainKey,
		Key:    convertApplicationAPIKey(applicationID, key),
	}), nil
}

func (s *ApplicationService) ListApplicationAPIKeys(ctx context.Context, req *connect.Request[v1pb.ListApplicationAPIKeysRequest]) (*connect.Response[v1pb.ListApplicationAPIKeysResponse], error) {
	applicationID, err := parseApplicationName(req.Msg.GetParent())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	keys, err := s.store.ListApplicationSQLAPIKeys(ctx, applicationID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the API keys")
	}
	response := &v1pb.ListApplicationAPIKeysResponse{}
	for _, key := range keys {
		response.Keys = append(response.Keys, convertApplicationAPIKey(applicationID, key))
	}
	return connect.NewResponse(response), nil
}

func (s *ApplicationService) RevokeApplicationAPIKey(ctx context.Context, req *connect.Request[v1pb.RevokeApplicationAPIKeyRequest]) (*connect.Response[emptypb.Empty], error) {
	applicationID, keyID, err := parseApplicationAPIKeyName(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.store.RevokeApplicationSQLAPIKey(ctx, applicationID, keyID); err != nil {
		return nil, errors.Wrap(err, "failed to revoke the API key")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *ApplicationService) ListApplicationSQLs(ctx context.Context, req *connect.Request[v1pb.ListApplicationSQLsRequest]) (*connect.Response[v1pb.ListApplicationSQLsResponse], error) {
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

	find := &store.FindApplicationSQLMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	if instanceID != "-" {
		find.InstanceResourceID = &instanceID
	}
	if databaseName != "-" {
		find.DatabaseName = &databaseName
	}
	if schemaName := strings.TrimSpace(req.Msg.GetSchemaName()); schemaName != "" {
		find.SchemaName = &schemaName
	}
	if application := strings.TrimSpace(req.Msg.GetApplication()); application != "" {
		find.ApplicationID = &application
	}
	if req.Msg.GetKind() != v1pb.ApplicationSQLKind_APPLICATION_SQL_KIND_UNSPECIFIED {
		kind := applicationSQLKindFromV1(req.Msg.GetKind())
		find.Kind = &kind
	}
	if queryText := strings.TrimSpace(req.Msg.GetQuery()); queryText != "" {
		find.Query = &queryText
	}

	list, err := s.store.ListApplicationSQLs(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the reported statements")
	}
	list, nextPageToken, err := paginate(list, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListApplicationSQLsResponse{NextPageToken: nextPageToken}
	for _, item := range list {
		response.ApplicationSqls = append(response.ApplicationSqls, convertApplicationSQL(item))
	}
	return connect.NewResponse(response), nil
}

func (s *ApplicationService) GetApplicationSQL(ctx context.Context, req *connect.Request[v1pb.GetApplicationSQLRequest]) (*connect.Response[v1pb.ApplicationSQL], error) {
	guid := strings.TrimSpace(req.Msg.GetGuid())
	if guid == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}
	statement, err := s.store.GetApplicationSQL(ctx, &store.FindApplicationSQLMessage{GUID: &guid})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the reported statement")
	}
	if statement == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("application SQL %q not found", guid))
	}
	// The detail view is where a reader wants to know who runs this statement, so the
	// per-application breakdown is read on demand rather than with every list row.
	if err := s.attachObservations(ctx, statement); err != nil {
		return nil, err
	}
	return connect.NewResponse(convertApplicationSQL(statement)), nil
}

func (s *ApplicationService) DeleteApplicationSQL(ctx context.Context, req *connect.Request[v1pb.DeleteApplicationSQLRequest]) (*connect.Response[emptypb.Empty], error) {
	guid := strings.TrimSpace(req.Msg.GetGuid())
	if guid == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}
	if err := s.store.DeleteApplicationSQL(ctx, guid); err != nil {
		return nil, errors.Wrap(err, "failed to delete the reported statement")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// attachObservations fills in the per-application breakdown of one statement.
func (s *ApplicationService) attachObservations(ctx context.Context, statement *store.ApplicationSQLMessage) error {
	if statement == nil {
		return nil
	}
	observations, err := s.store.ListApplicationSQLObservations(ctx, statement.ID)
	if err != nil {
		return errors.Wrap(err, "failed to read the statement's observations")
	}
	statement.Observations = observations
	return nil
}

// callerEmail is the created_by of everything this service writes. A reporting key is a
// credential, so issuing one is attributed to the person who did it.
func callerEmail(ctx context.Context) string {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return ""
	}
	return user.Email
}

func parseApplicationName(name string) (string, error) {
	applicationID, err := common.GetApplicationID(name)
	if err != nil {
		return "", errors.Wrap(err, "invalid application name")
	}
	return applicationID, nil
}

func parseApplicationAPIKeyName(name string) (string, int64, error) {
	applicationID, keyID, err := common.GetApplicationAPIKeyID(name)
	if err != nil {
		return "", 0, errors.Wrap(err, "invalid API key name")
	}
	return applicationID, keyID, nil
}

// timestampOrNil keeps an unset time out of the response rather than reporting the epoch.
func timestampOrNil(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func convertApplication(application *store.ApplicationMessage) *v1pb.Application {
	if application == nil {
		return nil
	}
	return &v1pb.Application{
		Name:          common.FormatApplication(application.ApplicationID),
		ApplicationId: application.ApplicationID,
		DisplayName:   application.Name,
		Description:   application.Description,
		Disabled:      application.Disabled,
		CreatedAt:     timestamppb.New(application.CreatedAt),
		UpdatedAt:     timestamppb.New(application.UpdatedAt),
	}
}

func convertApplicationAPIKey(applicationID string, key *store.ApplicationSQLAPIKeyMessage) *v1pb.ApplicationAPIKey {
	if key == nil {
		return nil
	}
	return &v1pb.ApplicationAPIKey{
		Name:                   common.FormatApplicationAPIKey(applicationID, key.ID),
		ApiKeyId:               strconv.FormatInt(key.ID, 10),
		MaskedKey:              key.MaskedKey,
		Description:            key.Description,
		CreatedAt:              timestamppb.New(key.CreatedAt),
		LastUsedAt:             timestampOrNil(key.LastUsedAt),
		RequestCount:           key.RequestCount,
		StatementAcceptedCount: key.StatementAcceptedCount,
		StatementRejectedCount: key.StatementRejectedCount,
	}
}

func convertApplicationSQL(statement *store.ApplicationSQLMessage) *v1pb.ApplicationSQL {
	if statement == nil {
		return nil
	}
	converted := &v1pb.ApplicationSQL{
		Guid:               statement.GUID,
		ApplicationSqlId:   statement.ApplicationSQLID,
		InstanceResourceId: statement.InstanceResourceID,
		DatabaseName:       statement.DatabaseName,
		SchemaName:         statement.SchemaName,
		Engine:             convertToEngine(statement.Engine),
		SqlText:            statement.SQLText,
		Kind:               applicationSQLKindToV1(statement.Kind),
		Fingerprint:        statement.Fingerprint,
		FingerprintVersion: int32(statement.FingerprintVersion),
		ExecutionCount:     statement.ExecutionCount,
		FirstSeenAt:        timestamppb.New(statement.FirstSeenAt),
		LastSeenAt:         timestamppb.New(statement.LastSeenAt),
	}
	for _, observation := range statement.Observations {
		converted.Observations = append(converted.Observations, &v1pb.ApplicationSQLObservation{
			Application:            common.FormatApplication(observation.ApplicationID),
			ApplicationDisplayName: observation.ApplicationName,
			ExecutionCount:         observation.ExecutionCount,
			FirstSeenAt:            timestamppb.New(observation.FirstSeenAt),
			LastSeenAt:             timestamppb.New(observation.LastSeenAt),
		})
	}
	return converted
}

func applicationSQLKindToV1(kind store.ApplicationSQLKind) v1pb.ApplicationSQLKind {
	switch kind {
	case store.ApplicationSQLKindRead:
		return v1pb.ApplicationSQLKind_APPLICATION_SQL_KIND_READ
	case store.ApplicationSQLKindWrite:
		return v1pb.ApplicationSQLKind_APPLICATION_SQL_KIND_WRITE
	case store.ApplicationSQLKindUnknown:
		return v1pb.ApplicationSQLKind_APPLICATION_SQL_KIND_UNKNOWN
	default:
		return v1pb.ApplicationSQLKind_APPLICATION_SQL_KIND_UNSPECIFIED
	}
}

func applicationSQLKindFromV1(kind v1pb.ApplicationSQLKind) store.ApplicationSQLKind {
	switch kind {
	case v1pb.ApplicationSQLKind_APPLICATION_SQL_KIND_READ:
		return store.ApplicationSQLKindRead
	case v1pb.ApplicationSQLKind_APPLICATION_SQL_KIND_WRITE:
		return store.ApplicationSQLKindWrite
	default:
		return store.ApplicationSQLKindUnknown
	}
}
