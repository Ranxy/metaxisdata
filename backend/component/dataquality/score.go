package dataquality

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ScoreMode is how one rule type's completed result becomes a 0-1 check score.
type ScoreMode int

const (
	// ScoreModeNone means the type is unknown to this build and carries no score.
	ScoreModeNone ScoreMode = iota
	// ScoreModeRows scores the share of rows that passed the check when the
	// observation carries a total row count.
	ScoreModeRows
	// ScoreModeStatus scores the verdict: PASS is 1, WARN is 0.5 and FAIL is 0.
	ScoreModeStatus
)

// ScoreModeFor reports how a rule type's result becomes a check score. The
// row-level types are the ones whose observation carries a filtered total
// (CountsTotal) plus NULL_PERCENT, derived from ruleTypeSpecs so the set can
// never drift from the type catalog.
func ScoreModeFor(ruleType storepb.QualityRuleType) ScoreMode {
	spec, ok := ruleTypeSpecFor(ruleType)
	if !ok {
		return ScoreModeNone
	}
	if spec.CountsTotal || ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT {
		return ScoreModeRows
	}
	return ScoreModeStatus
}

// CheckScore turns one completed check into its 0-1 score and reports whether it
// is judged. ERROR, SKIPPED and NOT_EVALUATED are not verdicts: they are excluded
// from both the numerator and the denominator of every score. A row-level check
// without a total row count falls back to the verdict score.
func CheckScore(ruleType storepb.QualityRuleType, status storepb.QualityStatus, failedRows, totalRows int64) (score float64, judged bool) {
	mode := ScoreModeFor(ruleType)
	if mode == ScoreModeNone {
		return 0, false
	}
	statusScore, statusJudged := verdictScore(status)
	if mode == ScoreModeRows && statusJudged && totalRows > 0 {
		return clampUnit(1 - float64(failedRows)/float64(totalRows)), true
	}
	return statusScore, statusJudged
}

// CheckWeight is the severity weight of one judged check: WARNING counts 1,
// ERROR 2 and CRITICAL 3. A rule with no severity counts like a warning so an
// older row still contributes instead of disappearing from the score.
func CheckWeight(severity storepb.QualitySeverity) float64 {
	switch severity {
	case storepb.QualitySeverity_QUALITY_SEVERITY_ERROR:
		return 2
	case storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL:
		return 3
	case storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED:
		return 1
	default:
		return 1
	}
}

// verdictScore maps one verdict onto its score. Only PASS, WARN and FAIL are
// judged; the operational states measured nothing to score.
func verdictScore(status storepb.QualityStatus) (score float64, judged bool) {
	switch status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS:
		return 1, true
	case storepb.QualityStatus_QUALITY_STATUS_WARN:
		return 0.5, true
	case storepb.QualityStatus_QUALITY_STATUS_FAIL:
		return 0, true
	case storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return 0, false
	default:
		return 0, false
	}
}

// clampUnit bounds a row-level score to [0, 1].
func clampUnit(score float64) float64 {
	return min(max(score, 0), 1)
}

// ScoreAggregate sums the weighted check scores of one scope: a target, the
// workspace, one dimension or one owner. The zero value is ready to use and means
// nothing has been judged.
type ScoreAggregate struct {
	weightedSum float64
	weightSum   float64
	scored      int32
	failing     int32
}

// Add folds one judged check into the aggregate. failing marks a FAIL verdict,
// which the breakdown rows report next to the score.
func (a *ScoreAggregate) Add(score float64, severity storepb.QualitySeverity, failing bool) {
	weight := CheckWeight(severity)
	a.weightedSum += weight * score
	a.weightSum += weight
	a.scored++
	if failing {
		a.failing++
	}
}

// Score is the weighted average on the 0-100 scale. The second result is false
// when nothing was judged, so a scope without results has no score at all rather
// than a 0 or a 100.
func (a *ScoreAggregate) Score() (float64, bool) {
	if a.weightSum <= 0 {
		return 0, false
	}
	return 100 * a.weightedSum / a.weightSum, true
}

// Scored is how many judged checks the aggregate holds.
func (a *ScoreAggregate) Scored() int32 {
	return a.scored
}

// Failing is how many of the judged checks returned FAIL.
func (a *ScoreAggregate) Failing() int32 {
	return a.failing
}
