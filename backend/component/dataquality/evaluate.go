package dataquality

import (
	"math"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Evaluate judges one observation against the rule's thresholds and returns the
// status plus the threshold snapshot a result records.
//
// Empty data is decided by the rule's on_empty behavior, never by a default pass:
// a table with no rows must not look healthy just because no row violated a
// predicate. Types whose observation is meaningful at zero rows (row_count) judge
// the zero instead.
func Evaluate(rule RuleSnapshot, obs Observation, _ time.Time) ResultState {
	if rule.Config == nil {
		return ResultState{
			Status:     storepb.QualityStatus_QUALITY_STATUS_ERROR,
			Thresholds: &storepb.QualityThresholds{OnEmpty: storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_UNSPECIFIED},
		}
	}
	onEmpty := emptyBehavior(rule.Config)
	switch rule.Type {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT:
		return evaluateRowCount(rule.Config, obs, onEmpty)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:
		return evaluateRowCountChange(rule.Config, obs, onEmpty)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:
		return evaluateNullPercent(rule.Config, obs, onEmpty)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:
		return evaluateCount(rule.Config, rule.Config.GetUnique().FailThreshold, rule.Config.GetUnique().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		return evaluateCount(rule.Config, rule.Config.GetUniqueCombination().FailThreshold, rule.Config.GetUniqueCombination().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:
		return evaluateCount(rule.Config, rule.Config.GetAcceptedValues().FailThreshold, rule.Config.GetAcceptedValues().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:
		return evaluateCount(rule.Config, rule.Config.GetRange().FailThreshold, rule.Config.GetRange().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		return evaluateCount(rule.Config, rule.Config.GetRegex().FailThreshold, rule.Config.GetRegex().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:
		return evaluateCount(rule.Config, rule.Config.GetStringLength().FailThreshold, rule.Config.GetStringLength().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY:
		return evaluateCount(rule.Config, rule.Config.GetReferentialIntegrity().FailThreshold, rule.Config.GetReferentialIntegrity().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:
		return evaluateCount(rule.Config, rule.Config.GetCrossColumn().FailThreshold, rule.Config.GetCrossColumn().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:
		// The custom statement counts failing rows itself; there is no total to
		// divide by and "no failing rows" is a pass, not missing data.
		return evaluateCount(rule.Config, rule.Config.GetCustomSql().FailThreshold, rule.Config.GetCustomSql().WarnThreshold, obs, onEmpty, false)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		// The observation is the drift-item count out of the snapshot; the metric is
		// COUNT (validation refuses PERCENT) and there is always a snapshot.
		return evaluateCount(rule.Config, rule.Config.GetSchemaDrift().FailThreshold, rule.Config.GetSchemaDrift().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:
		// total_rows is the group count: an empty table has no groups and follows
		// on_empty, while a table with groups judges the failed-group count or rate.
		return evaluateCount(rule.Config, rule.Config.GetGroupCheck().FailThreshold, rule.Config.GetGroupCheck().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE:
		return evaluateAggregate(rule.Config, obs, onEmpty)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE:
		return evaluateCrossTable(rule.Config, obs, onEmpty)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		return evaluateFreshness(rule.Config, obs, onEmpty)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		return evaluateCount(rule.Config, rule.Config.GetCelExpression().FailThreshold, rule.Config.GetCelExpression().WarnThreshold, obs, onEmpty, true)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	default:
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
}

// evaluateRowCount judges a row count against its min/max/exact bounds. Zero rows
// is a value here, not missing data: a rule with min_rows 1 must fail on an empty
// table.
func evaluateRowCount(config *storepb.QualityRuleConfig, obs Observation, onEmpty storepb.QualityEmptyBehavior) ResultState {
	rowCount := config.GetRowCount()
	if rowCount == nil {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
	observed := float64(obs.TotalRows)
	if obs.Observed != 0 {
		observed = obs.Observed
	}
	thresholds := &storepb.QualityThresholds{
		Comparison:     storepb.QualityComparison_QUALITY_COMPARISON_OUTSIDE_RANGE,
		OnEmpty:        onEmpty,
		FailValue:      int64Threshold(rowCount.MinRows),
		FailValueUpper: int64Threshold(rowCount.MaxRows),
		WarnValue:      int64Threshold(rowCount.WarnMinRows),
		WarnValueUpper: int64Threshold(rowCount.WarnMaxRows),
	}
	if rowCount.ExactRows != nil {
		thresholds.FailValue = int64Threshold(rowCount.ExactRows)
	}
	if outOfBounds(observed, rowCount.MinRows, rowCount.MaxRows, rowCount.ExactRows) {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, Thresholds: thresholds}
	}
	if warnOutOfBounds(observed, rowCount.WarnMinRows, rowCount.WarnMaxRows) {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_WARN, Thresholds: thresholds}
	}
	return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, Thresholds: thresholds}
}

// outOfBounds reports whether value violates the configured bounds.
func outOfBounds(value float64, minValue, maxValue, exact *int64) bool {
	if exact != nil {
		return value != float64(*exact)
	}
	if minValue != nil && value < float64(*minValue) {
		return true
	}
	return maxValue != nil && value > float64(*maxValue)
}

// warnOutOfBounds reports whether value is inside the fail bounds but outside a
// warn bound.
func warnOutOfBounds(value float64, warnMin, warnMax *int64) bool {
	if warnMin != nil && value < float64(*warnMin) {
		return true
	}
	return warnMax != nil && value > float64(*warnMax)
}

// evaluateRowCountChange compares the count's change against the rule's last
// successful observation. Without a recent, non-zero baseline there is nothing to
// compare, and on_empty decides the outcome.
func evaluateRowCountChange(config *storepb.QualityRuleConfig, obs Observation, onEmpty storepb.QualityEmptyBehavior) ResultState {
	change := config.GetRowCountChange()
	if change == nil {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		OnEmpty:    onEmpty,
		FailValue:  change.MaxIncreasePct,
		WarnValue:  change.WarnIncreasePct,
	}
	if obs.Baseline == nil || *obs.Baseline == 0 {
		return ResultState{Status: emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED), Thresholds: thresholds}
	}
	percent := (float64(obs.TotalRows) - *obs.Baseline) / *obs.Baseline * 100
	thresholds.FailValueUpper = change.MaxDecreasePct
	thresholds.WarnValueUpper = change.WarnDecreasePct
	failIncrease, failDecrease := change.MaxIncreasePct, change.MaxDecreasePct
	warnIncrease, warnDecrease := change.WarnIncreasePct, change.WarnDecreasePct
	if (failIncrease != nil && percent > *failIncrease) || (failDecrease != nil && -percent > *failDecrease) {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, Thresholds: thresholds}
	}
	if (warnIncrease != nil && percent > *warnIncrease) || (warnDecrease != nil && -percent > *warnDecrease) {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_WARN, Thresholds: thresholds}
	}
	return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, Thresholds: thresholds}
}

// evaluateNullPercent judges a column's nulls. Count and percentage threshold
// pairs are exclusive; which one is set decides the observed kind.
func evaluateNullPercent(config *storepb.QualityRuleConfig, obs Observation, onEmpty storepb.QualityEmptyBehavior) ResultState {
	nullPercent := config.GetNullPercent()
	if nullPercent == nil {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
	if obs.TotalRows == 0 {
		return ResultState{Status: emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED)}
	}
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		OnEmpty:    onEmpty,
	}
	var observed float64
	switch {
	case nullPercent.MaxNullCount != nil:
		observed = float64(obs.FailedRows)
		thresholds.FailValue = float64Ptr(float64(nullPercent.GetMaxNullCount()))
		if nullPercent.WarnNullCount != nil {
			thresholds.WarnValue = float64Ptr(float64(nullPercent.GetWarnNullCount()))
		}
	case nullPercent.MaxNullPct != nil:
		observed = float64(obs.FailedRows) / float64(obs.TotalRows) * 100
		thresholds.FailValue = float64Ptr(nullPercent.GetMaxNullPct())
		if nullPercent.WarnNullPct != nil {
			thresholds.WarnValue = float64Ptr(nullPercent.GetWarnNullPct())
		}
	default:
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
	return applyThresholds(observed, thresholds)
}

// evaluateCount judges a count-type rule. COUNT compares failed rows; PERCENT
// compares failed rows over total rows. A rule whose query returned no rows at all
// follows on_empty, because "no rows" means the predicate was never tested.
func evaluateCount(config *storepb.QualityRuleConfig, failThreshold, warnThreshold *float64, obs Observation, onEmpty storepb.QualityEmptyBehavior, hasTotal bool) ResultState {
	if hasTotal && obs.TotalRows == 0 && obs.FailedRows == 0 {
		return ResultState{Status: emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_PASS)}
	}
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		OnEmpty:    onEmpty,
	}
	observed := float64(obs.FailedRows)
	if thresholdMetric(config) == storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT {
		if obs.TotalRows == 0 {
			return ResultState{Status: emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_PASS), Thresholds: thresholds}
		}
		observed = float64(obs.FailedRows) / float64(obs.TotalRows) * 100
	}
	failValue := 0.0
	if failThreshold != nil {
		failValue = *failThreshold
	}
	thresholds.FailValue = float64Ptr(failValue)
	if warnThreshold != nil {
		thresholds.WarnValue = warnThreshold
	}
	return applyThresholds(observed, thresholds)
}

// evaluateFreshness judges how stale the newest timestamp is. A NULL timestamp is
// no data, which on_empty decides.
func evaluateFreshness(config *storepb.QualityRuleConfig, obs Observation, onEmpty storepb.QualityEmptyBehavior) ResultState {
	freshness := config.GetFreshness()
	if freshness == nil {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		OnEmpty:    onEmpty,
		FailValue:  float64Ptr(float64(freshness.GetMaxLagSeconds())),
	}
	if freshness.WarnLagSeconds != nil {
		thresholds.WarnValue = float64Ptr(float64(freshness.GetWarnLagSeconds()))
	}
	if obs.MaxTimestamp == nil {
		return ResultState{Status: emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED), Thresholds: thresholds}
	}
	observed := float64(obs.Observed)
	return applyThresholds(observed, thresholds)
}

// applyThresholds compares an observed value with the fail/warn thresholds and
// returns FAIL, WARN or PASS. The comparison is strict: a value equal to the
// threshold passes.
func applyThresholds(observed float64, thresholds *storepb.QualityThresholds) ResultState {
	comparison := thresholds.GetComparison()
	if violates(observed, thresholds.FailValue, thresholds.FailValueUpper, comparison) {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, Thresholds: thresholds}
	}
	if violates(observed, thresholds.WarnValue, thresholds.WarnValueUpper, comparison) {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_WARN, Thresholds: thresholds}
	}
	return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, Thresholds: thresholds}
}

