package qualityrunner

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// intakeAgentCases opens a durable case for every check that cannot conclude. A FAIL is
// deliberately excluded: it already has an issue and a notification, because a failing
// check is a conclusion about the data rather than work on the check. The case is what the
// agent resumes from after a restart, a compaction or a model change, so it is opened by
// the server and never by the model.
func (r *Runner) intakeAgentCases(ctx context.Context, workspaceID string, assessments []healthAssessment) {
	if len(assessments) == 0 || !r.currentSettings().AgentRunnerEnabled {
		return
	}
	targetGUID := ""
	for _, assessment := range assessments {
		if assessment.Execution != nil {
			targetGUID = assessment.Execution.TargetGUID
			break
		}
	}
	if targetGUID == "" {
		return
	}
	sessions, err := r.stores.ListQualityAgentSessionsByTarget(ctx, workspaceID, targetGUID)
	if err != nil {
		slog.Warn("failed to read the target's agent sessions for a case",
			slog.String("target", targetGUID), log.WithError(err))
		return
	}
	if len(sessions) == 0 {
		return
	}
	now := time.Now().UTC()
	opened, resolved := 0, 0
	for _, session := range sessions {
		agent, agentErr := r.stores.GetQualityAgentByID(ctx, session.AgentID)
		if agentErr != nil || agent == nil || agent.State != store.QualityAgentStateActive {
			continue
		}
		// One read for the agent's whole open set: a target may hold a hundred checks, and a
		// query per check would make this pass the most expensive thing in the batch.
		openCases, listErr := r.stores.ListOpenQualityAgentCases(ctx, agent.ID, 200)
		if listErr != nil {
			slog.Warn("failed to read the agent's open cases", slog.Int64("agent", agent.ID), log.WithError(listErr))
			continue
		}
		bySubject := make(map[string]*store.QualityAgentCaseMessage, len(openCases))
		for _, openCase := range openCases {
			bySubject[openCase.SubjectKey] = openCase
		}
		for _, assessment := range assessments {
			ruleID := assessment.Rule.ID
			subjectKey := store.QualityAgentCaseSubjectKey(assessment.Execution.TargetGUID, &ruleID, store.QualityAgentCaseKindExecution)
			openCase := bySubject[subjectKey]
			diagnosis := dataquality.DiagnoseExecution(dataquality.DiagnosisInput{
				Execution: assessment.Execution,
				Rule:      assessment.Rule,
				Now:       now,
			})
			if diagnosis == nil || diagnosis.Class == dataquality.ClassDataFailure {
				// The check concluded, so the question the case was asking is answered. A FAIL
				// is a conclusion too: the issue ledger owns it from here.
				if openCase == nil {
					continue
				}
				if _, applied, closeErr := r.stores.UpdateQualityAgentCaseState(ctx, openCase.ResourceID,
					openCase.State, store.QualityAgentCaseStateResolved, openCase.Attempts, nil,
					"the check produced a verdict"); closeErr != nil {
					slog.Warn("failed to resolve a quality agent case",
						slog.String("case", openCase.ResourceID), log.WithError(closeErr))
				} else if applied {
					resolved++
				}
				continue
			}
			if openCase != nil {
				// The problem is already claimed; re-diagnosing it is a later wake's job.
				continue
			}
			_, created, createErr := r.stores.CreateQualityAgentCase(ctx, &store.CreateQualityAgentCaseInput{
				WorkspaceID:  workspaceID,
				AgentID:      agent.ID,
				TargetGUID:   assessment.Execution.TargetGUID,
				TargetType:   assessment.Execution.TargetType,
				RuleID:       &ruleID,
				Instance:     assessment.Execution.InstanceResourceID,
				Kind:         store.QualityAgentCaseKindExecution,
				FailureClass: string(diagnosis.Class),
				Field:        diagnosis.Field,
				State:        store.QualityAgentCaseStateDiagnosed,
				ExecutionID:  &assessment.Execution.ID,
				Summary:      diagnosis.Summary,
				NextActionAt: &now,
			})
			if createErr != nil {
				slog.Warn("failed to open a quality agent case",
					slog.String("target", targetGUID),
					slog.Int64("execution", assessment.Execution.ID),
					log.WithError(createErr))
				continue
			}
			if created {
				opened++
			}
		}
	}
	if opened > 0 || resolved > 0 {
		slog.Info("settled quality agent cases",
			slog.String("target", targetGUID), slog.Int("opened", opened), slog.Int("resolved", resolved))
	}
}
