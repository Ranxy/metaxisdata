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

	// qualityAgentAdoptionAuditMethod labels the per-draft audit row of an internal
	// adoption write. It is deliberately not an RPC name: the wake pass and the tool
	// have no RPC, and the approval's RPC row is written by the interceptor.
	qualityAgentAdoptionAuditMethod = "qualityAgent/adopt"
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
	// An agent that does not hold the adopt capability cannot run the pass, so storing
	// an armed mode would disagree with what every read reports.
	if mode != store.QualityAgentAdoptionModeOff && mode != store.QualityAgentAdoptionModeUnspecified &&
		!containsCapability(agent.Capabilities, agentcapability.Adopt) {
		return nil, connectErrorInvalid("automatic adoption requires the %q capability", agentcapability.Adopt)
	}
	if mode == store.QualityAgentAdoptionModeOff || mode == store.QualityAgentAdoptionModeUnspecified {
		mode = store.QualityAgentAdoptionModeOff
	}
	updated, err := s.store.UpdateQualityAgentAdoption(ctx, agent.ID, mode)
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
	Adopted        int
	Skipped        int
	Failed         int
	Remaining      int
	RemainingKnown bool
	ItemsTruncated bool
	Items          []dataquality.AgentAdoptionItem
}

// report converts one pass into the tool-visible shape.
func (r adoptionPassResult) report() dataquality.AgentAdoptionReport {
	items := r.Items
	if items == nil {
		items = []dataquality.AgentAdoptionItem{}
	}
	return dataquality.AgentAdoptionReport{
		Adopted:        r.Adopted,
		Skipped:        r.Skipped,
		Failed:         r.Failed,
		Remaining:      r.Remaining,
		RemainingKnown: r.RemainingKnown,
		ItemsTruncated: r.ItemsTruncated,
		Items:          items,
	}
}

// recordItem keeps one draft's outcome in the tool-visible report.
func (r *adoptionPassResult) recordItem(suggestion *store.QualitySuggestionMessage, outcome, reason string) {
	if suggestion == nil {
		return
	}
	const maxReportItems = 20
	if len(r.Items) >= maxReportItems {
		r.ItemsTruncated = true
		return
	}
	r.Items = append(r.Items, dataquality.AgentAdoptionItem{
		ResourceID: suggestion.ResourceID,
		Title:      common.TruncateUTF8Bytes(suggestion.Payload.GetRule().GetTitle(), 120),
		Outcome:    outcome,
		Reason:     common.TruncateUTF8Bytes(reason, 200),
	})
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

// runAdoptionPass serializes every adoption for one agent. The wake hook, the
// owner-requested tool and the approval share the lock, so none can race another
// and adopt the same draft twice.
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
	return s.adoptQualitySuggestionsForAgentLocked(ctx, session, since, agent, &adopted, store.QualityAgentAdoptionOriginAuto, true, false)
}

// adoptionAgentReady is the configuration gate shared by the wake pass and the
// owner-requested tool: an active agent, adoption not off, and both capabilities.
func adoptionAgentReady(agent *store.QualityAgentMessage) bool {
	return dataquality.AgentAdoptionReady(agent)
}

