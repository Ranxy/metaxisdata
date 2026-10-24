package v1

import (
	"context"
	"fmt"
	"log/slog"
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
// (failed execution -> hypotheses). L1's prompt, validation and quota live in
// component/dataquality.Suggester, shared with the background drafting runner; the
// handler here only parses the request and converts the result. L2 (below) is still
// implemented here.

const (
	// qualityLLMFeatureExplainResult labels the L2 model calls in metrics.
	qualityLLMFeatureExplainResult = "dq_explain_result"
	// qualityExplainRecentExecutions is how many of the rule's recent executions
	// the explanation prompt carries: enough to show a trend, not a history dump.
	qualityExplainRecentExecutions = 10
	// qualityExplainSchemaHistory and qualityExplainLineage bound the context
	// blocks the same prompt carries.
	qualityExplainSchemaHistory = 5
	qualityExplainLineage       = 10
)

// SuggestQualityRule turns a natural-language intent into reviewed rule drafts.
// The pipeline (provider config, prompt, quota, validation, queued suggestions) is
// shared with the background suggestion runner and lives in
// component/dataquality.Suggester; this handler parses and normalizes the request
// and converts what was queued.
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
	if len(requestedColumns) > dataquality.MaxDraftColumns {
		return nil, connectErrorInvalid("at most %d columns may be named", dataquality.MaxDraftColumns)
	}
	// Target and column errors are the caller's, independent of the LLM switch: an
	// invalid target or column must report NotFound/InvalidArgument even when the
	// analysis is disabled. Draft resolves the target again while it drafts; that
	// second read keeps its preconditions local and costs one metadata lookup.
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

	config, settings, err := s.suggester.ResolveConfig(ctx)
	if err != nil {
		return nil, err
	}
	queued, summary, _, err := s.suggester.Draft(ctx, config, dataquality.LimitsFromSettings(settings),
		tableGUID, metaType, requestedColumns, intent, language, req.Msg.GetForceRegenerate(),
		principalIDPointer(currentUserID(ctx)))
	if err != nil {
		return nil, err
	}
	suggestions := make([]*v1pb.QualitySuggestion, 0, len(queued))
	for _, suggestion := range queued {
		suggestions = append(suggestions, convertQualitySuggestion(workspaceID, suggestion))
	}
	return connect.NewResponse(&v1pb.SuggestQualityRuleResponse{
		QualitySuggestions: suggestions,
		Summary:            convertQualitySuggestionQueueSummary(summary),
	}), nil
}

