package metadata

// The metadata tool bodies. MCP and the built-in agent both run these; the
// transport keeps its own schema, permission and audit envelope.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// defaultPageSize is what a listing returns when the caller does not say. A
// model's context is the scarce resource: 25 rows answer the question.
const defaultPageSize = 25

// maxPageSize caps one listing. A larger request is clamped rather than refused.
const maxPageSize = 200

// Tools runs the metadata tools over the read services.
type Tools struct {
	instances  InstanceReader
	databases  DatabaseReader
	principals PrincipalReader
	resolver   *Resolver
}

// PrincipalReader is the slice of the user service whoami uses.
type PrincipalReader interface {
	GetCurrentUser(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[v1pb.User], error)
}

// NewTools builds the metadata tool set over the read services.
func NewTools(instances InstanceReader, databases DatabaseReader, principals PrincipalReader) *Tools {
	return &Tools{
		instances:  instances,
		databases:  databases,
		principals: principals,
		resolver:   NewResolver(instances, databases),
	}
}

// decodeArguments decodes a tool's raw arguments into its typed request.
func decodeArguments(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return invalidArgument("the arguments are not valid JSON for this tool: %v", err)
	}
	return nil
}

// normalizePageSize applies the listing budget: a negative size is a caller
// mistake, and a larger one is clamped rather than refused.
func normalizePageSize(requested int32) (int32, error) {
	switch {
	case requested < 0:
		return 0, invalidArgument("page_size must not be negative")
	case requested == 0:
		return defaultPageSize, nil
	case requested > maxPageSize:
		return maxPageSize, nil
	default:
		return requested, nil
	}
}

// parseMetaType reads a meta type name. An empty value stays unspecified, which
// lets the server infer the type from a GUID.
func parseMetaType(value string) (v1pb.MetaType, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return v1pb.MetaType_UNSPECIFIED, nil
	}
	number, ok := v1pb.MetaType_value[strings.ToUpper(trimmed)]
	if !ok {
		return v1pb.MetaType_UNSPECIFIED, newError(CodeInvalidArgument, fmt.Sprintf("unknown meta type %q", value),
			"valid types include TABLE, VIEW, MATERIALIZED_VIEW, COLUMN, SCHEMA, MANUAL_SQL")
	}
	return v1pb.MetaType(number), nil
}

// parentGUIDOf drops an object's last segment, which is its parent.
func parentGUIDOf(guid string) string {
	if index := strings.LastIndex(guid, ";"); index > 0 {
		return guid[:index]
	}
	return ""
}

// stringsOrEmpty keeps a list field a list.
func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// protoJSON renders one message compactly with lowerCamelCase names and enums by
// name, without the unset fields a listing does not need.
func protoJSON(message proto.Message) (json.RawMessage, error) {
	payload, err := protojson.MarshalOptions{}.Marshal(message)
	if err != nil {
		return nil, internalFailure(err)
	}
	return json.RawMessage(payload), nil
}

// --- list_instances ---

type ListInstancesArgs struct {
	PageSize  int32  `json:"page_size"`
	PageToken string `json:"page_token"`
}

type instanceRow struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Engine      string `json:"engine"`
	Environment string `json:"environment"`
}

