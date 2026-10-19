package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// qualityTestPtr returns a pointer to a copy of value, for optional proto fields.
func qualityTestPtr[T any](value T) *T {
	return &value
}

// qualityConfigCases builds one store config per oneof arm with every optional
// field set, so the round-trip test pins every field of every arm.
func qualityConfigCases() []struct {
	name   string
	config *storepb.QualityRuleConfig
} {
	return []struct {
		name   string
		config *storepb.QualityRuleConfig
	}{
		{name: "row_count", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{
			MinRows:     qualityTestPtr(int64(1)),
			MaxRows:     qualityTestPtr(int64(1000)),
			ExactRows:   qualityTestPtr(int64(500)),
			WarnMinRows: qualityTestPtr(int64(10)),
			WarnMaxRows: qualityTestPtr(int64(900)),
		}}}},
		{name: "row_count_change", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCountChange{RowCountChange: &storepb.RowCountChangeConfig{
			MaxIncreasePct:  qualityTestPtr(10.5),
			MaxDecreasePct:  qualityTestPtr(20.5),
			WarnIncreasePct: qualityTestPtr(5.5),
			WarnDecreasePct: qualityTestPtr(10.5),
		}}}},
		{name: "null_percent", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
			Column:        "email",
			MaxNullCount:  qualityTestPtr(int64(10)),
			WarnNullCount: qualityTestPtr(int64(5)),
			MaxNullPct:    qualityTestPtr(0.5),
			WarnNullPct:   qualityTestPtr(0.1),
		}}}},
		{name: "unique", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{
			Column:        "id",
			CountNulls:    true,
			FailThreshold: qualityTestPtr(0.0),
			WarnThreshold: qualityTestPtr(0.25),
		}}}},
		{name: "unique_combination", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{UniqueCombination: &storepb.UniqueCombinationConfig{
			Columns:       []string{"a", "b"},
			AllColumns:    true,
			CountNulls:    true,
			FailThreshold: qualityTestPtr(1.5),
			WarnThreshold: qualityTestPtr(0.5),
		}}}},
		{name: "accepted_values", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{AcceptedValues: &storepb.AcceptedValuesConfig{
			Column:        "status",
			Values:        []string{"open", "closed"},
			AllowNull:     true,
			FailThreshold: qualityTestPtr(2.5),
			WarnThreshold: qualityTestPtr(1.5),
		}}}},
		{name: "range", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{
			Column:        "amount",
			Min:           qualityTestPtr("1"),
			Max:           qualityTestPtr("100"),
			Inclusive:     true,
			FailThreshold: qualityTestPtr(3.5),
			WarnThreshold: qualityTestPtr(2.5),
		}}}},
		{name: "regex", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{
			Column:        "email",
			Pattern:       `^[a-z]+@[a-z]+\.[a-z]+$`,
			FailThreshold: qualityTestPtr(4.5),
			WarnThreshold: qualityTestPtr(3.5),
		}}}},
		{name: "string_length", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StringLength{StringLength: &storepb.StringLengthConfig{
			Column:        "name",
			MinLen:        qualityTestPtr(int64(1)),
			MaxLen:        qualityTestPtr(int64(64)),
			FailThreshold: qualityTestPtr(5.5),
			WarnThreshold: qualityTestPtr(4.5),
		}}}},
		{name: "referential_integrity", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
			ChildColumn:   "customer_id",
			ParentTable:   "public.customers",
			ParentColumn:  "id",
			AllowNull:     true,
			FailThreshold: qualityTestPtr(6.5),
			WarnThreshold: qualityTestPtr(5.5),
		}}}},
		{name: "cross_column", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{CrossColumn: &storepb.CrossColumnConfig{
			LeftColumn:    "started_at",
			RightColumn:   "finished_at",
			Op:            storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LTE,
			Tolerance:     qualityTestPtr(0.001),
			FailThreshold: qualityTestPtr(7.5),
			WarnThreshold: qualityTestPtr(6.5),
		}}}},
		{name: "freshness", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{
			TimestampColumn: "updated_at",
			MaxLagSeconds:   3600,
			WarnLagSeconds:  qualityTestPtr(int32(1800)),
		}}}},
		{name: "custom_sql", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{CustomSql: &storepb.CustomSQLConfig{
			Sql:           "SELECT id FROM orders WHERE total < 0",
			FailThreshold: qualityTestPtr(8.5),
			WarnThreshold: qualityTestPtr(7.5),
		}}}},
		{name: "cel_expression", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CelExpression{CelExpression: &storepb.CelExpressionConfig{
			Expression:    "amount > 0",
			Columns:       []string{"amount"},
			SampleRows:    100,
			FailThreshold: qualityTestPtr(9.5),
			WarnThreshold: qualityTestPtr(8.5),
		}}}},
	}
}

