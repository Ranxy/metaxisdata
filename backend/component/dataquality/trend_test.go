package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// trendExecution builds one judged execution at revision 1 whose measurement is
// observed, with the failed/total row counts a count-shaped check records.
func trendExecution(id int64, observed float64, failed, total int64) TrendExecution {
	return TrendExecution{
		ID:            id,
		StartedAt:     time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(id) * time.Hour),
		Status:        storepb.QualityStatus_QUALITY_STATUS_PASS,
		ObservedValue: validationPtr(observed),
		FailedRows:    failed,
		TotalRows:     total,
		Revision:      1,
	}
}

// TestRuleTypeSpecsCarryATrendShape is the completeness guard of the chart
// contract: every type that measures something has a shape, and the unit and
// suggestion family follow the config where they depend on it.
func TestRuleTypeSpecsCarryATrendShape(t *testing.T) {
	t.Parallel()

	for _, spec := range ruleTypeSpecs {
		if spec.Type == storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT {
			require.Nil(t, spec.Trend, "schema_drift observes the registry, not a numeric value")
			continue
		}
		require.NotNil(t, spec.Trend, "%s has no trend shape", spec.Type)
	}

	percentUnique := &storepb.QualityRuleConfig{
		Config:          &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
		ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT,
	}
	countUnique := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
	}
	nullCount := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
			Column: "email", MaxNullCount: validationPtr(int64(3)),
		}},
	}
	nullPercent := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
			Column: "email", MaxNullPct: validationPtr(1.5),
		}},
	}
	rowCount := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{
			MinRows: validationPtr(int64(10)),
		}},
	}
	rowCountChange := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCountChange{RowCountChange: &storepb.RowCountChangeConfig{
			MaxIncreasePct: validationPtr(10.0),
		}},
	}
	freshness := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{
			TimestampColumn: "updated_at", MaxLagSeconds: 3600,
		}},
	}
	customSQL := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_CustomSql{CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT 1"}},
	}
	crossTable := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{
			Reference: &storepb.CrossTableReference{MetaGuid: "instances/ins1;shop;public;orders", MetaType: storepb.MetaType_TABLE},
			Tolerance: validationPtr(10.0),
		}},
	}
	aggregate := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Aggregate{Aggregate: &storepb.AggregateConfig{
			Column: "amount", Function: storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
			Min: validationPtr(0.0),
		}},
	}

	cases := []struct {
		name       string
		ruleType   storepb.QualityRuleType
		config     *storepb.QualityRuleConfig
		unit       TrendUnit
		band       bool
		suggestion trendSuggestionFamily
		notice     TrendNotice
	}{
		{"row count", storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, rowCount, TrendUnitCount, true, trendSuggestionObservedRange, TrendNoticeUnspecified},
		{"row count change", storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, rowCountChange, TrendUnitCount, false, trendSuggestionNone, TrendNoticeUnitNotComparable},
		{"null count", storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, nullCount, TrendUnitCount, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"null percent", storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, nullPercent, TrendUnitPercent, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"unique percent", storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, percentUnique, TrendUnitPercent, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"unique count", storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, countUnique, TrendUnitCount, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"accepted values percent", storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, percentUnique, TrendUnitPercent, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"cel percent", storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, percentUnique, TrendUnitPercent, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"freshness", storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, freshness, TrendUnitSeconds, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"custom sql", storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL, customSQL, TrendUnitCount, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"cross table", storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, crossTable, TrendUnitNumber, true, trendSuggestionUpperBound, TrendNoticeUnspecified},
		{"aggregate", storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, aggregate, TrendUnitNumber, true, trendSuggestionNone, TrendNoticeUnsupportedRuleType},
	}
	for _, testCase := range cases {
		spec := testSpecFor(t, testCase.ruleType)
		shape := spec.Trend(testCase.config)
		require.Equal(t, testCase.unit, shape.Unit, "%s unit", testCase.name)
		require.Equal(t, testCase.band, shape.Band, "%s band", testCase.name)
		require.Equal(t, testCase.suggestion, shape.Suggestion, "%s suggestion family", testCase.name)
		require.Equal(t, testCase.notice, shape.SuggestionNotice, "%s suggestion notice", testCase.name)
	}

	// A nil config must not panic and must fall back to the count unit.
	require.Equal(t, TrendUnitCount, testSpecFor(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE).Trend(nil).Unit)
}

