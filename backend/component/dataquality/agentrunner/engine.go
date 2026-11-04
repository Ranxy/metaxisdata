// Package agentrunner is the deterministic core of the built-in quality agent.
//
// M2 runs without a model: a maintain goal surveys the target's plan and records the
// uncovered properties as investigations plus one asset fact; an event goal turns a
// failed execution into an investigation. Every effect is journaled before it happens
// (effect sandwich) and deduplicated by its fingerprint, so a crash or a repeated wake
// cannot duplicate the work product. The model-driven turns replace this survey in M3.
package agentrunner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// maintainWake is how long a fully surveyed maintain goal sleeps before looking again.
	maintainWake = 24 * time.Hour
	// gapContinueWake is how soon a maintain goal continues when more gaps remain than
	// one turn records.
	gapContinueWake = time.Minute
	// maxGapInvestigations bounds the investigations one turn records.
	maxGapInvestigations = 5
)

// Store is the slice of *store.Store the engine needs.
type Store interface {
	GetQualityContract(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*store.QualityContractMessage, error)
	ListQualityRules(ctx context.Context, find *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error)
	GetQualityExecution(ctx context.Context, id int64) (*store.QualityExecutionMessage, error)
	CreateQualityInvestigation(ctx context.Context, input *store.CreateQualityInvestigationInput) (*store.QualityInvestigationMessage, bool, error)
	CreateQualityMemory(ctx context.Context, input *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error)
	ListQualityMemories(ctx context.Context, find *store.FindQualityMemoryMessage) ([]*store.QualityMemoryMessage, error)
	AppendQualityAgentGoalStep(ctx context.Context, step *store.QualityAgentGoalStepMessage) (*store.QualityAgentGoalStepMessage, error)
}

// Engine runs one deterministic turn of a goal.
type Engine struct {
	store Store
	now   func() time.Time
}

