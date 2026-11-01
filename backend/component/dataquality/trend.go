package dataquality

import (
	"math"
	"slices"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The observed-value trend is a read model over the executions a check already
// has: every verdict that measured something stores the measurement, so a
// check's own history is enough to plot it and enough to propose a threshold from
// what the data actually looked like. Nothing here writes, schedules or queries a
// target database; the API turns a result into a response, and the editor offers
// the suggestion to a person who still has to save the check.
//
// Two facts shape the model. An execution stores the raw measurement, not the
// quantity the judge compared: a PERCENT threshold metric judges failed rows over
// total rows while observed_value stays a row count, and a null percentage is
// likewise stored as a null count. And a rule's type may change, which bumps the
// revision without rewriting the executions the old definition judged. So the
// trend normalizes every point into the unit the rule's current definition judges
// in, and starts the series after the oldest definition change that makes the
// earlier points incomparable.

const (
	// TrendDefaultPoints is how many of a check's most recent judged executions the
	// trend reads when the caller names no limit.
	TrendDefaultPoints = 30
	// TrendMaxPoints is the ceiling: one point per day of the execution retention.
	TrendMaxPoints = 90

	// trendMinSuggestionSample is the smallest history a suggested threshold is
	// derived from. Below it the quantiles are the sample itself, and a bound built
	// from five values would look more certain than it is.
	trendMinSuggestionSample = 5
	// trendRangeSlackFraction widens observed row counts on both sides, so an
	// ordinary daily swing does not cross the bound the first day it grows.
	trendRangeSlackFraction = 0.1
	// trendQuantileEpsilon keeps a quantile that is mathematically an integer from
	// rounding up because of floating-point noise (3.0000000000000004 -> 3).
	trendQuantileEpsilon = 1e-9
)

// TrendUnit is the unit a trend's values and threshold bounds are expressed in.
type TrendUnit int

const (
	TrendUnitUnspecified TrendUnit = iota
	// TrendUnitCount is a number of rows: failed rows, a row count, a group count.
	TrendUnitCount
	// TrendUnitPercent is already a percentage in 0-100, never a 0-1 ratio: a
	// PERCENT threshold metric's failed rows over total rows, or a null percentage.
	TrendUnitPercent
	// TrendUnitSeconds is a lag.
	TrendUnitSeconds
	// TrendUnitNumber is an aggregate value or a table-to-table difference.
	TrendUnitNumber
)

// TrendNotice explains why a trend has no threshold band or no suggestion.
type TrendNotice int

const (
	TrendNoticeUnspecified TrendNotice = iota
	// TrendNoticeNoJudgedExecution means the check has no judged execution in the
	// window, so there is nothing to draw a band or a suggestion from.
	TrendNoticeNoJudgedExecution
	// TrendNoticeEditedSinceLastRun means the newest point was judged with an older
	// revision: the current thresholds never judged any drawn point.
	TrendNoticeEditedSinceLastRun
	// TrendNoticeUnitNotComparable means the plotted value is not the quantity the
	// thresholds bound, e.g. a row count plotted against percent-change bounds.
	TrendNoticeUnitNotComparable
	// TrendNoticeUnsupportedRuleType means the rule type has no numeric observation,
	// or no threshold shape this read model can suggest.
	TrendNoticeUnsupportedRuleType
	// TrendNoticeInsufficientHistory means fewer judged executions than a suggestion
	// needs.
	TrendNoticeInsufficientHistory
	// TrendNoticeUnsupportedConfig means the type could be suggested but this config
	// has no bound to fill in.
	TrendNoticeUnsupportedConfig
)

// TrendSuggestionMethod names the arithmetic behind a suggestion, so the editor
// can state how the number was reached.
type TrendSuggestionMethod int

const (
	TrendSuggestionMethodUnspecified TrendSuggestionMethod = iota
	// TrendSuggestionMethodP95P99 is a single upper bound: warn = p95, fail = p99.
	TrendSuggestionMethodP95P99
	// TrendSuggestionMethodObservedRange is a pair of row-count bounds: the observed
	// range widened by trendRangeSlackFraction, with the warn bounds at the
	// observed extremes.
	TrendSuggestionMethodObservedRange
)

// trendSuggestionFamily is how a rule type's thresholds relate to its plotted
// value, which decides whether history can propose one.
type trendSuggestionFamily int

const (
	trendSuggestionNone trendSuggestionFamily = iota
	trendSuggestionUpperBound
	trendSuggestionObservedRange
)

// trendShape is one rule type's chart contract: what its judged quantity is
// measured in, whether its thresholds bound that quantity, and how history can
// propose a threshold for it.
type trendShape struct {
	Unit       TrendUnit
	Band       bool
	Suggestion trendSuggestionFamily
	// SuggestionNotice explains the absent suggestion for a type whose family is
	// trendSuggestionNone. The default for that family is
	// TrendNoticeUnsupportedRuleType.
	SuggestionNotice TrendNotice
}

// TrendInput is the evidence one check's trend read may use. Executions and
// Revisions are read by the caller from the store: executions of the check
// against its current target, newest first, and the revision snapshots named by
// those executions.
type TrendInput struct {
	RuleType storepb.QualityRuleType
	// Revision is the rule's current revision. A threshold band is drawn only from
	// an execution judged with exactly this revision.
	Revision int32
	Config   *storepb.QualityRuleConfig
	// Executions are the check's judged executions, newest first.
	Executions []TrendExecution
	// Revisions is the definition history behind the window. A revision this slice
	// does not carry is treated as the current definition: the caller could not
	// prove a difference, and dropping the point would hide real measurements.
	Revisions []TrendRevision
}

// TrendExecution is one judged execution of the check, as the store reports it.
type TrendExecution struct {
	// ID is the caller's opaque execution id, echoed on the mapped point.
	ID        int64
	StartedAt time.Time
	Status    storepb.QualityStatus
	// ObservedValue is the stored measurement. A judged execution always has one;
	// a nil value is not a point.
	ObservedValue *float64
	FailedRows    int64
	TotalRows     int64
	Sampled       bool
	// Revision is the rule revision the execution was judged with.
	Revision int32
	// Thresholds is the snapshot the execution was judged with. It becomes the
	// band when the execution's revision is the current one.
	Thresholds *storepb.QualityThresholds
}

// TrendRevision is one immutable rule definition, enough to tell whether an
// execution was judged by the same kind of check.
type TrendRevision struct {
	Revision int32
	RuleType storepb.QualityRuleType
	Config   *storepb.QualityRuleConfig
}

// TrendPoint is one judged execution mapped into the trend's unit.
type TrendPoint struct {
	ExecutionID int64
	StartedAt   time.Time
	Status      storepb.QualityStatus
	// Value is the quantity the check judged, in the trend's unit. For a PERCENT
	// check this is failed rows over total rows; a row_count_change check plots its
	// row count.
	Value      float64
	FailedRows int64
	TotalRows  int64
	Sampled    bool
	Revision   int32
}

// TrendSuggestion is the threshold history proposes for the check's current
// definition. It never applies itself: the editor fills the form and a person
// saves the check.
type TrendSuggestion struct {
	// Thresholds is shaped like the snapshot the check records: fail_value/warn_value
	// for a single upper bound, the fail/warn pair for two-sided row-count bounds.
	// Nil when no suggestion could be derived, in which case Notice says why.
	Thresholds *storepb.QualityThresholds
	SampleSize int32
	Method     TrendSuggestionMethod
	Notice     TrendNotice
}

// TrendResult is one check's plotted history, its current band, and a suggested
// replacement bound.
type TrendResult struct {
	Unit TrendUnit
	// Points are ordered oldest first.
	Points []TrendPoint
	// Band is the threshold snapshot the check judges against now, or nil with
	// BandNotice explaining why it is absent.
	Band       *storepb.QualityThresholds
	BandNotice TrendNotice
	Suggestion TrendSuggestion
	// HistoryRestarted is true when executions from an earlier rule type were left
	// out, i.e. the drawn history starts at a type change.
	HistoryRestarted bool
}

// BuildTrend maps a check's judged executions into its plotted history, picks the
// band its current thresholds draw, and derives a suggestion from the series. It
// is a pure function of its input: the caller reads the executions and revisions,
// the decision about what the series means lives here.
func BuildTrend(input TrendInput) TrendResult {
	result := TrendResult{
		// A type without a descriptor below gets no series and no suggestion; the
		// zero notice already says why.
		BandNotice: TrendNoticeUnsupportedRuleType,
		Suggestion: TrendSuggestion{Notice: TrendNoticeUnsupportedRuleType},
	}
	spec, ok := ruleTypeSpecFor(input.RuleType)
	if !ok || spec.Trend == nil {
		return result
	}
	shape := spec.Trend(input.Config)
	result.Unit = shape.Unit

	revisions := make(map[int32]TrendRevision, len(input.Revisions))
	for _, revision := range input.Revisions {
		revisions[revision.Revision] = revision
	}

	kept := make([]TrendExecution, 0, len(input.Executions))
	points := make([]TrendPoint, 0, len(input.Executions))
	for _, execution := range input.Executions {
		revision, known := revisions[execution.Revision]
		if known && revision.RuleType != input.RuleType {
			// The check was a different kind of check when this execution was judged;
			// its measurement is not on this series' scale. Executions are newest
			// first, so everything older than this one is on the far side of the type
			// change too: the series is truncated here rather than stitched across a
			// type the reader cannot see.
			result.HistoryRestarted = true
			break
		}
		value, ok := trendPointValue(input.RuleType, shape.Unit, input.Config, revision, known, execution)
		if !ok {
			continue
		}
		kept = append(kept, execution)
		points = append(points, TrendPoint{
			ExecutionID: execution.ID,
			StartedAt:   execution.StartedAt,
			Status:      execution.Status,
			Value:       value,
			FailedRows:  execution.FailedRows,
			TotalRows:   execution.TotalRows,
			Sampled:     execution.Sampled,
			Revision:    execution.Revision,
		})
	}
	// The store returns newest first so the read can stop early; a chart reads left
	// to right, so both slices are flipped once here.
	slices.Reverse(points)
	slices.Reverse(kept)
	result.Points = points

	result.Band, result.BandNotice = trendBand(shape, input.Revision, kept)
	result.Suggestion = trendSuggestion(shape, input.Config, points)
	return result
}

// trendBand returns the threshold snapshot the newest point was judged with, when
// that point is still at the rule's current revision. A band from a revision the
// check has moved past would describe bounds no drawn point was compared with.
func trendBand(shape trendShape, currentRevision int32, executions []TrendExecution) (*storepb.QualityThresholds, TrendNotice) {
	if len(executions) == 0 {
		return nil, TrendNoticeNoJudgedExecution
	}
	if !shape.Band {
		return nil, TrendNoticeUnitNotComparable
	}
	newest := executions[len(executions)-1]
	if newest.Revision != currentRevision {
		return nil, TrendNoticeEditedSinceLastRun
	}
	if newest.Thresholds == nil {
		return nil, TrendNoticeNoJudgedExecution
	}
	return newest.Thresholds, TrendNoticeUnspecified
}

// trendSuggestion derives a threshold from the plotted series. An unavailable
// suggestion always explains itself, so the editor can say why the check is the
// one case where the number must be configured by hand.
func trendSuggestion(shape trendShape, config *storepb.QualityRuleConfig, points []TrendPoint) TrendSuggestion {
	if shape.Suggestion == trendSuggestionNone {
		notice := shape.SuggestionNotice
		if notice == TrendNoticeUnspecified {
			notice = TrendNoticeUnsupportedRuleType
		}
		return TrendSuggestion{Notice: notice}
	}
	if len(points) < trendMinSuggestionSample {
		return TrendSuggestion{Notice: TrendNoticeInsufficientHistory, SampleSize: int32(len(points))}
	}
	switch shape.Suggestion {
	case trendSuggestionNone:
		return TrendSuggestion{Notice: TrendNoticeUnsupportedRuleType, SampleSize: int32(len(points))}
	case trendSuggestionUpperBound:
		thresholds, method := trendUpperBoundSuggestion(shape.Unit, points)
		return TrendSuggestion{
			Thresholds: thresholds,
			SampleSize: int32(len(points)),
			Method:     method,
		}
	case trendSuggestionObservedRange:
		thresholds, ok := trendObservedRangeSuggestion(config, points)
		if !ok {
			return TrendSuggestion{Notice: TrendNoticeUnsupportedConfig, SampleSize: int32(len(points))}
		}
		return TrendSuggestion{
			Thresholds: thresholds,
			SampleSize: int32(len(points)),
			Method:     TrendSuggestionMethodObservedRange,
		}
	default:
		return TrendSuggestion{Notice: TrendNoticeUnsupportedRuleType, SampleSize: int32(len(points))}
	}
}

// trendUpperBoundSuggestion proposes fail = p99 and warn = p95 of the judged
// values. The quantiles are the reason no separate outlier filter exists: extreme
// runs move the tail order statistics by one position, not the bound.
func trendUpperBoundSuggestion(unit TrendUnit, points []TrendPoint) (*storepb.QualityThresholds, TrendSuggestionMethod) {
	values := make([]float64, 0, len(points))
	for _, point := range points {
		values = append(values, point.Value)
	}
	slices.Sort(values)
	fail := trendRoundUp(unit, trendQuantile(values, 0.99))
	warn := trendRoundUp(unit, trendQuantile(values, 0.95))
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		FailValue:  &fail,
	}
	// Equal bounds would make the warn level fire at the same value as the fail
	// level; omitting it is the honest rendering of "there is no warning zone".
	if warn < fail {
		thresholds.WarnValue = &warn
	}
	return thresholds, TrendSuggestionMethodP95P99
}