// adoptQualitySuggestionsForAgentLocked is the pass body. adopted is the running
// 24h count; the owner-requested tool shares it across targets so the ceiling is
// not re-read, and re-set, once per target.
func (s *DataQualityService) adoptQualitySuggestionsForAgentLocked(ctx context.Context, session *store.QualityAgentSessionMessage, since time.Time, agent *store.QualityAgentMessage, adopted *int32, origin store.QualityAgentAdoptionOrigin, raiseAttention, requireScope bool) adoptionPassResult {
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
	// The wake pass and the tool resolve the target before they get here (the runner
	// cannot create an out-of-scope session). The tool can name any target, so it
	// passes requireScope and the per-draft check below skips drafts whose database
	// left the agent's scope.
	var scope []string
	if requireScope {
		databases, scopeErr := s.store.ListQualityAgentDatabases(ctx, agent.ID)
		if scopeErr != nil {
			slog.Warn("failed to read the agent database scope for adoption", log.WithError(scopeErr))
			return result
		}
		scope = databases
	}
	ownerID := agent.OwnerPrincipalID
	var requestedBy int32
	if ownerID != nil {
		requestedBy = *ownerID
	}
	// Without an owner the agent still queues its drafts and escalates to the
	// administrators; only the rule write needs an owner to attribute the rule to.
	agentCtx, owner, canAdopt, skipReason := s.adoptionActorContext(ctx, agent)
	// A zero since means "every pending draft" and is what the tool's whole-target
	// pass uses. Otherwise a draft counts as this wake's only when it was created or
	// refreshed after the wake started; a clock skew absorbs the process/database
	// clock gap.
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
		// The agent's own doubt outranks every other guard, including the allowlist: a draft the model
		// flagged is one it can already see the false positives in, and the gate cannot tell on its own.
		if reason, flagged := draftConcernReason(suggestion.Concern); flagged {
			s.recordAdoption(ctx, session, suggestion, origin, store.QualityAgentAdoptionOutcomeSkipped, "", reason)
			result.recordItem(suggestion, "skipped", reason)
			result.Skipped++
			continue
		}
		if requireScope && !scopeCoversTarget(scope, suggestion.TargetGUID) {
			reason := "the target is outside the agent's database scope"
			s.recordAdoption(ctx, session, suggestion, origin, store.QualityAgentAdoptionOutcomeSkipped, "", reason)
			result.recordItem(suggestion, "skipped", reason)
			result.Skipped++
			continue
		}
		ruleType, ok := ruleTypeForDraftConfig(draft.GetConfig())
		if !ok || !adoptionAllowsRuleType(agent, ruleType) {
			s.recordAdoption(ctx, session, suggestion, origin, store.QualityAgentAdoptionOutcomeSkipped, "", "rule type is outside the adoption allowlist")
			result.recordItem(suggestion, "skipped", "rule type is outside the adoption allowlist")
			result.Skipped++
			continue
		}
		threshold := agent.AdoptionConfidenceThreshold
		if threshold <= 0 {
			threshold = 0.7
		}
		if suggestion.Confidence < threshold {
			reason := "confidence is below the adoption threshold"
			s.recordAdoption(ctx, session, suggestion, origin, store.QualityAgentAdoptionOutcomeSkipped, "", reason)
			result.recordItem(suggestion, "skipped", reason)
			result.Skipped++
			continue
		}
		if critical && !agent.AdoptionAllowCritical {
			reason := "the target is critical and critical adoption is disabled"
			s.recordAdoption(ctx, session, suggestion, origin, store.QualityAgentAdoptionOutcomeSkipped, "", reason)
			result.recordItem(suggestion, "skipped", reason)
			result.Skipped++
			continue
		}
		if !canAdopt {
			s.recordAdoption(ctx, session, suggestion, origin, store.QualityAgentAdoptionOutcomeSkipped, "", skipReason)
			result.recordItem(suggestion, "skipped", skipReason)
			result.Skipped++
			continue
		}
		if *adopted >= limit {
			break
		}
		created, state, err := s.createRuleFromAdoptionDraft(agentCtx, session.WorkspaceID, agent, suggestion, *ownerID)
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
			s.recordAdoption(ctx, session, suggestion, origin, store.QualityAgentAdoptionOutcomeFailed, "", reason)
			result.recordItem(suggestion, "failed", reason)
			result.Failed++
			continue
		}
		s.recordAdoption(ctx, session, suggestion, origin, store.QualityAgentAdoptionOutcomeAdopted, created.GetName(), "")
		s.recordAdoptionAudit(ctx, session, agent, suggestion, created, state, false, 0, int32(agent.PrincipalID), requestedBy)
		result.recordItem(suggestion, "adopted", "")
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
	result.Remaining = len(remaining)
	result.RemainingKnown = true
	if len(remaining) == 0 {
		s.resolveProposalAttentionIfDrained(ctx, agentTargetKey{agentID: session.AgentID, targetGUID: targetGUID},
			adoptionResolvedBy(owner))
		return result
	}
	if !raiseAttention {
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
	// Name a few drafts: the owner is being asked to approve this target's queue, and
	// "one draft" without a title is not enough to decide.
	titles := make([]string, 0, 5)
	for _, suggestion := range remaining {
		if len(titles) >= 5 {
			break
		}
		if title := strings.TrimSpace(suggestion.Payload.GetRule().GetTitle()); title != "" {
			titles = append(titles, title)
		}
	}
	if len(titles) > 0 {
		question += "\n\nPending drafts: " + strings.Join(titles, ", ")
	}
	// The pending drafts are an approval ask, not an attention row: the owner reads
	// them in the lead conversation and one answer covers the target. The dedupe key
	// keeps a second adoption pass from asking the same question twice.
	lead, leadErr := s.store.EnsureQualityAgentConversation(ctx, session.WorkspaceID, session.AgentID)
	if leadErr != nil || lead == nil {
		return result
	}
	dedupeKey := "proposal:" + targetGUID
	askResourceID, idErr := common.RandomString(12)
	if idErr != nil {
		return result
	}
	ask, createdAsk, err := s.store.CreateQualityAgentAskDeduped(ctx, &store.QualityAgentAskMessage{
		ResourceID:  askResourceID,
		WorkspaceID: session.WorkspaceID,
		AgentID:     session.AgentID,
		Kind:        store.QualityAgentAskKindApproval,
		Title:       agentlang.AttentionTitle(language, store.QualityAgentAttentionKindProposalPending),
		Question:    question,
		DedupeKey:   dedupeKey,
	}, lead, &store.QualityAgentTranscriptMessage{
		Wake:           lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_RUNNER,
		Content:        question,
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		IdempotencyKey: "proposal-ask:" + askResourceID,
	}, nil, time.Now().UTC())
	if err != nil {
		slog.Warn("failed to record a proposal ask", log.WithError(err))
		return result
	}
	if !createdAsk {
		return result
	}
	if s.notifier == nil {
		return result
	}
	detail := &storepb.QualityAgentAskDetail{
		Agent:    common.FormatWorkspace(session.WorkspaceID) + "/qualityAgents/" + agent.ResourceID,
		Ask:      common.FormatWorkspace(session.WorkspaceID) + "/qualityAgentAsks/" + ask.ResourceID,
		Kind:     int32(store.QualityAgentAskKindApproval),
		Title:    ask.Title,
		Question: ask.Question,
		Targets:  []string{targetGUID},
	}
	notificationKey := "dqagent:ask:" + ask.ResourceID
	if ownerID != nil {
		if sendErr := s.notifier.SendQualityAgentAsk(ctx, *ownerID, notificationKey, detail); sendErr != nil {
			slog.Warn("failed to send a proposal ask notification", log.WithError(sendErr))
		}
	} else if sendErr := s.notifier.SendQualityAgentAskToAdmins(ctx, notificationKey, detail); sendErr != nil {
		slog.Warn("failed to send a proposal ask notification to admins", log.WithError(sendErr))
	}
	return result
}

