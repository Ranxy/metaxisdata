package v1

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// maxAnalyzeSQLScopes bounds one request. The cap is what stops a caller
	// from turning "analyze this statement" into a scan of every database they
	// can see.
	maxAnalyzeSQLScopes = 10
	// maxAnalyzeSQLTextBytes bounds the statement. Parsing is CPU bound and the
	// endpoint is reachable by any workspace member.
	maxAnalyzeSQLTextBytes = 1 << 20
	// tempResultTable is the synthetic target the analyzers report for a query
	// whose result is not written anywhere. It never exists in the metadata
	// registry, so it must not be turned into a GUID.
	tempResultTable = "__result__"
)

// AnalyzeSQL parses a statement and resolves it against each requested scope.
// The analysis is stateless: nothing is written and no runner is triggered.
//
// Every scope is analyzed independently, because the scopes may live on
// different instances and even different engines. A scope that cannot be
// analyzed is reported as a warning on that scope alone; the request fails only
// when every scope failed the same way, which is what a single-scope call with
// an unusable scope looks like.
func (s *LineageService) AnalyzeSQL(ctx context.Context, req *connect.Request[v1pb.AnalyzeSQLRequest]) (*connect.Response[v1pb.AnalyzeSQLResponse], error) {
	scopes := req.Msg.GetScopes()
	if len(scopes) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("at least one scope is required"))
	}
	if len(scopes) > maxAnalyzeSQLScopes {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("at most %d scopes are supported, got %d", maxAnalyzeSQLScopes, len(scopes)))
	}
	sqlText := req.Msg.GetSqlText()
	if strings.TrimSpace(sqlText) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("sql_text is required"))
	}
	if len(sqlText) > maxAnalyzeSQLTextBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("sql_text must be at most %d bytes", maxAnalyzeSQLTextBytes))
	}

	response := &v1pb.AnalyzeSQLResponse{}
	failures := make([]*analyzeSQLFailure, 0, len(scopes))
	for _, scope := range scopes {
		result, failure := s.analyzeSQLScope(ctx, scope, sqlText)
		response.Results = append(response.Results, result)
		if failure != nil {
			failures = append(failures, failure)
		}
	}

	// A single unusable scope is an error rather than a silent empty answer,
	// while a list where only some scopes failed still succeeds: "--scope all"
	// must not be defeated by one engine that has no analyzer.
	if len(failures) == len(scopes) && sameAnalyzeSQLFailure(failures) {
		return nil, connect.NewError(failures[0].code, errors.New(failures[0].message))
	}
	return connect.NewResponse(response), nil
}

// analyzeSQLFailure is a scope that could not be analyzed at all, kept so the
// request can tell whether every scope failed for the same reason.
type analyzeSQLFailure struct {
	code    connect.Code
	message string
}

func sameAnalyzeSQLFailure(failures []*analyzeSQLFailure) bool {
	for _, failure := range failures[1:] {
		if failure.code != failures[0].code {
			return false
		}
	}
	return true
}

// analyzeSQLScope resolves one scope. A failure always comes with a result that
// carries the scope name, the scope GUID and a warning, so the caller can
// either report it per scope or fail the whole request.
func (s *LineageService) analyzeSQLScope(ctx context.Context, scope *v1pb.AnalysisScope, sqlText string) (*v1pb.AnalyzeSQLResult, *analyzeSQLFailure) {
	result := &v1pb.AnalyzeSQLResult{ScopeName: scope.GetName(), ScopeGuid: scope.GetGuid()}
	fail := func(code connect.Code, format string, args ...any) (*v1pb.AnalyzeSQLResult, *analyzeSQLFailure) {
		message := fmt.Sprintf(format, args...)
		result.Warnings = append(result.Warnings, message)
		return result, &analyzeSQLFailure{code: code, message: message}
	}

	analysisContext, err := analyzeSQLScopeContext(scope.GetGuid())
	if err != nil {
		return fail(connect.CodeInvalidArgument, "scope %q is not a valid analysis scope: %v", scope.GetGuid(), err)
	}

	instance, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &analysisContext.InstanceID})
	if err != nil {
		return fail(connect.CodeInternal, "failed to get instance %q: %v", analysisContext.InstanceID, err)
	}
	if instance == nil {
		return fail(connect.CodeNotFound, "instance %q not found", analysisContext.InstanceID)
	}
	engine := instance.Metadata.GetEngine()

	relations, err := lineage.GetAnalyzeRelation(catalog.WithAnalysisContext(ctx, analysisContext), engine, sqlText)
	if err != nil {
		if errors.Is(err, lineage.ErrorEngineNotSupported) {
			return fail(connect.CodeFailedPrecondition, "engine %s has no lineage analyzer", engine)
		}
		return fail(connect.CodeInvalidArgument, "failed to analyze the SQL statement: %v", err)
	}

	return s.buildAnalyzeSQLResult(ctx, analysisContext, relations, result), nil
}

// analyzeSQLRelationGUIDs is the pair of GUIDs one relation resolved to. The
// target is empty when the relation only exists inside the statement.
type analyzeSQLRelationGUIDs struct {
	source string
	target string
}

