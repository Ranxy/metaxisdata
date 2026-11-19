package v1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// adoptionClockSkew absorbs the difference between the process clock that
	// starts a wake and the process clock that stamps a draft; more than one
	// server may write the queue.
	adoptionClockSkew    = time.Minute
	adoptionDailyWindow  = 24 * time.Hour
	defaultAdoptionDaily = 10
)

// SetQualityAgentAdoption changes the adoption mode without exposing the rest of
// the configuration. An owner may turn it off or return it to PAUSED; ACTIVE stays
// an administrator decision.
func (s *DataQualityService) SetQualityAgentAdoption(ctx context.Context, req *connect.Request[v1pb.SetQualityAgentAdoptionRequest]) (*connect.Response[v1pb.QualityAgent], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetAdoption() == nil {
		return nil, connectErrorInvalid("an adoption setting is required")
	}
	mode, err := qualityAgentAdoptionModeFromV1(req.Msg.GetAdoption().GetMode())
	if err != nil {
		return nil, err
	}
	admin, err := s.callerIsQualityAgentAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if mode == store.QualityAgentAdoptionModeActive && !admin {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only an administrator may enable automatic adoption"))
	}
	agent.AdoptionMode = mode
	if mode == store.QualityAgentAdoptionModeOff || mode == store.QualityAgentAdoptionModeUnspecified {
		agent.AdoptionMode = store.QualityAgentAdoptionModeOff
	}
	updated, err := s.store.UpdateQualityAgentConfig(ctx, agent)
	if err != nil {
		return nil, err
	}
	view, err := s.qualityAgentView(ctx, workspaceID, updated)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(view), nil
}

// adoptionPassResult is what one pass did for one target.
type adoptionPassResult struct {
	Adopted int
	Skipped int
	Failed  int
}

// AdoptQualitySuggestions applies the agent's auto-adoption guardrails to the rule
// drafts a triage wake queued. since is when the wake started: a draft created or
// refreshed by that wake is eligible, while an older pending draft stays in the
// human queue. Every skipped or failed draft stays in the review queue.
func (s *DataQualityService) AdoptQualitySuggestions(ctx context.Context, session *store.QualityAgentSessionMessage, since time.Time) {
	if _, err := s.runAdoptionPass(ctx, session, since); err != nil && !errors.Is(err, store.ErrQualityAgentBusy) {
		slog.Warn("failed to run the quality agent adoption pass", log.WithError(err))
	}
}

// runAdoptionPass serializes every adoption for one agent. The wake hook and an
// explicit owner or administrator retry share the lock, so a retry cannot race a
// wake and adopt the same draft twice.
func (s *DataQualityService) runAdoptionPass(ctx context.Context, session *store.QualityAgentSessionMessage, since time.Time) (adoptionPassResult, error) {
	if session == nil || session.Kind == 2 || session.TargetGUID == "" {
		return adoptionPassResult{}, nil
	}
	var result adoptionPassResult
	err := s.store.WithQualityAgentAdvisoryLock(ctx, fmt.Sprintf("dqadopt:%d", session.AgentID), func() error {
		result = s.adoptQualitySuggestionsLocked(ctx, session, since)
		return nil
	})
	return result, err
}

// runAdoptionPassForAgent is the explicit-retry variant: the caller already loaded
// the agent and its daily budget, so the lock covers one target and the shared
// counter carries the ceiling across targets.
func (s *DataQualityService) runAdoptionPassForAgent(ctx context.Context, session *store.QualityAgentSessionMessage, since time.Time, agent *store.QualityAgentMessage, adopted *int32) (adoptionPassResult, error) {
	if session == nil || session.Kind == 2 || session.TargetGUID == "" {
		return adoptionPassResult{}, nil
	}
	var result adoptionPassResult
	err := s.store.WithQualityAgentAdvisoryLock(ctx, fmt.Sprintf("dqadopt:%d", session.AgentID), func() error {
		result = s.adoptQualitySuggestionsForAgentLocked(ctx, session, since, agent, adopted)
		return nil
	})
	return result, err
}

func (s *DataQualityService) adoptQualitySuggestionsLocked(ctx context.Context, session *store.QualityAgentSessionMessage, since time.Time) adoptionPassResult {
	agent, err := s.store.GetQualityAgentByID(ctx, session.AgentID)
	if err != nil || agent == nil {
		return adoptionPassResult{}
	}
	if !adoptionAgentReady(agent) {
		return adoptionPassResult{}
	}
	adopted, err := s.store.CountQualityAgentAdoptionsSince(ctx, session.AgentID, time.Now().UTC().Add(-adoptionDailyWindow))
	if err != nil {
		slog.Warn("failed to count the agent adoptions", log.WithError(err))
		return adoptionPassResult{}
	}
	return s.adoptQualitySuggestionsForAgentLocked(ctx, session, since, agent, &adopted)
}