// adoptionActorContext resolves the actor and owner one adoption needs. The rule
// is attributed to the agent principal and owned by the agent's owner; without
// either, the pass records a skip instead of writing a rule with no owner.
func (s *DataQualityService) adoptionActorContext(ctx context.Context, agent *store.QualityAgentMessage) (context.Context, *store.UserMessage, bool, string) {
	ownerID := agent.OwnerPrincipalID
	if ownerID == nil {
		return ctx, nil, false, "the agent has no owner to attribute the rule to"
	}
	owner, err := s.store.GetUserByID(ctx, int(*ownerID))
	if err != nil || owner == nil {
		return ctx, nil, false, "the agent's owner no longer exists"
	}
	agentUser, err := s.store.GetUserByID(ctx, agent.PrincipalID)
	if err != nil || agentUser == nil {
		return ctx, owner, false, "the agent principal is unavailable"
	}
	return context.WithValue(ctx, common.UserContextKey, agentUser), owner, true, ""
}

// createRuleFromAdoptionDraft writes one draft through the one rule-create path
// for the autonomous or owner-requested adoption. The caller has already decided
// the draft is eligible; this helper only maps the draft onto a rule and lets the
// create path validate it. A person's approval calls createRuleFromDraft directly
// with ENABLED: PAUSED describes an autonomous write, not a person's decision.
func (s *DataQualityService) createRuleFromAdoptionDraft(ctx context.Context, workspaceID string, agent *store.QualityAgentMessage, suggestion *store.QualitySuggestionMessage, ownerID int32) (*v1pb.QualityRule, v1pb.QualityRuleState, error) {
	state := v1pb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
	if agent.AdoptionMode == store.QualityAgentAdoptionModePaused {
		state = v1pb.QualityRuleState_QUALITY_RULE_STATE_PAUSED
	}
	return s.createRuleFromDraft(ctx, workspaceID, suggestion, ownerID, state)
}