func TestQualityRuleConfigRoundTrip(t *testing.T) {
	t.Parallel()

	filter := &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
		{
			Column: "dt",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE,
			Values: []string{"2026-01-01"},
		},
		{
			Column: "region",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IN,
			Values: []string{"cn", "us"},
		},
		{
			Column: "deleted_at",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NULL,
		},
	}}

	for _, testCase := range qualityConfigCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testCase.config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
			testCase.config.OnEmpty = storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS
			testCase.config.Filter = filter

			public := convertQualityRuleConfigToV1(testCase.config)
			require.NotNil(t, public)
			require.NotNil(t, public.GetConfig(), "the oneof arm must survive conversion")

			roundTripped := convertQualityRuleConfigFromV1(public)
			require.True(t, proto.Equal(testCase.config, roundTripped),
				"round trip changed the config: %v", roundTripped)
		})
	}
}

func TestConvertQualityRuleConfigNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, convertQualityRuleConfigToV1(nil))
	require.Nil(t, convertQualityRuleConfigFromV1(nil))
}

func TestConvertQualityFilterRoundTrip(t *testing.T) {
	t.Parallel()

	filter := &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
		{Column: "a", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ, Values: []string{"1"}},
		{Column: "b", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_NOT_IN, Values: []string{"x", "y"}},
	}}
	require.True(t, proto.Equal(filter, convertQualityFilterFromV1(convertQualityFilterToV1(filter))))
	require.Nil(t, convertQualityFilterToV1(nil))
	require.Nil(t, convertQualityFilterFromV1(nil))
}

func TestConvertQualityRule(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(2 * time.Hour)
	mutedUntil := createdAt.Add(24 * time.Hour)
	rule := &store.QualityRuleMessage{
		ResourceID:  "rule123",
		Title:       "Orders keep arriving",
		Description: "the table must not go quiet",
		Dimension:   storepb.QualityDimension_QUALITY_DIMENSION_TIMELINESS,
		RuleType:    storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		TargetGUID:  "instance_1;shop;public;orders",
		TargetType:  storepb.MetaType_TABLE,
		TargetPath: &storepb.QualityTargetPath{
			Instance: "instance_1",
			Database: "shop",
			Schema:   "public",
			Object:   "orders",
		},
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{
			TimestampColumn: "updated_at",
			MaxLagSeconds:   3600,
		}}},
		Severity:         storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL,
		State:            storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED,
		OwnerID:          qualityTestPtr(int32(7)),
		ScheduleInterval: 90 * time.Minute,
		MuteUntil:        &mutedUntil,
		Revision:         4,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
		Tags:             []string{"pii", "tier-1"},
	}

	converted := convertQualityRule("ws1", rule)
	require.NotNil(t, converted)
	require.Equal(t, "workspaces/ws1/qualityRules/rule123", converted.GetName())
	require.Equal(t, rule.Title, converted.GetTitle())
	require.Equal(t, rule.Description, converted.GetDescription())
	require.Equal(t, v1pb.QualityDimension_QUALITY_DIMENSION_TIMELINESS, converted.GetDimension())
	require.Equal(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, converted.GetRuleType())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_CRITICAL, converted.GetSeverity())
	require.Equal(t, v1pb.QualityRuleState_QUALITY_RULE_STATE_PAUSED, converted.GetState())
	require.Equal(t, "users/7", converted.GetOwner())
	require.Equal(t, 90*time.Minute, converted.GetScheduleInterval().AsDuration())
	require.Equal(t, int32(4), converted.GetRevision())
	require.Equal(t, createdAt, converted.GetCreateTime().AsTime())
	require.Equal(t, updatedAt, converted.GetUpdateTime().AsTime())
	require.Equal(t, mutedUntil, converted.GetMuteUntil().AsTime())
	require.Equal(t, []string{"pii", "tier-1"}, converted.GetTags())
	require.Equal(t, "instance_1;shop;public;orders", converted.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.MetaType_TABLE, converted.GetTarget().GetMetaType())
	require.Equal(t, "public", converted.GetTarget().GetPath().GetSchema())
	require.Equal(t, []string{"updated_at"}, converted.GetTarget().GetColumns())
	require.NotNil(t, converted.GetConfig().GetFreshness())

	require.Nil(t, convertQualityRule("ws1", nil))
}