// trendObservedRangeSuggestion proposes row-count bounds from the observed range.
// It only fills a bound the config already has: a check that guards a minimum must
// not silently grow a maximum it never asked for, because that adds a failure mode
// the reviewer did not choose. Warn bounds are filled only when the config already
// carries them, and are placed inside the fail bounds so the two stay consistent
// with the config's own validation rule.
func trendObservedRangeSuggestion(config *storepb.QualityRuleConfig, points []TrendPoint) (*storepb.QualityThresholds, bool) {
	rowCount := config.GetRowCount()
	if rowCount == nil || rowCount.ExactRows != nil {
		return nil, false
	}
	if rowCount.MinRows == nil && rowCount.MaxRows == nil {
		return nil, false
	}
	values := make([]float64, 0, len(points))
	for _, point := range points {
		values = append(values, point.Value)
	}
	slices.Sort(values)
	lowest, highest := values[0], values[len(values)-1]
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_OUTSIDE_RANGE,
	}
	if rowCount.MinRows != nil {
		lower := math.Floor(lowest * (1 - trendRangeSlackFraction))
		if lower < 0 {
			lower = 0
		}
		thresholds.FailValue = &lower
		if rowCount.WarnMinRows != nil {
			warn := math.Floor(lowest)
			thresholds.WarnValue = &warn
		}
	}
	if rowCount.MaxRows != nil {
		upper := math.Ceil(highest * (1 + trendRangeSlackFraction))
		thresholds.FailValueUpper = &upper
		if rowCount.WarnMaxRows != nil {
			warn := math.Ceil(highest)
			thresholds.WarnValueUpper = &warn
		}
	}
	return thresholds, thresholds.FailValue != nil || thresholds.FailValueUpper != nil
}

