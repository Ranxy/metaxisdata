package v1

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentpolicy"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// qualityAgentCasePrefix is the resource-name segment of one agent work item.
const qualityAgentCasePrefix = "qualityAgentCases/"

// AgentControlResult is what one state change did, in the shape the model reports and the
// transcript keeps.
type AgentControlResult struct {
	Case     string `json:"case"`
	Check    string `json:"check"`
	Action   string `json:"action"`
	State    string `json:"state"`
	Revision int32  `json:"revision"`
	Reason   string `json:"reason"`
}

// ControlAgentCheck pauses a check whose case says it cannot run. It is the one write that ends
// the noise of a broken check without deciding anything about the data: pausing a check that
// cannot produce a verdict hides no red, and the escalated case tells a person that a decision
// about the check is still owed.
func (s *DataQualityService) ControlAgentCheck(ctx context.Context, agentPrincipalID int32, caseResourceID, action string) (*AgentControlResult, error) {
	openCase, agent, err := s.agentCaseForRepair(ctx, agentPrincipalID, caseResourceID, permission.DataQualityRulesRepair)
	if err != nil {
		return nil, err
	}
	if openCase.RuleID == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the case is not about one check"))
	}
	rule, err := s.store.GetQualityRuleByID(ctx, *openCase.RuleID)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the case's check no longer exists"))
	}
	policy, err := s.agentPolicy(ctx, agent, openCase.TargetGUID)
	if err != nil {
		return nil, err
	}
	diagnosis, err := s.agentCaseDiagnosis(ctx, rule, openCase)
	if err != nil {
		return nil, err
	}
	requested := agentpolicy.ControlAction(strings.ToUpper(strings.TrimSpace(action)))
	verdict := policy.DecideControl(agentpolicy.CaseFacts{
		State:        openCase.State,
		Attempts:     openCase.Attempts,
		Diagnosis:    diagnosis,
		ManualLock:   rule.ManualLock,
		PinnedFields: rule.PinnedFields,
	}, requested)
	if !verdict.Allowed {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(verdict.Reason))
	}
	paused := storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED
	updated, err := s.store.UpdateQualityRule(ctx, rule.ResourceID, &store.UpdateQualityRuleMessage{
		State:            &paused,
		ExpectedRevision: &rule.Revision,
		UpdatedBy:        principalIDPointer(int(agentPrincipalID)),
	})
	if err != nil {
		if common.ErrorCode(err) == common.Conflict {
			return nil, connect.NewError(connect.CodeAborted, err)
		}
		return nil, errors.Wrap(err, "failed to pause the quality rule")
	}
	// A target whose last enabled check is paused must stop being due, so the schedule follows
	// the check. A failure here is logged and does not undo the pause: the check is already off.
	if err := s.store.SyncQualityTargetSchedule(ctx, updated.TargetGUID, updated.TargetType, time.Now().UTC()); err != nil {
		slog.Warn("failed to resync the target schedule after a pause",
			slog.String("rule", updated.ResourceID), log.WithError(err))
	}
	reason := "the agent paused the check because it cannot run; a person decides whether to resume, archive or replace it"
	if _, applied, caseErr := s.store.UpdateQualityAgentCaseState(ctx, openCase.ResourceID, openCase.State,
		store.QualityAgentCaseStateEscalated, openCase.Attempts, nil, reason); caseErr != nil {
		slog.Warn("failed to escalate the case after a pause",
			slog.String("case", openCase.ResourceID), log.WithError(caseErr))
	} else if !applied {
		slog.Warn("the case changed before it could be escalated",
			slog.String("case", openCase.ResourceID))
	} else {
		// An escalated case is a decision the owner owes, so it has to reach them. The notice is
		// deduped per case: a retried verb must not page twice.
		s.raiseCaseAttention(ctx, openCase, agent, caseNotice{
			Kind:     store.QualityAgentAttentionKindBlocked,
			Question: reason,
		})
	}
	return &AgentControlResult{
		Case:     openCase.ResourceID,
		Check:    "qualityRules/" + updated.ResourceID,
		Action:   string(requested),
		State:    strings.TrimPrefix(updated.State.String(), "QUALITY_RULE_STATE_"),
		Revision: updated.Revision,
		Reason:   reason,
	}, nil
}