// violates reports whether value crosses a threshold pair. An unset threshold is
// not a violation.
func violates(value float64, lower *float64, upper *float64, comparison storepb.QualityComparison) bool {
	switch comparison {
	case storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN:
		return lower != nil && value > *lower
	case storepb.QualityComparison_QUALITY_COMPARISON_LESS_THAN:
		return lower != nil && value < *lower
	case storepb.QualityComparison_QUALITY_COMPARISON_OUTSIDE_RANGE:
		if lower != nil && value < *lower {
			return true
		}
		return upper != nil && value > *upper
	case storepb.QualityComparison_QUALITY_COMPARISON_INSIDE_RANGE:
		if lower == nil || upper == nil {
			// A one-sided accepted band is a lower or upper bound, not a band.
			if lower != nil {
				return value < *lower
			}
			return upper != nil && value > *upper
		}
		return value >= *lower && value <= *upper
	case storepb.QualityComparison_QUALITY_COMPARISON_UNSPECIFIED:
		return false
	default:
		return false
	}
}

// emptyStatus maps on_empty to a verdict. The fallback covers UNSPECIFIED, which
// behaves as NOT_EVALUATED.
func emptyStatus(onEmpty storepb.QualityEmptyBehavior, fallback storepb.QualityStatus) storepb.QualityStatus {
	switch onEmpty {
	case storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS:
		return storepb.QualityStatus_QUALITY_STATUS_PASS
	case storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_FAIL:
		return storepb.QualityStatus_QUALITY_STATUS_FAIL
	case storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED:
		return storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED
	case storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_UNSPECIFIED:
		return fallback
	default:
		return fallback
	}
}

