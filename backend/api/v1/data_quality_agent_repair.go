package v1

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentpolicy"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

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
	openCase, agent, err := s.agentCaseForRepair(ctx, agentPrincipalID, caseResourceID)
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
func (s *DataQualityService) agentCaseForRepair(ctx context.Context, agentPrincipalID int32, caseResourceID string) (*store.QualityAgentCaseMessage, *store.QualityAgentMessage, error) {
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
	if err := s.checkAgentTargetPermission(ctx, agent, openCase.TargetGUID, permission.DataQualityRulesRepair); err != nil {
		return nil, nil, err
	}
	return openCase, agent, nil
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