// TestBuildTrendNormalizesPercent checks the reason this read model exists: the
// stored observed value of a PERCENT check is still a row count, and the plotted
// value must be the percentage the judge actually compared.
func TestBuildTrendNormalizesPercent(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config:          &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
		ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT,
	}
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		FailValue:  validationPtr(5.0),
	}
	newest := trendExecution(2, 10, 10, 1000)
	newest.Revision = 2
	newest.Thresholds = thresholds
	older := trendExecution(1, 5, 5, 1000)
	older.Revision = 2

	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Revision:   2,
		Config:     config,
		Executions: []TrendExecution{newest, older}, // newest first, as the store returns them
		Revisions:  []TrendRevision{{Revision: 2, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, Config: config}},
	})

	require.Equal(t, TrendUnitPercent, result.Unit)
	require.Len(t, result.Points, 2)
	// The response is oldest first so a chart draws left to right.
	require.Equal(t, int64(1), result.Points[0].ExecutionID)
	require.InDelta(t, 0.5, result.Points[0].Value, 1e-9) // 5 failed rows of 1000
	require.InDelta(t, 1.0, result.Points[1].Value, 1e-9) // 10 failed rows of 1000
	require.Equal(t, thresholds, result.Band)
	require.Equal(t, TrendNoticeUnspecified, result.BandNotice)
	// Two points are below the suggestion minimum.
	require.Nil(t, result.Suggestion.Thresholds)
	require.Equal(t, TrendNoticeInsufficientHistory, result.Suggestion.Notice)
	require.Equal(t, int32(2), result.Suggestion.SampleSize)
}

// TestBuildTrendBandNeedsTheCurrentRevision pins the band rule: only an execution
// judged with exactly the rule's current revision describes the bounds on screen.
func TestBuildTrendBandNeedsTheCurrentRevision(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
	}
	thresholds := &storepb.QualityThresholds{Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, FailValue: validationPtr(1.0)}
	newest := trendExecution(2, 2, 2, 100)
	newest.Revision = 2
	newest.Thresholds = thresholds
	older := trendExecution(1, 1, 1, 100)
	older.Revision = 1

	input := TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Revision:   3, // edited after both executions
		Config:     config,
		Executions: []TrendExecution{newest, older},
		Revisions: []TrendRevision{
			{Revision: 1, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, Config: config},
			{Revision: 2, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, Config: config},
		},
	}
	result := BuildTrend(input)
	require.Equal(t, 2, len(result.Points))
	require.Nil(t, result.Band)
	require.Equal(t, TrendNoticeEditedSinceLastRun, result.BandNotice)

	input.Revision = 2
	result = BuildTrend(input)
	require.Equal(t, thresholds, result.Band)
	require.Equal(t, TrendNoticeUnspecified, result.BandNotice)
}

// TestBuildTrendDropsAnEarlierRuleType makes sure history judged by another kind
// of check is not drawn on this check's scale, and that the omission is reported.
func TestBuildTrendDropsAnEarlierRuleType(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
	}
	oldConfig := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: validationPtr(int64(1))}},
	}
	current := trendExecution(2, 2, 2, 100)
	current.Revision = 2
	previous := trendExecution(1, 1000, 0, 1000)
	previous.Revision = 1

	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Revision:   2,
		Config:     config,
		Executions: []TrendExecution{current, previous},
		Revisions: []TrendRevision{
			{Revision: 1, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, Config: oldConfig},
			{Revision: 2, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, Config: config},
		},
	})
	require.True(t, result.HistoryRestarted)
	require.Len(t, result.Points, 1)
	require.Equal(t, int64(2), result.Points[0].ExecutionID)
}