// float64Ptr returns a pointer to v, or nil for a value that is exactly zero when
// zero itself is a meaningful threshold. Callers that need to distinguish zero
// from unset keep the proto's optional pointer instead.
func float64Ptr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}

// int64Threshold converts a nullable integer bound into the threshold snapshot's
// floating point field. Row counts beyond 2^53 do not occur; the snapshot keeps
// one representation so a result reads uniformly.
func int64Threshold(v *int64) *float64 {
	if v == nil {
		return nil
	}
	return float64Ptr(float64(*v))
}

// evaluateAggregate judges one aggregate value against its interval. A NULL
// aggregate (no rows, or every value NULL) is missing data, which on_empty
// decides; a real zero is a value and is judged. The interval is open on both ends
// unless the config marks it inclusive, matching the range rule.
func evaluateAggregate(config *storepb.QualityRuleConfig, obs Observation, onEmpty storepb.QualityEmptyBehavior) ResultState {
	aggregate := config.GetAggregate()
	if aggregate == nil {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
	thresholds := &storepb.QualityThresholds{
		Comparison:     storepb.QualityComparison_QUALITY_COMPARISON_OUTSIDE_RANGE,
		OnEmpty:        onEmpty,
		FailValue:      aggregate.Min,
		FailValueUpper: aggregate.Max,
		WarnValue:      aggregate.WarnMin,
		WarnValueUpper: aggregate.WarnMax,
	}
	if obs.ObservedNull {
		return ResultState{
			Status:     emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED),
			Thresholds: thresholds,
		}
	}
	if aggregateOutsideBounds(obs.Observed, aggregate) {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, Thresholds: thresholds}
	}
	if aggregateWarnOutsideBounds(obs.Observed, aggregate) {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_WARN, Thresholds: thresholds}
	}
	return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, Thresholds: thresholds}
}

