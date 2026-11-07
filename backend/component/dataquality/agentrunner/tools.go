package agentrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// evidence is the bounded, reference-only context a model wake reads. It never
// contains sample rows: the tools return identifiers, outcomes and counts.
type evidence struct {
	TargetGUID  string
	TargetType  string
	PlanState   string
	Criticality string
	Gaps        []string
	Executions  []string
	Memories    []string
	RuleCount   int
}

// gatherEvidence assembles the target's plan, recent executions and confirmed memories.
func (e *Engine) gatherEvidence(ctx context.Context, session *store.QualityAgentSessionMessage) (*evidence, error) {
	result := &evidence{
		TargetGUID: session.TargetGUID,
		TargetType: storepb.MetaType(session.TargetType).String(),
	}
	rules, err := e.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &session.TargetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's rules")
	}
	result.RuleCount = len(rules)
	contract, err := e.store.GetQualityContract(ctx, session.TargetGUID, storepb.MetaType(session.TargetType))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's contract")
	}
	var contractSettings *storepb.QualityContractSettings
	if contract != nil {
		contractSettings = contract.Settings
	}
	plan := dataquality.DerivePlan(contract != nil, dataquality.ActiveIntent(contractSettings.GetIntent(), e.now()), ruleShapes(rules))
	result.PlanState = planStateLabel(plan.State)
	result.Criticality = criticalityLabel(plan.Criticality)
	for _, requirement := range plan.Requirements {
		if requirement.Satisfied || requirement.Exempted {
			continue
		}
		result.Gaps = append(result.Gaps, gapLabel(requirement))
	}
	limit := 5
	executions, err := e.store.ListQualityExecutions(ctx, &store.FindQualityExecutionMessage{TargetGUID: &session.TargetGUID, Limit: &limit})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's executions")
	}
	for _, execution := range executions {
		result.Executions = append(result.Executions, fmt.Sprintf("%s %s (%d failed rows)",
			executionIDName(execution.ID), execution.Status, execution.FailedRows))
	}
	memoryLimit := 20
	memories, err := e.store.ListQualityMemories(ctx, &store.FindQualityMemoryMessage{Subject: &session.TargetGUID, Limit: &memoryLimit})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's memories")
	}
	for _, memory := range memories {
		if memory.Status != storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED {
			continue
		}
		result.Memories = append(result.Memories, memory.Statement)
	}
	return result, nil
}

// toolExecutor runs one tool call for a model wake and journals nothing itself: the
// wake recorder persists the assistant message, the tool result and the engine notes
// from the event stream.
func (e *Engine) toolExecutor(
	ctx context.Context,
	session *store.QualityAgentSessionMessage,
	isToolResult map[string]bool,
	blocked *bool,
	blockedCode, blockedWhy *string,
	nextWake **time.Time,
	scheduleReason *string,
) llm.ToolExecutor {
	return func(call llm.ToolCall) ([]llm.ToolResult, error) {
		result, err := e.executeTool(ctx, session, call.GetName(), call.Function.Arguments,
			blocked, blockedCode, blockedWhy, nextWake, scheduleReason)
		if err != nil {
			return nil, err
		}
		if len(result) > toolResultLimit {
			isToolResult[call.ID] = true
			result = truncateBytes(result, toolResultLimit)
		}
		return []llm.ToolResult{{ToolCallID: call.ID, Content: result}}, nil
	}
}

