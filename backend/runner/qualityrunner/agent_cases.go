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
			if openCase != nil && awaitingVerification(openCase.State) {
				// A repair was applied: this run is its verification. The transition is decided
				// deterministically and written as a compare-and-set, so two runners cannot both
				// advance the case.
				verification := caseResolution(openCase, assessment.Rule, assessment.Execution, diagnosis)
				state, nextActionAt, apply := caseTransition(verification, now)
				if !apply {
					continue
				}
				resolution := ""
				if state == store.QualityAgentCaseStateResolved ||
					state == store.QualityAgentCaseStateFrozen ||
					state == store.QualityAgentCaseStateEscalated {
					resolution = verification.Reason
				}
				if _, applied, updateErr := r.stores.UpdateQualityAgentCaseState(ctx, openCase.ResourceID,
					openCase.State, state, verification.Attempts, nextActionAt, resolution); updateErr != nil {
					slog.Warn("failed to advance a quality agent case",
						slog.String("case", openCase.ResourceID), log.WithError(updateErr))
				} else if applied {
					resolved++
					r.metrics.ObserveAgentCaseTransition(state.Name())
					slog.Info("advanced a quality agent case",
						slog.String("case", openCase.ResourceID),
						slog.String("from", openCase.State.Name()),
						slog.String("to", state.Name()),
						slog.String("outcome", string(verification.Outcome)))
					if state == store.QualityAgentCaseStateFrozen {
						// A frozen case is the agent saying "I cannot fix this": nobody else can see
						// that from the table, so it has to reach the owner.
						r.escalateFrozenCase(ctx, openCase, assessment.Rule)
					}
					if state == store.QualityAgentCaseStateEscalated {
						// A deterministic verifier can escalate when a repair's check was paused.
						// The case is still a decision a person owes, so it needs the same lead
						// request a model escalation would raise.
						r.escalatePausedCase(ctx, openCase, assessment.Rule)
					}
					if state.Terminal() {
						r.closeCaseRequests(ctx, agent.ID, openCase.ResourceID)
					}
				}
				continue
			}
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
					r.metrics.ObserveAgentCaseTransition(store.QualityAgentCaseStateResolved.Name())
					r.closeCaseRequests(ctx, agent.ID, openCase.ResourceID)
				}
				continue
			}
			if dataquality.IsFactNotWork(diagnosis.Class) {
				// The class describes the target, not the check: an empty table is an answer, not a
				// repair. Closing a case that already exists is the same honesty one step later —
				// otherwise the item would sit here for ever, because no verb can settle it.
				if openCase == nil {
					continue
				}
				if _, applied, closeErr := r.stores.UpdateQualityAgentCaseState(ctx, openCase.ResourceID,
					openCase.State, store.QualityAgentCaseStateResolved, openCase.Attempts, nil,
					diagnosis.Summary); closeErr != nil {
					slog.Warn("failed to resolve a case about a target holding no rows",
						slog.String("case", openCase.ResourceID), log.WithError(closeErr))
				} else if applied {
					resolved++
					r.metrics.ObserveAgentCaseTransition(store.QualityAgentCaseStateResolved.Name())
					r.closeCaseRequests(ctx, agent.ID, openCase.ResourceID)
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
				Summary:      caseSummary(assessment.Rule, diagnosis),
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
				r.metrics.ObserveAgentCaseOpened(string(diagnosis.Class))
			}
		}
	}
	if opened > 0 || resolved > 0 {
		slog.Info("settled quality agent cases",
			slog.String("target", targetGUID), slog.Int("opened", opened), slog.Int("resolved", resolved))
	}
}

// awaitingVerification reports whether a case has a repair pending its run. Only these cases
// are verified: a diagnosed case has spent no attempt, so a still-failing run must not burn
// one before the agent has actually changed anything.
func awaitingVerification(state store.QualityAgentCaseState) bool {
	return state == store.QualityAgentCaseStateActed || state == store.QualityAgentCaseStateAwaiting
}