// agentCaseForRepair resolves the case a verb was asked to act on and proves the caller is the
// agent that owns it. Both halves matter: the case carries the target and the evidence the verb
// must judge, and a case is the agent's own work item, so another agent's case is never a target.
func (s *DataQualityService) agentCaseForRepair(ctx context.Context, agentPrincipalID int32, caseResourceID string, perm permission.Permission) (*store.QualityAgentCaseMessage, *store.QualityAgentMessage, error) {
	resourceID := strings.TrimSpace(caseResourceID)
	if index := strings.LastIndex(resourceID, "/"); index >= 0 {
		resourceID = resourceID[index+1:]
	}
	if resourceID == "" {
		return nil, nil, connectErrorInvalid("a case resource name is required")
	}
	openCase, err := s.store.GetQualityAgentCaseByResourceID(ctx, resourceID)
	if err != nil {
		return nil, nil, err
	}
	if openCase == nil {
		return nil, nil, connect.NewError(connect.CodeNotFound, errors.Errorf("case %q not found", resourceID))
	}
	agent, err := s.store.GetQualityAgentByID(ctx, openCase.AgentID)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to read the case's agent")
	}
	if agent == nil || agent.PrincipalID != int(agentPrincipalID) {
		return nil, nil, connect.NewError(connect.CodePermissionDenied, errors.New("the case belongs to another agent"))
	}
	if err := s.checkAgentTargetPermission(ctx, agent, openCase.TargetGUID, perm); err != nil {
		return nil, nil, err
	}
	return openCase, agent, nil
}

// checkTitle names a check for prose, falling back to its id when a person never titled it.
func checkTitle(rule *store.QualityRuleMessage) string {
	if rule == nil {
		return ""
	}
	if title := strings.TrimSpace(rule.Title); title != "" {
		return title
	}
	return "qualityRules/" + rule.ResourceID
}

// agentPolicy resolves how far the agent may go on one target.
func (s *DataQualityService) agentPolicy(ctx context.Context, agent *store.QualityAgentMessage, targetGUID string) (agentpolicy.Policy, error) {
	if agent == nil {
		return agentpolicy.Policy{}, connect.NewError(connect.CodePermissionDenied, errors.New("the case has no agent"))
	}
	levels, err := s.store.GetQualityAgentParticipation(ctx, agent.ID, databaseRefOf(targetGUID), targetGUID)
	if err != nil {
		return agentpolicy.Policy{}, err
	}
	if levels == nil {
		return agentpolicy.Policy{}, connect.NewError(connect.CodeNotFound, errors.New("the agent no longer exists"))
	}
	return agentpolicy.Policy{
		Participation: agentpolicy.Effective(
			agentpolicy.Participation(levels.Agent),
			optionalParticipation(levels.Database),
			optionalParticipation(levels.Target),
		),
		Capabilities: agent.Capabilities,
	}, nil
}

// agentCaseDiagnosis re-derives the diagnosis a verb must judge. The model never supplies it:
// the case, the rule and the execution are the server's evidence, and a verb that trusted the
// model's summary would be acting on a claim.
func (s *DataQualityService) agentCaseDiagnosis(ctx context.Context, rule *store.QualityRuleMessage, openCase *store.QualityAgentCaseMessage) (*dataquality.Diagnosis, error) {
	if rule == nil || openCase == nil || openCase.ExecutionID == nil {
		return nil, nil
	}
	execution, err := s.store.GetQualityExecution(ctx, *openCase.ExecutionID)
	if err != nil {
		return nil, err
	}
	if execution == nil {
		return nil, nil
	}
	changed, err := s.targetSchemaChanges(ctx, []*store.QualityRuleMessage{rule})
	if err != nil {
		return nil, err
	}
	return dataquality.DiagnoseExecution(dataquality.DiagnosisInput{
		Execution:     execution,
		Rule:          rule,
		SchemaChanged: changed[rule.ID],
		Now:           time.Now().UTC(),
	}), nil
}