// NewEngine creates the deterministic engine.
func NewEngine(store Store) *Engine {
	return &Engine{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// Outcome is what the runner persists after a turn: the next phase, an optional
// blocker, the next wakeup and the checkpoint.
type Outcome struct {
	Phase         storepb.QualityAgentGoalPhase
	BlockedCode   string
	BlockedReason string
	NextWakeAt    *time.Time
	Checkpoint    *storepb.QualityAgentGoalCheckpoint
	Summary       string
}

// Run executes one deterministic turn for one goal.
func (e *Engine) Run(ctx context.Context, goal *store.QualityAgentGoalMessage) (Outcome, error) {
	switch goal.Kind {
	case storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_MAINTAIN_TARGET:
		return e.maintain(ctx, goal)
	case storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_INVESTIGATE_EVENT:
		return e.investigateEvent(ctx, goal)
	default:
		return Outcome{
			Phase:         storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED,
			BlockedCode:   "needs-human",
			BlockedReason: "a custom goal needs the model turn, which arrives with M3",
			Summary:       "blocked: custom goals need the model turn",
		}, nil
	}
}

// maintain surveys the target's plan. A fully surveyed target sleeps until the next
// daily look; uncovered declared properties become investigations, and one asset fact
// records the survey.
func (e *Engine) maintain(ctx context.Context, goal *store.QualityAgentGoalMessage) (Outcome, error) {
	rules, err := e.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &goal.TargetGUID})
	if err != nil {
		return Outcome{}, errors.Wrap(err, "failed to read the target's rules")
	}
	contract, err := e.store.GetQualityContract(ctx, goal.TargetGUID, storepb.MetaType(goal.TargetType))
	if err != nil {
		return Outcome{}, errors.Wrap(err, "failed to read the target's contract")
	}
	var contractSettings *storepb.QualityContractSettings
	if contract != nil {
		contractSettings = contract.Settings
	}
	intent := dataquality.ActiveIntent(contractSettings.GetIntent(), e.now())
	plan := dataquality.DerivePlan(contract != nil, intent, ruleShapes(rules))
	done := doneSet(goal.Checkpoint)
	checkpoint := &storepb.QualityAgentGoalCheckpoint{Phase: "survey", Summary: "surveyed the target's coverage"}
	checkpoint.Done = append(checkpoint.Done, goal.Checkpoint.GetDone()...)

	gapLabels := make([]string, 0, len(plan.Requirements))
	for _, requirement := range plan.Requirements {
		if requirement.Satisfied || requirement.Exempted {
			continue
		}
		gapLabels = append(gapLabels, gapLabel(requirement))
	}
	if len(gapLabels) == 0 {
		return Outcome{
			Phase:      storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE,
			NextWakeAt: ptrTime(e.now().Add(maintainWake)),
			Checkpoint: checkpoint,
			Summary:    "surveyed: no coverage gap",
		}, nil
	}

	written := 0
	remaining := 0
	for _, label := range gapLabels {
		if done["gap:"+label] {
			continue
		}
		if written >= maxGapInvestigations {
			remaining++
			continue
		}
		fingerprint := "coverage-gap:" + goal.TargetGUID + ":" + label
		input := &store.CreateQualityInvestigationInput{
			TargetGUID:       goal.TargetGUID,
			TargetType:       storepb.MetaType(goal.TargetType),
			Trigger:          storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_COVERAGE_GAP,
			TriggerReference: goal.TargetGUID + "#" + label,
			Status:           storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
			Verdict:          storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_CONTRIBUTING_FACTOR,
			Summary:          fmt.Sprintf("the target declares %s but no enabled check covers it", label),
			Evidence: &storepb.QualityInvestigationEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{
				{Kind: "PLAN", Reference: goal.TargetGUID},
				{Kind: "COVERAGE", Reference: label},
			}},
			RequestFingerprint: fingerprint,
			CreatedBy:          ptrInt32(goal.AgentPrincipalID),
		}
		if err := e.recordInvestigation(ctx, goal, input); err != nil {
			return Outcome{}, err
		}
		checkpoint.Done = append(checkpoint.Done, "gap:"+label)
		done["gap:"+label] = true
		written++
	}
	if !done["asset_fact"] {
		if err := e.recordAssetFact(ctx, goal, gapLabels); err != nil {
			return Outcome{}, err
		}
		checkpoint.Done = append(checkpoint.Done, "asset_fact")
	}
	if remaining > 0 {
		return Outcome{
			Phase:      storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE,
			NextWakeAt: ptrTime(e.now().Add(gapContinueWake)),
			Checkpoint: checkpoint,
			Summary:    fmt.Sprintf("surveyed %d uncovered propert(ies); %d remain for the next turn", len(gapLabels), remaining),
		}, nil
	}
	return Outcome{
		Phase:      storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE,
		NextWakeAt: ptrTime(e.now().Add(maintainWake)),
		Checkpoint: checkpoint,
		Summary:    fmt.Sprintf("surveyed %d uncovered propert(ies)", len(gapLabels)),
	}, nil
}