// createRuleFromDraft maps one draft onto the create path. state is the check's
// initial state; the create path claims the draft as the caller and validates the
// rule.
func (s *DataQualityService) createRuleFromDraft(ctx context.Context, workspaceID string, suggestion *store.QualitySuggestionMessage, ownerID int32, state v1pb.QualityRuleState) (*v1pb.QualityRule, v1pb.QualityRuleState, error) {
	draft := suggestion.Payload.GetRule()
	if draft == nil {
		return nil, v1pb.QualityRuleState_QUALITY_RULE_STATE_UNSPECIFIED, errors.New("the draft has no rule payload")
	}
	ruleType, ok := ruleTypeForDraftConfig(draft.GetConfig())
	if !ok {
		return nil, v1pb.QualityRuleState_QUALITY_RULE_STATE_UNSPECIFIED, errors.New("the draft has no known rule type")
	}
	// A draft carries no dimension; derive it from the rule type instead of
	// writing UNSPECIFIED, which the store rejects.
	dimension, _ := dataquality.DimensionForRuleType(ruleType)
	rule := &v1pb.QualityRule{
		Title:       draft.GetTitle(),
		Description: draft.GetRationale(),
		Dimension:   v1pb.QualityDimension(dimension),
		RuleType:    v1pb.QualityRuleType(ruleType),
		Config:      convertQualityRuleConfigToV1(draft.GetConfig()),
		Severity:    v1pb.QualitySeverity(draft.GetSeverity()),
		State:       state,
		Owner:       common.FormatUserUID(int(ownerID)),
		Target: &v1pb.QualityTarget{
			MetaGuid: suggestion.TargetGUID,
			MetaType: v1pb.MetaType(suggestion.TargetType),
		},
	}
	name := common.FormatWorkspace(workspaceID) + "/qualitySuggestions/" + suggestion.ResourceID
	created, _, err := s.createQualityRuleFromRequest(ctx, workspaceID, rule, name)
	return created, state, err
}