// TestBuildTrendStopsAtTheNewestTypeChange pins the truncation on a type change
// away and back: the series is the newest run of the current type, not the union
// of every run that type ever judged, because stitching across the excluded run
// would draw one line over two unrelated kinds of measurement.
func TestBuildTrendStopsAtTheNewestTypeChange(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
	}
	oldConfig := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: validationPtr(int64(1))}},
	}
	newest := trendExecution(3, 1, 1, 100)
	newest.Revision = 3
	middle := trendExecution(2, 1000, 0, 1000)
	middle.Revision = 2
	oldest := trendExecution(1, 2, 2, 100)
	oldest.Revision = 1

	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Revision:   3,
		Config:     config,
		Executions: []TrendExecution{newest, middle, oldest},
		Revisions: []TrendRevision{
			{Revision: 1, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, Config: config},
			{Revision: 2, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, Config: oldConfig},
			{Revision: 3, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, Config: config},
		},
	})
	require.True(t, result.HistoryRestarted)
	require.Len(t, result.Points, 1, "the older same-type run is on the far side of the type change")
	require.Equal(t, int64(3), result.Points[0].ExecutionID)
}

// A revision the caller did not carry proves nothing, so its point stays instead of
// disappearing from a chart that already shows it.
func TestBuildTrendKeepsAPointWhoseRevisionIsUnknown(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
	}
	execution := trendExecution(1, 1, 1, 100)
	execution.Revision = 7

	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Revision:   7,
		Config:     config,
		Executions: []TrendExecution{execution},
	})
	require.False(t, result.HistoryRestarted)
	require.Len(t, result.Points, 1)
}

// TestBuildTrendSuggestsUpperBound pins the arithmetic and rounding of the single
// upper bound: p95 for warn, p99 for fail, rounded up to the unit's precision.
func TestBuildTrendSuggestsUpperBound(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config:          &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
		ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT,
	}
	// Percentages over 1000 rows: 0, 0, 0.1, 0.2, 1.0.
	executions := []TrendExecution{
		trendExecution(5, 10, 10, 1000),
		trendExecution(4, 2, 2, 1000),
		trendExecution(3, 1, 1, 1000),
		trendExecution(2, 0, 0, 1000),
		trendExecution(1, 0, 0, 1000),
	}
	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Revision:   1,
		Config:     config,
		Executions: executions,
	})
	suggestion := result.Suggestion
	require.Equal(t, TrendSuggestionMethodP95P99, suggestion.Method)
	require.Equal(t, int32(5), suggestion.SampleSize)
	require.NotNil(t, suggestion.Thresholds)
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, suggestion.Thresholds.GetComparison())
	require.InDelta(t, 0.97, suggestion.Thresholds.GetFailValue(), 1e-9) // p99 = 0.968, rounded up
	require.InDelta(t, 0.84, suggestion.Thresholds.GetWarnValue(), 1e-9) // p95 = 0.84
}