// investigateEvent turns one failed execution into an investigation. The conclusion is
// deliberately inconclusive: without a model it records the facts, not a root cause.
func (e *Engine) investigateEvent(ctx context.Context, goal *store.QualityAgentGoalMessage) (Outcome, error) {
	executionID, parseReason := executionIDFromTrigger(goal.TriggerRef)
	if parseReason != "" {
		return Outcome{
			Phase:         storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED,
			BlockedCode:   "needs-human",
			BlockedReason: parseReason,
			Summary:       "blocked: the trigger reference is not an execution",
		}, nil
	}
	execution, err := e.store.GetQualityExecution(ctx, executionID)
	if err != nil {
		return Outcome{}, errors.Wrap(err, "failed to read the triggering execution")
	}
	if execution == nil {
		return Outcome{
			Phase:         storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED,
			BlockedCode:   "needs-human",
			BlockedReason: "the triggering execution no longer exists",
			Summary:       "blocked: the triggering execution is gone",
		}, nil
	}
	// The goal's target is the scope: an execution of another target is not evidence
	// for this goal, whatever its id.
	if execution.TargetGUID != goal.TargetGUID || execution.TargetType != storepb.MetaType(goal.TargetType) {
		return Outcome{
			Phase:         storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED,
			BlockedCode:   "needs-human",
			BlockedReason: "the triggering execution belongs to another target",
			Summary:       "blocked: the execution does not match the goal's target",
		}, nil
	}
	if goal.Trigger == storepb.QualityAgentGoalTrigger_QUALITY_AGENT_GOAL_TRIGGER_EXECUTION_FAILED &&
		execution.Status != storepb.QualityStatus_QUALITY_STATUS_FAIL {
		return Outcome{
			Phase:         storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED,
			BlockedCode:   "needs-human",
			BlockedReason: "the triggering execution did not fail",
			Summary:       "blocked: the execution is not a failure",
		}, nil
	}
	summary := fmt.Sprintf("execution %s finished %s with %d failed rows",
		goal.TriggerRef, execution.Status.String(), execution.FailedRows)
	if execution.ErrorCode != "" {
		summary = fmt.Sprintf("execution %s ended %s: %s", goal.TriggerRef, execution.ErrorCode, execution.ErrorMessage)
	}
	input := &store.CreateQualityInvestigationInput{
		TargetGUID:       goal.TargetGUID,
		TargetType:       storepb.MetaType(goal.TargetType),
		Trigger:          storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION,
		TriggerReference: goal.TriggerRef,
		Status:           storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		Verdict:          storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_INCONCLUSIVE,
		Summary:          summary,
		Evidence: &storepb.QualityInvestigationEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{
			{Kind: "EXECUTION", Reference: goal.TriggerRef},
		}},
		RequestFingerprint: "event-investigation:" + goal.TriggerRef,
		CreatedBy:          ptrInt32(goal.AgentPrincipalID),
	}
	if err := e.recordInvestigation(ctx, goal, input); err != nil {
		return Outcome{}, err
	}
	return Outcome{
		Phase:      storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_COMPLETE,
		Checkpoint: &storepb.QualityAgentGoalCheckpoint{Phase: "done", Summary: "investigated the event"},
		Summary:    "investigated the event",
	}, nil
}

// recordInvestigation is the effect sandwich around one investigation: the intent is
// journaled first, the store write is idempotent by fingerprint, and the result is
// journaled after. The two steps carry different keys so neither replaces the other.
func (e *Engine) recordInvestigation(ctx context.Context, goal *store.QualityAgentGoalMessage, input *store.CreateQualityInvestigationInput) error {
	key := "investigation:" + input.RequestFingerprint
	intentPayload := &storepb.QualityAgentGoalStepPayload{
		Tool: "record_investigation", Summary: "record an investigation", Resource: input.RequestFingerprint,
	}
	if err := e.journal(ctx, goal, storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_EFFECT_INTENT, intentPayload, key+":intent"); err != nil {
		return err
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return errors.Wrap(err, "failed to generate the investigation id")
	}
	input.ResourceID = resourceID
	message, _, err := e.store.CreateQualityInvestigation(ctx, input)
	if err != nil {
		return errors.Wrap(err, "failed to record the investigation")
	}
	resultPayload := &storepb.QualityAgentGoalStepPayload{
		Tool: "record_investigation", Summary: "investigation recorded", Resource: message.ResourceID,
	}
	return e.journal(ctx, goal, storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_EFFECT_RESULT, resultPayload, key+":result")
}

// recordAssetFact records what the survey observed as one asset fact. The statement is
// looked up first, so a turn that failed after writing the fact does not write a second
// one on the next wake.
func (e *Engine) recordAssetFact(ctx context.Context, goal *store.QualityAgentGoalMessage, gaps []string) error {
	statement := fmt.Sprintf("the plan declares %d uncovered propert(ies): %s", len(gaps), strings.Join(gaps, ", "))
	existing, err := e.store.ListQualityMemories(ctx, &store.FindQualityMemoryMessage{Subject: &goal.TargetGUID})
	if err != nil {
		return errors.Wrap(err, "failed to read the target's memories")
	}
	for _, memory := range existing {
		if memory.Subject == goal.TargetGUID && memory.Statement == statement &&
			memory.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED {
			return nil
		}
	}
	key := "memory:" + goal.TargetGUID + ":" + strconv.Itoa(len(gaps))
	intentPayload := &storepb.QualityAgentGoalStepPayload{
		Tool: "record_asset_fact", Summary: "record the coverage observation",
	}
	if err := e.journal(ctx, goal, storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_EFFECT_INTENT, intentPayload, key+":intent"); err != nil {
		return err
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return errors.Wrap(err, "failed to generate the memory id")
	}
	message, err := e.store.CreateQualityMemory(ctx, &store.CreateQualityMemoryInput{
		ResourceID: resourceID,
		Kind:       storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT,
		Subject:    goal.TargetGUID,
		Statement:  statement,
		Source:     storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		Status:     storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED,
		Evidence: &storepb.QualityMemoryEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{
			{Kind: "PLAN", Reference: goal.TargetGUID},
		}},
		CreatedBy: ptrInt32(goal.AgentPrincipalID),
	})
	if err != nil {
		return errors.Wrap(err, "failed to record the asset fact")
	}
	resultPayload := &storepb.QualityAgentGoalStepPayload{
		Tool: "record_asset_fact", Summary: "asset fact recorded", Resource: message.ResourceID,
	}
	return e.journal(ctx, goal, storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_EFFECT_RESULT, resultPayload, key+":result")
}