// trendPointValue maps one stored measurement onto the value the current
// definition judges. The revision's own config is the source for the two places
// where the meaning of the measurement changed over time: a PERCENT metric derives
// the percentage from the stored row counts, and cross_table subtracts the
// tolerance that its revision accepted.
func trendPointValue(ruleType storepb.QualityRuleType, unit TrendUnit, currentConfig *storepb.QualityRuleConfig, revision TrendRevision, known bool, execution TrendExecution) (float64, bool) {
	if execution.ObservedValue == nil {
		return 0, false
	}
	observed := *execution.ObservedValue
	switch unit {
	case TrendUnitUnspecified:
		return 0, false
	case TrendUnitPercent:
		if execution.TotalRows <= 0 {
			return 0, false
		}
		return float64(execution.FailedRows) / float64(execution.TotalRows) * 100, true
	case TrendUnitCount, TrendUnitSeconds, TrendUnitNumber:
		if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE {
			tolerance := 0.0
			config := currentConfig
			if known && revision.Config != nil {
				config = revision.Config
			}
			if cross := config.GetCrossTable(); cross != nil && cross.Tolerance != nil {
				tolerance = *cross.Tolerance
			}
			// The judge compares the part of the difference the tolerance does not
			// absorb; a difference inside the tolerance is a zero on this scale.
			if excess := math.Abs(observed) - tolerance; excess > 0 {
				return excess, true
			}
			return 0, true
		}
		return observed, true
	default:
		return 0, false
	}
}