// checkAgentTargetPermission proves the agent holds a permission on one target. The permission
// must be outside the member baseline: a baseline permission is granted to every principal on
// every target, so it cannot scope a verb to the agent's databases.
func (s *DataQualityService) checkAgentTargetPermission(ctx context.Context, agent *store.QualityAgentMessage, targetGUID string, perm permission.Permission) error {
	if s.authorizer == nil {
		return nil
	}
	actor, err := s.store.GetUserByID(ctx, agent.PrincipalID)
	if err != nil {
		return errors.Wrap(err, "failed to read the agent principal")
	}
	if actor == nil {
		return connect.NewError(connect.CodePermissionDenied, errors.New("the agent principal no longer exists"))
	}
	allowed, err := s.authorizer.Check(ctx, actor, perm, resource.NewRef(resource.TypeTable, targetGUID))
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the target permission"))
	}
	if !allowed {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", perm))
	}
	return nil
}

// databaseRefOf turns a target GUID into the database resource name the delegation cascade keys
// on. A GUID that does not name a database resolves to empty, which the cascade reads as "the
// database row does not apply".
func databaseRefOf(targetGUID string) string {
	parts := common.SplitMetaGUID(targetGUID)
	if len(parts) < 2 {
		return ""
	}
	return common.FormatDatabase(parts[0], parts[1])
}

// optionalParticipation maps the store's nullable level onto the policy's.
func optionalParticipation(level *store.QualityAgentParticipation) *agentpolicy.Participation {
	if level == nil {
		return nil
	}
	converted := agentpolicy.Participation(*level)
	return &converted
}

// planAgentRun plans one immediate execution of one check through the ordinary manual-run path,
// authorizing with the agent's own principal exactly as the adoption path does: the agent is the
// actor, and rules.run is checked on the target. It returns the execution the runner will pick up.
func (s *DataQualityService) planAgentRun(ctx context.Context, agent *store.QualityAgentMessage, rule *store.QualityRuleMessage) (string, error) {
	actor, err := s.store.GetUserByID(ctx, agent.PrincipalID)
	if err != nil {
		return "", errors.Wrap(err, "failed to read the agent principal")
	}
	if actor == nil {
		return "", connect.NewError(connect.CodePermissionDenied, errors.New("the agent principal no longer exists"))
	}
	agentCtx := context.WithValue(ctx, common.UserContextKey, actor)
	response, err := s.RunQualityRules(agentCtx, connect.NewRequest(&v1pb.RunQualityRulesRequest{
		Parent: "workspaces/-",
		Rules:  []string{"qualityRules/" + rule.ResourceID},
	}))
	if err != nil {
		return "", err
	}
	if len(response.Msg.GetExecutions()) == 0 {
		return "", connect.NewError(connect.CodeFailedPrecondition, errors.New("no execution was planned for the check"))
	}
	return response.Msg.GetExecutions()[0].GetName(), nil
}

// AgentRunResult is what one diagnostic run did: the execution the runner will pick up and the
// case's new state.
type AgentRunResult struct {
	Case      string `json:"case"`
	Check     string `json:"check"`
	Execution string `json:"execution"`
	State     string `json:"state"`
}

// RunAgentCheck asks the runner to execute one check now, so a diagnosis can be reproduced or a
// repair verified without waiting for the next scheduled slot. The run is the ordinary manual
// path: the API plans exactly this rule, the runner executes it, and the case waits for the result
// like any other verification.
func (s *DataQualityService) RunAgentCheck(ctx context.Context, agentPrincipalID int32, caseResourceID string) (*AgentRunResult, error) {
	openCase, agent, err := s.agentCaseForRepair(ctx, agentPrincipalID, caseResourceID, permission.DataQualityRulesRun)
	if err != nil {
		return nil, err
	}
	if openCase.State != store.QualityAgentCaseStateDiagnosed {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("only a diagnosed case can ask for a run; a repair already waiting for verification does not need another"))
	}
	if openCase.RuleID == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the case is not about one check"))
	}
	rule, err := s.store.GetQualityRuleByID(ctx, *openCase.RuleID)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the case's check no longer exists"))
	}
	if rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the check is paused, so it cannot run; a person decides whether to resume it"))
	}
	execution, err := s.planAgentRun(ctx, agent, rule)
	if err != nil {
		return nil, err
	}
	// Look again soon: the runner picks the execution up within its tick, and the next wake reads
	// the verdict.
	next := time.Now().UTC().Add(time.Minute)
	if _, applied, caseErr := s.store.UpdateQualityAgentCaseState(ctx, openCase.ResourceID,
		openCase.State, store.QualityAgentCaseStateActed, openCase.Attempts, &next, ""); caseErr != nil {
		slog.Warn("failed to mark the case as acted after a run",
			slog.String("case", openCase.ResourceID), log.WithError(caseErr))
	} else if !applied {
		slog.Warn("the case changed before it could be marked acted", slog.String("case", openCase.ResourceID))
	}
	return &AgentRunResult{
		Case:      openCase.ResourceID,
		Check:     "qualityRules/" + rule.ResourceID,
		Execution: execution,
		State:     store.QualityAgentCaseStateActed.Name(),
	}, nil
}

