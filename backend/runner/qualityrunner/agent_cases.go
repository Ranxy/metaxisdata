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
	opened := 0
	for _, session := range sessions {
		agent, agentErr := r.stores.GetQualityAgentByID(ctx, session.AgentID)
		if agentErr != nil || agent == nil || agent.State != store.QualityAgentStateActive {
			continue
		}
		for _, assessment := range assessments {
			diagnosis := dataquality.DiagnoseExecution(dataquality.DiagnosisInput{
				Execution: assessment.Execution,
				Rule:      assessment.Rule,
				Now:       now,
			})
			if diagnosis == nil || diagnosis.Class == dataquality.ClassDataFailure {
				continue
			}
			_, created, createErr := r.stores.CreateQualityAgentCase(ctx, &store.CreateQualityAgentCaseInput{
				WorkspaceID:  workspaceID,
				AgentID:      agent.ID,
				TargetGUID:   assessment.Execution.TargetGUID,
				TargetType:   assessment.Execution.TargetType,
				RuleID:       &assessment.Rule.ID,
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
	if opened > 0 {
		slog.Info("opened quality agent cases", slog.String("target", targetGUID), slog.Int("count", opened))
	}
}