// trendQuantile returns the q-quantile of an ascending slice by linear
// interpolation, the same definition a spreadsheet's PERCENTILE uses. A single
// value is its own quantile at every q.
func trendQuantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	position := q * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return sorted[lower]
	}
	fraction := position - float64(lower)
	return sorted[lower] + (sorted[upper]-sorted[lower])*fraction
}

// trendRoundUp rounds a proposed bound up to the precision of its unit: whole
// rows or seconds, two decimals for a percentage or a plain number. Rounding up
// keeps a suggested bound from being stricter than the history it came from.
func trendRoundUp(unit TrendUnit, value float64) float64 {
	switch unit {
	case TrendUnitCount, TrendUnitSeconds:
		return math.Ceil(value - trendQuantileEpsilon)
	case TrendUnitPercent, TrendUnitNumber:
		return math.Ceil(value*100-trendQuantileEpsilon) / 100
	case TrendUnitUnspecified:
		return value
	default:
		return value
	}
}

// The per-type shapes below are referenced from ruleTypeSpecs. They are functions
// because the unit of a count-shaped check follows its threshold metric, and a
// null percentage follows whichever null bound its config set.

// trendPercentCapableShape is the shape of a count-shaped check: it judges failed
// rows, or failed rows over total rows when its metric is PERCENT.
func trendPercentCapableShape(config *storepb.QualityRuleConfig) trendShape {
	unit := TrendUnitCount
	if config.GetThresholdMetric() == storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT {
		unit = TrendUnitPercent
	}
	return trendShape{Unit: unit, Band: true, Suggestion: trendSuggestionUpperBound}
}

