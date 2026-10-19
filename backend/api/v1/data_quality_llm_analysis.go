package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The two LLM analyses of design §9.2 (natural language -> rule draft) and §9.3
// (failed result -> hypotheses). Both are audited and permission-gated in the
// proto; the provider, prompt, validation and cache rules live here so the
// handlers only orchestrate.

const (
	// qualityExplainRecentResults is how many of the rule's recent results the
	// explanation prompt carries: enough to show a trend, not a history dump.
	qualityExplainRecentResults = 10
	// qualityExplainSchemaHistory and qualityExplainLineage bound the context
	// blocks the same prompt carries.
	qualityExplainSchemaHistory = 5
	qualityExplainLineage       = 10
	// qualitySuggestExistingRules bounds the rule summaries a draft prompt lists.
	qualitySuggestExistingRules = 20
	// qualitySuggestMaxColumns bounds the columns a suggestion request may scope.
	qualitySuggestMaxColumns = 50
)

// SuggestQualityRule turns a natural-language intent into reviewed rule drafts.
func (s *DataQualityService) SuggestQualityRule(ctx context.Context, req *connect.Request[v1pb.SuggestQualityRuleRequest]) (*connect.Response[v1pb.SuggestQualityRuleResponse], error) {
	if req == nil {
		return nil, connectErrorInvalid("a request is required")
	}
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	tableGUID := strings.TrimSpace(req.Msg.GetTableGuid())
	if tableGUID == "" {
		return nil, connectErrorInvalid("table_guid is required")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required")
	}
	intent := strings.TrimSpace(req.Msg.GetIntent())
	if intent == "" {
		return nil, connectErrorInvalid("intent is required")
	}
	if len(intent) > dataquality.MaxRuleDraftIntentChars {
		return nil, connectErrorInvalid("intent is longer than %d characters", dataquality.MaxRuleDraftIntentChars)
	}
	requestedColumns := trimmedQualityColumns(req.Msg.GetColumns())
	if len(requestedColumns) > qualitySuggestMaxColumns {
		return nil, connectErrorInvalid("at most %d columns may be named", qualitySuggestMaxColumns)
	}

	resolution, err := dataquality.ResolveTarget(ctx, s.store, tableGUID, metaType)
	if err != nil {
		return nil, err
	}
	for _, column := range requestedColumns {
		if _, ok := resolution.Target.Column(column); !ok {
			return nil, connectErrorInvalid("column %q does not exist on the target", column)
		}
	}
	language := dataquality.NormalizePromptLanguage(req.Msg.GetLanguage())

	config, _, err := s.qualityLLMConfig(ctx)
	if err != nil {
		return nil, err
	}
	existingRules, err := s.qualityRuleSummaries(ctx, tableGUID)
	if err != nil {
		return nil, err
	}
	systemPrompt, userPrompt := dataquality.BuildRuleDraftPrompt(
		dataquality.RuleDraftContext{
			Language:       language,
			DDL:            s.qualityTargetDDL(ctx, tableGUID, metaType),
			ColumnComments: qualityColumnComments(resolution.Metadata, metaType),
		},
		resolution.Target, requestedColumns, intent, existingRules)
	text, usage, err := qualityJSONCompletion(ctx, config, systemPrompt, userPrompt, qualityLLMMaxOutputTokens)
	if err != nil {
		return nil, err
	}
	drafts, err := dataquality.ParseRuleDrafts(text)
	if err != nil {
		slog.Warn("the model returned an invalid rule draft",
			slog.String("table_guid", tableGUID),
			slog.Any("error", err))
		return nil, connect.NewError(connect.CodeInternal,
			errors.Wrap(err, "the model returned a rule draft that is not valid JSON"))
	}

	provider, model, promptVersion := qualityLLMProviderFacts(config, dataquality.RuleDraftPromptVersion)
	callerID := principalIDPointer(currentUserID(ctx))
	now := time.Now().UTC()
	created := make([]*store.QualitySuggestionMessage, 0, len(drafts))
	for _, draft := range drafts {
		if !draftColumnsAllowed(draft.Columns, requestedColumns, resolution.Target) {
			slog.Warn("dropping a rule draft that names a column outside the request scope",
				slog.String("table_guid", tableGUID),
				slog.String("title", draft.Title))
			continue
		}
		draftConfig, referencedColumns, err := qualityDraftConfig(draft, requestedColumns)
		if err != nil {
			slog.Warn("dropping a rule draft whose config could not be built",
				slog.String("table_guid", tableGUID),
				slog.String("title", draft.Title),
				slog.Any("error", err))
			continue
		}
		if !draftColumnsAllowed(referencedColumns, requestedColumns, resolution.Target) {
			slog.Warn("dropping a rule draft that refers to a column outside the request scope",
				slog.String("table_guid", tableGUID),
				slog.String("title", draft.Title))
			continue
		}
		if _, err := dataquality.ValidateRule(ctx, s.store, draft.Type, tableGUID, metaType, draftConfig, nil); err != nil {
			slog.Warn("dropping a rule draft that failed validation",
				slog.String("table_guid", tableGUID),
				slog.String("title", draft.Title),
				slog.Any("error", err))
			continue
		}
		resourceID, err := common.RandomString(12)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate the suggestion id"))
		}
		suggestion, err := s.store.CreateQualitySuggestion(ctx, &store.QualitySuggestionMessage{
			ResourceID: resourceID,
			Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
			Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE,
			TargetGUID: tableGUID,
			TargetType: metaType,
			Payload: &storepb.QualitySuggestionPayload{
				Payload: &storepb.QualitySuggestionPayload_Rule{
					Rule: &storepb.QualityRuleDraftConfig{
						Config:     draftConfig,
						Severity:   draft.Severity,
						Title:      draft.Title,
						Rationale:  draft.Rationale,
						Confidence: draft.Confidence,
					},
				},
			},
			Status:        storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
			Provider:      provider,
			Model:         model,
			PromptVersion: promptVersion,
			CreatedBy:     callerID,
			CreatedAt:     now,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to queue a rule suggestion"))
		}
		created = append(created, suggestion)
	}
	if len(created) == 0 {
		// Every draft was refused by the rule pipeline. An empty answer is honest:
		// the intent did not produce a valid rule, and nothing was queued.
		slog.Warn("no rule draft survived validation",
			slog.String("table_guid", tableGUID),
			slog.Int("drafts", len(drafts)))
	}
	logQualityLLMUsage("suggest_rule", tableGUID, usage)

	suggestions := make([]*v1pb.QualitySuggestion, 0, len(created))
	for _, suggestion := range created {
		suggestions = append(suggestions, convertQualitySuggestion(workspaceID, suggestion))
	}
	return connect.NewResponse(&v1pb.SuggestQualityRuleResponse{QualitySuggestions: suggestions}), nil
}

