package v1

import (
	"context"
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

// The L3 flow of design §9.4: profile the columns, let the model name their
// semantic types, then derive labels and rule drafts deterministically. The model
// only proposes the semantic type; LabelPromptVersion, RuleTypesForSemantic and
// DefaultRuleDraft decide what a label means and which rules it implies.

const (
	// qualityLabelBatchColumns is how many columns one label prompt carries. A
	// batch stays small enough for the model to answer every column it was shown.
	qualityLabelBatchColumns = 20
	// qualityLabelMaxBatches bounds the model calls one request may make. With the
	// per-request column cap, three batches cover every accepted request.
	qualityLabelMaxBatches = 3
	// qualityLabelProfileMaxAgeDays is the age after which a cached profile is not
	// trusted for labeling. Label evidence must describe the current data.
	qualityLabelProfileMaxAgeDays = 7
	// qualityLabelProfileMaxAge is the same bound as a duration.
	qualityLabelProfileMaxAge = qualityLabelProfileMaxAgeDays * 24 * time.Hour
)

// SuggestColumnLabels profiles the requested columns, asks the model for their
// semantic types, and queues the resulting label and rule-draft suggestions.
//
// The flow refuses to guess: without a recent profile for every requested column
// the model would label blind, so the request fails with FailedPrecondition and
// points at ProfileColumns. A model batch that cannot be parsed fails the whole
// request as Internal; suggestions queued by earlier batches stay pending, which
// the reviewer's decision path already handles (reject them).
func (s *DataQualityService) SuggestColumnLabels(ctx context.Context, req *connect.Request[v1pb.SuggestColumnLabelsRequest]) (*connect.Response[v1pb.SuggestColumnLabelsResponse], error) {
	if req == nil {
		return nil, connectErrorInvalid("a request is required")
	}
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	guid := strings.TrimSpace(req.Msg.GetTableGuid())
	if guid == "" {
		return nil, connectErrorInvalid("table_guid is required")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required")
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	resolution, err := dataquality.ResolveTarget(ctx, s.store, guid, metaType)
	if err != nil {
		return nil, err
	}
	columns := trimmedQualityColumns(req.Msg.GetColumns())
	if len(columns) == 0 {
		columns = resolution.Target.ColumnNames()
	}
	if len(columns) == 0 {
		return nil, connectErrorInvalid("the target has no columns to label")
	}
	if len(columns) > settings.MaxColumnsPerAnalysis {
		return nil, connectErrorInvalid("at most %d columns may be labeled at once", settings.MaxColumnsPerAnalysis)
	}
	for _, column := range columns {
		if _, ok := resolution.Target.Column(column); !ok {
			return nil, connectErrorInvalid("column %q does not exist on the target", column)
		}
	}
	// The evidence and rationale are written in the request's language; an empty or
	// unsupported tag falls back to English.
	language := dataquality.NormalizePromptLanguage(req.Msg.GetLanguage())

	// Provider resolution comes before the profile read so a disabled LLM never
	// costs a database round trip.
	config, _, err := s.qualityLLMConfig(ctx)
	if err != nil {
		return nil, err
	}

	profiles, err := s.qualityLabelProfiles(ctx, resolution.Target, columns)
	if err != nil {
		return nil, err
	}
	existingLabels, err := s.qualityLabelExistingLabels(ctx, resolution.Target, columns)
	if err != nil {
		return nil, err
	}

	// Lineage is not part of the v1 label prompt (the graph is available but the
	// prompt contract has no block for it yet); labels still cite profile facts.
	var lineage []string

	provider, model, promptVersion := qualityLLMProviderFacts(config, dataquality.LabelPromptVersion)
	callerID := principalIDPointer(currentUserID(ctx))
	now := time.Now().UTC()
	created := make([]*store.QualitySuggestionMessage, 0, len(columns)*2)
	var totalUsage *llm.Usage
	for _, batch := range qualityLabelColumnBatches(columns, qualityLabelBatchColumns, qualityLabelMaxBatches) {
		systemPrompt, userPrompt := dataquality.BuildLabelPrompt(
			dataquality.LabelPromptVersion, language, resolution.Target, profiles, lineage, existingLabels, batch)
		text, usage, err := qualityJSONCompletion(ctx, config, systemPrompt, userPrompt, qualityLLMMaxOutputTokens)
		if err != nil {
			return nil, err
		}
		totalUsage = addLabelUsage(totalUsage, usage)

		outputs, err := dataquality.ParseLabelSuggestions(text, batch)
		if err != nil {
			slog.Warn("the model returned invalid column labels",
				slog.String("table_guid", guid),
				slog.Any("error", err))
			return nil, connect.NewError(connect.CodeInternal,
				errors.Wrap(err, "the model returned column labels that are not valid JSON"))
		}
		for _, output := range outputs {
			labelSuggestion := dataquality.BuildLabelSuggestion(output, profiles[output.Column])
			queued, err := s.queueQualityLabelSuggestion(ctx, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
				&storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Label{Label: labelSuggestion}},
				resolution.Target, provider, model, promptVersion, callerID, now)
			if err != nil {
				return nil, err
			}
			created = append(created, queued)

			// The rule drafts are deterministic: DefaultRuleDraft's parameters are
			// defaults the reviewer confirms, never the observed values. Each draft
			// still goes through the rule pipeline: a label can propose a rule whose
			// default parameters do not fit the column (a regex on a numeric column,
			// say), and an invalid draft is dropped rather than queued for review.
			for _, draft := range dataquality.BuildLabelRuleDrafts([]dataquality.LabelSuggestionOutput{output}, profiles) {
				ruleType, ok := qualityLabelDraftRuleType(draft)
				if !ok {
					slog.Warn("dropping a label rule draft with an unrecognized config",
						slog.String("table_guid", guid),
						slog.String("column", output.Column))
					continue
				}
				if _, err := dataquality.ValidateRule(ctx, s.store, ruleType, resolution.Target.GUID, resolution.Target.MetaType, draft.GetConfig(), nil); err != nil {
					slog.Warn("dropping a label rule draft that failed validation",
						slog.String("table_guid", guid),
						slog.String("column", output.Column),
						slog.String("rule_type", ruleType.String()),
						slog.Any("error", err))
					continue
				}
				queued, err := s.queueQualityLabelSuggestion(ctx, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
					&storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Rule{Rule: draft}},
					resolution.Target, provider, model, promptVersion, callerID, now)
				if err != nil {
					return nil, err
				}
				created = append(created, queued)
			}
		}
	}
	logQualityLLMUsage("suggest_column_labels", guid, totalUsage)

	suggestions := make([]*v1pb.QualitySuggestion, 0, len(created))
	for _, suggestion := range created {
		suggestions = append(suggestions, convertQualitySuggestion(workspaceID, suggestion))
	}
	return connect.NewResponse(&v1pb.SuggestColumnLabelsResponse{QualitySuggestions: suggestions}), nil
}