type listInstancesResult struct {
	Instances     []instanceRow `json:"instances"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

func (t *Tools) ListInstances(ctx context.Context, raw json.RawMessage) (any, error) {
	var args ListInstancesArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	response, err := t.instances.ListInstances(ctx, connect.NewRequest(&v1pb.ListInstancesRequest{
		PageSize:  pageSize,
		PageToken: args.PageToken,
	}))
	if err != nil {
		return nil, fromRPC(err)
	}
	result := listInstancesResult{Instances: make([]instanceRow, 0, len(response.Msg.GetInstances()))}
	for _, instance := range response.Msg.GetInstances() {
		result.Instances = append(result.Instances, instanceRow{
			Name:        instance.GetName(),
			Title:       instance.GetTitle(),
			Engine:      instance.GetEngine().String(),
			Environment: instance.GetEnvironment(),
		})
	}
	result.NextPageToken = response.Msg.GetNextPageToken()
	return result, nil
}

// --- list_databases ---

type ListDatabasesArgs struct {
	Instance  string `json:"instance"`
	PageSize  int32  `json:"page_size"`
	PageToken string `json:"page_token"`
}

type databaseRow struct {
	Name        string `json:"name"`
	GUID        string `json:"guid"`
	Environment string `json:"environment,omitempty"`
	Synced      string `json:"synced,omitempty"`
}

type listDatabasesResult struct {
	Databases     []databaseRow `json:"databases"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

func (t *Tools) ListDatabases(ctx context.Context, raw json.RawMessage) (any, error) {
	var args ListDatabasesArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Instance) == "" {
		return nil, newError(CodeInvalidArgument, "instance is required",
			"list_instances shows the instances this workspace has")
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	instanceName, err := t.resolver.ResolveInstance(ctx, args.Instance)
	if err != nil {
		return nil, err
	}
	response, err := t.databases.ListDatabases(ctx, connect.NewRequest(&v1pb.ListDatabasesRequest{
		Parent:    instanceName,
		PageSize:  pageSize,
		PageToken: args.PageToken,
	}))
	if err != nil {
		return nil, fromRPC(err)
	}
	result := listDatabasesResult{Databases: make([]databaseRow, 0, len(response.Msg.GetDatabases()))}
	for _, database := range response.Msg.GetDatabases() {
		row := databaseRow{
			Name:        lastPathSegment(database.GetName()),
			GUID:        database.GetGuid(),
			Environment: database.GetEffectiveEnvironment(),
		}
		if synced := database.GetSuccessfulSyncTime(); synced != nil && synced.AsTime().Unix() > 0 {
			row.Synced = synced.AsTime().UTC().Format(time.RFC3339)
		}
		result.Databases = append(result.Databases, row)
	}
	result.NextPageToken = response.Msg.GetNextPageToken()
	return result, nil
}

// --- search_metadata ---

type SearchMetadataArgs struct {
	Keyword   string `json:"keyword"`
	MetaType  string `json:"meta_type"`
	Instance  string `json:"instance"`
	Database  string `json:"database"`
	Schema    string `json:"schema"`
	PageSize  int32  `json:"page_size"`
	PageToken string `json:"page_token"`
}

type objectRow struct {
	GUID       string `json:"guid"`
	Name       string `json:"name"`
	MetaType   string `json:"metaType"`
	ParentGUID string `json:"parentGuid,omitempty"`
}

type objectListResult struct {
	Objects       []objectRow `json:"objects"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
}

func (t *Tools) SearchMetadata(ctx context.Context, raw json.RawMessage) (any, error) {
	var args SearchMetadataArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Keyword) == "" {
		return nil, invalidArgument("keyword is required")
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	request := &v1pb.SearchMetadataRequest{
		SearchStr: args.Keyword,
		PageSize:  pageSize,
		PageToken: args.PageToken,
	}
	if metaType != v1pb.MetaType_UNSPECIFIED {
		request.MetaType = &metaType
	}
	// A scope narrows the search; without one it is a search across the workspace,
	// which is what a caller who has only a name wants. A partial scope is
	// refused rather than ignored: silently searching everything would answer a
	// narrower question than the caller asked.
	scoped := strings.TrimSpace(args.Instance) != "" || strings.TrimSpace(args.Database) != "" || strings.TrimSpace(args.Schema) != ""
	if scoped {
		if strings.TrimSpace(args.Instance) == "" || strings.TrimSpace(args.Database) == "" {
			return nil, newError(CodeInvalidArgument, "a scoped search needs instance and database",
				"search the whole workspace by leaving instance, database and schema out")
		}
		parentGUID, err := t.resolver.ResolveScope(ctx, AnalysisScope{Instance: args.Instance, Database: args.Database, Schema: args.Schema})
		if err != nil {
			return nil, err
		}
		request.ParentGuidPrefix = &parentGUID
	}
	response, err := t.databases.SearchMetadata(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, fromRPC(err)
	}
	result := objectListResult{Objects: make([]objectRow, 0, len(response.Msg.GetResults()))}
	for _, found := range response.Msg.GetResults() {
		result.Objects = append(result.Objects, objectRow{
			GUID:       found.GetGuid(),
			Name:       StoredMetadataName(found.GetMetadata()),
			MetaType:   found.GetMetaType().String(),
			ParentGUID: parentGUIDOf(found.GetGuid()),
		})
	}
	result.NextPageToken = response.Msg.GetNextPageToken()
	return result, nil
}

// --- list_metadata ---

type ListMetadataArgs struct {
	Parent    ObjectRef `json:"parent"`
	MetaType  string    `json:"meta_type"`
	PageSize  int32     `json:"page_size"`
	PageToken string    `json:"page_token"`
}

type metadataListResult struct {
	Objects []objectRow `json:"objects"`
	// NextPageTokens is per type, because a listing that was not narrowed to one
	// type carries a token per type: continuing it means naming a type.
	NextPageTokens map[string]string `json:"nextPageTokens,omitempty"`
}

func (t *Tools) ListMetadata(ctx context.Context, raw json.RawMessage) (any, error) {
	var args ListMetadataArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if args.PageToken != "" && metaType == v1pb.MetaType_UNSPECIFIED {
		// A listing that was not narrowed to one type carries one token per type,
		// so a token without a type says nothing about where to continue.
		return nil, newError(CodeInvalidArgument, "page_token needs meta_type",
			"a listing that was not narrowed to one type carries one token per type; name the type you are continuing")
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	parent, err := t.resolver.ResolveParent(ctx, args.Parent, metaType)
	if err != nil {
		return nil, err
	}
	request := &v1pb.ListMetadataRequest{
		ParentGuid: parent.GUID,
		PageSize:   pageSize,
		PageToken:  args.PageToken,
	}
	if metaType != v1pb.MetaType_UNSPECIFIED {
		request.MetaType = &metaType
	}
	response, err := t.databases.ListMetadata(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, fromRPC(err)
	}
	result := metadataListResult{Objects: make([]objectRow, 0)}
	tokens := map[string]string{}
	for _, group := range response.Msg.GetTypesStoredMetadata() {
		for _, stored := range group.GetList() {
			result.Objects = append(result.Objects, objectRow{
				GUID:       stored.GetGuid(),
				Name:       StoredMetadataName(stored),
				MetaType:   group.GetMetaType().String(),
				ParentGUID: parent.GUID,
			})
		}
		if group.GetNextPageToken() != "" {
			tokens[group.GetMetaType().String()] = group.GetNextPageToken()
		}
	}
	if len(tokens) > 0 {
		result.NextPageTokens = tokens
	}
	return result, nil
}

// --- get_metadata ---

type GetMetadataArgs struct {
	Object   ObjectRef `json:"object"`
	MetaType string    `json:"meta_type"`
}

type getMetadataResult struct {
	GUID     string          `json:"guid"`
	MetaType string          `json:"metaType,omitempty"`
	Name     string          `json:"name,omitempty"`
	Metadata json.RawMessage `json:"metadata"`
}

func (t *Tools) GetMetadata(ctx context.Context, raw json.RawMessage) (any, error) {
	var args GetMetadataArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		// The registry cannot infer an object's type from its GUID, and the RPC
		// refuses without one. Saying that here is the difference between a model
		// retrying the same call and one that fixes it.
		return nil, newError(CodeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
	}
	object, err := t.resolver.ResolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	response, err := t.databases.GetMetadata(ctx, connect.NewRequest(&v1pb.GetMetadataRequest{
		Guid:     object.GUID,
		MetaType: object.MetaType,
	}))
	if err != nil {
		return nil, fromRPC(err)
	}
	metadata, err := protoJSON(response.Msg.GetMetadata())
	if err != nil {
		return nil, err
	}
	return getMetadataResult{
		GUID:     object.GUID,
		MetaType: object.MetaType.String(),
		Name:     object.Name,
		Metadata: metadata,
	}, nil
}

// --- get_ddl ---

type GetDDLArgs struct {
	Object   ObjectRef `json:"object"`
	MetaType string    `json:"meta_type"`
}

type getDDLResult struct {
	GUID   string `json:"guid"`
	Schema string `json:"schema"`
}

func (t *Tools) GetDDL(ctx context.Context, raw json.RawMessage) (any, error) {
	var args GetDDLArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		return nil, newError(CodeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
	}
	object, err := t.resolver.ResolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	response, err := t.databases.GetSchemaString(ctx, connect.NewRequest(&v1pb.GetSchemaStringRequest{
		Guid:     object.GUID,
		MetaType: object.MetaType,
	}))
	if err != nil {
		return nil, fromRPC(err)
	}
	return getDDLResult{GUID: object.GUID, Schema: response.Msg.GetSchema()}, nil
}

type whoamiResult struct {
	User        whoamiUser `json:"user"`
	Permissions []string   `json:"permissions"`
}

type whoamiUser struct {
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

func (t *Tools) Whoami(ctx context.Context) (any, error) {
	response, err := t.principals.GetCurrentUser(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return nil, fromRPC(err)
	}
	return whoamiResult{
		User:        whoamiUser{Email: response.Msg.GetEmail(), Name: response.Msg.GetTitle()},
		Permissions: stringsOrEmpty(response.Msg.GetPermissions()),
	}, nil
}