// caseNotice is what one verb tells the owner about its case. It is a value rather than a set of
// parameters because a notice is one thing: the kind picks the headline, the patch says what to
// change, and the question says why.
type caseNotice struct {
	Kind store.QualityAgentAttentionKind
	// Title overrides the kind's default headline. A proposal that asks for one concrete value is
	// a different thing from a queue of drafts, though both are pending decisions.
	Title string
	// Question is the body, in the owner's language.
	Question string
	// Patch is the change the owner is asked to make, when there is exactly one. It is stored on
	// the notice so the owner reviews a field and a value, not a paragraph.
	Patch *storepb.QualityAgentFieldPatch
}

// agentOwnerLanguage resolves the language an agent's server prose is written in for its owner.
func (s *DataQualityService) agentOwnerLanguage(ctx context.Context, agent *store.QualityAgentMessage) (string, *store.UserMessage) {
	var owner *store.UserMessage
	if agent != nil && agent.OwnerPrincipalID != nil {
		owner, _ = s.store.GetUserByID(ctx, int(*agent.OwnerPrincipalID))
	}
	return agentlang.ForAgent(agent, owner), owner
}

// revisedAgentConfig is the definition one repair would write. The value is the server's when the
// case names no field: aligning a structure snapshot means writing the columns the registry holds
// now, and a value the model supplied would let it pin a snapshot that never existed.
func (s *DataQualityService) revisedAgentConfig(ctx context.Context, rule *store.QualityRuleMessage, field, value string) (*storepb.QualityRuleConfig, error) {
	if strings.TrimSpace(field) != "" {
		patched, err := dataquality.PatchField(rule.Config, field, value)
		if err != nil {
			return nil, connectErrorInvalid("%v", err)
		}
		return patched, nil
	}
	resolution, err := dataquality.ResolveTarget(ctx, s.store, rule.TargetGUID, rule.TargetType)
	if err != nil {
		return nil, err
	}
	aligned, ok := proto.Clone(rule.Config).(*storepb.QualityRuleConfig)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("the check's config could not be copied"))
	}
	if !alignSchemaDriftSnapshot(aligned, resolution.Target.Columns) {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the check has no column snapshot to align"))
	}
	return aligned, nil
}

