package dataquality

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
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// MaxDraftColumns bounds the columns one draft request (synchronous or job
	// target) may scope.
	MaxDraftColumns = 50
	// MaxLLMOutputTokens bounds what one analysis may generate. The prompts ask
	// for a small JSON document; this keeps a runaway model cheap.
	MaxLLMOutputTokens = 4096
	// LLMFeatureRuleDraft labels the L1 model calls in metrics.
	LLMFeatureRuleDraft = "dq_rule_draft"
	// draftExistingRules bounds the rule summaries a draft prompt lists.
	draftExistingRules = 20
)

// Suggester is the one implementation of the L1 drafting pipeline: it resolves
// the provider configuration, builds the prompt, runs one JSON completion,
// validates every draft through the rule pipeline and queues the survivors as
// PENDING suggestions. The synchronous SuggestQualityRule RPC and the background
// suggestion runner both call it, so the prompt, validation and quota rules cannot
// drift between them.
type Suggester struct {
	store    *store.Store
	registry *llm.Registry
	quota    *LLMQuota
	metrics  *metrics.Metrics
}

// NewSuggester creates the shared L1 suggester. registry may be nil, in which case
// ResolveConfig reports the LLM unconfigured; quota may be nil, in which case
// drafting is unlimited (test-only).
func NewSuggester(st *store.Store, registry *llm.Registry, quota *LLMQuota, m *metrics.Metrics) *Suggester {
	return &Suggester{store: st, registry: registry, quota: quota, metrics: m}
}

// ResolveConfig checks the workspace switch, the provider registry and the
// workspace allowlist and returns the configuration one analysis runs with.
func (s *Suggester) ResolveConfig(ctx context.Context) (llm.ResolvedConfig, Settings, error) {
	var empty llm.ResolvedConfig
	settings, err := LoadSettings(ctx, s.store)
	if err != nil {
		if common.ErrorCode(err) != common.Internal {
			return empty, settings, err
		}
		return empty, settings, connect.NewError(connect.CodeInternal,
			errors.Wrap(err, "failed to load the data quality settings"))
	}
	if !settings.EnableLLMAnalysis {
		return empty, settings, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("data quality LLM analysis is disabled in the workspace settings"))
	}
	if s.registry == nil {
		return empty, settings, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no LLM registry is configured"))
	}
	configs, err := s.registry.ListEnabled(ctx)
	if err != nil {
		return empty, settings, connect.NewError(connect.CodeFailedPrecondition,
			errors.Wrap(err, "failed to list the enabled LLM provider profiles"))
	}
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return empty, settings, connect.NewError(connect.CodeInternal,
			errors.Wrap(err, "failed to read the workspace LLM allowlist"))
	}
	config, err := llm.ResolveConfig(configs, setting.GetAllowedLlmProviderProfiles(), settings.LLMProviderProfile)
	if err != nil {
		return empty, settings, err
	}
	return *config, settings, nil
}

// Draft turns one natural-language intent into queued rule drafts for one target.
// It resolves the target, spends one L1 analysis from the shared quota, asks the
// model once, and stores every draft the rule pipeline accepts as a PENDING
// NATURAL_LANGUAGE suggestion. The returned rows are the ones it created.
func (s *Suggester) Draft(ctx context.Context, config llm.ResolvedConfig, targetGUID string, metaType storepb.MetaType, columns []string, intent, language string, createdBy *int32) ([]*store.QualitySuggestionMessage, error) {
	resolution, err := ResolveTarget(ctx, s.store, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	for _, column := range columns {
		if _, ok := resolution.Target.Column(column); !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.Errorf("column %q does not exist on the target", column))
		}
	}
	// The target resolves, so the analysis may spend the workspace's daily quota
	// and one concurrency slot. The check runs after resolution so an invalid
	// target consumes neither.
	release, err := s.quota.Begin(ctx, LLMAnalysisQuota, 0)
	if err != nil {
		return nil, err
	}
	defer release()

	existingRules, err := s.ruleSummaries(ctx, targetGUID)
	if err != nil {
		return nil, err
	}
	systemPrompt, userPrompt := BuildRuleDraftPrompt(
		RuleDraftContext{
			Language:       NormalizePromptLanguage(language),
			DDL:            s.targetDDL(ctx, targetGUID, metaType),
			ColumnComments: columnComments(resolution.Metadata, metaType),
		},
		resolution.Target, columns, intent, existingRules)
	text, usage, err := s.JSONCompletion(ctx, config, LLMFeatureRuleDraft, systemPrompt, userPrompt, MaxLLMOutputTokens)
	if err != nil {
		return nil, err
	}
	drafts, err := ParseRuleDrafts(text)
	if err != nil {
		s.metrics.CountLLMParseFailure(LLMFeatureRuleDraft)
		slog.Warn("the model returned an invalid rule draft",
			slog.String("table_guid", targetGUID),
			slog.Any("error", err))
		return nil, connect.NewError(connect.CodeInternal,
			errors.Wrap(err, "the model returned a rule draft that is not valid JSON"))
	}

	provider, model, promptVersion := ProviderFacts(config, RuleDraftPromptVersion)
	now := time.Now().UTC()
	created := make([]*store.QualitySuggestionMessage, 0, len(drafts))
	for _, draft := range drafts {
		if !draftColumnsAllowed(draft.Columns, columns, resolution.Target) {
			slog.Warn("dropping a rule draft that names a column outside the request scope",
				slog.String("table_guid", targetGUID),
				slog.String("title", draft.Title))
			continue
		}
		draftConfig, referencedColumns, err := draftConfig(draft, columns)
		if err != nil {
			slog.Warn("dropping a rule draft whose config could not be built",
				slog.String("table_guid", targetGUID),
				slog.String("title", draft.Title),
				slog.Any("error", err))
			continue
		}
		if !draftColumnsAllowed(referencedColumns, columns, resolution.Target) {
			slog.Warn("dropping a rule draft that refers to a column outside the request scope",
				slog.String("table_guid", targetGUID),
				slog.String("title", draft.Title))
			continue
		}
		if _, err := ValidateRule(ctx, s.store, draft.Type, targetGUID, metaType, draftConfig, nil); err != nil {
			slog.Warn("dropping a rule draft that failed validation",
				slog.String("table_guid", targetGUID),
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
			TargetGUID: targetGUID,
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
			CreatedBy:     createdBy,
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
			slog.String("table_guid", targetGUID),
			slog.Int("drafts", len(drafts)))
	}
	LogLLMUsage("suggest_rule", targetGUID, usage)
	return created, nil
}