// caseResolution classifies what one batch execution means for an open case. It is pure so the
// transition is testable without a database; the runner writes only what it returns.
func caseResolution(open *store.QualityAgentCaseMessage, rule *store.QualityRuleMessage, execution *store.QualityExecutionMessage, diagnosis *dataquality.Diagnosis) dataquality.Verification {
	// The case's own evidence is not a verification run: re-processing the batch that opened it
	// (a retried pass) must not spend an attempt.
	if open.ExecutionID != nil && execution != nil && execution.ID == *open.ExecutionID {
		return dataquality.Verification{Outcome: dataquality.VerifyRearm, Attempts: open.Attempts,
			Class: dataquality.FailureClass(open.FailureClass), Reason: "the run is the case's own evidence"}
	}
	newClass := dataquality.FailureClass("")
	if diagnosis != nil && diagnosis.Class != dataquality.ClassDataFailure {
		newClass = diagnosis.Class
	}
	ruleState := storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
	if rule != nil {
		ruleState = rule.State
	}
	return dataquality.VerifyCase(dataquality.VerificationInput{
		State:       open.State,
		Class:       dataquality.FailureClass(open.FailureClass),
		Attempts:    open.Attempts,
		MaxAttempts: open.MaxAttempts,
		Execution:   execution,
		NewClass:    newClass,
		RuleState:   ruleState,
	})
}

// caseTransition maps a verification onto the row it writes. apply is false when the case must
// stay where it is (a run that has not finished), which is what keeps a queued execution from
// spending an attempt.
func caseTransition(verification dataquality.Verification, now time.Time) (store.QualityAgentCaseState, *time.Time, bool) {
	switch verification.Outcome {
	case dataquality.VerifyResolved:
		return store.QualityAgentCaseStateResolved, nil, true
	case dataquality.VerifyFrozen:
		return store.QualityAgentCaseStateFrozen, nil, true
	case dataquality.VerifyEscalated:
		return store.QualityAgentCaseStateEscalated, nil, true
	case dataquality.VerifyRetry, dataquality.VerifyReopen:
		// Look again soon, but not so soon that the three-second session tick spins on a case
		// that just failed.
		next := now.Add(time.Minute)
		return store.QualityAgentCaseStateDiagnosed, &next, true
	case dataquality.VerifyRearm:
		return store.QualityAgentCaseStateUnspecified, nil, false
	default:
		return store.QualityAgentCaseStateUnspecified, nil, false
	}
}

// caseSummary names the check the case is about before it explains the failure. The wake prompt
// renders this sentence and the model resolves the check by it; a summary without the name would
// leave several open cases indistinguishable, and inspect_check would fall back to "the newest
// failing check" — the wrong one as soon as a target has more than one.
func caseSummary(rule *store.QualityRuleMessage, diagnosis *dataquality.Diagnosis) string {
	if rule == nil || diagnosis == nil {
		return ""
	}
	title := rule.Title
	if title == "" {
		title = "a check"
	}
	return fmt.Sprintf("check %q (qualityRules/%s): %s", title, rule.ResourceID, diagnosis.Summary)
}

// closeCaseRequests closes the requests (and any drained asks) of a terminal case.
// It is best effort: the case row is the durable record.
func (r *Runner) closeCaseRequests(ctx context.Context, agentID int64, caseResourceID string) {
	now := time.Now().UTC()
	if _, err := r.stores.ResolveQualityAgentRequestsForCase(ctx, agentID, caseResourceID, now); err != nil {
		slog.Warn("failed to close a case's requests", slog.String("case", caseResourceID), log.WithError(err))
	}
	if _, err := r.stores.ResolveDrainedQualityAgentAsks(ctx, agentID, now); err != nil {
		slog.Warn("failed to close drained asks", slog.Int64("agent", agentID), log.WithError(err))
	}
}
