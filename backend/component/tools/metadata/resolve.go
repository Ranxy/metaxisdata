package metadata

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// ObjectRef addresses one object the way a person would: by name. Every field
// takes a name or the id a listing returned, and GUID short-circuits the rest — a
// caller that already has one, from an earlier result, passes it through instead
// of re-deriving it.
//
// Nothing here builds a GUID. The resolvers read the ones the read RPCs return,
// because the segment layout differs per engine (a MySQL-family schema segment is
// empty, PostgreSQL needs it), and getting that wrong produces an identifier that
// silently matches nothing.
type ObjectRef struct {
	GUID     string `json:"guid,omitempty"`
	Instance string `json:"instance,omitempty"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Name     string `json:"name,omitempty"`
}

// Candidate is one plausible answer when a name was unknown or ambiguous: what a
// model needs to make its next call right instead of guessing again.
type Candidate struct {
	GUID     string `json:"guid"`
	Name     string `json:"name"`
	MetaType string `json:"metaType,omitempty"`
}

// Resolved is what an object-addressed tool needs.
type Resolved struct {
	GUID     string
	Name     string
	MetaType v1pb.MetaType
}

// AnalysisScope is one instance/database/schema a statement is analyzed against.
type AnalysisScope struct {
	Instance string `json:"instance"`
	Database string `json:"database"`
	Schema   string `json:"schema,omitempty"`
}

// ScopeCandidate is one database a caller could analyze a statement against.
type ScopeCandidate struct {
	Instance string `json:"instance"`
	Database string `json:"database"`
	GUID     string `json:"guid"`
}

// InstanceReader is the slice of the instance service the resolvers use.
type InstanceReader interface {
	ListInstances(context.Context, *connect.Request[v1pb.ListInstancesRequest]) (*connect.Response[v1pb.ListInstancesResponse], error)
}

// DatabaseReader is the slice of the database service the resolvers use.
type DatabaseReader interface {
	ListDatabases(context.Context, *connect.Request[v1pb.ListDatabasesRequest]) (*connect.Response[v1pb.ListDatabasesResponse], error)
	ListMetadata(context.Context, *connect.Request[v1pb.ListMetadataRequest]) (*connect.Response[v1pb.MetadataResponse], error)
	SearchMetadata(context.Context, *connect.Request[v1pb.SearchMetadataRequest]) (*connect.Response[v1pb.SearchMetadataResponse], error)
}

// Resolver turns name paths into the identifiers the read RPCs take.
type Resolver struct {
	instances InstanceReader
	databases DatabaseReader
}

// NewResolver builds a resolver over the read services.
func NewResolver(instances InstanceReader, databases DatabaseReader) *Resolver {
	return &Resolver{instances: instances, databases: databases}
}

// refLookupPageSize bounds the listings a resolver walks while matching a name.
// They are lookups, not answers: a name that needs a second page is a name the
// caller should search for instead.
const refLookupPageSize = 100

// instanceNamePrefix is the resource-name prefix an instance id is bare of.
const instanceNamePrefix = "instances/"

// ResolveObject turns an object reference into the GUID the read RPCs take.
func (r *Resolver) ResolveObject(ctx context.Context, ref ObjectRef, metaType v1pb.MetaType) (Resolved, error) {
	if ref.GUID != "" {
		return Resolved{GUID: ref.GUID, MetaType: metaType}, nil
	}
	if ref.Name == "" {
		return Resolved{}, invalidArgument("object.name is required unless object.guid is given")
	}
	if ref.Instance == "" || ref.Database == "" {
		return Resolved{}, invalidArgument("object.instance and object.database are required unless object.guid is given")
	}

	instanceName, err := r.ResolveInstance(ctx, ref.Instance)
	if err != nil {
		return Resolved{}, err
	}
	parentGUID, err := r.ResolveDatabase(ctx, instanceName, ref.Database)
	if err != nil {
		return Resolved{}, err
	}
	if ref.Schema != "" {
		parentGUID, err = r.ResolveSchema(ctx, parentGUID, ref.Schema)
		if err != nil {
			return Resolved{}, err
		}
	}
	return r.SearchObject(ctx, parentGUID, ref.Name, metaType)
}

// ResolveScope turns one scope into the GUID the SQL analyzer resolves unqualified
// names against.
func (r *Resolver) ResolveScope(ctx context.Context, scope AnalysisScope) (string, error) {
	if scope.Instance == "" || scope.Database == "" {
		return "", invalidArgument("a scope needs both instance and database")
	}
	instanceName, err := r.ResolveInstance(ctx, scope.Instance)
	if err != nil {
		return "", err
	}
	databaseGUID, err := r.ResolveDatabase(ctx, instanceName, scope.Database)
	if err != nil {
		return "", err
	}
	if scope.Schema == "" {
		return databaseGUID, nil
	}
	return r.ResolveSchema(ctx, databaseGUID, scope.Schema)
}

// ResolveInstance accepts a resource name ("instances/1"), a bare id, or a
// display title, and returns the resource name the listings take.
func (r *Resolver) ResolveInstance(ctx context.Context, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, instanceNamePrefix) {
		return trimmed, nil
	}
	response, err := r.instances.ListInstances(ctx, connect.NewRequest(&v1pb.ListInstancesRequest{
		PageSize: refLookupPageSize,
	}))
	if err != nil {
		return "", fromRPC(err)
	}
	var matches []*v1pb.Instance
	for _, instance := range response.Msg.GetInstances() {
		if strings.EqualFold(instance.GetTitle(), trimmed) || instance.GetName() == instanceNamePrefix+trimmed {
			matches = append(matches, instance)
		}
	}
	switch len(matches) {
	case 0:
		return "", notFound("instance", value, instanceCandidates(response.Msg.GetInstances()))
	case 1:
		return matches[0].GetName(), nil
	default:
		return "", ambiguous("instance", value, instanceCandidates(matches))
	}
}

// ResolveDatabase accepts a GUID (a semicolon means the caller pasted one), a
// resource name, or a bare database name, and returns the canonical GUID.
func (r *Resolver) ResolveDatabase(ctx context.Context, instanceName, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, ";") {
		// A pasted GUID short-circuits the lookup, but it has to belong to the
		// instance the caller named: otherwise the reference silently addresses an
		// object in a different instance.
		if !guidBelongsToInstance(trimmed, instanceName) {
			return "", invalidArgument("the database guid does not belong to that instance")
		}
		return trimmed, nil
	}
	response, err := r.databases.ListDatabases(ctx, connect.NewRequest(&v1pb.ListDatabasesRequest{
		Parent:   instanceName,
		PageSize: refLookupPageSize,
	}))
	if err != nil {
		return "", fromRPC(err)
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
		return "", notFound("database", value, databaseCandidates(response.Msg.GetDatabases()))
	case 1:
		return matches[0].GetGuid(), nil
	default:
		return "", ambiguous("database", value, databaseCandidates(matches))
	}
}

// ResolveSchema finds a schema by name under a database, which is only meaningful
// on engines that have a named schema level; on the MySQL family the level exists
// but is empty, and a caller that names a schema there gets the candidates back.
func (r *Resolver) ResolveSchema(ctx context.Context, databaseGUID, value string) (string, error) {
	response, err := r.databases.ListMetadata(ctx, connect.NewRequest(&v1pb.ListMetadataRequest{
		ParentGuid: databaseGUID,
		MetaType:   pointer(v1pb.MetaType_SCHEMA),
		PageSize:   refLookupPageSize,
	}))
	if err != nil {
		return "", fromRPC(err)
	}
	var matches []*v1pb.StoredMetadata
	var candidates []Candidate
	for _, group := range response.Msg.GetTypesStoredMetadata() {
		if group.GetMetaType() != v1pb.MetaType_SCHEMA {
			continue
		}
		for _, stored := range group.GetList() {
			candidates = append(candidates, Candidate{GUID: stored.GetGuid(), Name: StoredMetadataName(stored), MetaType: group.GetMetaType().String()})
			if strings.EqualFold(StoredMetadataName(stored), value) {
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

// SearchObject finds one object by name under a parent GUID. The search itself is
// the candidate list when nothing matches exactly: a near miss is what tells the
// caller whether the name is wrong or the scope is.
func (r *Resolver) SearchObject(ctx context.Context, parentGUID, name string, metaType v1pb.MetaType) (Resolved, error) {
	request := &v1pb.SearchMetadataRequest{
		SearchStr:        name,
		PageSize:         refLookupPageSize,
		ParentGuidPrefix: &parentGUID,
	}
	if metaType != v1pb.MetaType_UNSPECIFIED {
		request.MetaType = &metaType
	}
	response, err := r.databases.SearchMetadata(ctx, connect.NewRequest(request))
	if err != nil {
		return Resolved{}, fromRPC(err)
	}
	var matches []*v1pb.SearchMetadataResult
	for _, result := range response.Msg.GetResults() {
		// The prefix filter is a string prefix, so "1;shop" would also match
		// "1;shop_archive"; requiring the parent to be a whole segment prefix keeps
		// a sibling database's objects out of the answer.
		if !underParent(result.GetGuid(), parentGUID) {
			continue
		}
		if !strings.EqualFold(StoredMetadataName(result.GetMetadata()), name) {
			continue
		}
		if metaType != v1pb.MetaType_UNSPECIFIED && result.GetMetaType() != metaType {
			continue
		}
		matches = append(matches, result)
	}
	switch len(matches) {
	case 0:
		return Resolved{}, notFound("object", name, searchCandidates(response.Msg.GetResults()))
	case 1:
		return Resolved{
			GUID:     matches[0].GetGuid(),
			Name:     StoredMetadataName(matches[0].GetMetadata()),
			MetaType: matches[0].GetMetaType(),
		}, nil
	default:
		return Resolved{}, ambiguous("object", name, searchCandidates(matches))
	}
}

// ScopeRequired answers a statement that arrived without a scope. The databases
// are listed because the analyzer cannot choose one: picking a database would
// silently answer about whichever one it picked.
func (r *Resolver) ScopeRequired(ctx context.Context) *Error {
	databases, err := r.AvailableDatabases(ctx)
	if err != nil {
		return newError(CodeScopeRequired, "analyze_sql needs at least one scope",
			"pass scopes: [{instance, database, schema?}]; the databases could not be listed right now")
	}
	failure := newError(CodeScopeRequired, "analyze_sql needs at least one scope",
		"pass scopes: [{instance, database, schema?}]; which database a statement belongs to is a decision only the caller can make")
	if len(databases) > 0 {
		failure.Details = map[string]any{"databases": databases}
	}
	return failure
}

// AvailableDatabases lists what a scope could name, bounded by the lookup page
// size so a deployment with many databases does not turn one failure into a large
// answer.
func (r *Resolver) AvailableDatabases(ctx context.Context) ([]ScopeCandidate, error) {
	instances, err := r.instances.ListInstances(ctx, connect.NewRequest(&v1pb.ListInstancesRequest{
		PageSize: refLookupPageSize,
	}))
	if err != nil {
		return nil, fromRPC(err)
	}
	var candidates []ScopeCandidate
	for _, instance := range instances.Msg.GetInstances() {
		databases, err := r.databases.ListDatabases(ctx, connect.NewRequest(&v1pb.ListDatabasesRequest{
			Parent:   instance.GetName(),
			PageSize: refLookupPageSize,
		}))
		if err != nil {
			continue
		}
		for _, database := range databases.Msg.GetDatabases() {
			candidates = append(candidates, ScopeCandidate{
				Instance: instance.GetTitle(),
				Database: lastPathSegment(database.GetName()),
				GUID:     database.GetGuid(),
			})
		}
	}
	return candidates, nil
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

// instanceCandidates lists what an instance name could have meant.
func instanceCandidates(instances []*v1pb.Instance) []Candidate {
	candidates := make([]Candidate, 0, len(instances))
	for _, instance := range instances {
		candidates = append(candidates, Candidate{GUID: instance.GetName(), Name: instance.GetTitle()})
	}
	return candidates
}

// databaseCandidates lists what a database name could have meant.
func databaseCandidates(databases []*v1pb.Database) []Candidate {
	candidates := make([]Candidate, 0, len(databases))
	for _, database := range databases {
		candidates = append(candidates, Candidate{GUID: database.GetGuid(), Name: lastPathSegment(database.GetName())})
	}
	return candidates
}

// searchCandidates lists what an object search found.
func searchCandidates(results []*v1pb.SearchMetadataResult) []Candidate {
	candidates := make([]Candidate, 0, len(results))
	for _, result := range results {
		candidates = append(candidates, Candidate{
			GUID:     result.GetGuid(),
			Name:     StoredMetadataName(result.GetMetadata()),
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

// StoredMetadataName reads an object's name out of the stored payload, which is
// the one thing the typed metadata messages have in common.
func StoredMetadataName(stored *v1pb.StoredMetadata) string {
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

// pointer returns a pointer to value, for request fields that need one.
func pointer[T any](value T) *T { return &value }