func TestConvertQualityRuleWithoutOptionalFields(t *testing.T) {
	t.Parallel()

	rule := &store.QualityRuleMessage{ResourceID: "rule1"}
	converted := convertQualityRule("ws1", rule)
	require.Empty(t, converted.GetOwner())
	require.Nil(t, converted.GetMuteUntil())
	require.Nil(t, converted.GetTarget().GetPath())
	require.Empty(t, converted.GetTarget().GetColumns())
	require.NotNil(t, converted.GetScheduleInterval())
	require.Equal(t, time.Duration(0), converted.GetScheduleInterval().AsDuration())
}

func TestConvertQualityRun(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(3 * time.Minute)
	run := &store.QualityRunMessage{
		ID:                 11,
		ResourceID:         "run123",
		InstanceResourceID: "instance_1",
		Trigger:            storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_SCHEDULED,
		Status:             storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED,
		Outcome:            storepb.QualityStatus_QUALITY_STATUS_FAIL,
		Counts: &storepb.QualityRunCounts{
			Total: 4, Pass: 2, Fail: 1, Error: 1,
		},
		InitiatorID: qualityTestPtr(int32(9)),
		StartedAt:   startedAt,
		FinishedAt:  &finishedAt,
	}

	converted := convertQualityRun("ws1", run)
	require.NotNil(t, converted)
	require.Equal(t, "workspaces/ws1/qualityRuns/run123", converted.GetName())
	require.Equal(t, v1pb.QualityRunTrigger_QUALITY_RUN_TRIGGER_SCHEDULED, converted.GetTrigger())
	require.Equal(t, v1pb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED, converted.GetStatus())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, converted.GetOutcome())
	require.Equal(t, int32(4), converted.GetCounts().GetTotal())
	require.Equal(t, "users/9", converted.GetInitiator())
	require.Equal(t, "instances/instance_1", converted.GetInstance())
	require.Equal(t, startedAt, converted.GetStartTime().AsTime())
	require.Equal(t, finishedAt, converted.GetFinishTime().AsTime())

	pending := convertQualityRun("ws1", &store.QualityRunMessage{
		ResourceID: "run456",
		Status:     storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING,
		StartedAt:  startedAt,
	})
	require.Equal(t, v1pb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING, pending.GetStatus())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, pending.GetOutcome())
	require.Empty(t, pending.GetInitiator())
	require.Nil(t, pending.GetFinishTime())
	require.Nil(t, pending.GetCounts())

	require.Nil(t, convertQualityRun("ws1", nil))
}

