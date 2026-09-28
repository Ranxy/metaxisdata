package mcp

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// objectRef addresses one object the way a person would: by name. Every field
// takes a name or the id a listing returned, and GUID short-circuits the rest — a
// caller that already has one, from an earlier result, passes it through instead
// of re-deriving it.
//
// Nothing here builds a GUID. The resolvers read the ones the read RPCs return,
// because the segment layout differs per engine (a MySQL-family schema segment is
// empty, PostgreSQL needs it), and getting that wrong produces an identifier that
// silently matches nothing.
type objectRef struct {
	GUID     string `json:"guid,omitempty"`
	Instance string `json:"instance,omitempty"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Name     string `json:"name,omitempty"`
}

// candidate is one plausible answer when a name was unknown or ambiguous: what a
// model needs to make its next call right instead of guessing again.
type candidate struct {
	GUID     string `json:"guid"`
	Name     string `json:"name"`
	MetaType string `json:"metaType,omitempty"`
}

// resolvedObject is what an object-addressed tool needs.
type resolvedObject struct {
	GUID     string
	Name     string
	MetaType v1pb.MetaType
}

// refLookupPageSize bounds the listings a resolver walks while matching a name.
// They are lookups, not answers: a name that needs a second page is a name the
// caller should search for instead.
const refLookupPageSize = 100

// resolveObject turns an object reference into the GUID the read RPCs take.
func (s *Server) resolveObject(ctx context.Context, ref objectRef, metaType v1pb.MetaType) (resolvedObject, error) {
	if ref.GUID != "" {
		return resolvedObject{GUID: ref.GUID, MetaType: metaType}, nil
	}
	if ref.Name == "" {
		return resolvedObject{}, newToolError(codeInvalidArgument, "object.name is required unless object.guid is given",
			"address the object as {instance, database, schema?, name}, or pass a guid from an earlier result")
	}
	if ref.Instance == "" || ref.Database == "" {
		return resolvedObject{}, newToolError(codeInvalidArgument, "object.instance and object.database are required unless object.guid is given",
			"an object's name is unique only inside one database")
	}

	instanceName, err := s.resolveInstance(ctx, ref.Instance)
	if err != nil {
		return resolvedObject{}, err
	}
	parentGUID, err := s.resolveDatabase(ctx, instanceName, ref.Database)
	if err != nil {
		return resolvedObject{}, err
	}
	if ref.Schema != "" {
		parentGUID, err = s.resolveSchema(ctx, parentGUID, ref.Schema)
		if err != nil {
			return resolvedObject{}, err
		}
	}
	return s.searchObject(ctx, parentGUID, ref.Name, metaType)
}

// analysisScope is one instance/database/schema a statement is analyzed against.
type analysisScope struct {
	Instance string `json:"instance"`
	Database string `json:"database"`
	Schema   string `json:"schema,omitempty"`
}

// resolveScope turns one scope into the GUID the SQL analyzer resolves unqualified
// names against.
func (s *Server) resolveScope(ctx context.Context, scope analysisScope) (string, error) {
	if scope.Instance == "" || scope.Database == "" {
		return "", newToolError(codeInvalidArgument, "a scope needs both instance and database",
			"a statement's unqualified names cannot say which database they belong to")
	}
	instanceName, err := s.resolveInstance(ctx, scope.Instance)
	if err != nil {
		return "", err
	}
	databaseGUID, err := s.resolveDatabase(ctx, instanceName, scope.Database)
	if err != nil {
		return "", err
	}
	if scope.Schema == "" {
		return databaseGUID, nil
	}
	return s.resolveSchema(ctx, databaseGUID, scope.Schema)
}

// resolveInstance accepts a resource name ("instances/1"), a bare id, or a
// display title, and returns the resource name the listings take.
func (s *Server) resolveInstance(ctx context.Context, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "instances/") {
		return trimmed, nil
	}
	response, err := s.config.Instances.ListInstances(ctx, connect.NewRequest(&v1pb.ListInstancesRequest{
		PageSize: refLookupPageSize,
	}))
	if err != nil {
		return "", toolErrorFromRPC(err)
	}
	var matches []*v1pb.Instance
	for _, instance := range response.Msg.GetInstances() {
		if strings.EqualFold(instance.GetTitle(), trimmed) || instance.GetName() == instanceNamePrefix+trimmed {
			matches = append(matches, instance)
		}
	}
	switch len(matches) {
	case 0:
		candidates := make([]candidate, 0, len(response.Msg.GetInstances()))
		for _, instance := range response.Msg.GetInstances() {
			candidates = append(candidates, candidate{GUID: instance.GetName(), Name: instance.GetTitle()})
		}
		return "", notFound("instance", value, candidates)
	case 1:
		return matches[0].GetName(), nil
	default:
		return "", ambiguous("instance", value, instanceCandidates(matches))
	}
}

// resolveDatabase accepts a GUID (a semicolon means the caller pasted one), a
// resource name, or a bare database name, and returns the canonical GUID.
func (s *Server) resolveDatabase(ctx context.Context, instanceName, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, ";") {
		// A pasted GUID short-circuits the lookup, but it has to belong to the
		// instance the caller named: otherwise the reference silently addresses an
		// object in a different instance, which is the one thing the reference is
		// supposed to make impossible.
		if !guidBelongsToInstance(trimmed, instanceName) {
			return "", newToolError(codeInvalidArgument, "the database guid does not belong to that instance",
				"pass a database name, or a guid from list_databases of the instance you named")
		}
		return trimmed, nil
	}
	response, err := s.config.Databases.ListDatabases(ctx, connect.NewRequest(&v1pb.ListDatabasesRequest{
		Parent:   instanceName,
		PageSize: refLookupPageSize,
	}))
	if err != nil {
		return "", toolErrorFromRPC(err)
	}
	var matches []*v1pb.Database
	for _, database := range response.Msg.GetDatabases() {
		if strings.EqualFold(database.GetName(), trimmed) ||
			strings.EqualFold(lastPathSegment(database.GetName()), trimmed) ||
			database.GetGuid() == trimmed {
			matches = append(matches, database)
		}
	}
	switch len(matches) {
	case 0:
		candidates := make([]candidate, 0, len(response.Msg.GetDatabases()))
		for _, database := range response.Msg.GetDatabases() {
			candidates = append(candidates, candidate{GUID: database.GetGuid(), Name: lastPathSegment(database.GetName())})
		}
		return "", notFound("database", value, candidates)
	case 1:
		return matches[0].GetGuid(), nil
	default:
		return "", ambiguous("database", value, databaseCandidates(matches))
	}
}

// resolveSchema finds a schema by name under a database, which is only meaningful
// on engines that have a named schema level; on the MySQL family the level exists
// but is empty, and a caller that names a schema there gets the candidates back.
func (s *Server) resolveSchema(ctx context.Context, databaseGUID, value string) (string, error) {
	response, err := s.config.Databases.ListMetadata(ctx, connect.NewRequest(&v1pb.ListMetadataRequest{
		ParentGuid: databaseGUID,
		MetaType:   pointer(v1pb.MetaType_SCHEMA),
		PageSize:   refLookupPageSize,
	}))
	if err != nil {
		return "", toolErrorFromRPC(err)
	}
	var matches []*v1pb.StoredMetadata
	var candidates []candidate
	for _, group := range response.Msg.GetTypesStoredMetadata() {
		if group.GetMetaType() != v1pb.MetaType_SCHEMA {
			continue
		}
		for _, stored := range group.GetList() {
			candidates = append(candidates, candidate{GUID: stored.GetGuid(), Name: storedMetadataName(stored), MetaType: group.GetMetaType().String()})
			if strings.EqualFold(storedMetadataName(stored), value) {
				matches = append(matches, stored)
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", notFound("schema", value, candidates)
	case 1:
		return matches[0].GetGuid(), nil
	default:
		return "", ambiguous("schema", value, candidates)
	}
}

// searchObject finds one object by name under a parent GUID. The search itself is
// the candidate list when nothing matches exactly: a near miss is what tells the
// caller whether the name is wrong or the scope is.
func (s *Server) searchObject(ctx context.Context, parentGUID, name string, metaType v1pb.MetaType) (resolvedObject, error) {
	request := &v1pb.SearchMetadataRequest{
		SearchStr:        name,
		PageSize:         refLookupPageSize,
		ParentGuidPrefix: &parentGUID,
	}
	if metaType != v1pb.MetaType_UNSPECIFIED {
		request.MetaType = &metaType
	}
	response, err := s.config.Databases.SearchMetadata(ctx, connect.NewRequest(request))
	if err != nil {
		return resolvedObject{}, toolErrorFromRPC(err)
	}
	var matches []*v1pb.SearchMetadataResult
	for _, result := range response.Msg.GetResults() {
		// The prefix filter is a string prefix, so "1;shop" would also match
		// "1;shop_archive"; requiring the parent to be a whole segment prefix keeps
		// a sibling database's objects out of the answer.
		if !underParent(result.GetGuid(), parentGUID) {
			continue
		}
		if !strings.EqualFold(storedMetadataName(result.GetMetadata()), name) {
			continue
		}
		if metaType != v1pb.MetaType_UNSPECIFIED && result.GetMetaType() != metaType {
			continue
		}
		matches = append(matches, result)
	}
	switch len(matches) {
	case 0:
		return resolvedObject{}, notFound("object", name, searchCandidates(response.Msg.GetResults()))
	case 1:
		return resolvedObject{
			GUID:     matches[0].GetGuid(),
			Name:     storedMetadataName(matches[0].GetMetadata()),
			MetaType: matches[0].GetMetaType(),
		}, nil
	default:
		return resolvedObject{}, ambiguous("object", name, searchCandidates(matches))
	}
}

// scopeRequired answers a statement that arrived without a scope. The databases
// are listed because the analyzer cannot choose one: picking a database would
// silently answer about whichever one it picked.
func (s *Server) scopeRequired(ctx context.Context) *toolError {
	databases, err := s.availableDatabases(ctx)
	if err != nil {
		return newToolError(codeScopeRequired, "analyze_sql needs at least one scope",
			"pass scopes: [{instance, database, schema?}]; the databases could not be listed right now")
	}
	failure := newToolError(codeScopeRequired, "analyze_sql needs at least one scope",
		"pass scopes: [{instance, database, schema?}]; which database a statement belongs to is a decision only the caller can make")
	if len(databases) > 0 {
		failure.Details = map[string]any{"databases": databases}
	}
	return failure
}

// availableDatabases lists what a scope could name, bounded by the lookup page
// size so a deployment with many databases does not turn one failure into a large
// answer.
func (s *Server) availableDatabases(ctx context.Context) ([]scopeCandidate, error) {
	instances, err := s.config.Instances.ListInstances(ctx, connect.NewRequest(&v1pb.ListInstancesRequest{
		PageSize: refLookupPageSize,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	var candidates []scopeCandidate
	for _, instance := range instances.Msg.GetInstances() {
		databases, err := s.config.Databases.ListDatabases(ctx, connect.NewRequest(&v1pb.ListDatabasesRequest{
			Parent:   instance.GetName(),
			PageSize: refLookupPageSize,
		}))
		if err != nil {
			continue
		}
		for _, database := range databases.Msg.GetDatabases() {
			candidates = append(candidates, scopeCandidate{
				Instance: instance.GetTitle(),
				Database: lastPathSegment(database.GetName()),
				GUID:     database.GetGuid(),
			})
		}
	}
	return candidates, nil
}

// scopeCandidate is one database a caller could analyze a statement against.
type scopeCandidate struct {
	Instance string `json:"instance"`
	Database string `json:"database"`
	GUID     string `json:"guid"`
}

// guidBelongsToInstance reports whether a GUID's first segment is the instance's
// own id, which is what keeps a guid from one instance from addressing another.
func guidBelongsToInstance(guid, instanceName string) bool {
	instanceID := strings.TrimPrefix(instanceName, instanceNamePrefix)
	return instanceID != "" && strings.HasPrefix(guid, instanceID+";")
}

// underParent reports whether guid is a direct child of parent. Both are
// semicolon-separated identifiers, so the parent has to match a whole segment:
// that is what keeps "1;shop" from swallowing "1;shop_archive".
func underParent(guid, parent string) bool {
	return parent != "" && strings.HasPrefix(guid, parent+";")
}

func instanceCandidates(instances []*v1pb.Instance) []candidate {
	candidates := make([]candidate, 0, len(instances))
	for _, instance := range instances {
		candidates = append(candidates, candidate{GUID: instance.GetName(), Name: instance.GetTitle()})
	}
	return candidates
}

func databaseCandidates(databases []*v1pb.Database) []candidate {
	candidates := make([]candidate, 0, len(databases))
	for _, database := range databases {
		candidates = append(candidates, candidate{GUID: database.GetGuid(), Name: lastPathSegment(database.GetName())})
	}
	return candidates
}

func searchCandidates(results []*v1pb.SearchMetadataResult) []candidate {
	candidates := make([]candidate, 0, len(results))
	for _, result := range results {
		candidates = append(candidates, candidate{
			GUID:     result.GetGuid(),
			Name:     storedMetadataName(result.GetMetadata()),
			MetaType: result.GetMetaType().String(),
		})
	}
	return candidates
}

// lastPathSegment reads the trailing name out of a resource name such as
// "instances/1/databases/shop".
func lastPathSegment(name string) string {
	if index := strings.LastIndex(name, "/"); index >= 0 {
		return name[index+1:]
	}
	return name
}

// storedMetadataName reads an object's name out of the stored payload, which is
// the one thing the typed metadata messages have in common. The CLI has the same
// switch for the same reason.
func storedMetadataName(stored *v1pb.StoredMetadata) string {
	switch typed := stored.GetType().(type) {
	case *v1pb.StoredMetadata_DatabaseSchemaMetadata:
		return typed.DatabaseSchemaMetadata.GetName()
	case *v1pb.StoredMetadata_SchemaMetadata:
		return typed.SchemaMetadata.GetName()
	case *v1pb.StoredMetadata_TableMetadata:
		return typed.TableMetadata.GetName()
	case *v1pb.StoredMetadata_ExternalTableMetadata:
		return typed.ExternalTableMetadata.GetName()
	case *v1pb.StoredMetadata_ViewMetadata:
		return typed.ViewMetadata.GetName()
	case *v1pb.StoredMetadata_MaterializedViewMetadata:
		return typed.MaterializedViewMetadata.GetName()
	case *v1pb.StoredMetadata_FunctionMetadata:
		return typed.FunctionMetadata.GetName()
	case *v1pb.StoredMetadata_ProcedureMetadata:
		return typed.ProcedureMetadata.GetName()
	case *v1pb.StoredMetadata_SequenceMetadata:
		return typed.SequenceMetadata.GetName()
	case *v1pb.StoredMetadata_ColumnMetadata:
		return typed.ColumnMetadata.GetName()
	case *v1pb.StoredMetadata_ManualSqlMetadata:
		return typed.ManualSqlMetadata.GetTitle()
	default:
		return ""
	}
}

func pointer[T any](value T) *T { return &value }

// instanceNamePrefix is the resource-name prefix an instance id is bare of.
const instanceNamePrefix = "instances/"
