package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// scopedMethods are the RPCs whose permission is decided per resource rather
// than workspace-wide. The ACL interceptor asks the resource authorizer for the
// caller's scopes and refuses when there are none; the handler then checks the
// specific target. Adding a method here without a handler check would let a
// caller with a grant on one target reach another.
var scopedMethods = map[string]bool{
	"/metaxisdata.v1.DataQualityService/GetQualityRule":                 true,
	"/metaxisdata.v1.DataQualityService/ListQualityRules":               true,
	"/metaxisdata.v1.DataQualityService/CreateQualityRule":              true,
	"/metaxisdata.v1.DataQualityService/UpdateQualityRule":              true,
	"/metaxisdata.v1.DataQualityService/DeleteQualityRule":              true,
	"/metaxisdata.v1.DataQualityService/RunQualityRules":                true,
	"/metaxisdata.v1.DataQualityService/BatchCreateQualityRules":        true,
	"/metaxisdata.v1.DataQualityService/BatchUpdateQualityRules":        true,
	"/metaxisdata.v1.DataQualityService/UpdateQualityContract":          true,
	"/metaxisdata.v1.DataQualityService/PublishQualityContract":         true,
	"/metaxisdata.v1.DataQualityService/UpdateQualityIssue":             true,
	"/metaxisdata.v1.DataQualityService/AcknowledgeQualityTargetSchema": true,
	"/metaxisdata.v1.DataQualityService/ProfileColumns":                 true,
	"/metaxisdata.v1.DataQualityService/UpdateColumnLabel":              true,
	"/metaxisdata.v1.DataQualityService/SuggestColumnLabels":            true,
	"/metaxisdata.v1.DataQualityService/SuggestQualityRule":             true,
	"/metaxisdata.v1.DataQualityService/RecommendQualityChecks":         true,
	"/metaxisdata.v1.DataQualityService/ListQualitySuggestions":         true,
	"/metaxisdata.v1.DataQualityService/GetQualitySuggestion":           true,
	"/metaxisdata.v1.DataQualityService/UpdateQualitySuggestion":        true,
	"/metaxisdata.v1.DataQualityService/BatchUpdateQualitySuggestions":  true,
	"/metaxisdata.v1.DataQualityService/UpdateQualityTargetSchedule":    true,
	"/metaxisdata.v1.DataQualityService/PreviewQualityRule":             true,
}

// qualityRuleTarget resolves the target a create writes to. A named suggestion
// supplies the target; otherwise the request's rule target does. The pre-read
// duplicates one lookup so the scoped check runs before the write.
func (s *DataQualityService) qualityRuleTarget(ctx context.Context, suggestionName string, rule *v1pb.QualityRule) (string, error) {
	if name := strings.TrimSpace(suggestionName); name != "" {
		suggestion, err := s.pendingRuleDraftSuggestion(ctx, name)
		if err != nil {
			return "", err
		}
		return suggestion.TargetGUID, nil
	}
	if rule == nil || rule.GetTarget() == nil {
		return "", nil
	}
	return strings.TrimSpace(rule.GetTarget().GetMetaGuid()), nil
}

// requireQualityTargetPermission checks one resource-scoped permission on the
// target a quality rule lives on. A nil authorizer means the service was built
// without the scoped path (focused unit tests and internal callers); the ACL
// interceptor fails closed for scoped methods in production.
func (s *DataQualityService) requireQualityTargetPermission(ctx context.Context, perm permission.Permission, targetGUID string) error {
	if s.authorizer == nil {
		return connect.NewError(connect.CodeInternal, errors.New("the resource authorizer is not configured"))
	}
	if strings.TrimSpace(targetGUID) == "" {
		return connectErrorInvalid("a target resource is required")
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	allowed, err := s.authorizer.Check(ctx, user, perm, resource.NewRef(resource.TypeTable, targetGUID))
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the target permission"))
	}
	if !allowed {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", perm))
	}
	return nil
}

// requireQualityRuleRead maps a denied read to NotFound so the reader cannot
// learn that a rule exists outside its scope.
func (s *DataQualityService) requireQualityRuleRead(ctx context.Context, targetGUID string) error {
	err := s.requireQualityTargetPermission(ctx, permission.DataQualityRulesGet, targetGUID)
	if err != nil && connect.CodeOf(err) == connect.CodePermissionDenied {
		return connect.NewError(connect.CodeNotFound, errors.New("the quality rule is not found"))
	}
	return err
}