func TestConvertQualityRuleResult(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(4 * time.Second)
	observed := 3.5
	result := &store.QualityRuleResultMessage{
		ID:                42,
		RunID:             11,
		RuleID:            7,
		RuleRevision:      2,
		RuleEngineVersion: "dq.v1",
		TargetGUID:        "instance_1;shop;public;orders",
		TargetType:        storepb.MetaType_TABLE,
		Status:            storepb.QualityStatus_QUALITY_STATUS_FAIL,
		ObservedValue:     &observed,
		ObservedKind:      storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		Thresholds: &storepb.QualityThresholds{
			FailValue:  qualityTestPtr(0.0),
			Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
			OnEmpty:    storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED,
		},
		FailedRows:  3,
		TotalRows:   100,
		Sampled:     true,
		SampleRows:  20,
		Truncated:   true,
		Sample:      &storepb.QualityFailureSamples{Samples: []*storepb.FailureSample{{Columns: []*storepb.ColumnValue{{Column: "id", Value: "1"}, {Column: "amount", Value: "-5"}}}}},
		ExecutedSQL: "SELECT COUNT(*) FROM orders",
		DurationMS:  1500,
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
	}

	converted := convertQualityRuleResult("ws1", result,
		"workspaces/ws1/qualityRuns/run123", "workspaces/ws1/qualityRules/rule123")
	require.NotNil(t, converted)
	require.Equal(t, "workspaces/ws1/qualityRuleResults/42", converted.GetName())
	require.Equal(t, "workspaces/ws1/qualityRuns/run123", converted.GetRun())
	require.Equal(t, "workspaces/ws1/qualityRules/rule123", converted.GetRule())
	require.Equal(t, int32(2), converted.GetRuleRevision())
	require.Equal(t, "dq.v1", converted.GetRuleEngineVersion())
	require.Equal(t, "instance_1;shop;public;orders", converted.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, converted.GetStatus())
	require.Equal(t, 3.5, converted.GetObservedValue())
	require.Equal(t, v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, converted.GetObservedKind())
	require.NotNil(t, converted.GetThresholds().FailValue, "an explicit zero threshold must stay set")
	require.Equal(t, 0.0, converted.GetThresholds().GetFailValue())
	require.Equal(t, v1pb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, converted.GetThresholds().GetComparison())
	require.Equal(t, int64(3), converted.GetFailedRows())
	require.Equal(t, int64(100), converted.GetTotalRows())
	require.True(t, converted.GetSampled())
	require.Equal(t, int64(20), converted.GetSampleRows())
	require.True(t, converted.GetTruncated())
	require.Len(t, converted.GetSamples(), 1)
	require.Len(t, converted.GetSamples()[0].GetColumns(), 2)
	require.Equal(t, "amount", converted.GetSamples()[0].GetColumns()[1].GetColumn())
	require.Equal(t, "-5", converted.GetSamples()[0].GetColumns()[1].GetValue())
	require.Equal(t, "SELECT COUNT(*) FROM orders", converted.GetExecutedSql())
	require.Equal(t, 1500*time.Millisecond, converted.GetDuration().AsDuration())
	require.Equal(t, startedAt, converted.GetStartTime().AsTime())
	require.Equal(t, finishedAt, converted.GetFinishTime().AsTime())

	// An absent observation must not invent a value; the status carries the
	// difference between "zero" and "not measured".
	absent := convertQualityRuleResult("ws1", &store.QualityRuleResultMessage{
		ID: 43, Status: storepb.QualityStatus_QUALITY_STATUS_ERROR, StartedAt: startedAt, FinishedAt: finishedAt,
	}, "", "")
	require.Equal(t, 0.0, absent.GetObservedValue())
	require.Nil(t, absent.GetThresholds())
	require.Empty(t, absent.GetSamples())
	require.Nil(t, convertQualityRuleResult("ws1", nil, "", ""))
}

func TestConvertQualityResultSummary(t *testing.T) {
	t.Parallel()

	finishedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	result := &store.QualityRuleResultMessage{
		ID:            42,
		RunID:         11,
		Status:        storepb.QualityStatus_QUALITY_STATUS_WARN,
		ObservedValue: qualityTestPtr(1.25),
		ObservedKind:  storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO,
		FailedRows:    5,
		TotalRows:     100,
		FinishedAt:    finishedAt,
	}
	converted := convertQualityResultSummary(result, "workspaces/ws1/qualityRuns/run123")
	require.NotNil(t, converted)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_WARN, converted.GetStatus())
	require.Equal(t, 1.25, converted.GetObservedValue())
	require.Equal(t, v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO, converted.GetObservedKind())
	require.Equal(t, int64(5), converted.GetFailedRows())
	require.Equal(t, int64(100), converted.GetTotalRows())
	require.Equal(t, "workspaces/ws1/qualityRuns/run123", converted.GetRun())
	require.Equal(t, finishedAt, converted.GetFinishTime().AsTime())
	require.Nil(t, convertQualityResultSummary(nil, ""))
}

func TestQualityResourceNameParsing(t *testing.T) {
	t.Parallel()

	workspace, ruleID, err := parseQualityRuleName("workspaces/ws1/qualityRules/rule123")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, "rule123", ruleID)

	workspace, runID, err := parseQualityRunName("workspaces/ws1/qualityRuns/run123")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, "run123", runID)

	workspace, resultID, err := parseQualityRuleResultName("workspaces/ws1/qualityRuleResults/42")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, "42", resultID)

	invalid := []string{
		"",
		"qualityRules/rule123",
		"workspaces/ws1/qualityRules",
		"workspaces/ws1/qualityRules/rule123/extra",
		"workspaces/ws1/qualityRules/",
		"workspaces//qualityRules/rule123",
		"workspaces/ws1/qualityRuns/run123",
		"workspaces/ws1/qualityRuleResults/42",
	}
	for _, name := range invalid {
		_, _, err := parseQualityRuleName(name)
		require.Error(t, err, "name %q", name)
	}
	require.Error(t, func() error {
		_, _, err := parseQualityRunName("workspaces/ws1/qualityRules/rule123")
		return err
	}())
	require.Error(t, func() error {
		_, _, err := parseQualityRuleResultName("workspaces/ws1/qualityRuns/run123")
		return err
	}())
}