// aggregateOutsideBounds reports whether value is outside the fail interval. An
// exclusive bound makes the endpoint a violation (the interval is open); an
// inclusive one allows it.
func aggregateOutsideBounds(value float64, config *storepb.AggregateConfig) bool {
	if config.Min != nil {
		if config.GetInclusive() {
			if value < *config.Min {
				return true
			}
		} else if value <= *config.Min {
			return true
		}
	}
	if config.Max != nil {
		if config.GetInclusive() {
			if value > *config.Max {
				return true
			}
		} else if value >= *config.Max {
			return true
		}
	}
	return false
}

// aggregateWarnOutsideBounds reports whether value is inside the fail interval but
// outside a warn bound. The warn interval follows the same open/closed rule.
func aggregateWarnOutsideBounds(value float64, config *storepb.AggregateConfig) bool {
	if config.WarnMin != nil {
		if config.GetInclusive() {
			if value < *config.WarnMin {
				return true
			}
		} else if value <= *config.WarnMin {
			return true
		}
	}
	if config.WarnMax != nil {
		if config.GetInclusive() {
			if value > *config.WarnMax {
				return true
			}
		} else if value >= *config.WarnMax {
			return true
		}
	}
	return false
}

// evaluateCrossTable judges the difference between the two aggregates: the
// observed value is the raw absolute difference, the tolerance absorbs the part
// of it that is acceptable, and the configured fail/warn thresholds bound the
// rest. A NULL on either side is missing data, which on_empty decides.
func evaluateCrossTable(config *storepb.QualityRuleConfig, obs Observation, onEmpty storepb.QualityEmptyBehavior) ResultState {
	cross := config.GetCrossTable()
	if cross == nil {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
	tolerance := 0.0
	if cross.Tolerance != nil {
		tolerance = *cross.Tolerance
	}
	failThreshold := 0.0
	if cross.FailThreshold != nil {
		failThreshold = *cross.FailThreshold
	}
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		OnEmpty:    onEmpty,
		FailValue:  float64Ptr(failThreshold),
		WarnValue:  copyOptionalFloat(cross.WarnThreshold),
	}
	if obs.ObservedNull {
		return ResultState{
			Status:     emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED),
			Thresholds: thresholds,
		}
	}
	excess := math.Abs(obs.Observed) - tolerance
	if excess < 0 {
		excess = 0
	}
	return applyThresholds(excess, thresholds)
}

// copyOptionalFloat returns the pointer itself: the threshold snapshot shares the
// caller's immutable config value.
func copyOptionalFloat(value *float64) *float64 {
	return value
}
