package dataquality

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// CaseState is where one agent work item stands. A case is durable state: it survives a
// restart, a compaction and a model change, which is what "the agent continues where it
// left off" actually requires.
type CaseState string

const (
	// CaseOpen is "something is wrong and nobody has looked yet".
	CaseOpen CaseState = "OPEN"
	// CaseDiagnosed carries a class and the actions that class permits.
	CaseDiagnosed CaseState = "DIAGNOSED"
	// CaseActed means a repair was applied and a verification run is expected.
	CaseActed CaseState = "ACTED"
	// CaseAwaitingVerification means the verification run exists but has not finished.
	CaseAwaitingVerification CaseState = "AWAITING_VERIFICATION"
	// CaseResolved means the check concluded, or the case turned out to be "no verdict
	// possible" with no defect in the check.
	CaseResolved CaseState = "RESOLVED"
	// CaseFrozen means repeated attempts made no progress. The rule keeps running; the
	// agent stops touching it and a person decides.
	CaseFrozen CaseState = "FROZEN"
	// CaseEscalated means a person has been asked to decide.
	CaseEscalated CaseState = "ESCALATED"
)

// Terminal reports whether the state releases its subject. A subject with a terminal
// case can open a new one; without this, one frozen case would make the check
// permanently invisible to the agent.
func (s CaseState) Terminal() bool {
	switch s {
	case CaseResolved, CaseFrozen, CaseEscalated:
		return true
	case CaseOpen, CaseDiagnosed, CaseActed, CaseAwaitingVerification:
		return false
	default:
		return false
	}
}

// defaultCaseMaxAttempts is how many same-class repairs a case tolerates before it
// freezes: one repair and one retry, then a person.
const defaultCaseMaxAttempts = 2

// VerificationOutcome is what one verification run means for the case.
type VerificationOutcome string

const (
	// VerifyResolved closes the case: the check concluded, or its lack of a verdict is a
	// fact about the target rather than a defect.
	VerifyResolved VerificationOutcome = "RESOLVED"
	// VerifyRetry keeps the case in DIAGNOSED with one more attempt spent.
	VerifyRetry VerificationOutcome = "RETRY"
	// VerifyReopen keeps the case but replaces its class: the failure changed.
	VerifyReopen VerificationOutcome = "REOPEN"
	// VerifyRearm waits for the run to finish. It never spends an attempt: a queued or
	// abandoned execution is not a failed repair.
	VerifyRearm VerificationOutcome = "REARM"
	// VerifyFrozen stops the agent: the same class survived the attempt budget.
	VerifyFrozen VerificationOutcome = "FROZEN"
	// VerifyEscalated hands the case to a person.
	VerifyEscalated VerificationOutcome = "ESCALATED"
)

// Verification is the case's next state after one look at its verification run.
type Verification struct {
	Outcome VerificationOutcome
	// Attempts is the new attempt count; it only moves on a same-class failure.
	Attempts int
	// Class is the case's class after verification.
	Class FailureClass
	// Reason explains the outcome in one sentence for the transcript and the report.
	Reason string
}

// VerificationInput is everything one verification is decided from.
type VerificationInput struct {
	// State and Class are the case as it stands.
	State    CaseState
	Class    FailureClass
	Attempts int
	// MaxAttempts overrides defaultCaseMaxAttempts when positive.
	MaxAttempts int
	// Execution is the verification run; nil when no row was produced at all.
	Execution *store.QualityExecutionMessage
	// NewClass is the diagnosis of that run, empty for a judged verdict. The caller
	// computes it with DiagnoseExecution so the two models cannot drift.
	NewClass FailureClass
	// RuleState is the check's current state. A paused or archived check will never run
	// again, so a case must not wait for it.
	RuleState storepb.QualityRuleState
}

// VerifyCase decides the case's next state deterministically. The model is not asked:
// "did the repair work" is a comparison between two classifications, and asking a model
// to make it would replace a reproducible answer with an opinion.
func VerifyCase(input VerificationInput) Verification {
	maxAttempts := input.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultCaseMaxAttempts
	}
	// A check that can no longer run must not stay in a waiting state: PAUSE and ARCHIVE
	// both stop executions, so the verification would never arrive.
	if input.RuleState == storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED {
		return Verification{Outcome: VerifyEscalated, Attempts: input.Attempts, Class: input.Class,
			Reason: "the check is paused, so no verification run will arrive; a person decides whether to resume or archive it"}
	}
	if input.RuleState == storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED {
		return Verification{Outcome: VerifyResolved, Attempts: input.Attempts, Class: input.Class,
			Reason: "the check is archived"}
	}
	execution := input.Execution
	if execution == nil {
		return Verification{Outcome: VerifyRearm, Attempts: input.Attempts, Class: input.Class,
			Reason: "the verification run has not produced a row yet"}
	}
	switch execution.State {
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING:
		return Verification{Outcome: VerifyRearm, Attempts: input.Attempts, Class: input.Class,
			Reason: "the verification run has not finished"}
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED:
		return Verification{Outcome: VerifyRearm, Attempts: input.Attempts, Class: input.Class,
			Reason: "the verification run was abandoned before it produced a verdict"}
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED:
	default:
		return Verification{Outcome: VerifyRearm, Attempts: input.Attempts, Class: input.Class,
			Reason: "the verification run is not in a terminal state"}
	}
	switch execution.Status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL:
		return Verification{Outcome: VerifyResolved, Attempts: input.Attempts, Class: input.Class,
			Reason: "the check produced a verdict"}
	case storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
		return Verification{Outcome: VerifyRearm, Attempts: input.Attempts, Class: input.Class,
			Reason: "the verification run was skipped"}
	}
	// No verdict. An empty target stays an empty target, which is an answer rather than a
	// failed repair; only the case that was about an empty target may close that way.
	if execution.Status == storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED &&
		execution.TotalRows == 0 && input.Class == ClassNoVerdictEmpty {
		return Verification{Outcome: VerifyResolved, Attempts: input.Attempts, Class: input.Class,
			Reason: "the target still holds no rows, so there is no verdict to produce"}
	}
	if input.NewClass != input.Class {
		class := input.NewClass
		if class == "" {
			class = ClassExecutionFault
		}
		return Verification{Outcome: VerifyReopen, Attempts: input.Attempts, Class: class,
			Reason: "the failure changed class, so the case is re-diagnosed"}
	}
	attempts := input.Attempts + 1
	if attempts >= maxAttempts {
		return Verification{Outcome: VerifyFrozen, Attempts: attempts, Class: input.Class,
			Reason: "the same failure survived the attempt budget"}
	}
	return Verification{Outcome: VerifyRetry, Attempts: attempts, Class: input.Class,
		Reason: "the repair did not resolve the failure; one more attempt is allowed"}
}