// queueQualityLabelSuggestion stores one pending suggestion with the provider and
// prompt facts the review queue records.
func (s *DataQualityService) queueQualityLabelSuggestion(ctx context.Context, kind storepb.QualitySuggestionKind, payload *storepb.QualitySuggestionPayload, target dataquality.ResolvedTarget, provider, model, promptVersion string, createdBy *int32, now time.Time) (*store.QualitySuggestionMessage, error) {
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate the suggestion id"))
	}
	suggestion, err := s.store.CreateQualitySuggestion(ctx, &store.QualitySuggestionMessage{
		ResourceID:    resourceID,
		Kind:          kind,
		Source:        storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		TargetGUID:    target.GUID,
		TargetType:    target.MetaType,
		Payload:       payload,
		Status:        storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		Provider:      provider,
		Model:         model,
		PromptVersion: promptVersion,
		CreatedBy:     createdBy,
		CreatedAt:     now,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to queue a column-label suggestion"))
	}
	return suggestion, nil
}

// qualityLabelProfiles reads the cached profiles the prompt cites, and refuses
// the request when any column has no profile younger than qualityLabelProfileMaxAge.
// A stale profile would let the model label today's column with yesterday's shape.
func (s *DataQualityService) qualityLabelProfiles(ctx context.Context, target dataquality.ResolvedTarget, columns []string) (map[string]*storepb.ColumnProfile, error) {
	guid, metaType := target.GUID, target.MetaType
	rows, err := s.store.ListColumnProfiles(ctx, &store.FindColumnProfileMessage{
		TableGUID:   &guid,
		TableType:   &metaType,
		ColumnNames: columns,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the column profiles"))
	}
	stored := make(map[string]*store.ColumnProfileMessage, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		stored[row.ColumnName] = row
	}
	if stale := qualityLabelStaleProfileColumns(stored, columns, time.Now().UTC()); len(stale) > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
			"columns %s have no profile newer than %d days; profile the table first with ProfileColumns, or use \"Analyze columns\" on the asset page",
			strings.Join(stale, ", "), qualityLabelProfileMaxAgeDays))
	}
	profiles := make(map[string]*storepb.ColumnProfile, len(stored))
	for name, row := range stored {
		profiles[name] = row.Profile
	}
	return profiles, nil
}