// TestBuildTrendSuggestsRowCountRange pins the two-sided suggestion: it only fills
// bounds the config already has, so a minimum-only check does not silently grow a
// maximum.
func TestBuildTrendSuggestsRowCountRange(t *testing.T) {
	t.Parallel()

	executions := []TrendExecution{
		trendExecution(1, 100, 0, 100),
		trendExecution(2, 200, 0, 200),
		trendExecution(3, 300, 0, 300),
		trendExecution(4, 400, 0, 400),
		trendExecution(5, 500, 0, 500),
	}
	fullBounded := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{
			MinRows: validationPtr(int64(10)), MaxRows: validationPtr(int64(1000)),
			WarnMinRows: validationPtr(int64(10)), WarnMaxRows: validationPtr(int64(1000)),
		}},
	}
	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Revision:   1,
		Config:     fullBounded,
		Executions: executions,
	})
	thresholds := result.Suggestion.Thresholds
	require.NotNil(t, thresholds)
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_OUTSIDE_RANGE, thresholds.GetComparison())
	require.Equal(t, 90.0, thresholds.GetFailValue())       // floor(100 * 0.9)
	require.Equal(t, 550.0, thresholds.GetFailValueUpper()) // ceil(500 * 1.1)
	require.Equal(t, 100.0, thresholds.GetWarnValue())
	require.Equal(t, 500.0, thresholds.GetWarnValueUpper())
	require.Equal(t, TrendSuggestionMethodObservedRange, result.Suggestion.Method)

	minimumOnly := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{
			MinRows: validationPtr(int64(10)),
		}},
	}
	result = BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Revision:   1,
		Config:     minimumOnly,
		Executions: executions,
	})
	require.NotNil(t, result.Suggestion.Thresholds)
	require.Nil(t, result.Suggestion.Thresholds.FailValueUpper)
	require.Nil(t, result.Suggestion.Thresholds.WarnValue)

	exact := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{
			ExactRows: validationPtr(int64(100)),
		}},
	}
	result = BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Revision:   1,
		Config:     exact,
		Executions: executions,
	})
	require.Nil(t, result.Suggestion.Thresholds)
	require.Equal(t, TrendNoticeUnsupportedConfig, result.Suggestion.Notice)
}

// TestBuildTrendSuggestionNeedsHistory keeps a three-point series from producing a
// bound that looks more certain than it is.
func TestBuildTrendSuggestionNeedsHistory(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
	}
	result := BuildTrend(TrendInput{
		RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Revision: 1,
		Config:   config,
		Executions: []TrendExecution{
			trendExecution(3, 3, 3, 100),
			trendExecution(2, 2, 2, 100),
			trendExecution(1, 1, 1, 100),
		},
	})
	require.Nil(t, result.Suggestion.Thresholds)
	require.Equal(t, TrendNoticeInsufficientHistory, result.Suggestion.Notice)
	require.Equal(t, int32(3), result.Suggestion.SampleSize)
}

// TestBuildTrendRowCountChangeHasNoThresholdBand documents the one plotted type
// whose thresholds bound a different quantity: a percentage change against a moving
// baseline, not the row count on the axis.
func TestBuildTrendRowCountChangeHasNoThresholdBand(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCountChange{RowCountChange: &storepb.RowCountChangeConfig{
			MaxIncreasePct: validationPtr(10.0),
		}},
	}
	executions := make([]TrendExecution, 0, 5)
	for index := int64(1); index <= 5; index++ {
		execution := trendExecution(index, float64(1000+index), 0, 1000)
		execution.Thresholds = &storepb.QualityThresholds{
			Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
			FailValue:  validationPtr(10.0),
		}
		executions = append([]TrendExecution{execution}, executions...)
	}
	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		Revision:   1,
		Config:     config,
		Executions: executions,
	})
	require.Equal(t, TrendUnitCount, result.Unit)
	require.Len(t, result.Points, 5)
	require.Nil(t, result.Band)
	require.Equal(t, TrendNoticeUnitNotComparable, result.BandNotice)
	require.Nil(t, result.Suggestion.Thresholds)
	require.Equal(t, TrendNoticeUnitNotComparable, result.Suggestion.Notice)
}