// journal appends one step, using the effect key as the step's idempotency key.
func (e *Engine) journal(ctx context.Context, goal *store.QualityAgentGoalMessage, kind storepb.QualityAgentGoalStepKind, payload *storepb.QualityAgentGoalStepPayload, key string) error {
	if _, err := e.store.AppendQualityAgentGoalStep(ctx, &store.QualityAgentGoalStepMessage{
		GoalID: goal.ID, WorkspaceID: goal.WorkspaceID, Kind: kind, Payload: payload, IdempotencyKey: key,
	}); err != nil {
		return errors.Wrap(err, "failed to journal the goal effect")
	}
	return nil
}

// executionIDFromTrigger parses a quality execution reference, either its bare
// resource id ("qualityExecutions/7") or its full name
// ("workspaces/{workspace}/qualityExecutions/7"). A non-empty reason means the
// reference is not an execution, which the caller parks as needs-human.
func executionIDFromTrigger(reference string) (int64, string) {
	const segment = "qualityExecutions/"
	index := strings.LastIndex(reference, segment)
	if index == -1 {
		return 0, fmt.Sprintf("the trigger reference %q is not a quality execution", reference)
	}
	id, err := strconv.ParseInt(reference[index+len(segment):], 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Sprintf("the trigger reference %q is not a quality execution", reference)
	}
	return id, ""
}

// ruleShapes is the plan derivation's view of the live rules: enabled rules only, with
// the column each one reads.
func ruleShapes(rules []*store.QualityRuleMessage) []dataquality.RuleShape {
	shapes := make([]dataquality.RuleShape, 0, len(rules))
	for _, rule := range rules {
		if rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			continue
		}
		shapes = append(shapes, dataquality.RuleShape{
			Type: rule.RuleType, Column: dataquality.ConfigColumn(rule.Config), Enabled: true,
		})
	}
	return shapes
}

// gapLabel renders one uncovered requirement.
func gapLabel(requirement dataquality.PlanRequirement) string {
	label := propertyLabel(requirement.Property)
	if requirement.Column != "" {
		label += "(" + requirement.Column + ")"
	}
	return label
}

func propertyLabel(property dataquality.CoverageProperty) string {
	switch property {
	case dataquality.CoverageVolume:
		return "volume"
	case dataquality.CoverageFreshness:
		return "freshness"
	case dataquality.CoverageUniqueness:
		return "uniqueness"
	case dataquality.CoverageSchemaStability:
		return "schema_stability"
	case dataquality.CoverageNullRate:
		return "null_rate"
	case dataquality.CoverageAcceptedValues:
		return "accepted_values"
	case dataquality.CoverageFormat:
		return "format"
	case dataquality.CoverageRange:
		return "range"
	default:
		return "unknown"
	}
}

// doneSet reads the checkpoint's completed labels.
func doneSet(checkpoint *storepb.QualityAgentGoalCheckpoint) map[string]bool {
	done := make(map[string]bool, len(checkpoint.GetDone()))
	for _, label := range checkpoint.GetDone() {
		done[label] = true
	}
	return done
}

func ptrTime(value time.Time) *time.Time { return &value }

func ptrInt32(value int32) *int32 { return &value }