// buildAnalyzeSQLResult completes the identifiers reported by the analyzer and
// resolves the object types of the GUIDs it produced.
func (s *LineageService) buildAnalyzeSQLResult(ctx context.Context, analysisContext catalog.AnalysisContext, relations []model.ColumnRelation, result *v1pb.AnalyzeSQLResult) *v1pb.AnalyzeSQLResult {
	converted, guids := buildAnalyzeSQLRelations(analysisContext, relations)
	result.Relations = converted

	lookups := make(map[string]struct{}, 2*len(guids))
	for _, pair := range guids {
		lookups[pair.source] = struct{}{}
		if pair.target != "" {
			lookups[pair.target] = struct{}{}
		}
	}

	types := s.resolveAnalyzeSQLTypes(ctx, lookups, result)
	for i, relation := range result.Relations {
		relation.SourceType = types[guids[i].source]
		if guids[i].target != "" {
			relation.TargetType = types[guids[i].target]
		}
	}
	return result
}

// buildAnalyzeSQLRelations converts the analyzer's relations into response
// relations, completing each identifier against the scope. It returns the pair
// of GUIDs behind every relation so the caller can fill in the object types
// with a single batched lookup.
func buildAnalyzeSQLRelations(analysisContext catalog.AnalysisContext, relations []model.ColumnRelation) ([]*v1pb.AnalyzeSQLRelation, []analyzeSQLRelationGUIDs) {
	// A statement that writes somewhere real also reports the synthetic result
	// of its own SELECT: the analyzers emit "__result__" edges alongside the
	// real target for CREATE VIEW / CREATE TABLE AS. Those duplicate what the
	// real target already says and are dropped. For a bare SELECT there is no
	// real target and they are the only information there is, so they are kept —
	// that is how a caller sees the query's output columns.
	hasRealTarget := slices.ContainsFunc(relations, func(relation model.ColumnRelation) bool {
		return !relation.IsTemp && relation.Target.Table.Name != tempResultTable
	})

	converted := make([]*v1pb.AnalyzeSQLRelation, 0, len(relations))
	guids := make([]analyzeSQLRelationGUIDs, 0, len(relations))

	for _, relation := range relations {
		// A temporary target only exists inside the statement, so completing it
		// against the scope would invent an object that cannot be looked up. The
		// column name still carries the query's output alias, which is what a
		// caller wants to see.
		isTemp := relation.IsTemp || relation.Target.Table.Name == tempResultTable
		if isTemp && hasRealTarget {
			continue
		}
		sourceGUID := analysisContext.Complete(relation.Source.Table).GUID()

		targetGUID := ""
		if !isTemp {
			targetGUID = analysisContext.Complete(relation.Target.Table).GUID()
		}
		guids = append(guids, analyzeSQLRelationGUIDs{source: sourceGUID, target: targetGUID})

		converted = append(converted, &v1pb.AnalyzeSQLRelation{
			SourceGuid:      sourceGUID,
			SourceColumn:    relation.Source.Name,
			TargetGuid:      targetGUID,
			TargetColumn:    relation.Target.Name,
			RelationType:    convertRelationType(relation.RelationType),
			Transformations: convertTransformations(relation.Transformation),
			IsTemp:          isTemp,
		})
	}
	return converted, guids
}

// resolveAnalyzeSQLTypes fills in the object type of every GUID the analyzer
// produced, recording the ones the registry does not know as warnings: a
// statement may well reference a table that was never synced.
func (s *LineageService) resolveAnalyzeSQLTypes(ctx context.Context, lookups map[string]struct{}, result *v1pb.AnalyzeSQLResult) map[string]v1pb.MetaType {
	types := make(map[string]v1pb.MetaType, len(lookups))
	if len(lookups) == 0 {
		return types
	}

	guids := make([]string, 0, len(lookups))
	for guid := range lookups {
		guids = append(guids, guid)
	}
	metas, err := s.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{GUIDs: &guids})
	if err != nil {
		// The relations themselves are already resolved; only the type
		// annotation is missing, so this is reported instead of failing.
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to resolve object types: %v", err))
		return types
	}
	for _, meta := range metas {
		types[meta.GUID] = v1pb.MetaType(meta.ObjectType)
	}

	// One line for all of them: a statement that touches a dozen unsynced
	// objects would otherwise bury the result it did produce under a dozen
	// near-identical warnings.
	var missing []string
	for _, guid := range guids {
		if _, ok := types[guid]; !ok {
			missing = append(missing, guid)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%d object(s) referenced by this statement are not in the metadata registry: %s",
				len(missing), strings.Join(missing, ", ")))
	}
	return types
}

// analyzeSQLScopeContext turns a scope GUID into an analysis context. The GUID
// is opaque to callers, so the segment count is what says whether a schema was
// supplied: "instance;database" leaves the schema empty, which is exactly right
// for MySQL-family engines, while PostgreSQL-like engines need the third
// segment. Neither the database nor the schema is verified to exist; an
// unqualified name that resolves into a missing object is reported by the
// caller as a warning.
func analyzeSQLScopeContext(scopeGUID string) (catalog.AnalysisContext, error) {
	parts := common.SplitMetaGUID(scopeGUID)
	if len(parts) != 2 && len(parts) != 3 {
		return catalog.AnalysisContext{}, errors.New(`expected "instance;database" or "instance;database;schema"`)
	}
	if parts[0] == "" || parts[1] == "" {
		return catalog.AnalysisContext{}, errors.New("both the instance and the database are required")
	}
	context := catalog.AnalysisContext{InstanceID: parts[0], Database: parts[1]}
	if len(parts) == 3 {
		context.Schema = parts[2]
	}
	return context, nil
}