// ExplainQualityExecution streams an explanation of one failed execution.
func (s *DataQualityService) ExplainQualityExecution(ctx context.Context, req *connect.Request[v1pb.ExplainQualityExecutionRequest], stream *connect.ServerStream[v1pb.ExplainQualityExecutionResponse]) error {
	if req == nil || stream == nil {
		return connectErrorInvalid("a request and a stream are required")
	}
	workspaceID, executionID, err := parseQualityExecutionID(req.Msg.GetName())
	if err != nil {
		return err
	}
	if err := s.validateResourceWorkspace(ctx, workspaceID); err != nil {
		return err
	}
	execution, err := s.fetchQualityExecution(ctx, executionID)
	if err != nil {
		return err
	}
	rules, err := s.store.ListQualityRulesByIDs(ctx, []int64{execution.RuleID})
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the quality rule"))
	}
	if len(rules) == 0 {
		return connect.NewError(connect.CodeNotFound, errors.New("the rule this execution belongs to no longer exists"))
	}
	rule := rules[0]

	config, settings, err := s.suggester.ResolveConfig(ctx)
	if err != nil {
		return err
	}
	language := dataquality.NormalizePromptLanguage(req.Msg.GetLanguage())
	cacheKey := dataquality.ExplainCacheKey(execution.ID, execution.RuleRevision, execution.Status, language, config.ProfileName, config.ModelName)

	if !req.Msg.GetForceRegenerate() {
		replayed, err := s.replayCachedExplanation(ctx, cacheKey, stream)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
	}

	// Only a real model call spends the workspace's daily quota and a concurrency
	// slot: a cache hit replayed above returns before this point.
	release, err := s.quota.Begin(ctx, dataquality.LLMAnalysisQuota, dataquality.LimitsFromSettings(settings))
	if err != nil {
		return err
	}
	defer release()

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
		RuleConfig:    s.qualityRuleConfigAtRevision(ctx, rule, execution.RuleRevision),
		Result:        qualityExecutionSnapshot(execution),
		RecentResults: s.qualityRecentExecutionSnapshots(ctx, rule.ID, execution.ID),
		SchemaHistory: s.qualitySchemaHistory(ctx, rule.TargetGUID, rule.TargetType),
		Lineage:       s.qualityLineage(ctx, rule.TargetGUID, rule.TargetType),
		IssueStatus:   s.qualityIssueStatus(ctx, rule.ID),
	}
	systemPrompt, userPrompt := dataquality.BuildExplainPrompt(explainContext)
	thinking := &v1pb.ExplainQualityExecutionResponse{
		Payload: &v1pb.ExplainQualityExecutionResponse_Progress{
			Progress: &v1pb.ExplainSQLProgress{Type: "thinking", Turn: 1},
		},
	}
	if err := stream.Send(thinking); err != nil {
		return err
	}
	text, usage, _, err := s.suggester.JSONCompletion(ctx, config, qualityLLMFeatureExplainResult,
		dataquality.ExplainPromptVersion, systemPrompt, userPrompt, dataquality.OutputTokenBudget(config))
	if err != nil {
		return err
	}
	explanation, err := dataquality.ParseExplanation(text)
	if err != nil {
		s.metrics.CountLLMParseFailure(qualityLLMFeatureExplainResult)
		slog.Warn("the model returned an invalid explanation",
			slog.Int64("execution_id", execution.ID),
			slog.Any("error", err))
		return connect.NewError(connect.CodeInternal,
			errors.Wrap(err, "the model returned an explanation that is not valid JSON"))
	}
	s.cacheExplanation(ctx, cacheKey, execution, explanation, config, language, usage)
	dataquality.LogLLMUsage("explain_execution", rule.TargetGUID, usage)

	// The stream's content is the rendered explanation, not the raw JSON: the
	// panel shows prose, and one frame is enough because the structured answer was
	// completed before anything was shown.
	content := &v1pb.ExplainQualityExecutionResponse{
		Payload: &v1pb.ExplainQualityExecutionResponse_Content{Content: renderExplanationMarkdown(explanation)},
	}
	if err := stream.Send(content); err != nil {
		return err
	}
	provider, model, _ := dataquality.ProviderFacts(config, dataquality.ExplainPromptVersion)
	return stream.Send(&v1pb.ExplainQualityExecutionResponse{
		Payload: &v1pb.ExplainQualityExecutionResponse_Metadata{
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

// cachedContent builds the content frame a cached explanation replays.
func cachedContent(explanation dataquality.ExplanationOutput) *v1pb.ExplainQualityExecutionResponse {
	return &v1pb.ExplainQualityExecutionResponse{
		Payload: &v1pb.ExplainQualityExecutionResponse_Content{Content: renderExplanationMarkdown(explanation)},
	}
}

// replayCachedExplanation streams a cached explanation and reports whether it did.
func (s *DataQualityService) replayCachedExplanation(ctx context.Context, cacheKey string, stream *connect.ServerStream[v1pb.ExplainQualityExecutionResponse]) (bool, error) {
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
	return true, stream.Send(&v1pb.ExplainQualityExecutionResponse{
		Payload: &v1pb.ExplainQualityExecutionResponse_Metadata{
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
func (s *DataQualityService) cacheExplanation(ctx context.Context, cacheKey string, execution *store.QualityExecutionMessage, explanation dataquality.ExplanationOutput, config llm.ResolvedConfig, language string, usage *llm.Usage) {
	hypotheses := make([]*storepb.ExplanationHypothesis, 0, len(explanation.Hypotheses))
	for _, hypothesis := range explanation.Hypotheses {
		hypotheses = append(hypotheses, &storepb.ExplanationHypothesis{
			Cause:    hypothesis.Cause,
			Evidence: append([]string(nil), hypothesis.Evidence...),
			NextStep: hypothesis.NextStep,
		})
	}
	provider, model, promptVersion := dataquality.ProviderFacts(config, dataquality.ExplainPromptVersion)
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
		TargetGUID:    execution.TargetGUID,
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

// qualityExecutionSnapshot maps one stored execution to the prompt's plain
// snapshot.
func qualityExecutionSnapshot(execution *store.QualityExecutionMessage) dataquality.ResultSnapshot {
	return dataquality.ResultSnapshot{
		Status:        execution.Status,
		ObservedValue: execution.ObservedValue,
		ObservedKind:  execution.ObservedKind,
		FailedRows:    execution.FailedRows,
		TotalRows:     execution.TotalRows,
		Sampled:       execution.Sampled,
		SampleRows:    execution.SampleRows,
		Truncated:     execution.Truncated,
		Thresholds:    execution.Thresholds,
		ErrorCode:     execution.ErrorCode,
		ErrorMessage:  execution.ErrorMessage,
		StartedAt:     execution.StartedAt,
	}
}

// qualityRuleConfigAtRevision returns the rule definition the execution was
// judged with. The revision row is the truth; the current head is the fallback
// when the revision row was pruned.
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

// qualityRecentExecutionSnapshots lists the rule's recent completed executions,
// newest first, with the execution being explained omitted so the prompt does not
// see it twice.
func (s *DataQualityService) qualityRecentExecutionSnapshots(ctx context.Context, ruleID, excludeID int64) []dataquality.ResultSnapshot {
	executions, err := s.store.ListRecentQualityExecutions(ctx, ruleID, qualityExplainRecentExecutions+1)
	if err != nil {
		slog.Warn("failed to read recent executions for the explanation", slog.Int64("rule_id", ruleID), slog.Any("error", err))
		return nil
	}
	snapshots := make([]dataquality.ResultSnapshot, 0, len(executions))
	for _, execution := range executions {
		if execution.ID == excludeID {
			continue
		}
		if len(snapshots) == qualityExplainRecentExecutions {
			break
		}
		snapshots = append(snapshots, qualityExecutionSnapshot(execution))
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