func TestQualityResourceNameFormatting(t *testing.T) {
	t.Parallel()

	require.Equal(t, "workspaces/ws1/qualityRules/rule123", formatQualityRuleName("ws1", "rule123"))
	require.Equal(t, "workspaces/ws1/qualityRuns/run123", formatQualityRunName("ws1", "run123"))
	require.Equal(t, "workspaces/ws1/qualityRuleResults/42", formatQualityRuleResultName("ws1", "42"))
}

func TestIsCurrentWorkspace(t *testing.T) {
	t.Parallel()

	require.True(t, isCurrentWorkspace("ws1", "ws1"))
	require.False(t, isCurrentWorkspace("ws2", "ws1"))
	require.False(t, isCurrentWorkspace("", "ws1"))
}

func TestQualityRuleReferencedColumns(t *testing.T) {
	t.Parallel()

	filter := &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
		{Column: "dt", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE, Values: []string{"2026-01-01"}},
		// A column already named by the config must not be repeated.
		{Column: "email", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NOT_NULL},
	}}

	cases := []struct {
		name     string
		config   *storepb.QualityRuleConfig
		expected []string
	}{
		{name: "row_count", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: qualityTestPtr(int64(1))}}}, expected: []string{"dt", "email"}},
		{name: "null_percent", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{Column: "email"}}}, expected: []string{"email", "dt"}},
		{name: "unique_combination", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{UniqueCombination: &storepb.UniqueCombinationConfig{Columns: []string{"a", "b"}}}}, expected: []string{"a", "b", "dt", "email"}},
		{name: "cross_column", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{CrossColumn: &storepb.CrossColumnConfig{LeftColumn: "started_at", RightColumn: "finished_at"}}}, expected: []string{"started_at", "finished_at", "dt", "email"}},
		{name: "custom_sql_has_no_named_column", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT 1"}}}, expected: []string{"dt", "email"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			testCase.config.Filter = filter
			require.Equal(t, testCase.expected, qualityRuleReferencedColumns(testCase.config))
		})
	}
	require.Nil(t, qualityRuleReferencedColumns(nil))
}

func TestFormatQueryParameter(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	require.Equal(t, "NULL", formatQueryParameter(nil))
	require.Equal(t, "42", formatQueryParameter(int64(42)))
	require.Equal(t, "1.5", formatQueryParameter(1.5))
	require.Equal(t, "text", formatQueryParameter("text"))
	require.Equal(t, "2026-10-07T09:30:00Z", formatQueryParameter(at))
	require.Equal(t, "bytes", formatQueryParameter([]byte("bytes")))
}

func TestConvertQualityThresholdsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, convertQualityThresholdsToV1(nil))
	require.Nil(t, convertQualityRunCountsToV1(nil))
	require.Nil(t, convertQualityFailureSamplesToV1(nil))
	require.Nil(t, convertQualityTargetPathToV1(nil))
}

func TestDurationFromMilliseconds(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1500*time.Millisecond, durationFromMilliseconds(1500).AsDuration())
	require.Equal(t, time.Duration(0), durationFromMilliseconds(0).AsDuration())
	require.Equal(t, durationpb.New(2*time.Second).AsDuration(), durationFromMilliseconds(2000).AsDuration())
}

