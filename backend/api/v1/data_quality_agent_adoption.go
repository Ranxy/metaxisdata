package v1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	adoptionWindow       = 15 * time.Minute
	adoptionDailyWindow  = 24 * time.Hour
	defaultAdoptionDaily = 10
)

// AdoptQualitySuggestions applies the agent's auto-adoption guardrails to the rule
// drafts a triage wake queued. It is called by the session engine after a
// successful triage wake; every skipped or failed draft stays in the review queue.
func (s *DataQualityService) AdoptQualitySuggestions(ctx context.Context, session *store.QualityAgentSessionMessage) {
	if session == nil || session.Kind == 2 || session.TargetGUID == "" {
		return
	}
	err := s.store.WithQualityAgentAdvisoryLock(ctx, fmt.Sprintf("dqadopt:%d", session.AgentID), func() error {
		s.adoptQualitySuggestionsLocked(ctx, session)
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrQualityAgentBusy) {
		slog.Warn("failed to run the quality agent adoption pass", log.WithError(err))
	}
}

func (s *DataQualityService) adoptQualitySuggestionsLocked(ctx context.Context, session *store.QualityAgentSessionMessage) {
	config, err := s.store.GetQualityAgentConfig(ctx, session.AgentID)
	if err != nil || config == nil {
		return
	}
	if config.State != store.QualityAgentStateActive ||
		(config.AdoptionMode != store.QualityAgentAdoptionModeActive &&
			config.AdoptionMode != store.QualityAgentAdoptionModePaused) ||
		!slices.Contains(config.Capabilities, "adopt") ||
		!slices.Contains(config.Capabilities, "suggest") {
		return
	}
	limit := config.AdoptionMaxPerDay
	if limit <= 0 {
		limit = defaultAdoptionDaily
	}
	adopted, err := s.store.CountQualityAgentAdoptionsSince(ctx, session.AgentID, time.Now().UTC().Add(-adoptionDailyWindow))
	if err != nil {
		slog.Warn("failed to count the agent adoptions", log.WithError(err))
		return
	}
	if adopted >= limit {
		return
	}
	ownerID := config.OwnerPrincipalID
	agent, err := s.store.GetQualityAgentByID(ctx, session.AgentID)
	if err != nil || agent == nil {
		return
	}
	// Without an owner the agent still queues its drafts and escalates to the
	// administrators; only the rule write needs an owner to attribute the rule to.
	var owner *store.UserMessage
	agentCtx := ctx
	canAdopt := false
	if ownerID != nil {
		var ownerErr error
		owner, ownerErr = s.store.GetUserByID(ctx, int(*ownerID))
		if ownerErr == nil && owner != nil {
			if agentUser, userErr := s.store.GetUserByID(ctx, agent.PrincipalID); userErr == nil && agentUser != nil {
				agentCtx = context.WithValue(ctx, common.UserContextKey, agentUser)
				canAdopt = true
			}
		}
	}
	since := time.Now().UTC().Add(-adoptionWindow)
	agentID := session.AgentID
	targetGUID := session.TargetGUID
	suggestions, err := s.store.ListQualitySuggestions(ctx, &store.FindQualitySuggestionMessage{
		AgentID:      &agentID,
		TargetGUID:   &targetGUID,
		Statuses:     []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:        []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		CreatedAfter: &since,
		Limit:        intPointer(int(limit)),
	})
	if err != nil {
		slog.Warn("failed to list the agent drafts for adoption", log.WithError(err))
		return
	}
	if len(suggestions) == 0 {
		return
	}
	critical := s.targetIsCritical(ctx, session.TargetGUID, session.TargetType)
	for _, suggestion := range suggestions {
		draft := suggestion.Payload.GetRule()
		if draft == nil || !canAdopt {
			continue
		}
		ruleType, ok := ruleTypeForDraftConfig(draft.GetConfig())
		if !ok || !adoptionAllowsRuleType(config, ruleType) {
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeSkipped, "", "rule type is outside the adoption allowlist")
			continue
		}
		threshold := config.AdoptionConfidenceThreshold
		if threshold <= 0 {
			threshold = 0.7
		}
		if suggestion.Confidence < threshold {
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeSkipped, "", "confidence is below the adoption threshold")
			continue
		}
		if critical && !config.AdoptionAllowCritical {
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeSkipped, "", "the target is critical and critical adoption is disabled")
			continue
		}
		if adopted >= limit {
			return
		}
		state := v1pb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
		if config.AdoptionMode == store.QualityAgentAdoptionModePaused {
			state = v1pb.QualityRuleState_QUALITY_RULE_STATE_PAUSED
		}
		rule := &v1pb.QualityRule{
			Title:       draft.GetTitle(),
			Description: draft.GetRationale(),
			Dimension:   v1pb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED,
			RuleType:    v1pb.QualityRuleType(ruleType),
			Config:      convertQualityRuleConfigToV1(draft.GetConfig()),
			Severity:    v1pb.QualitySeverity(draft.GetSeverity()),
			State:       state,
			Owner:       common.FormatUserUID(int(*ownerID)),
			Target: &v1pb.QualityTarget{
				MetaGuid: suggestion.TargetGUID,
				MetaType: v1pb.MetaType(suggestion.TargetType),
			},
		}
		name := common.FormatWorkspace(session.WorkspaceID) + "/qualitySuggestions/" + suggestion.ResourceID
		created, err := s.createQualityRuleFromRequest(agentCtx, session.WorkspaceID, rule, name)
		if err != nil {
			slog.Warn("failed to auto-adopt a quality draft",
				slog.String("suggestion", suggestion.ResourceID), log.WithError(err))
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeFailed, "", "rule creation failed")
			continue
		}
		s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeAdopted, created.GetName(), "")
		s.recordAdoptionAudit(ctx, session, agent, suggestion, created, state)
		adopted++
	}
	// Whatever is still pending after the pass is the owner's queue: one attention
	// per target (deduped) instead of one per draft.
	remaining, err := s.store.ListQualitySuggestions(ctx, &store.FindQualitySuggestionMessage{
		AgentID:    &agentID,
		TargetGUID: &targetGUID,
		Statuses:   []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:      []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		Limit:      intPointer(100),
	})
	if err != nil || len(remaining) == 0 {
		return
	}
	language := agentlang.English
	if owner != nil {
		language = agentlang.Of(owner)
	}
	attention, created, err := s.store.CreateQualityAgentAttention(ctx, &store.QualityAgentAttentionMessage{
		WorkspaceID: session.WorkspaceID,
		AgentID:     session.AgentID,
		Kind:        store.QualityAgentAttentionKindProposalPending,
		Reference:   targetGUID,
		Title:       agentlang.AttentionTitle(language, store.QualityAgentAttentionKindProposalPending),
		Question:    agentlang.ProposalQuestion(language, len(remaining)),
		DedupeKey:   "proposal:" + targetGUID,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil || !created || s.notifier == nil {
		return
	}
	detail := &storepb.QualityAgentAttentionDetail{
		Agent:     common.FormatWorkspace(session.WorkspaceID) + "/qualityAgents/" + agent.ResourceID,
		Attention: common.FormatWorkspace(session.WorkspaceID) + "/qualityAgentAttentions/" + attention.ResourceID,
		Kind:      int32(store.QualityAgentAttentionKindProposalPending),
		Title:     attention.Title,
		Question:  attention.Question,
	}
	dedupeKey := "dqagent:" + attention.ResourceID
	if ownerID != nil {
		if sendErr := s.notifier.SendQualityAgentAttention(ctx, *ownerID, dedupeKey, detail); sendErr != nil {
			slog.Warn("failed to send a proposal attention notification", log.WithError(sendErr))
		}
	} else if sendErr := s.notifier.SendQualityAgentAttentionToAdmins(ctx, dedupeKey, detail); sendErr != nil {
		slog.Warn("failed to send a proposal attention notification to admins", log.WithError(sendErr))
	}
}

// recordAdoptionAudit writes the permanent ledger row for one automatic
// adoption. The rule path is internal, so no RPC interceptor sees this write; the
// actor is the agent principal and the owner is named in the request payload.
func (s *DataQualityService) recordAdoptionAudit(ctx context.Context, session *store.QualityAgentSessionMessage, agent *store.QualityAgentMessage, suggestion *store.QualitySuggestionMessage, rule *v1pb.QualityRule, state v1pb.QualityRuleState) {
	request, err := structpb.NewStruct(map[string]any{
		"suggestion": suggestion.ResourceID,
		"agent":      agent.ResourceID,
		"target":     suggestion.TargetGUID,
		"ruleState":  state.String(),
	})
	if err != nil {
		return
	}
	response, err := structpb.NewStruct(map[string]any{
		"rule": rule.GetName(),
	})
	if err != nil {
		return
	}
	_, auditErr := s.store.CreateAuditLog(ctx, &storepb.AuditLog{
		Parent:   common.FormatWorkspace(session.WorkspaceID),
		Method:   "metaxisdata.v1.DataQualityService/AdoptQualitySuggestion",
		Resource: rule.GetName(),
		User:     common.FormatUserUID(agent.PrincipalID),
		Severity: storepb.AuditLogSeverity_INFO,
		Request:  request,
		Response: response,
		Status:   &storepb.AuditLogStatus{Message: "ok"},
	})
	if auditErr != nil {
		slog.Warn("failed to write the adoption audit row", log.WithError(auditErr))
	}
}

func (s *DataQualityService) recordAdoption(ctx context.Context, session *store.QualityAgentSessionMessage, suggestion *store.QualitySuggestionMessage, outcome store.QualityAgentAdoptionOutcome, ruleResourceID, reason string) {
	recordErr := s.store.RecordQualityAgentAdoption(ctx, &store.QualityAgentAdoptionMessage{
		AgentID:        session.AgentID,
		SuggestionID:   suggestion.ID,
		Outcome:        outcome,
		RuleResourceID: ruleResourceID,
		Reason:         reason,
		CreatedAt:      time.Now().UTC(),
	})
	if recordErr != nil {
		slog.Warn("failed to record a quality agent adoption",
			slog.String("suggestion", suggestion.ResourceID), log.WithError(recordErr))
	}
}

func adoptionAllowsRuleType(config *store.QualityAgentConfigMessage, ruleType storepb.QualityRuleType) bool {
	if len(config.AdoptionRuleTypes) == 0 {
		return agentcapability.IsLowRiskRuleType(ruleType)
	}
	for _, allowed := range config.AdoptionRuleTypes {
		if storepb.QualityRuleType(allowed) == ruleType {
			return true
		}
	}
	return false
}

// targetIsCritical reports whether the target's contract intent marks it critical.
func (s *DataQualityService) targetIsCritical(ctx context.Context, targetGUID string, targetType storepb.MetaType) bool {
	contract, err := s.store.GetQualityContract(ctx, targetGUID, targetType)
	if err != nil || contract == nil || contract.Settings == nil || contract.Settings.Intent == nil {
		return false
	}
	return contract.Settings.Intent.GetCriticality() == storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL
}

// ruleTypeForDraftConfig maps the oneof arm of a model draft back to its rule type.
func ruleTypeForDraftConfig(config *storepb.QualityRuleConfig) (storepb.QualityRuleType, bool) {
	if config == nil {
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, false
	}
	switch config.GetConfig().(type) {
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
	case *storepb.QualityRuleConfig_SchemaDrift:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, true
	case *storepb.QualityRuleConfig_GroupCheck:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, true
	case *storepb.QualityRuleConfig_Aggregate:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, true
	case *storepb.QualityRuleConfig_CrossTable:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, true
	case *storepb.QualityRuleConfig_StandardCode:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE, true
	case *storepb.QualityRuleConfig_SegmentCheck:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, true
	default:
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, false
	}
}

func intPointer(value int) *int { return &value }