// qualityRuleSummaries renders the target's existing rules as prompt lines.
func (s *DataQualityService) qualityRuleSummaries(ctx context.Context, targetGUID string) ([]string, error) {
	limit := qualitySuggestExistingRules
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{
		TargetGUID: &targetGUID,
		Limit:      &limit,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the target's existing rules"))
	}
	summaries := make([]string, 0, len(rules))
	for _, rule := range rules {
		summaries = append(summaries,
			fmt.Sprintf("%s (%s, %s)", strings.TrimSpace(rule.Title), rule.RuleType.String(), rule.Severity.String()))
	}
	return summaries, nil
}

// qualityTargetDDL reads the engine's stored CREATE statement when the sync
// recorded one. A missing or unreadable definition is not an error: the prompt
// simply has one block less.
func (s *DataQualityService) qualityTargetDDL(ctx context.Context, guid string, metaType storepb.MetaType) string {
	ddl, ok, err := s.store.GetMetaRegistrySchema(ctx, guid, metaType)
	if err != nil {
		slog.Warn("failed to read the target DDL for the rule-draft prompt",
			slog.String("guid", guid), slog.Any("error", err))
		return ""
	}
	if !ok {
		return ""
	}
	return ddl
}

// qualityColumnComments maps the target's column names to their comments. The
// prompt uses them as context; they are data, never instructions.
func qualityColumnComments(metadata *storepb.StoredMetadata, metaType storepb.MetaType) map[string]string {
	var columns []*storepb.ColumnMetadata
	switch metaType {
	case storepb.MetaType_TABLE:
		columns = metadata.GetTableMetadata().GetColumns()
	case storepb.MetaType_EXTERNAL_TABLE:
		columns = metadata.GetExternalTableMetadata().GetColumns()
	case storepb.MetaType_VIEW:
		columns = metadata.GetViewMetadata().GetColumns()
	case storepb.MetaType_MATERIALIZED_VIEW:
		columns = metadata.GetMaterializedViewMetadata().GetColumns()
	case storepb.MetaType_INSTANCE, storepb.MetaType_DATABASE, storepb.MetaType_SCHEMA, storepb.MetaType_COLUMN,
		storepb.MetaType_INDEX, storepb.MetaType_FOREIGN_KEY, storepb.MetaType_SEQUENCE, storepb.MetaType_MANUAL_SQL,
		storepb.MetaType_APPLICATION_SQL, storepb.MetaType_OPENLINEAGE, storepb.MetaType_EXTERNAL_DATASET,
		storepb.MetaType_PROCEDURE, storepb.MetaType_FUNCTION, storepb.MetaType_UNSPECIFIED:
		return nil
	default:
		return nil
	}
	comments := make(map[string]string, len(columns))
	for _, column := range columns {
		if column == nil {
			continue
		}
		if comment := strings.TrimSpace(column.GetComment()); comment != "" {
			comments[column.GetName()] = comment
		}
	}
	if len(comments) == 0 {
		return nil
	}
	return comments
}

