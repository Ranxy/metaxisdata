package dataquality

import (
	"fmt"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ReplayWindow bounds how many recent executions a replay re-judges. The point of a replay is to
// notice that a change turns a red run green, and the newest runs are the ones a person remembers;
// a longer window costs a read per execution and cannot prove more.

const ReplayWindow = 20

// Replay is what re-judging a check's recent executions under a new definition concluded.
type Replay struct {
	// Available reports that at least one recent execution could be re-judged. False means the
	// change is unverifiable: no run recorded an observation, so nothing here can tell whether the
	// change would hide a red, and an unverifiable change is a proposal rather than a write.
	Available bool
	// Considered counts the executions that were re-judged.
	Considered int
	// Loosened names the executions that would stop failing under the new definition. It is the
	// whole point of the exercise: a change is only safe if it leaves every red run red.
	Loosened []string
}

// ReplayRule re-judges the recent executions of a check under a proposed definition.
//
// The comparison is between verdicts, not between numbers: what matters is whether a run that
// failed would now pass, and the only honest way to know is to judge it again with the same
// observation. The evaluator is pure, so this needs no connection and no model — the observation is
// the row the execution recorded.
func ReplayRule(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig, executions []*store.QualityExecutionMessage) Replay {
	replay := Replay{}
	if config == nil {
		return replay
	}
	rule := RuleSnapshot{Type: ruleType, Config: config}
	for _, execution := range executions {
		observation, ok := ReplayObservation(execution)
		if !ok {
			continue
		}
		replay.Considered++
		if !loosensVerdict(execution.Status, Evaluate(rule, observation, time.Time{}).Status) {
			continue
		}
		replay.Loosened = append(replay.Loosened, fmt.Sprintf("qualityExecutions/%d", execution.ID))
	}
	replay.Available = replay.Considered > 0
	return replay
}

// ReplayObservation rebuilds the observation one stored execution recorded, so the evaluator can
// judge it again. It reports false for a run that recorded no observation: an ERROR or a SKIPPED
// execution has nothing to re-judge, and inventing a value would manufacture a verdict.
func ReplayObservation(execution *store.QualityExecutionMessage) (Observation, bool) {
	if execution == nil || execution.State != storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED {
		return Observation{}, false
	}
	switch execution.Status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
		return Observation{}, false
	default:
		// An unknown verdict is not evidence about a change either.
		return Observation{}, false
	}
	observation := Observation{
		ObservedKind: execution.ObservedKind,
		FailedRows:   execution.FailedRows,
		TotalRows:    execution.TotalRows,
		Sampled:      execution.Sampled,
		SampleRows:   execution.SampleRows,
		Baseline:     execution.BaselineValue,
	}
	if execution.ObservedValue == nil {
		// A row without an observed value is a row whose observation was NULL, which is how an empty
		// target or an aggregate over no rows is recorded.
		observation.ObservedNull = true
		return observation, true
	}
	observation.Observed = *execution.ObservedValue
	// Only the freshness judge reads the newest timestamp, and only to decide whether there was one;
	// the lag it compares is the observed value the row already holds. Marking a timestamp present
	// keeps that decision faithful without pretending to know the instant, and every other judge
	// ignores the field.
	present := time.Time{}
	observation.MaxTimestamp = &present
	return observation, true
}

// HasReplayableExecution reports whether any of these runs can be re-judged, which is what makes a
// criterion change checkable at all. It answers the dossier's question - "can this target verify
// such a change?" - and it is a property of the history, not of the change: the verb replays the
// change itself.
func HasReplayableExecution(executions []*store.QualityExecutionMessage) bool {
	for _, execution := range executions {
		if _, ok := ReplayObservation(execution); ok {
			return true
		}
	}
	return false
}

// loosensVerdict reports whether a re-judged verdict is less alarming than the recorded one. A run
// that failed and now passes is the case that must be refused; so is a run that reached no verdict
// and now looks green, because "no verdict" is not a pass.
func loosensVerdict(recorded, replayed storepb.QualityStatus) bool {
	if replayed != storepb.QualityStatus_QUALITY_STATUS_PASS {
		return false
	}
	switch recorded {
	case storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return true
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
		return false
	default:
		return false
	}
}