// executeTool dispatches one model tool call.
func (e *Engine) executeTool(
	ctx context.Context,
	session *store.QualityAgentSessionMessage,
	name, arguments string,
	blocked *bool,
	blockedCode, blockedWhy *string,
	nextWake **time.Time,
	scheduleReason *string,
) (string, error) {
	switch name {
	case "get_target_summary":
		evidence, err := e.gatherEvidence(ctx, session)
		if err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{
			"target":      evidence.TargetGUID,
			"type":        evidence.TargetType,
			"planState":   evidence.PlanState,
			"criticality": evidence.Criticality,
			"ruleCount":   evidence.RuleCount,
			"gaps":        emptyIfNil(evidence.Gaps),
		})
	case "list_recent_executions":
		evidence, err := e.gatherEvidence(ctx, session)
		if err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{"executions": emptyIfNil(evidence.Executions)})
	case "get_memory":
		evidence, err := e.gatherEvidence(ctx, session)
		if err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{"memories": emptyIfNil(evidence.Memories)})
	case "record_investigation":
		var input struct {
			Summary   string `json:"summary"`
			Verdict   string `json:"verdict"`
			Reference string `json:"reference"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid record_investigation arguments")
		}
		if strings.TrimSpace(input.Summary) == "" {
			return "", errors.New("record_investigation needs a summary")
		}
		investigation := &store.CreateQualityInvestigationInput{
			TargetGUID:         session.TargetGUID,
			TargetType:         storepb.MetaType(session.TargetType),
			Trigger:            storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_MANUAL,
			TriggerReference:   session.ResourceID,
			Status:             storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
			Verdict:            verdictFromString(input.Verdict),
			Summary:            input.Summary,
			Evidence:           &storepb.QualityInvestigationEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{{Kind: "SESSION", Reference: session.ResourceID}}},
			RequestFingerprint: investigationFingerprint(session.TargetGUID, input.Summary, input.Reference),
			CreatedBy:          ptrInt32(session.AgentPrincipalID),
		}
		if input.Reference != "" {
			investigation.Trigger = storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION
			investigation.TriggerReference = input.Reference
			investigation.Evidence.Evidence = append(investigation.Evidence.Evidence,
				&storepb.QualityInvestigationEvidence{Kind: "EXECUTION", Reference: input.Reference})
		}
		message, err := e.recordInvestigation(ctx, investigation)
		if err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{"recorded": true, "investigation": message.ResourceID})
	case "record_asset_fact":
		var input struct {
			Statement string `json:"statement"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid record_asset_fact arguments")
		}
		if strings.TrimSpace(input.Statement) == "" {
			return "", errors.New("record_asset_fact needs a statement")
		}
		if err := e.recordAssetFact(ctx, session, strings.TrimSpace(input.Statement)); err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{"recorded": true})
	case "record_decision":
		var input struct {
			Statement string `json:"statement"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid record_decision arguments")
		}
		statement := strings.TrimSpace(input.Statement)
		if statement == "" {
			return "", errors.New("record_decision needs a statement")
		}
		if err := e.recordDecision(ctx, session, truncateRunes(statement, maxDecisionStatement)); err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{"recorded": true})
	case "block_session":
		var input struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid block_session arguments")
		}
		if input.Code == "" || input.Reason == "" {
			return "", errors.New("block_session needs a code and a reason")
		}
		*blocked = true
		*blockedCode = input.Code
		*blockedWhy = input.Reason
		return marshalJSON(map[string]any{"blocked": true, "threshold": blockedThreshold})
	case "schedule_wake":
		var input struct {
			AfterMinutes int32  `json:"after_minutes"`
			Reason       string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid schedule_wake arguments")
		}
		minutes := int(input.AfterMinutes)
		if minutes < 1 {
			minutes = 1
		}
		if minutes > 10080 {
			minutes = 10080
		}
		*nextWake = ptrTime(e.now().Add(time.Duration(minutes) * time.Minute))
		*scheduleReason = truncateRunes(input.Reason, 200)
		return marshalJSON(map[string]any{"scheduled": true, "after_minutes": minutes})
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// verdictFromString maps the model's verdict word to the stored enum.
func verdictFromString(value string) storepb.QualityInvestigationVerdict {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "root_cause":
		return storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE
	case "contributing_factor":
		return storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_CONTRIBUTING_FACTOR
	default:
		return storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_INCONCLUSIVE
	}
}

// recordInvestigation writes one investigation; the store dedupes by fingerprint.
func (e *Engine) recordInvestigation(ctx context.Context, input *store.CreateQualityInvestigationInput) (*store.QualityInvestigationMessage, error) {
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the investigation id")
	}
	input.ResourceID = resourceID
	message, _, err := e.store.CreateQualityInvestigation(ctx, input)
	if err != nil {
		return nil, errors.Wrap(err, "failed to record the investigation")
	}
	return message, nil
}

// recordAssetFact writes one model-observed fact unless a confirmed one already says
// the same thing.
func (e *Engine) recordAssetFact(ctx context.Context, session *store.QualityAgentSessionMessage, statement string) error {
	existing, err := e.store.ListQualityMemories(ctx, &store.FindQualityMemoryMessage{Subject: &session.TargetGUID})
	if err != nil {
		return errors.Wrap(err, "failed to read the target's memories")
	}
	for _, memory := range existing {
		if memory.Statement == statement &&
			memory.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED {
			return nil
		}
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return errors.Wrap(err, "failed to generate the memory id")
	}
	_, err = e.store.CreateQualityMemory(ctx, &store.CreateQualityMemoryInput{
		ResourceID: resourceID,
		Kind:       storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT,
		Subject:    session.TargetGUID,
		Statement:  statement,
		Source:     storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		Status:     storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED,
		Evidence: &storepb.QualityMemoryEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{
			{Kind: "MODEL", Reference: session.ResourceID},
		}},
		CreatedBy: ptrInt32(session.AgentPrincipalID),
	})
	if err != nil {
		return errors.Wrap(err, "failed to record the asset fact")
	}
	return nil
}

// recordDecision writes the model's conclusion as a PROPOSED decision memory. It never
// confirms itself: a person is what turns it into knowledge. A critical asset needs a
// person (the A1-6 amplifier), so the model must block instead.
func (e *Engine) recordDecision(ctx context.Context, session *store.QualityAgentSessionMessage, statement string) error {
	criticality := e.planCriticality(ctx, session)
	decision := dataquality.DecideAction(dataquality.PolicyInput{
		Action:          dataquality.ActionRecordDecisionWhy,
		Actor:           dataquality.ActorAgent,
		Criticality:     criticality,
		AutonomyEnabled: true,
		AgentEnabled:    true,
	})
	if !decision.Allowed || decision.RequiresApproval {
		return errors.New("a decision about a critical asset needs a person; record the conclusion with block_session instead")
	}
	kind := storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_DECISION
	existing, err := e.store.ListQualityMemories(ctx, &store.FindQualityMemoryMessage{Subject: &session.TargetGUID, Kind: &kind})
	if err != nil {
		return errors.Wrap(err, "failed to read the target's memories")
	}
	for _, memory := range existing {
		if memory.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_REJECTED {
			continue
		}
		if memory.ExpiresAt != nil && !memory.ExpiresAt.After(e.now()) {
			continue
		}
		if memory.Statement == statement {
			return nil
		}
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return errors.Wrap(err, "failed to generate the memory id")
	}
	_, err = e.store.CreateQualityMemory(ctx, &store.CreateQualityMemoryInput{
		ResourceID: resourceID,
		Kind:       storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_DECISION,
		Subject:    session.TargetGUID,
		Statement:  statement,
		Source:     storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT,
		Status:     storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_PROPOSED,
		Evidence: &storepb.QualityMemoryEvidenceList{Evidence: []*storepb.QualityInvestigationEvidence{
			{Kind: sessionEvidenceKind, Reference: session.ResourceID},
		}},
		CreatedBy: ptrInt32(session.AgentPrincipalID),
	})
	if err != nil {
		return errors.Wrap(err, "failed to record the decision memory")
	}
	return nil
}

// planCriticality derives the target's plan criticality for the policy gate.
func (e *Engine) planCriticality(ctx context.Context, session *store.QualityAgentSessionMessage) dataquality.AssetCriticality {
	rules, err := e.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &session.TargetGUID})
	if err != nil {
		return 0
	}
	contract, err := e.store.GetQualityContract(ctx, session.TargetGUID, storepb.MetaType(session.TargetType))
	if err != nil {
		return 0
	}
	var settings *storepb.QualityContractSettings
	if contract != nil {
		settings = contract.Settings
	}
	plan := dataquality.DerivePlan(contract != nil, dataquality.ActiveIntent(settings.GetIntent(), e.now()), ruleShapes(rules))
	return plan.Criticality
}

// emptyIfNil turns a nil slice into an empty one so JSON carries [] not null.
func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