// ownerAdoptionDraft resolves one draft an owner named and proves it is still
// pending for this agent. The workspace check keeps a conversation from naming a
// draft of another deployment.
func (s *DataQualityService) ownerAdoptionDraft(ctx context.Context, workspaceID string, agentID int64, name string) (*store.QualitySuggestionMessage, error) {
	workspace, resourceID, err := s.qualitySuggestionResourceID(ctx, name)
	if err != nil {
		return nil, err
	}
	if workspace != workspaceID {
		return nil, connectErrorInvalid("the draft belongs to another workspace")
	}
	suggestion, err := s.fetchQualitySuggestion(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if suggestion.Kind != storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT {
		return nil, connectErrorInvalid("quality suggestion %q is not a rule draft", resourceID)
	}
	if suggestion.Status != storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING {
		return nil, connect.NewError(connect.CodeAborted,
			fmt.Errorf("quality suggestion %q is already decided", resourceID))
	}
	if suggestion.AgentID == nil || *suggestion.AgentID != agentID {
		return nil, connectErrorInvalid("quality suggestion %q belongs to another agent", resourceID)
	}
	return suggestion, nil
}

// adoptionTargetInScope reports whether the agent's database scope still covers
// the draft's target. An explicit request must not write a rule outside scope.
func (s *DataQualityService) adoptionTargetInScope(ctx context.Context, agent *store.QualityAgentMessage, targetGUID string) (bool, error) {
	scope, err := s.store.ListQualityAgentDatabases(ctx, agent.ID)
	if err != nil {
		return false, err
	}
	return scopeCoversTarget(scope, targetGUID), nil
}

// scopeCoversTarget reports whether one target GUID belongs to a database ref set.
func scopeCoversTarget(scope []string, targetGUID string) bool {
	parts := common.SplitMetaGUID(targetGUID)
	if len(parts) < 2 {
		return false
	}
	return slices.Contains(scope, common.FormatDatabase(parts[0], parts[1]))
}

// adoptionResolvedBy names the agent's owner as the resolver of a drained queue.
func adoptionResolvedBy(owner *store.UserMessage) *int32 {
	if owner == nil {
		return nil
	}
	return principalIDPointer(owner.ID)
}

// agentTargetKey is one agent's target: the unit a proposal attention belongs to.
type agentTargetKey struct {
	agentID    int64
	targetGUID string
}

// proposalAttentionKey names the attention a decided suggestion can drain. Only a
// rule draft an agent proposed has one; a label suggestion does not.
func proposalAttentionKey(suggestion *store.QualitySuggestionMessage) (agentTargetKey, bool) {
	if suggestion == nil || suggestion.AgentID == nil || *suggestion.AgentID <= 0 ||
		suggestion.Kind != storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT ||
		suggestion.TargetGUID == "" {
		return agentTargetKey{}, false
	}
	return agentTargetKey{agentID: *suggestion.AgentID, targetGUID: suggestion.TargetGUID}, true
}

// resolveProposalAttentionIfDrained closes one target's proposal attention once no
// pending draft is left. A pass resolves it after adoption; a person deciding the
// last draft is the other way the queue empties.
func (s *DataQualityService) resolveProposalAttentionIfDrained(ctx context.Context, key agentTargetKey, resolvedBy *int32) {
	now := time.Now().UTC()
	if _, err := s.store.ResolveQualityAgentAskByDedupe(ctx, key.agentID, "proposal:"+key.targetGUID,
		store.QualityAgentAskStatusResolved, now); err != nil {
		slog.Warn("failed to resolve a drained proposal ask", log.WithError(err))
	}
	// The legacy row is still resolved for any attention written before the cutover;
	// no new one is created.
	if _, err := s.store.ResolveDrainedProposalAttention(ctx, key.agentID, key.targetGUID, resolvedBy, now); err != nil {
		slog.Warn("failed to resolve a drained proposal attention", log.WithError(err))
	}
}

// resolveProposalAttentionForSuggestion resolves the one target a just-decided
// draft drained. The single-item decision and create paths use it; their batch
// variants collect keys and resolve once per target instead.
func (s *DataQualityService) resolveProposalAttentionForSuggestion(ctx context.Context, suggestion *store.QualitySuggestionMessage) {
	if key, ok := proposalAttentionKey(suggestion); ok {
		s.resolveProposalAttentionIfDrained(ctx, key, principalIDPointer(currentUserID(ctx)))
	}
}

// resolveDrainedProposalAttentions resolves a batch's attentions after its writes,
// one count per touched target instead of one per decided draft.
func (s *DataQualityService) resolveDrainedProposalAttentions(ctx context.Context, keys map[agentTargetKey]struct{}, resolvedBy *int32) {
	for key := range keys {
		s.resolveProposalAttentionIfDrained(ctx, key, resolvedBy)
	}
}

// countPendingDrafts counts the pending rule drafts an adoption report should
// mention as remaining. An empty target counts the agent's whole queue.
func (s *DataQualityService) countPendingDrafts(ctx context.Context, agentID int64, targetGUID string) (int, error) {
	find := &store.FindQualitySuggestionMessage{
		AgentID:  &agentID,
		Statuses: []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:    []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		Limit:    intPointer(1000),
	}
	if targetGUID != "" {
		find.TargetGUID = &targetGUID
	}
	rows, err := s.store.ListQualitySuggestions(ctx, find)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// adoptQualitySuggestionsExplicitly adopts the drafts an owner named. Naming a
// draft is the owner's decision, so it bypasses the automatic guardrails
// (allow-list, confidence, critical switch); the active agent, owner, database
// scope, daily ceiling and the rule validation still apply.
func (s *DataQualityService) adoptQualitySuggestionsExplicitly(ctx context.Context, req dataquality.AgentAdoptionRequest, agent *store.QualityAgentMessage, adopted *int32) adoptionPassResult {
	var result adoptionPassResult
	limit := agent.AdoptionMaxPerDay
	if limit <= 0 {
		limit = defaultAdoptionDaily
	}
	agentCtx, owner, canAdopt, skipReason := s.adoptionActorContext(ctx, agent)
	var ownerID int32
	if owner != nil {
		ownerID = int32(owner.ID)
	}
	targetGUID := strings.TrimSpace(req.TargetGUID)
	if req.Session.Kind == 1 {
		targetGUID = req.Session.TargetGUID
	}
	for _, name := range req.DraftIDs {
		suggestion, err := s.ownerAdoptionDraft(ctx, req.Session.WorkspaceID, agent.ID, name)
		if err != nil {
			result.recordItem(&store.QualitySuggestionMessage{ResourceID: name}, "failed", adoptionFailureReason(err))
			result.Failed++
			continue
		}
		if targetGUID != "" && suggestion.TargetGUID != targetGUID {
			result.recordItem(suggestion, "skipped", "the draft belongs to another target")
			result.Skipped++
			continue
		}
		if !canAdopt {
			s.recordAdoption(ctx, req.Session, suggestion, store.QualityAgentAdoptionOriginTool, store.QualityAgentAdoptionOutcomeSkipped, "", skipReason)
			result.recordItem(suggestion, "skipped", skipReason)
			result.Skipped++
			continue
		}
		if *adopted >= limit {
			reason := "the daily adoption limit is reached"
			s.recordAdoption(ctx, req.Session, suggestion, store.QualityAgentAdoptionOriginTool, store.QualityAgentAdoptionOutcomeSkipped, "", reason)
			result.recordItem(suggestion, "skipped", reason)
			result.Skipped++
			continue
		}
		inScope, scopeErr := s.adoptionTargetInScope(ctx, agent, suggestion.TargetGUID)
		if scopeErr != nil {
			result.recordItem(suggestion, "failed", adoptionFailureReason(scopeErr))
			result.Failed++
			continue
		}
		if !inScope {
			reason := "the target is outside the agent's database scope"
			s.recordAdoption(ctx, req.Session, suggestion, store.QualityAgentAdoptionOriginTool, store.QualityAgentAdoptionOutcomeSkipped, "", reason)
			result.recordItem(suggestion, "skipped", reason)
			result.Skipped++
			continue
		}
		created, state, createErr := s.createRuleFromAdoptionDraft(agentCtx, req.Session.WorkspaceID, agent, suggestion, ownerID)
		if createErr != nil {
			reason := adoptionFailureReason(createErr)
			slog.Warn("failed to adopt an owner-requested quality draft",
				slog.String("suggestion", suggestion.ResourceID),
				slog.String("agent", agent.ResourceID),
				slog.String("target", suggestion.TargetGUID),
				log.WithError(createErr))
			s.recordAdoption(ctx, req.Session, suggestion, store.QualityAgentAdoptionOriginTool, store.QualityAgentAdoptionOutcomeFailed, "", reason)
			result.recordItem(suggestion, "failed", reason)
			result.Failed++
			continue
		}
		s.recordAdoption(ctx, req.Session, suggestion, store.QualityAgentAdoptionOriginTool, store.QualityAgentAdoptionOutcomeAdopted, created.GetName(), "")
		s.recordAdoptionAudit(ctx, req.Session, agent, suggestion, created, state, true, req.Wake, int32(agent.PrincipalID), ownerID)
		result.recordItem(suggestion, "adopted", "")
		(*adopted)++
		result.Adopted++
	}
	remaining, err := s.countPendingDrafts(ctx, agent.ID, targetGUID)
	if err != nil {
		slog.Warn("failed to count the remaining adoption drafts", log.WithError(err))
	} else {
		result.Remaining = remaining
		result.RemainingKnown = true
		if remaining == 0 {
			s.resolveProposalAttentionIfDrained(ctx, agentTargetKey{agentID: agent.ID, targetGUID: targetGUID},
				adoptionResolvedBy(owner))
		}
	}
	return result
}

// AdoptQualitySuggestionsForOwner executes an adoption the owner explicitly asked
// for in the current wake. It is the in-process implementation of the adopt tool:
// it reuses the wake pass's lock, ledger and audit, and it never creates an
// attention item (the owner is already in the conversation).
func (s *DataQualityService) AdoptQualitySuggestionsForOwner(ctx context.Context, req dataquality.AgentAdoptionRequest) (dataquality.AgentAdoptionReport, error) {
	if req.Session == nil {
		return dataquality.AgentAdoptionReport{}, connectErrorInvalid("an adoption needs a session")
	}
	agent, err := s.store.GetQualityAgentByID(ctx, req.Session.AgentID)
	if err != nil {
		return dataquality.AgentAdoptionReport{}, fmt.Errorf("failed to read the adoption agent: %w", err)
	}
	if agent == nil {
		return dataquality.AgentAdoptionReport{}, connect.NewError(connect.CodeNotFound, errors.New("the adoption agent no longer exists"))
	}
	if !dataquality.AgentAdoptionReady(agent) {
		return dataquality.AgentAdoptionReport{}, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("adoption is not enabled for this agent"))
	}
	limit := agent.AdoptionMaxPerDay
	if limit <= 0 {
		limit = defaultAdoptionDaily
	}
	targetGUID := strings.TrimSpace(req.TargetGUID)
	if req.Session.Kind == 1 {
		targetGUID = req.Session.TargetGUID
	}
	if len(req.DraftIDs) == 0 && targetGUID == "" {
		return dataquality.AgentAdoptionReport{}, connectErrorInvalid("name a target or the drafts to adopt")
	}
	var result adoptionPassResult
	lockErr := s.store.WithQualityAgentAdvisoryLock(ctx, fmt.Sprintf("dqadopt:%d", agent.ID), func() error {
		// Read the daily counter inside the lock: two owner passes that read it outside
		// could both observe an under-limit value and together exceed the ceiling.
		adopted, countErr := s.store.CountQualityAgentAdoptionsSince(ctx, agent.ID, time.Now().UTC().Add(-adoptionDailyWindow))
		if countErr != nil {
			return countErr
		}
		if adopted >= limit && len(req.DraftIDs) == 0 {
			return connect.NewError(connect.CodeResourceExhausted,
				fmt.Errorf("the agent's daily adoption limit (%d) is spent; it resets within 24 hours", limit))
		}
		if len(req.DraftIDs) > 0 {
			result = s.adoptQualitySuggestionsExplicitly(ctx, req, agent, &adopted)
			return nil
		}
		session, sessionErr := s.ownerAdoptionSession(ctx, req.Session, agent, targetGUID)
		if sessionErr != nil {
			return sessionErr
		}
		result = s.adoptQualitySuggestionsForAgentLocked(ctx, session, time.Time{}, agent, &adopted, store.QualityAgentAdoptionOriginTool, false, true)
		return nil
	})
	if lockErr != nil {
		if errors.Is(lockErr, store.ErrQualityAgentBusy) {
			return dataquality.AgentAdoptionReport{}, connect.NewError(connect.CodeAborted,
				errors.New("another adoption pass is running; retry shortly"))
		}
		return dataquality.AgentAdoptionReport{}, lockErr
	}
	return result.report(), nil
}

// AdoptQualityAgentPendingSuggestions creates the agent's pending drafts as the
// caller's decision. The owner or an administrator may call it; the approval runs
// through the internal create path, which does not check the caller's rule-write
// permission because its only input is the agent's already-queued drafts. The check
// starts enabled, is attributed to the caller and is owned by the agent's owner. The
// autonomous guardrails do not apply: the allowlist, confidence threshold, critical
// switch, scope and daily ceiling are not consulted. Rule validation still runs, and
// a target GUID narrows the approval to one target.
func (s *DataQualityService) AdoptQualityAgentPendingSuggestions(ctx context.Context, req *connect.Request[v1pb.AdoptQualityAgentPendingSuggestionsRequest]) (*connect.Response[v1pb.AdoptQualityAgentPendingSuggestionsResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	agentID := agent.ID
	find := &store.FindQualitySuggestionMessage{
		AgentID:  &agentID,
		Statuses: []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:    []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		Limit:    intPointer(qualityListMaximum),
	}
	if targetGUID != "" {
		find.TargetGUID = &targetGUID
	}
	drafts, err := s.store.ListQualitySuggestions(ctx, find)
	if err != nil {
		return nil, err
	}
	// Oldest first per target, so a queue larger than one request makes progress
	// instead of approving the newest rows on every retry.
	slices.SortFunc(drafts, func(a, b *store.QualitySuggestionMessage) int {
		if c := strings.Compare(a.TargetGUID, b.TargetGUID); c != 0 {
			return c
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	callerID := int32(currentUserID(ctx))
	// The check stays owned by the agent's owner, who is accountable for the target;
	// an ownerless or deleted owner falls back to the person approving.
	ownerID := callerID
	if agent.OwnerPrincipalID != nil {
		ownerUser, ownerErr := s.store.GetUserByID(ctx, int(*agent.OwnerPrincipalID))
		if ownerErr != nil {
			return nil, ownerErr
		}
		if ownerUser != nil && !ownerUser.MemberDeleted {
			ownerID = *agent.OwnerPrincipalID
		}
	}
	response := &v1pb.AdoptQualityAgentPendingSuggestionsResponse{}
	drained := make(map[agentTargetKey]struct{})
	lockErr := s.store.WithQualityAgentAdvisoryLock(ctx, fmt.Sprintf("dqadopt:%d", agent.ID), func() error {
		for index, draft := range drafts {
			if index >= qualityBatchRequestLimit {
				break
			}
			inScope, scopeErr := s.adoptionTargetInScope(ctx, agent, draft.TargetGUID)
			if scopeErr != nil {
				return scopeErr
			}
			if !inScope {
				response.Skipped++
				continue
			}
			session := &store.QualityAgentSessionMessage{
				WorkspaceID: workspaceID,
				AgentID:     agent.ID,
				TargetGUID:  draft.TargetGUID,
				TargetType:  draft.TargetType,
				Kind:        1,
			}
			created, state, createErr := s.createRuleFromDraft(ctx, workspaceID, draft, ownerID, v1pb.QualityRuleState_QUALITY_RULE_STATE_ENABLED)
			if createErr != nil {
				// Another reviewer decided the draft first; that is not a failure.
				if connect.CodeOf(createErr) == connect.CodeAborted {
					response.Skipped++
					continue
				}
				reason := adoptionFailureReason(createErr)
				// The durable failure trail is shared with the autonomous pass: the
				// home's failed count and last failure must cover an approval that could
				// not be written too, and the row never spends the daily ceiling.
				s.recordAdoption(ctx, session, draft, store.QualityAgentAdoptionOriginApproval,
					store.QualityAgentAdoptionOutcomeFailed, "", reason)
				slog.Warn("failed to approve a quality draft",
					slog.String("suggestion", draft.ResourceID),
					slog.String("agent", agent.ResourceID),
					slog.String("target", draft.TargetGUID),
					log.WithError(createErr))
				response.Failed++
				continue
			}
			s.recordAdoption(ctx, session, draft, store.QualityAgentAdoptionOriginApproval,
				store.QualityAgentAdoptionOutcomeAdopted, created.GetName(), "")
			s.recordAdoptionAudit(ctx, session, agent, draft, created, state, true, 0, callerID, callerID)
			response.Adopted++
			drained[agentTargetKey{agentID: agent.ID, targetGUID: draft.TargetGUID}] = struct{}{}
		}
		return nil
	})
	if lockErr != nil {
		if errors.Is(lockErr, store.ErrQualityAgentBusy) {
			return nil, connect.NewError(connect.CodeAborted,
				errors.New("another adoption pass is running; retry shortly"))
		}
		return nil, lockErr
	}
	s.resolveDrainedProposalAttentions(ctx, drained, &callerID)
	remaining, err := s.countPendingDrafts(ctx, agent.ID, targetGUID)
	if err != nil {
		slog.Warn("failed to count the remaining drafts after an approval", log.WithError(err))
	} else {
		response.RemainingPending = int32(remaining)
	}
	return connect.NewResponse(response), nil
}

// ownerAdoptionSession builds the target-scoped session one guarded pass needs.
// A target session already carries the target; a conversation names it and the
// target type is recovered from the queued drafts.
func (s *DataQualityService) ownerAdoptionSession(ctx context.Context, session *store.QualityAgentSessionMessage, agent *store.QualityAgentMessage, targetGUID string) (*store.QualityAgentSessionMessage, error) {
	if session.Kind == 1 {
		copied := *session
		copied.AgentID = agent.ID
		return &copied, nil
	}
	targetType := storepb.MetaType(0)
	pending, err := s.store.ListQualitySuggestions(ctx, &store.FindQualitySuggestionMessage{
		AgentID:    &agent.ID,
		TargetGUID: &targetGUID,
		Statuses:   []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:      []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
		Limit:      intPointer(1),
	})
	if err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		targetType = pending[0].TargetType
	}
	return &store.QualityAgentSessionMessage{
		ResourceID:  session.ResourceID,
		WorkspaceID: session.WorkspaceID,
		AgentID:     agent.ID,
		TargetGUID:  targetGUID,
		TargetType:  targetType,
		Kind:        1,
	}, nil
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

// recordAdoptionAudit writes the permanent ledger row for one adoption. The rule
// path is internal, so no RPC interceptor sees this write; actorID is the principal
// the write is attributed to (the agent for an autonomous adoption, the person for
// an approval) and requestedBy names who asked for it when that differs.
func (s *DataQualityService) recordAdoptionAudit(ctx context.Context, session *store.QualityAgentSessionMessage, agent *store.QualityAgentMessage, suggestion *store.QualitySuggestionMessage, rule *v1pb.QualityRule, state v1pb.QualityRuleState, explicit bool, wake int32, actorID int32, requestedBy int32) {
	payload := map[string]any{
		"suggestion": suggestion.ResourceID,
		"agent":      agent.ResourceID,
		"target":     suggestion.TargetGUID,
		"ruleState":  state.String(),
		"wake":       wake,
		"explicit":   explicit,
	}
	// An approval has no work session; omit it rather than store an empty id.
	if session.ResourceID != "" {
		payload["session"] = session.ResourceID
	}
	if requestedBy > 0 {
		payload["requestedBy"] = common.FormatUserUID(int(requestedBy))
	}
	request, err := structpb.NewStruct(payload)
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
		Parent: common.FormatWorkspace(session.WorkspaceID),
		// The internal adoption write has no RPC. The approval also gets the
		// interceptor's row for AdoptQualityAgentPendingSuggestions; this row is the
		// per-draft detail, so it carries the internal method instead of a phantom RPC.
		Method:   qualityAgentAdoptionAuditMethod,
		Resource: rule.GetName(),
		User:     common.FormatUserUID(int(actorID)),
		Severity: storepb.AuditLogSeverity_INFO,
		Request:  request,
		Response: response,
		Status:   &storepb.AuditLogStatus{Message: "ok"},
	})
	if auditErr != nil {
		slog.Warn("failed to write the adoption audit row", log.WithError(auditErr))
	}
}

func (s *DataQualityService) recordAdoption(ctx context.Context, session *store.QualityAgentSessionMessage, suggestion *store.QualitySuggestionMessage, origin store.QualityAgentAdoptionOrigin, outcome store.QualityAgentAdoptionOutcome, ruleResourceID, reason string) {
	recordErr := s.store.RecordQualityAgentAdoption(ctx, &store.QualityAgentAdoptionMessage{
		AgentID:        session.AgentID,
		SuggestionID:   suggestion.ID,
		Origin:         origin,
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

// draftConcernReason renders the skip reason for a draft its own agent flagged, and reports whether
// the draft carries a concern at all. A concern is the model's objection, not a person's, so it is
// quoted rather than paraphrased: the owner reads the doubt in the words that raised it.
func draftConcernReason(concern string) (string, bool) {
	concern = strings.TrimSpace(concern)
	if concern == "" {
		return "", false
	}
	return "the agent flagged its own draft: " + common.TruncateUTF8Bytes(concern, 300), true
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
	return dataquality.RuleTypeForDraftConfig(config)
}

func intPointer(value int) *int { return &value }