// qualityLabelExistingLabels reads the labels already asserted on the requested
// columns, which the prompt shows so the model does not relabel them blindly.
func (s *DataQualityService) qualityLabelExistingLabels(ctx context.Context, target dataquality.ResolvedTarget, columns []string) (map[string]storepb.QualitySemanticType, error) {
	guid, metaType := target.GUID, target.MetaType
	labels, err := s.store.ListColumnSemanticLabels(ctx, &store.FindColumnSemanticLabelMessage{
		TableGUID:   &guid,
		TableType:   &metaType,
		ColumnNames: columns,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the existing column labels"))
	}
	return qualityLabelLabelMap(labels), nil
}

// qualityLabelStaleProfileColumns returns the requested columns whose profile is
// missing, empty or older than qualityLabelProfileMaxAge, in request order. It is
// pure so the freshness rule is testable without a store.
func qualityLabelStaleProfileColumns(profiles map[string]*store.ColumnProfileMessage, columns []string, now time.Time) []string {
	var stale []string
	for _, column := range columns {
		profile, ok := profiles[column]
		if !ok || profile == nil || profile.Profile == nil || now.Sub(profile.ProfiledAt) > qualityLabelProfileMaxAge {
			stale = append(stale, column)
		}
	}
	return stale
}

// qualityLabelColumnBatches splits the columns into batches of at most batchSize,
// capped at maxBatches. The caller bounded the total column count, so the cap is
// a guard against a future change rather than a truncation path.
func qualityLabelColumnBatches(columns []string, batchSize, maxBatches int) [][]string {
	if batchSize <= 0 || maxBatches <= 0 || len(columns) == 0 {
		return nil
	}
	batches := make([][]string, 0, maxBatches)
	for start := 0; start < len(columns) && len(batches) < maxBatches; start += batchSize {
		end := start + batchSize
		if end > len(columns) {
			end = len(columns)
		}
		batches = append(batches, columns[start:end])
	}
	return batches
}

// qualityLabelLabelMap reduces stored labels to the map the prompt renders.
func qualityLabelLabelMap(labels []*store.ColumnSemanticLabelMessage) map[string]storepb.QualitySemanticType {
	if len(labels) == 0 {
		return nil
	}
	result := make(map[string]storepb.QualitySemanticType, len(labels))
	for _, label := range labels {
		if label == nil || label.Label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED {
			continue
		}
		result[label.ColumnName] = label.Label
	}
	return result
}

// labelSuggestionToLabelMessage builds the column_semantic_label row that
// accepting a label suggestion writes. The suggestion carries the table identity
// and the payload carries the column, label, confidence and evidence.
func labelSuggestionToLabelMessage(suggestion *store.QualitySuggestionMessage, acceptedBy *int32, at time.Time) (*store.ColumnSemanticLabelMessage, error) {
	if suggestion == nil {
		return nil, common.Errorf(common.Invalid, "a quality suggestion is required")
	}
	if suggestion.Kind != storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL {
		return nil, common.Errorf(common.Invalid, "quality suggestion %q is not a label suggestion", suggestion.ResourceID)
	}
	payload := suggestion.Payload.GetLabel()
	if payload == nil {
		return nil, common.Errorf(common.Invalid, "quality suggestion %q has no label payload", suggestion.ResourceID)
	}
	column := strings.TrimSpace(payload.GetColumn())
	if column == "" {
		return nil, common.Errorf(common.Invalid, "quality suggestion %q names no column", suggestion.ResourceID)
	}
	label := payload.GetLabel()
	if label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality suggestion %q has no semantic type", suggestion.ResourceID)
	}
	acceptedAt := at
	return &store.ColumnSemanticLabelMessage{
		TableGUID:  suggestion.TargetGUID,
		TableType:  suggestion.TargetType,
		ColumnName: column,
		Label:      label,
		// The label was proposed by the model and accepted by a human: LLM is the
		// provenance of the proposal, ACCEPTED is its state.
		Source:     storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_LLM,
		Status:     storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED,
		Confidence: payload.GetConfidence(),
		Evidence:   payload.GetEvidence(),
		AcceptedBy: acceptedBy,
		AcceptedAt: &acceptedAt,
	}, nil
}

// qualityLabelDraftRuleType resolves the rule type a label-generated draft belongs
// to. A draft carries only its config, and the config's oneof arm names exactly
// one rule type, so the arm is the type: mapping it here lets the handler run the
// same validation as a hand-written or model-written rule draft.
func qualityLabelDraftRuleType(draft *storepb.QualityRuleDraftConfig) (storepb.QualityRuleType, bool) {
	if draft == nil || draft.GetConfig() == nil {
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, false
	}
	switch draft.GetConfig().GetConfig().(type) {
	case *storepb.QualityRuleConfig_RowCount:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, true
	case *storepb.QualityRuleConfig_RowCountChange:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, true
	case *storepb.QualityRuleConfig_NullPercent:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, true
	case *storepb.QualityRuleConfig_Unique:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, true
	case *storepb.QualityRuleConfig_UniqueCombination:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION, true
	case *storepb.QualityRuleConfig_AcceptedValues:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, true
	case *storepb.QualityRuleConfig_Range:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE, true
	case *storepb.QualityRuleConfig_Regex:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, true
	case *storepb.QualityRuleConfig_StringLength:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH, true
	case *storepb.QualityRuleConfig_ReferentialIntegrity:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, true
	case *storepb.QualityRuleConfig_CrossColumn:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN, true
	case *storepb.QualityRuleConfig_Freshness:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, true
	case *storepb.QualityRuleConfig_CustomSql:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL, true
	case *storepb.QualityRuleConfig_CelExpression:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, true
	case nil:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, false
	default:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, false
	}
}

// addLabelUsage accumulates one batch's token usage across a multi-batch label
// request.
func addLabelUsage(total *llm.Usage, next *llm.Usage) *llm.Usage {
	if next == nil {
		return total
	}
	if total == nil {
		total = &llm.Usage{}
	}
	total.PromptTokens += next.PromptTokens
	total.CompletionTokens += next.CompletionTokens
	total.TotalTokens += next.TotalTokens
	return total
}
