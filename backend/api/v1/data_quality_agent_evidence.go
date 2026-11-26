package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentpolicy"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Bounds of one evidence read. The tool result as a whole has a hard budget, so the
// read is bounded here instead of leaving the model to receive a cut string.
const (
	defaultAgentEvidenceExecutions = 8
	maxAgentEvidenceExecutions     = 20
)

// ReadTargetExecutions reads a target's recent executions as evidence. It is the
// application-layer read the agent's tools use: the actor is the agent principal,
// passed explicitly because an in-process caller has no RPC context, and the
// permission is rules.evidence rather than the member-baseline executions.*, so the
// scoped grant actually bounds which targets the agent can read.
func (s *DataQualityService) ReadTargetExecutions(ctx context.Context, agentPrincipalID int32, targetGUID string, limit int) ([]dataquality.ExecutionEvidence, error) {
	targetGUID, err := s.requireAgentEvidence(ctx, agentPrincipalID, targetGUID)
	if err != nil {
		return nil, err
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's rules")
	}
	executions, err := s.store.ListQualityExecutions(ctx, &store.FindQualityExecutionMessage{
		TargetGUID: &targetGUID,
		Limit:      intPointer(boundAgentEvidenceExecutions(limit)),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's executions")
	}
	return dataquality.BuildExecutionEvidence(executions, rules), nil
}

// ReadCheckEvidence reads one check's definition and its own executions. An empty
// rule inspects the check behind the newest failed execution; a rule the target does
// not have is NotFound, because the caller named a check it cannot see rather than a
// target it cannot see.
func (s *DataQualityService) ReadCheckEvidence(ctx context.Context, agentPrincipalID int32, agentID int64, targetGUID string, ruleResourceID string, limit int) (*dataquality.CheckEvidence, error) {
	targetGUID, err := s.requireAgentEvidence(ctx, agentPrincipalID, targetGUID)
	if err != nil {
		return nil, err
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's rules")
	}
	executions, err := s.store.ListQualityExecutions(ctx, &store.FindQualityExecutionMessage{
		TargetGUID: &targetGUID,
		Limit:      intPointer(boundAgentEvidenceExecutions(limit)),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's executions")
	}
	rule := dataquality.SelectInspectedRule(rules, executions, ruleResourceID)
	if rule == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("the target has no check %q", ruleResourceID))
	}
	own := make([]*store.QualityExecutionMessage, 0, len(executions))
	for _, execution := range executions {
		if execution != nil && execution.RuleID == rule.ID {
			own = append(own, execution)
		}
	}
	evidence := dataquality.BuildCheckEvidence(targetGUID, rule, own, rules)
	if evidence == nil {
		return nil, nil
	}
	// The diagnosis is what makes the evidence actionable: it names the class, the field
	// and the verbs the class permits. It is computed here, where the registry read for a
	// structure change is available, and never asked of the model.
	if len(own) > 0 {
		changed, err := s.targetSchemaChanges(ctx, []*store.QualityRuleMessage{rule})
		if err != nil {
			return nil, err
		}
		evidence.Diagnosis = dataquality.DiagnoseExecution(dataquality.DiagnosisInput{
			Execution:     own[0],
			Rule:          rule,
			SchemaChanged: changed[rule.ID],
			Now:           time.Now().UTC(),
		})
	}
	// The action list is filtered by the delegation, the pins and the case, through the same
	// policy the verbs enforce. A read that listed a class's full action set would promise the
	// model permissions it does not have, and the refusal would arrive one tool call later.
	if evidence.Diagnosis != nil {
		allowed, err := s.checkEvidenceActions(ctx, agentID, rule, targetGUID, evidence.Diagnosis)
		if err != nil {
			return nil, err
		}
		evidence.AllowedActions = allowed
	}
	return evidence, nil
}

// agentEvidenceScope resolves the agent principal and proves the target is inside its
// delegated scope. The authorization is rules.evidence, which is deliberately not in
// the member baseline: every permission the baseline carries is granted to every
// principal on every target, so only a non-baseline permission can express "this
// agent, these databases".
func (s *DataQualityService) requireAgentEvidence(ctx context.Context, agentPrincipalID int32, targetGUID string) (string, error) {
	targetGUID = strings.TrimSpace(targetGUID)
	if targetGUID == "" {
		return "", connectErrorInvalid("a target resource is required")
	}
	if agentPrincipalID == 0 {
		return "", connect.NewError(connect.CodePermissionDenied, errors.New("an agent principal is required to read quality evidence"))
	}
	actor, err := s.store.GetUserByID(ctx, int(agentPrincipalID))
	if err != nil {
		return "", errors.Wrap(err, "failed to read the agent principal")
	}
	if actor == nil {
		return "", connect.NewError(connect.CodePermissionDenied, errors.New("the agent principal no longer exists"))
	}
	if err := s.checkEvidenceTarget(ctx, actor, targetGUID); err != nil {
		return "", err
	}
	return targetGUID, nil
}

// checkEvidenceTarget is the object-level half of the read: the agent may read one
// target's evidence only if its delegated grant covers that target. The permission is
// rules.evidence, so the scoped authorizer really evaluates the grant chain instead of
// short-circuiting on the member baseline.
func (s *DataQualityService) checkEvidenceTarget(ctx context.Context, actor *store.UserMessage, targetGUID string) error {
	// A focused unit test builds the service without an authorizer; the evidence read
	// keeps working there, the same way the list reads keep their legacy behavior.
	if s.authorizer == nil {
		return nil
	}
	allowed, err := s.authorizer.Check(ctx, actor, permission.DataQualityRulesEvidence, resource.NewRef(resource.TypeTable, targetGUID))
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check the evidence permission"))
	}
	if !allowed {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", permission.DataQualityRulesEvidence))
	}
	return nil
}

// boundAgentEvidenceExecutions applies the read's own budget.
func boundAgentEvidenceExecutions(limit int) int {
	switch {
	case limit <= 0:
		return defaultAgentEvidenceExecutions
	case limit > maxAgentEvidenceExecutions:
		return maxAgentEvidenceExecutions
	default:
		return limit
	}
}

// checkEvidenceActions filters a diagnosis's actions for one check. A missing agent or case is
// not an error: the dossier still shows the diagnosis, with the actions a check without a case
// permits.
func (s *DataQualityService) checkEvidenceActions(ctx context.Context, agentID int64, rule *store.QualityRuleMessage, targetGUID string, diagnosis *dataquality.Diagnosis) ([]dataquality.RepairAction, error) {
	if agentID == 0 {
		return nil, nil
	}
	agent, err := s.store.GetQualityAgentByID(ctx, agentID)
	if err != nil || agent == nil {
		return nil, err
	}
	policy, err := s.agentPolicy(ctx, agent, targetGUID)
	if err != nil {
		return nil, err
	}
	facts := agentpolicy.CaseFacts{
		Diagnosis:    diagnosis,
		ManualLock:   rule.ManualLock,
		PinnedFields: rule.PinnedFields,
	}
	ruleID := rule.ID
	openCase, caseErr := s.store.GetOpenQualityAgentCase(ctx, agentID,
		store.QualityAgentCaseSubjectKey(targetGUID, &ruleID, store.QualityAgentCaseKindExecution))
	if caseErr != nil {
		return nil, caseErr
	}
	if openCase != nil {
		facts.State = openCase.State
		facts.Attempts = openCase.Attempts
	}
	return policy.AllowedActions(facts), nil
}
