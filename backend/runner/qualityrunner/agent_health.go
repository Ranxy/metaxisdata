package qualityrunner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// healthAssessment is one check this batch ran, with the rule and execution it is
// about. It is what the pure health classification needs, and nothing more.
type healthAssessment struct {
	Rule      *store.QualityRuleMessage
	Execution *store.QualityExecutionMessage
}

// healthFinding is one check worth telling the agent about: it ran and produced no
// verdict. The class says why, the summary says it in a sentence, and the key makes
// one check's class per day one event.
type healthFinding struct {
	RuleID  int64
	Rule    string
	Class   dataquality.HealthClass
	Summary string
	Key     string
}

// healthFindings decides which checks a batch should report as "ran, but did not
// conclude". Only NOT_EVALUATED reaches the health path: a FAIL or an ERROR already
// has its own execution event, and repeating it as "no verdict" would double the wake
// for the same fact. SKIPPED is operational and transient, so it stays silent too.
func healthFindings(assessments []healthAssessment, now time.Time) []healthFinding {
	findings := make([]healthFinding, 0, len(assessments))
	day := now.UTC().Format("2006-01-02")
	for _, assessment := range assessments {
		if assessment.Rule == nil || assessment.Execution == nil {
			continue
		}
		if assessment.Execution.Status != storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED {
			continue
		}
		if assessment.Rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			continue
		}
		health := dataquality.DiagnoseCheckHealth(dataquality.HealthInput{
			Rule:       assessment.Rule,
			Executions: []*store.QualityExecutionMessage{assessment.Execution},
			Now:        now,
		})
		switch health.Class {
		case dataquality.HealthNeverJudged:
		case dataquality.HealthNoVerdictEmpty:
			// An empty target is not a defect the agent can act on, and a daily event about it is a
			// daily wake for nothing. It is a fact about the target, so it stays in the dossier and out
			// of the inbox.
			continue
		default:
			continue
		}
		title := assessment.Rule.Title
		if title == "" {
			title = "a check"
		}
		findings = append(findings, healthFinding{
			RuleID: assessment.Rule.ID,
			Rule:   "qualityRules/" + assessment.Rule.ResourceID,
			Class:  health.Class,
			Summary: fmt.Sprintf("[event] Check %q ran and produced no verdict (%s): %s. "+
				"Decide whether the check or the target is the problem; do not guess from the counts alone.",
				title, health.Class, health.Reason),
			Key: fmt.Sprintf("health:%s:%d:%s", health.Class, assessment.Rule.ID, day),
		})
	}
	return findings
}

// healthAssessments pairs each written execution with the rule it ran. A batch
// carries one execution per check, so the batch itself is the window a post-batch
// health read sees.
func healthAssessments(executions []*ruleExecution) []healthAssessment {
	assessments := make([]healthAssessment, 0, len(executions))
	for _, execution := range executions {
		if execution == nil || execution.head == nil || execution.result == nil {
			continue
		}
		assessments = append(assessments, healthAssessment{Rule: execution.head, Execution: execution.result})
	}
	return assessments
}

// intakeHealthEvents tells each session covering the target that a check ran without
// concluding. It is the perception the failure path cannot provide: an execution that
// is NOT_EVALUATED never becomes a FAIL or an ERROR, so no execution event exists for
// it, and the agent used to learn about it only by accident.
func (r *Runner) intakeHealthEvents(ctx context.Context, workspaceID string, assessments []healthAssessment) {
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
	now := time.Now().UTC()
	findings := healthFindings(assessments, now)
	if len(findings) == 0 {
		return
	}
	sessions, err := r.stores.ListQualityAgentSessionsByTarget(ctx, workspaceID, targetGUID)
	if err != nil {
		slog.Warn("failed to read the target's agent sessions for a health event",
			slog.String("target", targetGUID), log.WithError(err))
		return
	}
	for _, session := range sessions {
		for _, finding := range findings {
			if r.healthFindingHasCase(ctx, session.AgentID, targetGUID, finding.RuleID) {
				// The case carries this check, and the wake prompt renders open cases: an event would
				// repeat the same work as a message the model has to read and discard.
				continue
			}
			r.appendSessionEvent(ctx, session, workspaceID, agentEvent{
				Content:   finding.Summary,
				Reference: finding.Rule,
				Key:       finding.Key,
			}, now)
		}
	}
}

// healthFindingHasCase reports whether an open case already carries this check for this agent. A read
// failure answers "no": the event is best effort, and a missing case read must not silence the one
// signal that check has.
func (r *Runner) healthFindingHasCase(ctx context.Context, agentID int64, targetGUID string, ruleID int64) bool {
	rule := ruleID
	openCase, err := r.stores.GetOpenQualityAgentCase(ctx, agentID,
		store.QualityAgentCaseSubjectKey(targetGUID, &rule, store.QualityAgentCaseKindExecution))
	if err != nil {
		slog.Warn("failed to read a case for a health event",
			slog.Int64("rule", ruleID), log.WithError(err))
		return false
	}
	return openCase != nil
}