// recentRuleExecutions reads the completed runs a replay re-judges, newest first. A run that never
// finished has no observation, so it is not evidence about the change.
func (s *DataQualityService) recentRuleExecutions(ctx context.Context, ruleID int64, limit int) ([]*store.QualityExecutionMessage, error) {
	executions, err := s.store.ListQualityExecutions(ctx, &store.FindQualityExecutionMessage{
		RuleID: &ruleID,
		States: []storepb.QualityExecutionState{storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED},
		Limit:  &limit,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the check's recent executions")
	}
	return executions, nil
}

// AgentRevisionResult is what one revision wrote: the field and its new value, the revision the
// CAS produced and the execution that will verify the repair.
type AgentRevisionResult struct {
	Case      string `json:"case"`
	Check     string `json:"check"`
	Field     string `json:"field"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Revision  int32  `json:"revision"`
	Execution string `json:"execution,omitempty"`
	State     string `json:"state"`
}

// ReviseAgentField applies the one deterministic repair a case's diagnosis names.
//
// The value is the server's when the case names no field: aligning a structure snapshot means
// writing the columns the registry holds now, and a value the model supplied would let it pin a
// snapshot that never existed. A field the model does name is a criterion, which this verb refuses
// unless the delegation is `tune` and the change replays against the recent executions — the same
// table every other write uses, read from the same facts.
//
// The write is the last step and it is one compare-and-set: the config, validated and compiled
// first, is written with the revision the decision was based on, so a person editing the check
// meanwhile makes this fail instead of being overwritten. It deliberately does not reuse the
// shared rule-update path, which writes before it decides and authorizes with rules.update.
func (s *DataQualityService) ReviseAgentField(ctx context.Context, agentPrincipalID int32, caseResourceID, field, value string) (*AgentRevisionResult, error) {
	openCase, agent, err := s.agentCaseForRepair(ctx, agentPrincipalID, caseResourceID, permission.DataQualityRulesRepair)
	if err != nil {
		return nil, err
	}
	if openCase.RuleID == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the case is not about one check"))
	}
	rule, err := s.store.GetQualityRuleByID(ctx, *openCase.RuleID)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the case's check no longer exists"))
	}
	if rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
		// A repair is only verified by a run, and a paused check never runs: writing one would leave
		// the case waiting for evidence that cannot arrive.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the check is paused, so a repair could never be verified; a person decides whether to resume it"))
	}
	policy, err := s.agentPolicy(ctx, agent, openCase.TargetGUID)
	if err != nil {
		return nil, err
	}
	diagnosis, err := s.agentCaseDiagnosis(ctx, rule, openCase)
	if err != nil {
		return nil, err
	}
	field = strings.TrimSpace(field)
	config, err := s.revisedAgentConfig(ctx, rule, field, value)
	if err != nil {
		return nil, err
	}
	// A criterion change is only checkable by re-judging the runs it would affect, so the replay is
	// computed before the decision and handed to it: the facts the gate reads are the facts the
	// refusal names. A snapshot repair changes no verdict, so it replays nothing.
	replay := dataquality.Replay{}
	if field != "" {
		executions, err := s.recentRuleExecutions(ctx, rule.ID, dataquality.ReplayWindow)
		if err != nil {
			return nil, err
		}
		replay = dataquality.ReplayRule(rule.RuleType, config, executions)
	}
	verdict := policy.DecideRevise(agentpolicy.CaseFacts{
		State:           openCase.State,
		Attempts:        openCase.Attempts,
		Diagnosis:       diagnosis,
		ManualLock:      rule.ManualLock,
		PinnedFields:    rule.PinnedFields,
		ReplayAvailable: replay.Available,
	}, agentpolicy.FieldChange{Field: field})
	if !verdict.Allowed {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(verdict.Reason))
	}
	if len(replay.Loosened) > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
			"the change would turn failing runs into passes (%s), so it is not a repair; propose it instead",
			strings.Join(replay.Loosened, ", ")))
	}
	validated, _, err := s.validateRuleInput(ctx, openCase.WorkspaceID, rule.RuleType, rule.TargetGUID, rule.TargetType, config)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.Wrap(err, "the repaired definition would not save"))
	}
	// A server-computed snapshot changes a value the registry owns, not the statement the check
	// runs, so there is nothing new to probe. A value the caller supplied does change the
	// statement, and a probe proves it binds before anything is written.
	if field != "" {
		if err := s.probeQualityRule(ctx, validated); err != nil {
			return nil, err
		}
	}
	// The check's own fingerprint moves with the definition: leaving the old one behind would make
	// the next run report the same drift the repair just settled.
	schemaHash, err := s.targetSchemaHash(ctx, validated.Target)
	if err != nil {
		return nil, err
	}
	updated, err := s.store.UpdateQualityRule(ctx, rule.ResourceID, &store.UpdateQualityRuleMessage{
		Config:           config,
		TargetMetaHash:   &schemaHash,
		ExpectedRevision: &rule.Revision,
		UpdatedBy:        principalIDPointer(int(agentPrincipalID)),
	})
	if err != nil {
		if common.ErrorCode(err) == common.Conflict {
			return nil, connect.NewError(connect.CodeAborted, err)
		}
		return nil, errors.Wrap(err, "failed to write the repaired definition")
	}
	// The repair is only a repair once a run proves it. The run is planned before the case moves, so
	// the next batch verifies it; when planning fails the case still moves, because the check's next
	// scheduled run verifies it just the same.
	execution, runErr := s.planAgentRun(ctx, agent, updated)
	if runErr != nil {
		slog.Warn("failed to plan the verification run of a repair",
			slog.String("case", openCase.ResourceID), log.WithError(runErr))
		execution = ""
	}
	next := time.Now().UTC().Add(time.Minute)
	if _, applied, caseErr := s.store.UpdateQualityAgentCaseState(ctx, openCase.ResourceID,
		openCase.State, store.QualityAgentCaseStateActed, openCase.Attempts, &next, ""); caseErr != nil {
		slog.Warn("failed to mark the case as acted after a repair",
			slog.String("case", openCase.ResourceID), log.WithError(caseErr))
	} else if !applied {
		slog.Warn("the case changed before it could be marked acted", slog.String("case", openCase.ResourceID))
	}
	result := &AgentRevisionResult{
		Case:      openCase.ResourceID,
		Check:     "qualityRules/" + updated.ResourceID,
		Revision:  updated.Revision,
		Execution: execution,
		State:     store.QualityAgentCaseStateActed.Name(),
	}
	if diagnosis != nil {
		result.Field, result.From = diagnosis.Field, diagnosis.FieldValue
	}
	if field != "" {
		result.To = value
	} else {
		result.From = ""
		result.To = "the target's current columns"
	}
	return result, nil
}

// AgentProposalResult is what one proposal asked for: the change, the case it escalated and the
// check it is about.
type AgentProposalResult struct {
	Case   string `json:"case"`
	Check  string `json:"check"`
	Field  string `json:"field"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
	State  string `json:"state"`
}

// ProposeAgentRevision asks the owner to apply one change the delegation withholds: a criterion,
// a field a person pinned, a value on a check someone else manages. It is what makes "the agent
// cannot fix this" a concrete request instead of a paragraph.
//
// The proposal never writes the check. It validates the value with the same pipeline a save runs,
// so the owner is never handed a change that would not store, and it records the change on the
// escalation, so what the owner reads is the field and the value rather than a summary of them.
// The participation level, the pins and the replay gate all permit a proposal: they withhold the
// write, and asking a person is precisely the alternative they name.
func (s *DataQualityService) ProposeAgentRevision(ctx context.Context, agentPrincipalID int32, caseResourceID, field, value, rationale string) (*AgentProposalResult, error) {
	openCase, agent, err := s.agentCaseForRepair(ctx, agentPrincipalID, caseResourceID, permission.DataQualityRulesEvidence)
	if err != nil {
		return nil, err
	}
	if openCase.RuleID == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the case is not about one check"))
	}
	rule, err := s.store.GetQualityRuleByID(ctx, *openCase.RuleID)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the case's check no longer exists"))
	}
	policy, err := s.agentPolicy(ctx, agent, openCase.TargetGUID)
	if err != nil {
		return nil, err
	}
	diagnosis, err := s.agentCaseDiagnosis(ctx, rule, openCase)
	if err != nil {
		return nil, err
	}
	field = strings.TrimSpace(field)
	verdict := policy.DecidePropose(agentpolicy.CaseFacts{
		State:        openCase.State,
		Attempts:     openCase.Attempts,
		Diagnosis:    diagnosis,
		ManualLock:   rule.ManualLock,
		PinnedFields: rule.PinnedFields,
	}, agentpolicy.FieldChange{Field: field})
	if !verdict.Allowed {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(verdict.Reason))
	}
	// The proposed value must be one the check could save. The patch is applied to a copy and
	// validated; nothing is written and no probe runs, because a proposal is a request for a
	// decision, not a run. A value that would not store is worth refusing here: a person who
	// applies a broken patch learns nothing about the failure the case is about.
	patched, err := dataquality.PatchField(rule.Config, field, value)
	if err != nil {
		return nil, connectErrorInvalid("%v", err)
	}
	if _, _, err := s.validateRuleInput(ctx, openCase.WorkspaceID, rule.RuleType, rule.TargetGUID, rule.TargetType, patched); err != nil {
		return nil, connectErrorInvalid("the proposed value would not save: %v", err)
	}
	effect := agentpolicy.EffectOf(diagnosis)
	patch := &storepb.QualityAgentFieldPatch{
		Field:        field,
		Value:        value,
		BaseRevision: rule.Revision,
		Effect:       effect.Name(),
		Rationale:    common.TruncateUTF8Bytes(strings.TrimSpace(rationale), 1000),
	}
	language, _ := s.agentOwnerLanguage(ctx, agent)
	question := agentlang.CaseRevisionQuestion(language, agentlang.CaseRevision{
		Check:     checkTitle(rule),
		Field:     field,
		From:      diagnosis.FieldValue,
		To:        value,
		Effect:    patch.GetEffect(),
		Rationale: patch.GetRationale(),
	})
	reason := "the agent proposed " + field + " = " + value + "; a person decides whether to apply it"
	if _, applied, caseErr := s.store.UpdateQualityAgentCaseState(ctx, openCase.ResourceID,
		openCase.State, store.QualityAgentCaseStateEscalated, openCase.Attempts, nil, reason); caseErr != nil {
		slog.Warn("failed to escalate the case after a proposal",
			slog.String("case", openCase.ResourceID), log.WithError(caseErr))
	} else if !applied {
		slog.Warn("the case changed before it could be escalated", slog.String("case", openCase.ResourceID))
	} else {
		s.raiseCaseAttention(ctx, openCase, agent, caseNotice{
			Kind:     store.QualityAgentAttentionKindProposalPending,
			Title:    agentlang.CaseRevisionTitle(language),
			Question: question,
			Patch:    patch,
		})
	}
	return &AgentProposalResult{
		Case:   openCase.ResourceID,
		Check:  "qualityRules/" + rule.ResourceID,
		Field:  field,
		Value:  value,
		Effect: patch.GetEffect(),
		State:  store.QualityAgentCaseStateEscalated.Name(),
	}, nil
}