// JSONCompletion runs one non-agent completion and returns its text. The prompt
// asks for a single JSON document, so the call fixes temperature at zero and asks
// the provider for JSON when it supports the mode; a provider that ignores the mode
// still answers something the strict parser can read.
//
// feature labels the call in metrics; calls, duration and tokens are recorded by
// the agent loop, not here.
func (s *Suggester) JSONCompletion(ctx context.Context, config llm.ResolvedConfig, feature, systemPrompt, userPrompt string, maxOutputTokens int) (string, *llm.Usage, error) {
	if maxOutputTokens <= 0 {
		maxOutputTokens = MaxLLMOutputTokens
	}
	zero := 0.0
	events := llm.RunAgentLoop(ctx, llm.AgentConfig{
		Provider:       config,
		Feature:        feature,
		Metrics:        s.metrics,
		SystemPrompt:   systemPrompt,
		UserPrompt:     userPrompt,
		MaxTurns:       1,
		MaxTokens:      maxOutputTokens,
		Temperature:    &zero,
		ResponseFormat: llm.JSONObjectResponseFormat(),
		IncludeUsage:   true,
	})
	var (
		builder strings.Builder
		usage   *llm.Usage
	)
	for event := range events {
		switch event.Type {
		case llm.AgentEventContent:
			_, _ = builder.WriteString(event.Content)
		case llm.AgentEventUsage:
			if event.Usage == nil {
				continue
			}
			if usage == nil {
				usage = &llm.Usage{}
			}
			usage.PromptTokens += event.Usage.PromptTokens
			usage.CompletionTokens += event.Usage.CompletionTokens
			usage.TotalTokens += event.Usage.TotalTokens
		case llm.AgentEventError:
			if event.Error != nil {
				return "", usage, connect.NewError(connect.CodeInternal, errors.Wrap(event.Error, "the LLM analysis failed"))
			}
			return "", usage, connect.NewError(connect.CodeInternal, errors.New("the LLM analysis failed"))
		case llm.AgentEventTurnStart, llm.AgentEventToolStart, llm.AgentEventToolEnd, llm.AgentEventAgentEnd:
			// One turn, no tools: nothing to do for these events.
		default:
			// An event kind added later needs no handling here until it matters.
		}
	}
	if builder.Len() == 0 {
		return "", usage, connect.NewError(connect.CodeInternal, errors.New("the LLM returned an empty response"))
	}
	return builder.String(), usage, nil
}

// ProviderFacts returns the provider/model/prompt-version facts a suggestion or
// cache row records.
func ProviderFacts(config llm.ResolvedConfig, promptVersion string) (provider, model, version string) {
	return config.ProfileName, config.ModelName, promptVersion
}

// ruleSummaries renders the target's existing rules as prompt lines.
func (s *Suggester) ruleSummaries(ctx context.Context, targetGUID string) ([]string, error) {
	limit := draftExistingRules
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

// targetDDL reads the engine's stored CREATE statement when the sync recorded one.
// A missing or unreadable definition is not an error: the prompt simply has one
// block less.
func (s *Suggester) targetDDL(ctx context.Context, guid string, metaType storepb.MetaType) string {
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

// columnComments maps the target's column names to their comments. The prompt uses
// them as context; they are data, never instructions.
func columnComments(metadata *storepb.StoredMetadata, metaType storepb.MetaType) map[string]string {
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
func draftColumnsAllowed(columns, requested []string, target ResolvedTarget) bool {
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

// draftConfig turns one validated draft into the typed config the rule pipeline
// accepts. The model writes snake_case keys, which are the proto field names, so
// the config is decoded through protojson after the parser has already rejected
// unknown keys.
func draftConfig(draft RuleDraftOutput, columns []string) (*storepb.QualityRuleConfig, []string, error) {
	spec, ok := DraftRuleSpecFor(draft.Type)
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
func fillDraftColumnFields(spec DraftRuleSpec, fields map[string]json.RawMessage, columns []string) {
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
func normalizeDraftConfigValues(spec DraftRuleSpec, fields map[string]json.RawMessage) {
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

// draftReferencedColumns lists the target columns a config refers to, so the caller
// can enforce the request's column scope on the config too.
func draftReferencedColumns(spec DraftRuleSpec, fields map[string]json.RawMessage) []string {
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

// LogLLMUsage records the token usage one analysis reported. The provider may
// report none; usage is observability, not billing.
func LogLLMUsage(analysis, target string, usage *llm.Usage) {
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