// draftColumnsAllowed reports whether every named column exists on the target and,
// when the request scoped the analysis to some columns, stays inside that scope.
func draftColumnsAllowed(columns, requested []string, target dataquality.ResolvedTarget) bool {
	for _, column := range columns {
		if strings.TrimSpace(column) == "" {
			continue
		}
		if _, ok := target.Column(column); !ok {
			return false
		}
		if len(requested) > 0 && !slices.Contains(requested, column) {
			return false
		}
	}
	return true
}

// qualityDraftConfig turns one validated draft into the typed config the rule
// pipeline accepts. The model writes snake_case keys, which are the proto field
// names, so the config is decoded through protojson after the parser has already
// rejected unknown keys.
func qualityDraftConfig(draft dataquality.RuleDraftOutput, columns []string) (*storepb.QualityRuleConfig, []string, error) {
	spec, ok := dataquality.DraftRuleSpecFor(draft.Type)
	if !ok {
		return nil, nil, errors.Errorf("rule type %s has no natural-language draft", draft.Type)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(draft.Config, &fields); err != nil {
		return nil, nil, errors.Wrap(err, "the draft's config is not a JSON object")
	}
	fillDraftColumnFields(spec, fields, columns)
	normalizeDraftConfigValues(spec, fields)
	referenced := draftReferencedColumns(spec, fields)

	wrapped, err := json.Marshal(map[string]json.RawMessage{spec.Name: mustMarshalJSON(fields)})
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to marshal the draft's config")
	}
	config := &storepb.QualityRuleConfig{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(wrapped, config); err != nil {
		return nil, nil, errors.Wrap(err, "the draft's config does not match the rule type")
	}
	return config, referenced, nil
}

// fillDraftColumnFields copies the request's column selection into a config that
// omitted the column keys the rule type needs. An explicit config value wins: the
// model may name a column inside config and leave the top-level list empty.
func fillDraftColumnFields(spec dataquality.DraftRuleSpec, fields map[string]json.RawMessage, columns []string) {
	if len(columns) == 0 {
		return
	}
	if spec.Type == storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION {
		if _, ok := fields["columns"]; !ok {
			fields["columns"] = mustMarshalJSON(columns)
		}
		return
	}
	// A rule that needs two columns (cross_column) takes them in column order.
	for index, field := range spec.ColumnFields {
		if _, ok := fields[field]; ok {
			continue
		}
		if index < len(columns) {
			fields[field] = mustMarshalJSON(columns[index])
		}
	}
}

// draftComparisonOperators maps the short comparison names the prompt documents
// onto the proto enum names protojson expects.
var draftComparisonOperators = map[string]string{
	"EQ":  "QUALITY_COMPARISON_OPERATOR_EQ",
	"NE":  "QUALITY_COMPARISON_OPERATOR_NE",
	"LT":  "QUALITY_COMPARISON_OPERATOR_LT",
	"LTE": "QUALITY_COMPARISON_OPERATOR_LTE",
	"GT":  "QUALITY_COMPARISON_OPERATOR_GT",
	"GTE": "QUALITY_COMPARISON_OPERATOR_GTE",
}

// normalizeDraftConfigValues rewrites the few values whose model-friendly JSON
// form is not the proto's JSON form: a range bound written as a number becomes the
// string the proto expects, accepted values written as numbers become strings, and
// a short comparison operator becomes its enum name.
func normalizeDraftConfigValues(spec dataquality.DraftRuleSpec, fields map[string]json.RawMessage) {
	switch spec.Type {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:
		for _, key := range []string{"min", "max"} {
			if raw, ok := fields[key]; ok {
				fields[key] = mustMarshalJSON(stringFromJSONValue(raw))
			}
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:
		if raw, ok := fields["values"]; ok {
			var values []json.RawMessage
			if err := json.Unmarshal(raw, &values); err == nil {
				converted := make([]string, 0, len(values))
				for _, value := range values {
					converted = append(converted, stringFromJSONValue(value))
				}
				fields["values"] = mustMarshalJSON(converted)
			}
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:
		if raw, ok := fields["op"]; ok {
			if operator, ok := draftComparisonOperators[strings.ToUpper(stringFromJSONValue(raw))]; ok {
				fields["op"] = mustMarshalJSON(operator)
			}
		}
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		// Nothing to normalize.
	default:
		// Nothing to normalize.
	}
}

// draftReferencedColumns lists the target columns a config refers to, so the
// handler can enforce the request's column scope on the config too.
func draftReferencedColumns(spec dataquality.DraftRuleSpec, fields map[string]json.RawMessage) []string {
	var columns []string
	for _, field := range spec.ColumnFields {
		raw, ok := fields[field]
		if !ok {
			continue
		}
		if field == "columns" {
			var list []string
			if err := json.Unmarshal(raw, &list); err == nil {
				columns = append(columns, list...)
			}
			continue
		}
		var name string
		if err := json.Unmarshal(raw, &name); err == nil && strings.TrimSpace(name) != "" {
			columns = append(columns, name)
		}
	}
	return columns
}

// stringFromJSONValue renders a JSON scalar as the string the proto field expects.
func stringFromJSONValue(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}
	var boolean bool
	if err := json.Unmarshal(raw, &boolean); err == nil {
		return strconv.FormatBool(boolean)
	}
	return strings.TrimSpace(string(raw))
}

// mustMarshalJSON encodes a value that is always encodable (a map of raw messages,
// a string, or a string slice); the error is a programming defect, so the empty
// encoding is returned rather than propagated into a user-visible failure.
func mustMarshalJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

// ExplainQualityResult streams an explanation of one failed result.
func (s *DataQualityService) ExplainQualityResult(ctx context.Context, req *connect.Request[v1pb.ExplainQualityResultRequest], stream *connect.ServerStream[v1pb.ExplainQualityResultResponse]) error {
	if req == nil || stream == nil {
		return connectErrorInvalid("a request and a stream are required")
	}
	workspace, resultID, err := parseExplainResultName(req.Msg.GetName())
	if err != nil {
		return err
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return err
	}
	result, err := s.store.GetQualityRuleResult(ctx, resultID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the quality rule result"))
	}
	if result == nil {
		return connect.NewError(connect.CodeNotFound, errors.Errorf("quality rule result %d not found", resultID))
	}
	rules, err := s.store.ListQualityRulesByIDs(ctx, []int64{result.RuleID})
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the quality rule"))
	}
	if len(rules) == 0 {
		return connect.NewError(connect.CodeNotFound, errors.New("the rule this result belongs to no longer exists"))
	}
	rule := rules[0]

	config, _, err := s.qualityLLMConfig(ctx)
	if err != nil {
		return err
	}
	language := dataquality.NormalizePromptLanguage(req.Msg.GetLanguage())
	cacheKey := dataquality.ExplainCacheKey(result.ID, result.RuleRevision, result.Status, language, config.ProfileName, config.ModelName)

	if !req.Msg.GetForceRegenerate() {
		replayed, err := s.replayCachedExplanation(ctx, cacheKey, stream)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
	}

	resolution, err := dataquality.ResolveTarget(ctx, s.store, rule.TargetGUID, rule.TargetType)
	if err != nil {
		if errors.Is(err, dataquality.ErrOrphaned) {
			return connect.NewError(connect.CodeFailedPrecondition,
				errors.New("the rule's target no longer exists, so the failure cannot be explained"))
		}
		return err
	}
	explainContext := dataquality.ExplainContext{
		Language:      language,
		Target:        resolution.Target,
		RuleTitle:     rule.Title,
		RuleType:      rule.RuleType,
		RuleConfig:    s.qualityRuleConfigAtRevision(ctx, rule, result.RuleRevision),
		Result:        qualityResultSnapshot(result),
		RecentResults: s.qualityRecentResultSnapshots(ctx, rule.ID, result.ID),
		SchemaHistory: s.qualitySchemaHistory(ctx, rule.TargetGUID, rule.TargetType),
		Lineage:       s.qualityLineage(ctx, rule.TargetGUID, rule.TargetType),
		IssueStatus:   s.qualityIssueStatus(ctx, rule.ID),
	}
	systemPrompt, userPrompt := dataquality.BuildExplainPrompt(explainContext)
	thinking := &v1pb.ExplainQualityResultResponse{
		Payload: &v1pb.ExplainQualityResultResponse_Progress{
			Progress: &v1pb.ExplainSQLProgress{Type: "thinking", Turn: 1},
		},
	}
	if err := stream.Send(thinking); err != nil {
		return err
	}
	text, usage, err := qualityJSONCompletion(ctx, config, systemPrompt, userPrompt, qualityLLMMaxOutputTokens)
	if err != nil {
		return err
	}
	explanation, err := dataquality.ParseExplanation(text)
	if err != nil {
		slog.Warn("the model returned an invalid explanation",
			slog.Int64("result_id", result.ID),
			slog.Any("error", err))
		return connect.NewError(connect.CodeInternal,
			errors.Wrap(err, "the model returned an explanation that is not valid JSON"))
	}
	s.cacheExplanation(ctx, cacheKey, result, explanation, config, language, usage)
	logQualityLLMUsage("explain_result", rule.TargetGUID, usage)

	// The stream's content is the rendered explanation, not the raw JSON: the
	// panel shows prose, and one frame is enough because the structured answer was
	// completed before anything was shown.
	content := &v1pb.ExplainQualityResultResponse{
		Payload: &v1pb.ExplainQualityResultResponse_Content{Content: renderExplanationMarkdown(explanation)},
	}
	if err := stream.Send(content); err != nil {
		return err
	}
	provider, model, _ := qualityLLMProviderFacts(config, dataquality.ExplainPromptVersion)
	return stream.Send(&v1pb.ExplainQualityResultResponse{
		Payload: &v1pb.ExplainQualityResultResponse_Metadata{
			Metadata: &v1pb.ExplainSQLMetadata{
				Summary:   explanation.Summary,
				Provider:  provider,
				Model:     model,
				CacheKey:  cacheKey,
				FromCache: false,
			},
		},
	})
}

// parseExplainResultName resolves the result resource name to its workspace and
// the numeric row id the name carries. It is pure, so a malformed name is refused
// before any store read.
func parseExplainResultName(name string) (string, int64, error) {
	workspace, resultIDText, err := parseQualityRuleResultName(name)
	if err != nil {
		return "", 0, connectErrorInvalid("%v", err)
	}
	resultID, err := strconv.ParseInt(strings.TrimSpace(resultIDText), 10, 64)
	if err != nil || resultID <= 0 {
		return "", 0, connectErrorInvalid("the quality rule result name does not carry a result id")
	}
	return workspace, resultID, nil
}

// cachedContent builds the content frame a cached explanation replays.
func cachedContent(explanation dataquality.ExplanationOutput) *v1pb.ExplainQualityResultResponse {
	return &v1pb.ExplainQualityResultResponse{
		Payload: &v1pb.ExplainQualityResultResponse_Content{Content: renderExplanationMarkdown(explanation)},
	}
}

// replayCachedExplanation streams a cached explanation and reports whether it did.
func (s *DataQualityService) replayCachedExplanation(ctx context.Context, cacheKey string, stream *connect.ServerStream[v1pb.ExplainQualityResultResponse]) (bool, error) {
	cached, err := s.store.GetDataQualityLLMCache(ctx, cacheKey)
	if err != nil {
		// The cache is an optimization, not the answer: a read failure regenerates
		// rather than failing the request.
		slog.Warn("failed to read the explanation cache", slog.String("cache_key", cacheKey), slog.Any("error", err))
		return false, nil
	}
	if cached == nil {
		return false, nil
	}
	stored := cached.Result.GetExplanation()
	if stored == nil {
		return false, nil
	}
	explanation := dataquality.ExplanationOutput{
		Summary:    stored.GetSummary(),
		Confidence: stored.GetConfidence(),
	}
	for _, hypothesis := range stored.GetHypotheses() {
		explanation.Hypotheses = append(explanation.Hypotheses, dataquality.ExplanationHypothesis{
			Cause:    hypothesis.GetCause(),
			Evidence: append([]string(nil), hypothesis.GetEvidence()...),
			NextStep: hypothesis.GetNextStep(),
		})
	}
	if err := stream.Send(cachedContent(explanation)); err != nil {
		return false, err
	}
	return true, stream.Send(&v1pb.ExplainQualityResultResponse{
		Payload: &v1pb.ExplainQualityResultResponse_Metadata{
			Metadata: &v1pb.ExplainSQLMetadata{
				Summary:        explanation.Summary,
				Provider:       cached.Provider,
				Model:          cached.Model,
				CacheKey:       cached.CacheKey,
				FromCache:      true,
				CacheCreatedAt: cached.CreatedAt.UTC().Format(time.RFC3339),
			},
		},
	})
}

// cacheExplanation persists one validated explanation for the next identical
// request. A write failure is logged and swallowed: the user already has the
// answer.
func (s *DataQualityService) cacheExplanation(ctx context.Context, cacheKey string, result *store.QualityRuleResultMessage, explanation dataquality.ExplanationOutput, config llm.ResolvedConfig, language string, usage *llm.Usage) {
	hypotheses := make([]*storepb.ExplanationHypothesis, 0, len(explanation.Hypotheses))
	for _, hypothesis := range explanation.Hypotheses {
		hypotheses = append(hypotheses, &storepb.ExplanationHypothesis{
			Cause:    hypothesis.Cause,
			Evidence: append([]string(nil), hypothesis.Evidence...),
			NextStep: hypothesis.NextStep,
		})
	}
	provider, model, promptVersion := qualityLLMProviderFacts(config, dataquality.ExplainPromptVersion)
	cached := &storepb.DataQualityLLMResult{
		Result: &storepb.DataQualityLLMResult_Explanation{
			Explanation: &storepb.ExplanationResult{
				Summary:    explanation.Summary,
				Confidence: explanation.Confidence,
				Hypotheses: hypotheses,
			},
		},
		PromptVersion: promptVersion,
		Provider:      provider,
		Model:         model,
	}
	if usage != nil {
		cached.PromptTokens = usage.PromptTokens
		cached.CompletionTokens = usage.CompletionTokens
		cached.TotalTokens = usage.TotalTokens
	}
	cacheRow := &store.DataQualityLLMCacheRow{
		CacheKey:      cacheKey,
		Kind:          store.DataQualityLLMCacheKindExplanation,
		TargetGUID:    result.TargetGUID,
		Provider:      provider,
		Model:         model,
		Language:      language,
		PromptVersion: promptVersion,
		Result:        cached,
		CreatedAt:     time.Now().UTC(),
	}
	if err := s.store.UpsertDataQualityLLMCache(ctx, cacheRow); err != nil {
		slog.Warn("failed to cache the quality explanation", slog.String("cache_key", cacheKey), slog.Any("error", err))
	}
}

// qualityResultSnapshot maps one stored result to the prompt's plain snapshot.
func qualityResultSnapshot(result *store.QualityRuleResultMessage) dataquality.ResultSnapshot {
	return dataquality.ResultSnapshot{
		Status:        result.Status,
		ObservedValue: result.ObservedValue,
		ObservedKind:  result.ObservedKind,
		FailedRows:    result.FailedRows,
		TotalRows:     result.TotalRows,
		Sampled:       result.Sampled,
		SampleRows:    result.SampleRows,
		Truncated:     result.Truncated,
		Thresholds:    result.Thresholds,
		ErrorCode:     result.ErrorCode,
		ErrorMessage:  result.ErrorMessage,
		StartedAt:     result.StartedAt,
	}
}

// qualityRuleConfigAtRevision returns the rule definition the result was judged
// with. The revision row is the truth; the current head is the fallback when the
// revision row was pruned.
func (s *DataQualityService) qualityRuleConfigAtRevision(ctx context.Context, rule *store.QualityRuleMessage, revision int32) *storepb.QualityRuleConfig {
	revisions, err := s.store.ListQualityRuleRevisions(ctx, rule.ID, nil, nil)
	if err != nil {
		slog.Warn("failed to read the rule revision for the explanation", slog.Int64("rule_id", rule.ID), slog.Any("error", err))
		return rule.Config
	}
	for _, stored := range revisions {
		if stored.Revision == revision {
			return stored.Config
		}
	}
	return rule.Config
}

// qualityRecentResultSnapshots lists the rule's recent results, newest first, with
// the result being explained omitted so the prompt does not see it twice.
func (s *DataQualityService) qualityRecentResultSnapshots(ctx context.Context, ruleID, excludeID int64) []dataquality.ResultSnapshot {
	results, err := s.store.ListRecentQualityRuleResults(ctx, ruleID, qualityExplainRecentResults+1)
	if err != nil {
		slog.Warn("failed to read recent results for the explanation", slog.Int64("rule_id", ruleID), slog.Any("error", err))
		return nil
	}
	snapshots := make([]dataquality.ResultSnapshot, 0, len(results))
	for _, result := range results {
		if result.ID == excludeID {
			continue
		}
		if len(snapshots) == qualityExplainRecentResults {
			break
		}
		snapshots = append(snapshots, qualityResultSnapshot(result))
	}
	return snapshots
}

// qualitySchemaHistory renders the target's recent schema changes.
func (s *DataQualityService) qualitySchemaHistory(ctx context.Context, guid string, metaType storepb.MetaType) []string {
	limit := qualityExplainSchemaHistory
	history, err := s.store.ListMetaRegistryHistory(ctx, &store.FindMetaRegistryHistoryMessage{
		GUID:       &guid,
		ObjectType: &metaType,
		Limit:      &limit,
		OrderDesc:  true,
	})
	if err != nil {
		slog.Warn("failed to read schema history for the explanation", slog.String("guid", guid), slog.Any("error", err))
		return nil
	}
	items := make([]string, 0, len(history))
	for _, entry := range history {
		items = append(items, fmt.Sprintf("%s: %s metadata changed (%d columns)",
			entry.ValidFrom.UTC().Format(time.RFC3339), entry.ObjectType.String(), storedMetadataColumnCount(entry.Metadata)))
	}
	return items
}

// storedMetadataColumnCount counts the columns of a stored metadata row without
// caring which object type it is.
func storedMetadataColumnCount(metadata *storepb.StoredMetadata) int {
	switch {
	case metadata.GetTableMetadata() != nil:
		return len(metadata.GetTableMetadata().GetColumns())
	case metadata.GetExternalTableMetadata() != nil:
		return len(metadata.GetExternalTableMetadata().GetColumns())
	case metadata.GetViewMetadata() != nil:
		return len(metadata.GetViewMetadata().GetColumns())
	case metadata.GetMaterializedViewMetadata() != nil:
		return len(metadata.GetMaterializedViewMetadata().GetColumns())
	default:
		return 0
	}
}

// qualityLineage renders the target's stored relations.
func (s *DataQualityService) qualityLineage(ctx context.Context, guid string, metaType storepb.MetaType) []string {
	limit := qualityExplainLineage
	relations, err := s.store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{
		MetaGUID: &guid,
		MetaType: &metaType,
		Limit:    &limit,
	})
	if err != nil {
		slog.Warn("failed to read lineage for the explanation", slog.String("guid", guid), slog.Any("error", err))
		return nil
	}
	items := make([]string, 0, len(relations))
	for _, relation := range relations {
		items = append(items, fmt.Sprintf("%s -> %s",
			lineageEndpoint(relation.SourceGUID, relation.SourceColumn),
			lineageEndpoint(relation.TargetGUID, relation.TargetColumn)))
	}
	return items
}