func TestConvertQualityIssue(t *testing.T) {
	t.Parallel()

	firstSeenAt := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	lastSeenAt := firstSeenAt.Add(48 * time.Hour)
	resolvedAt := lastSeenAt.Add(time.Hour)
	acknowledgedAt := firstSeenAt.Add(2 * time.Hour)
	mutedUntil := lastSeenAt.Add(24 * time.Hour)
	issue := &store.QualityIssueMessage{
		ID:               5,
		ResourceID:       "issue123",
		RuleID:           7,
		TargetGUID:       "instance_1;shop;public;orders",
		TargetType:       storepb.MetaType_TABLE,
		Status:           storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED,
		Severity:         storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		FirstSeenRunID:   qualityTestPtr(int64(11)),
		LastSeenRunID:    qualityTestPtr(int64(12)),
		FirstSeenAt:      firstSeenAt,
		LastSeenAt:       lastSeenAt,
		ResolvedAt:       &resolvedAt,
		ResolutionReason: storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED,
		ResolutionNote:   "backfilled by hand",
		AcknowledgedBy:   qualityTestPtr(int32(9)),
		AcknowledgedAt:   &acknowledgedAt,
		MutedUntil:       &mutedUntil,
		OccurrenceCount:  3,
	}
	runNames := map[int64]string{
		11: "workspaces/ws1/qualityRuns/run11",
		12: "workspaces/ws1/qualityRuns/run12",
	}

	converted := convertQualityIssue("ws1", issue, "workspaces/ws1/qualityRules/rule123", runNames)
	require.NotNil(t, converted)
	require.Equal(t, "workspaces/ws1/qualityIssues/issue123", converted.GetName())
	require.Equal(t, "workspaces/ws1/qualityRules/rule123", converted.GetRule())
	require.Equal(t, "instance_1;shop;public;orders", converted.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.MetaType_TABLE, converted.GetTarget().GetMetaType())
	require.Equal(t, v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED, converted.GetStatus())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR, converted.GetSeverity())
	require.Equal(t, "workspaces/ws1/qualityRuns/run11", converted.GetFirstSeenRun())
	require.Equal(t, "workspaces/ws1/qualityRuns/run12", converted.GetLastSeenRun())
	require.Equal(t, firstSeenAt, converted.GetFirstSeenTime().AsTime())
	require.Equal(t, lastSeenAt, converted.GetLastSeenTime().AsTime())
	require.Equal(t, resolvedAt, converted.GetResolvedTime().AsTime())
	require.Equal(t, v1pb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED, converted.GetResolutionReason())
	require.Equal(t, "backfilled by hand", converted.GetResolutionNote())
	require.Equal(t, "users/9", converted.GetAcknowledgedBy())
	require.Equal(t, acknowledgedAt, converted.GetAcknowledgedTime().AsTime())
	require.Equal(t, mutedUntil, converted.GetMutedUntil().AsTime())
	require.Equal(t, int32(3), converted.GetOccurrenceCount())

	// Nullable foreign keys and timestamps stay unset, not zero-valued.
	open := convertQualityIssue("ws1", &store.QualityIssueMessage{
		ResourceID:  "issue456",
		RuleID:      7,
		Status:      storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		FirstSeenAt: firstSeenAt,
		LastSeenAt:  firstSeenAt,
	}, "", nil)
	require.Empty(t, open.GetFirstSeenRun())
	require.Empty(t, open.GetLastSeenRun())
	require.Nil(t, open.GetResolvedTime())
	require.Nil(t, open.GetAcknowledgedTime())
	require.Nil(t, open.GetMutedUntil())
	require.Empty(t, open.GetAcknowledgedBy())
	require.Nil(t, convertQualityIssue("ws1", nil, "", nil))
}

func TestQualityIssueResourceName(t *testing.T) {
	t.Parallel()

	workspace, issueID, err := parseQualityIssueName("workspaces/ws1/qualityIssues/issue123")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, "issue123", issueID)

	for _, name := range []string{
		"",
		"qualityIssues/issue123",
		"workspaces/ws1/qualityIssues",
		"workspaces/ws1/qualityIssues/issue123/extra",
		"workspaces/ws1/qualityIssues/",
		"workspaces/ws1/qualityRules/rule123",
	} {
		_, _, err := parseQualityIssueName(name)
		require.Error(t, err, "name %q", name)
	}
	require.Equal(t, "workspaces/ws1/qualityIssues/issue123", formatQualityIssueName("ws1", "issue123"))
}

// TestResolveCreateScheduleInterval pins that a new rule without an interval takes
// the workspace default instead of being rejected, while an explicit interval wins.
func TestResolveCreateScheduleInterval(t *testing.T) {
	t.Parallel()

	settings := dataquality.Settings{DefaultScheduleInterval: 2 * time.Hour}
	require.Equal(t, 30*time.Minute, resolveCreateScheduleInterval(30*time.Minute, settings))
	require.Equal(t, 2*time.Hour, resolveCreateScheduleInterval(0, settings))
}

func TestQualitySuggestionPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	ruleDraft := &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Rule{
		Rule: &storepb.QualityRuleDraftConfig{
			Config: &storepb.QualityRuleConfig{
				Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
					Column:     "email",
					MaxNullPct: qualityTestPtr(0.5),
				}},
				ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT,
				OnEmpty:         storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED,
				Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
					{Column: "dt", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE, Values: []string{"2026-01-01"}},
				}},
			},
			Severity:   storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			Title:      "email must not be empty",
			Rationale:  "the column is the contact address",
			Confidence: 0.82,
		},
	}}
	label := &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Label{
		Label: &storepb.SemanticLabelSuggestion{
			Column:     "email",
			Label:      storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
			Confidence: 0.91,
			Evidence: &storepb.SemanticLabelEvidence{
				Columns:   []string{"customer_id"},
				Metrics:   []string{"null_rate=0.01"},
				Lineage:   []string{"join key of orders"},
				Rationale: "values look like addresses",
			},
			SuggestedRuleTypes: []storepb.QualityRuleType{
				storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
				storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			},
		},
	}}

	for name, payload := range map[string]*storepb.QualitySuggestionPayload{
		"rule_draft": ruleDraft,
		"label":      label,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			public := convertQualitySuggestionPayloadToV1(payload)
			require.NotNil(t, public)
			require.NotNil(t, public.GetPayload())
			roundTripped := convertQualitySuggestionPayloadFromV1(public)
			require.True(t, proto.Equal(payload, roundTripped),
				"round trip changed the payload: %v", roundTripped)
		})
	}

	require.Nil(t, convertQualitySuggestionPayloadToV1(nil))
	require.Nil(t, convertQualitySuggestionPayloadFromV1(nil))
}

func TestColumnProfileStatsRoundTrip(t *testing.T) {
	t.Parallel()

	stats := &storepb.ColumnProfile{
		TotalRows:         12000,
		NullCount:         12,
		DistinctCount:     950,
		DistinctEstimated: true,
		MinValue:          "a",
		MaxValue:          "zzz",
		MinLength:         qualityTestPtr(int64(1)),
		MaxLength:         qualityTestPtr(int64(64)),
		TopValues: []*storepb.ProfileValue{
			{Value: "open", Count: 8000},
			{Value: "closed", Count: 4000},
		},
		Patterns: []string{"alpha", "mixed"},
	}
	public := convertColumnProfileStatsToV1(stats)
	require.NotNil(t, public)
	require.True(t, public.GetDistinctEstimated())
	require.Equal(t, int64(64), public.GetMaxLength())
	require.Len(t, public.GetTopValues(), 2)
	require.Equal(t, int64(8000), public.GetTopValues()[0].GetCount())
	require.True(t, proto.Equal(stats, convertColumnProfileStatsFromV1(public)))

	// A nullable bound that is unset must stay unset through the round trip.
	sparse := &storepb.ColumnProfile{TotalRows: 1}
	sparsePublic := convertColumnProfileStatsToV1(sparse)
	require.Nil(t, sparsePublic.MinLength)
	require.Nil(t, sparsePublic.MaxLength)
	require.True(t, proto.Equal(sparse, convertColumnProfileStatsFromV1(sparsePublic)))

	require.Nil(t, convertColumnProfileStatsToV1(nil))
	require.Nil(t, convertColumnProfileStatsFromV1(nil))
}

func TestConvertColumnProfile(t *testing.T) {
	t.Parallel()

	profiledAt := time.Date(2026, time.October, 10, 8, 0, 0, 0, time.UTC)
	profile := &store.ColumnProfileMessage{
		TableGUID:     "instance_1;shop;public;orders",
		TableType:     storepb.MetaType_TABLE,
		ColumnName:    "email",
		Profile:       &storepb.ColumnProfile{TotalRows: 100, NullCount: 2},
		SampleMethod:  "sample",
		SampleRows:    1000,
		Truncated:     true,
		EngineVersion: "profiler.v1",
		ProfiledAt:    profiledAt,
	}
	converted := convertColumnProfileToV1(profile)
	require.NotNil(t, converted)
	require.Equal(t, "instance_1;shop;public;orders", converted.GetTableGuid())
	require.Equal(t, v1pb.MetaType_TABLE, converted.GetMetaType())
	require.Equal(t, "email", converted.GetColumn())
	require.Equal(t, int64(100), converted.GetProfile().GetTotalRows())
	require.Equal(t, "sample", converted.GetSampleMethod())
	require.Equal(t, int64(1000), converted.GetSampleRows())
	require.True(t, converted.GetTruncated())
	require.Equal(t, "profiler.v1", converted.GetEngineVersion())
	require.Equal(t, profiledAt, converted.GetProfiledAt().AsTime())
	require.Nil(t, convertColumnProfileToV1(nil))
}