// adoptionAgentReady is the configuration gate shared by the wake pass and an
// explicit retry: an active agent, adoption not off, and both capabilities.
func adoptionAgentReady(agent *store.QualityAgentMessage) bool {
	return agent != nil &&
		agent.State == store.QualityAgentStateActive &&
		(agent.AdoptionMode == store.QualityAgentAdoptionModeActive ||
			agent.AdoptionMode == store.QualityAgentAdoptionModePaused) &&
		slices.Contains(agent.Capabilities, agentcapability.Adopt) &&
		slices.Contains(agent.Capabilities, agentcapability.Suggest)
}

// adoptQualitySuggestionsForAgentLocked is the pass body. adopted is the running
// 24h count; the explicit retry shares it across targets so the ceiling is not
// re-read, and re-set, once per target.
func (s *DataQualityService) adoptQualitySuggestionsForAgentLocked(ctx context.Context, session *store.QualityAgentSessionMessage, since time.Time, agent *store.QualityAgentMessage, adopted *int32) adoptionPassResult {
	var result adoptionPassResult
	if !adoptionAgentReady(agent) {
		return result
	}
	limit := agent.AdoptionMaxPerDay
	if limit <= 0 {
		limit = defaultAdoptionDaily
	}
	if *adopted >= limit {
		return result
	}
	ownerID := agent.OwnerPrincipalID
	// Without an owner the agent still queues its drafts and escalates to the
	// administrators; only the rule write needs an owner to attribute the rule to.
	var owner *store.UserMessage
	agentCtx := ctx
	canAdopt := false
	skipReason := "the agent has no owner to attribute the rule to"
	if ownerID != nil {
		var ownerErr error
		owner, ownerErr = s.store.GetUserByID(ctx, int(*ownerID))
		if ownerErr == nil && owner != nil {
			if agentUser, userErr := s.store.GetUserByID(ctx, agent.PrincipalID); userErr == nil && agentUser != nil {
				agentCtx = context.WithValue(ctx, common.UserContextKey, agentUser)
				canAdopt = true
			} else {
				skipReason = "the agent principal is unavailable"
			}
		} else {
			skipReason = "the agent's owner no longer exists"
		}
	}
	// A zero since means "every pending draft" and is what an explicit retry uses.
	// Otherwise a draft counts as this wake's only when it was created or refreshed
	// after the wake started; a clock skew absorbs the process/database clock gap.
	var queuedAfter *time.Time
	if !since.IsZero() {
		cutoff := since.UTC().Add(-adoptionClockSkew)
		queuedAfter = &cutoff
	}
	agentID := session.AgentID
	targetGUID := session.TargetGUID
	find := &store.FindQualitySuggestionMessage{
		AgentID:     &agentID,
		TargetGUID:  &targetGUID,
		Statuses:    []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:       []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		QueuedAfter: queuedAfter,
		Limit:       intPointer(int(limit)),
	}
	suggestions, err := s.store.ListQualitySuggestions(ctx, find)
	if err != nil {
		slog.Warn("failed to list the agent drafts for adoption", log.WithError(err))
		return result
	}
	if len(suggestions) == 0 {
		return result
	}
	critical := s.targetIsCritical(ctx, session.TargetGUID, session.TargetType)
	lastFailure := ""
	for _, suggestion := range suggestions {
		draft := suggestion.Payload.GetRule()
		if draft == nil {
			continue
		}
		ruleType, ok := ruleTypeForDraftConfig(draft.GetConfig())
		if !ok || !adoptionAllowsRuleType(agent, ruleType) {
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeSkipped, "", "rule type is outside the adoption allowlist")
			result.Skipped++
			continue
		}
		threshold := agent.AdoptionConfidenceThreshold
		if threshold <= 0 {
			threshold = 0.7
		}
		if suggestion.Confidence < threshold {
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeSkipped, "", "confidence is below the adoption threshold")
			result.Skipped++
			continue
		}
		if critical && !agent.AdoptionAllowCritical {
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeSkipped, "", "the target is critical and critical adoption is disabled")
			result.Skipped++
			continue
		}
		if !canAdopt {
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeSkipped, "", skipReason)
			result.Skipped++
			continue
		}
		if *adopted >= limit {
			break
		}
		state := v1pb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
		if agent.AdoptionMode == store.QualityAgentAdoptionModePaused {
			state = v1pb.QualityRuleState_QUALITY_RULE_STATE_PAUSED
		}
		// A draft carries no dimension; derive it from the rule type instead of
		// writing UNSPECIFIED, which the store rejects. The create path fills the
		// same value for every other caller.
		dimension, _ := dataquality.DimensionForRuleType(ruleType)
		rule := &v1pb.QualityRule{
			Title:       draft.GetTitle(),
			Description: draft.GetRationale(),
			Dimension:   v1pb.QualityDimension(dimension),
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
			reason := adoptionFailureReason(err)
			if lastFailure == "" {
				lastFailure = reason
			}
			slog.Warn("failed to auto-adopt a quality draft",
				slog.String("suggestion", suggestion.ResourceID),
				slog.String("agent", agent.ResourceID),
				slog.String("target", suggestion.TargetGUID),
				log.WithError(err))
			s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeFailed, "", reason)
			result.Failed++
			continue
		}
		s.recordAdoption(ctx, session, suggestion, store.QualityAgentAdoptionOutcomeAdopted, created.GetName(), "")
		s.recordAdoptionAudit(ctx, session, agent, suggestion, created, state)
		(*adopted)++
		result.Adopted++
	}
	// Whatever is still pending after the pass is the owner's queue: one attention
	// per target (deduped) instead of one per draft.
	remaining, err := s.store.ListQualitySuggestions(ctx, &store.FindQualitySuggestionMessage{
		AgentID:    &agentID,
		TargetGUID: &targetGUID,
		Statuses:   []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:      []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		Limit:      intPointer(1000),
	})
	if err != nil {
		return result
	}
	if len(remaining) == 0 {
		return result
	}
	language := agentlang.ForAgent(agent, owner)
	question := agentlang.ProposalQuestion(language, agentlang.ProposalCounts{
		Remaining: len(remaining),
		Adopted:   result.Adopted,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
		Failure:   lastFailure,
	})
	attention, created, err := s.store.CreateQualityAgentAttention(ctx, &store.QualityAgentAttentionMessage{
		WorkspaceID: session.WorkspaceID,
		AgentID:     session.AgentID,
		Kind:        store.QualityAgentAttentionKindProposalPending,
		Title:       agentlang.AttentionTitle(language, store.QualityAgentAttentionKindProposalPending),
		Question:    question,
		DedupeKey:   "proposal:" + targetGUID,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		return result
	}
	if !created {
		// The owner already has one open item for this target. Keep its wording
		// truthful as later passes change the counts; only a new item notifies.
		if updateErr := s.store.SetOpenQualityAgentAttentionQuestion(ctx, session.AgentID, "proposal:"+targetGUID, question); updateErr != nil {
			slog.Warn("failed to refresh a proposal attention", log.WithError(updateErr))
		}
		return result
	}
	if s.notifier == nil {
		return result
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
	return result
}

// adoptionFailureReason keeps the real cause in the durable ledger. The generic
// "rule creation failed" hid a server-side validation error for weeks; the code
// and the bounded message are what let the owner and the operator tell a bad draft
// from a broken configuration.
func adoptionFailureReason(err error) string {
	if err == nil {
		return ""
	}
	reason := "rule creation failed"
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		reason += " (" + connectErr.Code().String() + "): " + connectErr.Message()
	} else {
		reason += ": " + err.Error()
	}
	return common.TruncateUTF8Bytes(reason, 240)
}