// raiseCaseAttention tells the owner that a case needs a decision. It is best effort: a failed
// notice never undoes the write it reports, and the case row stays the durable record.
func (s *DataQualityService) raiseCaseAttention(ctx context.Context, openCase *store.QualityAgentCaseMessage, agent *store.QualityAgentMessage, notice caseNotice) {
	if openCase == nil || agent == nil {
		return
	}
	language, owner := s.agentOwnerLanguage(ctx, agent)
	title := notice.Title
	if title == "" {
		title = agentlang.AttentionTitle(language, notice.Kind)
	}
	// Link the target session when there is one, so the card carries the target; an unlinked
	// item is still an agent-level notice rather than a lost one.
	var sessionID *int64
	if sessions, err := s.store.ListQualityAgentSessionsByTarget(ctx, openCase.WorkspaceID, openCase.TargetGUID); err == nil {
		for _, session := range sessions {
			if session.AgentID == agent.ID {
				sessionID = &session.ID
				break
			}
		}
	}
	attention, created, err := s.store.CreateQualityAgentAttention(ctx, &store.QualityAgentAttentionMessage{
		WorkspaceID:    openCase.WorkspaceID,
		AgentID:        agent.ID,
		Kind:           notice.Kind,
		SessionID:      sessionID,
		Title:          title,
		Question:       common.TruncateUTF8Bytes(notice.Question, 2000),
		DedupeKey:      "case:" + openCase.ResourceID,
		CaseResourceID: openCase.ResourceID,
		SuggestedPatch: notice.Patch,
		CreatedAt:      time.Now().UTC(),
	})
	if err != nil || !created {
		return
	}
	if s.notifier == nil {
		return
	}
	detail := &storepb.QualityAgentAttentionDetail{
		Agent:     common.FormatWorkspace(openCase.WorkspaceID) + "/qualityAgents/" + agent.ResourceID,
		Attention: common.FormatWorkspace(openCase.WorkspaceID) + "/qualityAgentAttentions/" + attention.ResourceID,
		Kind:      int32(notice.Kind),
		Title:     attention.Title,
		Question:  attention.Question,
	}
	dedupeKey := "dqagent:" + attention.ResourceID
	if owner != nil {
		if sendErr := s.notifier.SendQualityAgentAttention(ctx, int32(owner.ID), dedupeKey, detail); sendErr != nil {
			slog.Warn("failed to send a case attention notification", log.WithError(sendErr))
		}
		return
	}
	if sendErr := s.notifier.SendQualityAgentAttentionToAdmins(ctx, dedupeKey, detail); sendErr != nil {
		slog.Warn("failed to send a case attention notification to admins", log.WithError(sendErr))
	}
}