// trendRowCountShape plots the count itself against its min/max bounds.
func trendRowCountShape(_ *storepb.QualityRuleConfig) trendShape {
	return trendShape{Unit: TrendUnitCount, Band: true, Suggestion: trendSuggestionObservedRange}
}

// trendRowCountChangeShape plots the count but cannot draw its thresholds: they
// bound a percentage change against a baseline, not the count on the axis.
func trendRowCountChangeShape(_ *storepb.QualityRuleConfig) trendShape {
	return trendShape{
		Unit:             TrendUnitCount,
		Band:             false,
		Suggestion:       trendSuggestionNone,
		SuggestionNotice: TrendNoticeUnitNotComparable,
	}
}

// trendNullPercentShape plots the null count or the null percentage, whichever
// bound the config set (the count wins when both are present, matching the judge).
func trendNullPercentShape(config *storepb.QualityRuleConfig) trendShape {
	unit := TrendUnitPercent
	if nullPercent := config.GetNullPercent(); nullPercent != nil && nullPercent.MaxNullCount != nil {
		unit = TrendUnitCount
	}
	return trendShape{Unit: unit, Band: true, Suggestion: trendSuggestionUpperBound}
}

// trendFreshnessShape plots the observed lag in seconds.
func trendFreshnessShape(_ *storepb.QualityRuleConfig) trendShape {
	return trendShape{Unit: TrendUnitSeconds, Band: true, Suggestion: trendSuggestionUpperBound}
}

// trendCustomSQLShape plots the failing-row count of the user statement.
func trendCustomSQLShape(_ *storepb.QualityRuleConfig) trendShape {
	return trendShape{Unit: TrendUnitCount, Band: true, Suggestion: trendSuggestionUpperBound}
}

// trendAggregateShape plots one aggregate value against a two-sided interval. The
// bounds are open by default and the acceptable interval is a modelling choice
// (a sum's ceiling is not a quantile of its history), so it gets no suggestion.
func trendAggregateShape(_ *storepb.QualityRuleConfig) trendShape {
	return trendShape{
		Unit:             TrendUnitNumber,
		Band:             true,
		Suggestion:       trendSuggestionNone,
		SuggestionNotice: TrendNoticeUnsupportedRuleType,
	}
}

// trendCrossTableShape plots the absolute difference the tolerance did not absorb.
func trendCrossTableShape(_ *storepb.QualityRuleConfig) trendShape {
	return trendShape{Unit: TrendUnitNumber, Band: true, Suggestion: trendSuggestionUpperBound}
}