// TestBuildTrendCrossTablePlotsTheExcess checks the tolerance subtraction, using the
// tolerance of the revision that judged each execution rather than the current one.
func TestBuildTrendCrossTablePlotsTheExcess(t *testing.T) {
	t.Parallel()

	currentConfig := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{
			Reference: &storepb.CrossTableReference{MetaGuid: "instances/ins1;shop;public;orders", MetaType: storepb.MetaType_TABLE},
			Tolerance: validationPtr(10.0),
		}},
	}
	oldConfig := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{
			Reference: &storepb.CrossTableReference{MetaGuid: "instances/ins1;shop;public;orders", MetaType: storepb.MetaType_TABLE},
			Tolerance: validationPtr(4.0),
		}},
	}
	newest := trendExecution(3, 3, 0, 0)
	newest.Revision = 2
	second := trendExecution(2, 20, 0, 0)
	second.Revision = 2
	oldest := trendExecution(1, 20, 0, 0)
	oldest.Revision = 1

	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		Revision:   2,
		Config:     currentConfig,
		Executions: []TrendExecution{newest, second, oldest},
		Revisions: []TrendRevision{
			{Revision: 1, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, Config: oldConfig},
			{Revision: 2, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, Config: currentConfig},
		},
	})
	require.Equal(t, TrendUnitNumber, result.Unit)
	require.Len(t, result.Points, 3)
	require.InDelta(t, 16.0, result.Points[0].Value, 1e-9) // |20| - 4, the revision's tolerance
	require.InDelta(t, 10.0, result.Points[1].Value, 1e-9) // |20| - 10
	require.InDelta(t, 0.0, result.Points[2].Value, 1e-9)  // inside tolerance
}

// TestBuildTrendUnsupportedTypeIsEmpty pins the schema_drift answer: it has no
// numeric observation, so it gets no series at all rather than a fabricated one.
func TestBuildTrendUnsupportedTypeIsEmpty(t *testing.T) {
	t.Parallel()

	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		Revision:   1,
		Executions: []TrendExecution{trendExecution(1, 1, 1, 1)},
	})
	require.Empty(t, result.Points)
	require.Equal(t, TrendUnitUnspecified, result.Unit)
	require.Nil(t, result.Band)
	require.Equal(t, TrendNoticeUnsupportedRuleType, result.BandNotice)
	require.Nil(t, result.Suggestion.Thresholds)
	require.Equal(t, TrendNoticeUnsupportedRuleType, result.Suggestion.Notice)
}

// TestBuildTrendSkipsUnmeasuredExecutions keeps a nil observation (which a judged
// execution should not have, but history predating a column could) out of the line.
func TestBuildTrendSkipsUnmeasuredExecutions(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "uid"}},
	}
	unmeasured := trendExecution(2, 0, 0, 0)
	unmeasured.ObservedValue = nil
	measured := trendExecution(1, 1, 1, 100)

	result := BuildTrend(TrendInput{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Revision:   1,
		Config:     config,
		Executions: []TrendExecution{unmeasured, measured},
	})
	require.Len(t, result.Points, 1)
	require.Equal(t, int64(1), result.Points[0].ExecutionID)
}

func TestTrendQuantile(t *testing.T) {
	t.Parallel()

	sorted := []float64{1, 2, 3, 4}
	require.Equal(t, 1.0, trendQuantile(sorted, 0))
	require.Equal(t, 4.0, trendQuantile(sorted, 1))
	require.InDelta(t, 2.5, trendQuantile(sorted, 0.5), 1e-9)
	require.InDelta(t, 3.85, trendQuantile(sorted, 0.95), 1e-9)
	require.Equal(t, 7.0, trendQuantile([]float64{7}, 0.5))
	require.Equal(t, 0.0, trendQuantile(nil, 0.5))
}

func TestTrendRoundUp(t *testing.T) {
	t.Parallel()

	require.Equal(t, 4.0, trendRoundUp(TrendUnitCount, 3.2))
	require.Equal(t, 3.0, trendRoundUp(TrendUnitCount, 3.0))
	require.Equal(t, 90.0, trendRoundUp(TrendUnitSeconds, 90.0))
	require.InDelta(t, 0.97, trendRoundUp(TrendUnitPercent, 0.968), 1e-9)
	require.InDelta(t, 2.5, trendRoundUp(TrendUnitNumber, 2.5), 1e-9)
}