// lineageEndpoint renders one end of a relation.
func lineageEndpoint(guid, column string) string {
	segments := common.SplitMetaGUID(guid)
	name := segments[len(segments)-1]
	if strings.TrimSpace(column) == "" {
		return name
	}
	return name + "." + column
}

// qualityIssueStatus describes the rule's open issue, or returns empty when there
// is none.
func (s *DataQualityService) qualityIssueStatus(ctx context.Context, ruleID int64) string {
	issue, err := s.store.GetOpenQualityIssueForRule(ctx, ruleID)
	if err != nil {
		slog.Warn("failed to read the open issue for the explanation", slog.Int64("rule_id", ruleID), slog.Any("error", err))
		return ""
	}
	if issue == nil {
		return ""
	}
	return fmt.Sprintf("%s, severity %s, seen %d times, last seen %s",
		issue.Status.String(), issue.Severity.String(), issue.OccurrenceCount, issue.LastSeenAt.UTC().Format(time.RFC3339))
}

// renderExplanationMarkdown renders the hypotheses as the markdown panel content.
func renderExplanationMarkdown(explanation dataquality.ExplanationOutput) string {
	var builder strings.Builder
	if summary := strings.TrimSpace(explanation.Summary); summary != "" {
		_, _ = builder.WriteString(summary)
		_, _ = builder.WriteString("\n\n")
	}
	for index, hypothesis := range explanation.Hypotheses {
		fmt.Fprintf(&builder, "### %d. %s\n\n", index+1, hypothesis.Cause)
		if len(hypothesis.Evidence) > 0 {
			_, _ = builder.WriteString("**Evidence**\n")
			for _, evidence := range hypothesis.Evidence {
				fmt.Fprintf(&builder, "- %s\n", evidence)
			}
			_, _ = builder.WriteString("\n")
		}
		if nextStep := strings.TrimSpace(hypothesis.NextStep); nextStep != "" {
			fmt.Fprintf(&builder, "**Next step:** %s\n\n", nextStep)
		}
	}
	fmt.Fprintf(&builder, "_confidence %.2f_\n", explanation.Confidence)
	return builder.String()
}

// logQualityLLMUsage records the token usage one analysis reported. The provider
// may report none; usage is observability, not billing.
func logQualityLLMUsage(analysis, target string, usage *llm.Usage) {
	if usage == nil {
		return
	}
	slog.Debug("data quality LLM analysis usage",
		slog.String("analysis", analysis),
		slog.String("target", target),
		slog.Int64("prompt_tokens", usage.PromptTokens),
		slog.Int64("completion_tokens", usage.CompletionTokens),
		slog.Int64("total_tokens", usage.TotalTokens))
}