// AdoptQualityAgentPendingSuggestions applies the configured guardrails to every
// pending rule draft the agent already proposed. It exists for recovery: a failed
// adoption pass leaves drafts in the queue, and waiting for a model wake that may
// only refresh them is not a dependable way to drain it. It writes nothing a wake
// would not have written.
func (s *DataQualityService) AdoptQualityAgentPendingSuggestions(ctx context.Context, req *connect.Request[v1pb.AdoptQualityAgentPendingSuggestionsRequest]) (*connect.Response[v1pb.AdoptQualityAgentPendingSuggestionsResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	if agent.State != store.QualityAgentStateActive {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("only an active agent can adopt drafts"))
	}
	if agent.AdoptionMode != store.QualityAgentAdoptionModeActive &&
		agent.AdoptionMode != store.QualityAgentAdoptionModePaused {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("automatic adoption is off for this agent"))
	}
	if !slices.Contains(agent.Capabilities, agentcapability.Adopt) ||
		!slices.Contains(agent.Capabilities, agentcapability.Suggest) {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the agent needs the suggest and adopt capabilities"))
	}
	limit := agent.AdoptionMaxPerDay
	if limit <= 0 {
		limit = defaultAdoptionDaily
	}
	adoptedToday, err := s.store.CountQualityAgentAdoptionsSince(ctx, agent.ID,
		time.Now().UTC().Add(-adoptionDailyWindow))
	if err != nil {
		return nil, err
	}
	if adoptedToday >= limit {
		return nil, connect.NewError(connect.CodeResourceExhausted,
			fmt.Errorf("the agent's daily adoption limit (%d) is spent; it resets within 24 hours", limit))
	}
	agentID := agent.ID
	pending := &store.FindQualitySuggestionMessage{
		AgentID:  &agentID,
		Statuses: []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:    []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		Limit:    intPointer(1000),
	}
	drafts, err := s.store.ListQualitySuggestions(ctx, pending)
	if err != nil {
		return nil, err
	}
	type targetKey struct {
		guid string
		kind storepb.MetaType
	}
	targets := make(map[targetKey][]*store.QualitySuggestionMessage, len(drafts))
	for _, draft := range drafts {
		key := targetKey{guid: draft.TargetGUID, kind: draft.TargetType}
		targets[key] = append(targets[key], draft)
	}
	scope, err := s.store.ListQualityAgentDatabases(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	inScope := make(map[string]struct{}, len(scope))
	for _, database := range scope {
		inScope[database] = struct{}{}
	}
	ordered := make([]targetKey, 0, len(targets))
	for key := range targets {
		ordered = append(ordered, key)
	}
	slices.SortFunc(ordered, func(a, b targetKey) int {
		if c := strings.Compare(a.guid, b.guid); c != 0 {
			return c
		}
		switch {
		case a.kind < b.kind:
			return -1
		case a.kind > b.kind:
			return 1
		default:
			return 0
		}
	})
	response := &v1pb.AdoptQualityAgentPendingSuggestionsResponse{}
	for _, target := range ordered {
		session := &store.QualityAgentSessionMessage{
			WorkspaceID: workspaceID,
			AgentID:     agentID,
			TargetGUID:  target.guid,
			TargetType:  target.kind,
			Kind:        1,
		}
		// The wake path can never reach a target outside the agent's current
		// database scope. The explicit retry must not write a rule there either;
		// the ledger records why the drafts stay pending.
		parts := common.SplitMetaGUID(target.guid)
		if len(parts) < 2 {
			s.recordOutOfScopeDrafts(ctx, session, targets[target])
			response.Skipped += int32(len(targets[target]))
			continue
		}
		if _, ok := inScope[common.FormatDatabase(parts[0], parts[1])]; !ok {
			s.recordOutOfScopeDrafts(ctx, session, targets[target])
			response.Skipped += int32(len(targets[target]))
			continue
		}
		result, passErr := s.runAdoptionPassForAgent(ctx, session, time.Time{}, agent, &adoptedToday)
		if passErr != nil {
			if errors.Is(passErr, store.ErrQualityAgentBusy) {
				return nil, connect.NewError(connect.CodeAborted,
					errors.New("another adoption pass is running; retry shortly"))
			}
			return nil, passErr
		}
		response.Adopted += int32(result.Adopted)
		response.Skipped += int32(result.Skipped)
		response.Failed += int32(result.Failed)
	}
	remaining, err := s.store.ListQualitySuggestions(ctx, pending)
	if err != nil {
		return nil, err
	}
	response.RemainingPending = int32(len(remaining))
	return connect.NewResponse(response), nil
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

// recordOutOfScopeDrafts keeps the skip visible in the durable ledger. The drafts
// stay pending: the database may come back into scope, and a person can still
// decide them by hand.
func (s *DataQualityService) recordOutOfScopeDrafts(ctx context.Context, session *store.QualityAgentSessionMessage, drafts []*store.QualitySuggestionMessage) {
	for _, draft := range drafts {
		s.recordAdoption(ctx, session, draft, store.QualityAgentAdoptionOutcomeSkipped, "", "the target is outside the agent's database scope")
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

func adoptionAllowsRuleType(agent *store.QualityAgentMessage, ruleType storepb.QualityRuleType) bool {
	if len(agent.AdoptionRuleTypes) == 0 {
		return agentcapability.IsLowRiskRuleType(ruleType)
	}
	for _, allowed := range agent.AdoptionRuleTypes {
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