// qualityRuleListScopes returns the caller's scope set for the rule list. A nil
// authorizer keeps the legacy unfiltered behavior for focused unit tests.
func (s *DataQualityService) qualityListScopes(ctx context.Context, perm permission.Permission) (resource.ScopeSet, error) {
	if s.authorizer == nil {
		return resource.ScopeSet{All: true}, nil
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return resource.ScopeSet{}, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	scopes, err := s.authorizer.Scopes(ctx, user, perm)
	if err != nil {
		return resource.ScopeSet{}, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the permission scopes"))
	}
	return scopes.Normalize(), nil
}

// qualityRuleViewerChecks are the row actions a rule list or read reports.
var qualityRuleViewerChecks = []permission.Permission{
	permission.DataQualityRulesGet,
	permission.DataQualityRulesCreate,
	permission.DataQualityRulesUpdate,
	permission.DataQualityRulesDelete,
	permission.DataQualityRulesRun,
}

// qualityRuleViewerPermissions reports, per rule, which row actions the caller
// holds on the rule's target. The scope set is computed once per permission and
// each rule target is tested against it, so a list stays a fixed number of
// authorization reads instead of one per rule.
func (s *DataQualityService) qualityRuleViewerPermissions(ctx context.Context, rules []*store.QualityRuleMessage) (map[int64][]string, error) {
	if s.authorizer == nil || len(rules) == 0 {
		return nil, nil
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, nil
	}
	scopeSets := make([]resource.ScopeSet, len(qualityRuleViewerChecks))
	for i, perm := range qualityRuleViewerChecks {
		scopes, err := s.authorizer.Scopes(ctx, user, perm)
		if err != nil {
			return nil, err
		}
		scopeSets[i] = scopes
	}
	viewer := make(map[int64][]string, len(rules))
	for _, rule := range rules {
		ref := resource.NewRef(resource.TypeTable, rule.TargetGUID)
		for i, perm := range qualityRuleViewerChecks {
			if scopeSets[i].Contains(ref) {
				viewer[rule.ID] = append(viewer[rule.ID], string(perm))
			}
		}
	}
	return viewer, nil
}

// qualityListScopeFilter turns the caller's scope set into a store filter: nil
// means no filter (the permission is workspace-wide), an empty non-nil slice
// means the caller holds it nowhere, and otherwise the metadata scopes.
func (s *DataQualityService) qualityListScopeFilter(ctx context.Context, perm permission.Permission) ([]resource.Scope, error) {
	scopes, err := s.qualityListScopes(ctx, perm)
	if err != nil {
		return nil, err
	}
	if scopes.All {
		return nil, nil
	}
	return scopes.MetadataScopes(), nil
}

// qualitySuggestionViewerChecks are the row actions a suggestion read reports
// for its target: the review decision and turning a draft into a rule.
var qualitySuggestionViewerChecks = []permission.Permission{
	permission.DataQualityRulesCreate,
	permission.DataQualitySuggest,
}

// qualitySuggestionViewerPermissions reports, per suggestion, which target
// actions the caller holds, so the queue gates "create rule" and "decide" per
// row instead of assuming the page-wide navigation grant applies to every row.
func (s *DataQualityService) qualitySuggestionViewerPermissions(ctx context.Context, suggestions []*store.QualitySuggestionMessage) (map[int64][]string, error) {
	if s.authorizer == nil || len(suggestions) == 0 {
		return nil, nil
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, nil
	}
	scopeSets := make([]resource.ScopeSet, len(qualitySuggestionViewerChecks))
	for i, perm := range qualitySuggestionViewerChecks {
		scopes, err := s.authorizer.Scopes(ctx, user, perm)
		if err != nil {
			return nil, err
		}
		scopeSets[i] = scopes
	}
	viewer := make(map[int64][]string, len(suggestions))
	for _, suggestion := range suggestions {
		ref := resource.NewRef(resource.TypeTable, suggestion.TargetGUID)
		for i, perm := range qualitySuggestionViewerChecks {
			if scopeSets[i].Contains(ref) {
				viewer[suggestion.ID] = append(viewer[suggestion.ID], string(perm))
			}
		}
	}
	return viewer, nil
}

// requireQualitySuggestionRead maps a denied suggestion read to NotFound so a
// reader cannot learn that a draft exists outside its scope.
func (s *DataQualityService) requireQualitySuggestionRead(ctx context.Context, suggestion *store.QualitySuggestionMessage) error {
	if suggestion == nil {
		return nil
	}
	err := s.requireQualityTargetPermission(ctx, permission.DataQualitySuggest, suggestion.TargetGUID)
	if err != nil && connect.CodeOf(err) == connect.CodePermissionDenied {
		return connect.NewError(connect.CodeNotFound, errors.New("the quality suggestion is not found"))
	}
	return err
}

// requireQualityRuleDefinitionScope confines a scoped author's rule definition to
// the resources the grant covers. Custom SQL runs on the target's connection,
// query-sourced accepted values run arbitrary read-only SQL, and a cross-table
// reference may name another database or instance; a workspace-wide author is
// unrestricted. The check runs before the definition is saved.
func (s *DataQualityService) requireQualityRuleDefinitionScope(ctx context.Context, perm permission.Permission, ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) error {
	scopes, err := s.qualityListScopes(ctx, perm)
	if err != nil {
		return err
	}
	if scopes.All {
		return nil
	}
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL {
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("a scoped grant cannot author a custom SQL check"))
	}
	if config != nil && config.GetAcceptedValues().GetQuery() != nil {
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("a scoped grant cannot author a check whose accepted values come from a query"))
	}
	if config != nil {
		if reference := strings.TrimSpace(config.GetCrossTable().GetReference().GetMetaGuid()); reference != "" {
			if !scopes.Contains(resource.NewRef(resource.TypeTable, reference)) {
				return connect.NewError(connect.CodePermissionDenied,
					errors.New("a scoped grant cannot reference a table outside its scope"))
			}
		}
	}
	return nil
}