func TestConvertColumnSemanticLabel(t *testing.T) {
	t.Parallel()

	acceptedAt := time.Date(2026, time.October, 10, 8, 0, 0, 0, time.UTC)
	updatedAt := acceptedAt.Add(time.Hour)
	label := &store.ColumnSemanticLabelMessage{
		TableGUID:  "instance_1;shop;public;orders",
		TableType:  storepb.MetaType_TABLE,
		ColumnName: "email",
		Label:      storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
		Source:     storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_LLM,
		Status:     storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED,
		Confidence: 0.9,
		Evidence: &storepb.SemanticLabelEvidence{
			Metrics:   []string{"null_rate=0.01"},
			Rationale: "address shape",
		},
		AcceptedBy: qualityTestPtr(int32(9)),
		AcceptedAt: &acceptedAt,
		UpdatedAt:  updatedAt,
	}
	converted := convertColumnSemanticLabelToV1(label)
	require.NotNil(t, converted)
	require.Equal(t, v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL, converted.GetLabel())
	require.Equal(t, v1pb.QualityLabelSource_QUALITY_LABEL_SOURCE_LLM, converted.GetSource())
	require.Equal(t, v1pb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED, converted.GetStatus())
	require.Equal(t, 0.9, converted.GetConfidence())
	require.Equal(t, "address shape", converted.GetEvidence().GetRationale())
	require.Equal(t, "users/9", converted.GetAcceptedBy())
	require.Equal(t, acceptedAt, converted.GetAcceptedTime().AsTime())
	require.Equal(t, updatedAt, converted.GetUpdateTime().AsTime())

	// A label without acceptance metadata keeps the nullable fields unset.
	pending := convertColumnSemanticLabelToV1(&store.ColumnSemanticLabelMessage{
		TableGUID: "guid", TableType: storepb.MetaType_TABLE, ColumnName: "c",
		Label:  storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ID,
		Source: storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_MANUAL,
		Status: storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_SUGGESTED,
	})
	require.Empty(t, pending.GetAcceptedBy())
	require.Nil(t, pending.GetAcceptedTime())
	require.Nil(t, convertColumnSemanticLabelToV1(nil))
}

func TestConvertQualitySuggestion(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 10, 8, 0, 0, 0, time.UTC)
	decidedAt := createdAt.Add(time.Hour)
	suggestion := &store.QualitySuggestionMessage{
		ID:         3,
		ResourceID: "suggestion123",
		Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
		Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		TargetGUID: "instance_1;shop;public;orders",
		TargetType: storepb.MetaType_TABLE,
		Payload: &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Label{
			Label: &storepb.SemanticLabelSuggestion{Column: "email"},
		}},
		Status:        storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
		Provider:      "openai",
		Model:         "gpt-4.1",
		PromptVersion: "dq.column_label.v1",
		CreatedAt:     createdAt,
		DecidedBy:     qualityTestPtr(int32(9)),
		DecidedAt:     &decidedAt,
		DecisionNote:  "matches the schema comment",
	}
	converted := convertQualitySuggestion("ws1", suggestion)
	require.NotNil(t, converted)
	require.Equal(t, "workspaces/ws1/qualitySuggestions/suggestion123", converted.GetName())
	require.Equal(t, v1pb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL, converted.GetKind())
	require.Equal(t, v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE, converted.GetSource())
	require.Equal(t, "instance_1;shop;public;orders", converted.GetTarget().GetMetaGuid())
	require.Equal(t, "email", converted.GetPayload().GetLabel().GetColumn())
	require.Equal(t, v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, converted.GetStatus())
	require.Equal(t, "openai", converted.GetProvider())
	require.Equal(t, "gpt-4.1", converted.GetModel())
	require.Equal(t, "dq.column_label.v1", converted.GetPromptVersion())
	require.Equal(t, createdAt, converted.GetCreateTime().AsTime())
	require.Equal(t, "users/9", converted.GetDecidedBy())
	require.Equal(t, decidedAt, converted.GetDecideTime().AsTime())
	require.Equal(t, "matches the schema comment", converted.GetDecisionNote())
	require.Nil(t, convertQualitySuggestion("ws1", nil))
}

func TestQualitySuggestionResourceName(t *testing.T) {
	t.Parallel()

	workspace, suggestionID, err := parseQualitySuggestionName("workspaces/ws1/qualitySuggestions/suggestion123")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, "suggestion123", suggestionID)

	for _, name := range []string{
		"",
		"qualitySuggestions/suggestion123",
		"workspaces/ws1/qualitySuggestions",
		"workspaces/ws1/qualitySuggestions/",
		"workspaces/ws1/qualitySuggestions/suggestion123/extra",
		"workspaces/ws1/qualityRules/rule123",
	} {
		_, _, err := parseQualitySuggestionName(name)
		require.Error(t, err, "name %q", name)
	}
	require.Equal(t,
		"workspaces/ws1/qualitySuggestions/suggestion123",
		formatQualitySuggestionName("ws1", "suggestion123"))
}
