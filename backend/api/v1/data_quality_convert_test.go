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
			NullValues:    []string{"N/A", "-"},
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
		{name: "schema_drift", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{
			ExpectedColumns: []*storepb.SchemaDriftColumn{
				{Name: "id", Type: "bigint", Nullable: qualityTestPtr(false)},
				{Name: "email", Type: "varchar(255)", Nullable: qualityTestPtr(true)},
			},
			CheckTypes:        qualityTestPtr(true),
			CheckOrder:        true,
			AllowExtraColumns: true,
			FailThreshold:     qualityTestPtr(0.0),
			WarnThreshold:     qualityTestPtr(1.5),
		}}}},
		{name: "group_check", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_GroupCheck{GroupCheck: &storepb.GroupCheckConfig{
			Columns:         []string{"region", "tier"},
			MinRowsPerGroup: qualityTestPtr(int64(10)),
			MaxRowsPerGroup: qualityTestPtr(int64(1000)),
			FailThreshold:   qualityTestPtr(2.5),
			WarnThreshold:   qualityTestPtr(1.5),
		}}}},
		{name: "aggregate", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Aggregate{Aggregate: &storepb.AggregateConfig{
			Column:     "amount",
			Function:   storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE,
			Percentile: qualityTestPtr(95.0),
			Min:        qualityTestPtr(1.5),
			Max:        qualityTestPtr(99.5),
			WarnMin:    qualityTestPtr(2.5),
			WarnMax:    qualityTestPtr(98.5),
			Inclusive:  true,
		}}}},
		{name: "cross_table", config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{
			RightTable:    "billing.invoices",
			LeftFunction:  storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
			RightFunction: storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
			RightColumn:   "total",
			Tolerance:     qualityTestPtr(0.5),
			FailThreshold: qualityTestPtr(1.5),
			WarnThreshold: qualityTestPtr(0.5),
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

func TestConvertQualityExecution(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(3 * time.Minute)
	observed := 3.5
	execution := &store.QualityExecutionMessage{
		ID:                42,
		RunID:             11,
		RuleID:            7,
		RuleRevision:      2,
		RuleEngineVersion: "dq.v1",
		TargetGUID:        "instance_1;shop;public;orders",
		TargetType:        storepb.MetaType_TABLE,
		TargetPath: &storepb.QualityTargetPath{
			Instance: "prod",
			Database: "shop",
			Schema:   "public",
			Object:   "orders",
		},
		InstanceResourceID: "instance_1",
		State:              storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
		Status:             storepb.QualityStatus_QUALITY_STATUS_FAIL,
		ObservedValue:      &observed,
		ObservedKind:       storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		Thresholds: &storepb.QualityThresholds{
			FailValue:  qualityTestPtr(0.0),
			Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
			OnEmpty:    storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED,
		},
		FailedRows: 3,
		TotalRows:  100,
		Sampled:    true,
		SampleRows: 20,
		Truncated:  true,
		Sample: &storepb.QualityFailureSamples{Samples: []*storepb.FailureSample{{Columns: []*storepb.ColumnValue{
			{Column: "id", Value: "1", IsNull: proto.Bool(false)},
			{Column: "amount", Value: "-5"},
			{Column: "note", IsNull: proto.Bool(true)},
			{Column: "legacy"},
		}}}},
		ExecutedSQL: "SELECT COUNT(*) FROM orders",
		DurationMS:  1500,
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
	}

	converted := convertQualityExecution("ws1", execution, "workspaces/ws1/qualityRules/rule123")
	require.NotNil(t, converted)
	require.Equal(t, "workspaces/ws1/qualityExecutions/42", converted.GetName())
	require.Equal(t, "workspaces/ws1/qualityRules/rule123", converted.GetRule())
	require.Equal(t, int32(2), converted.GetRuleRevision())
	require.Equal(t, "dq.v1", converted.GetRuleEngineVersion())
	require.Equal(t, "instance_1;shop;public;orders", converted.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.MetaType_TABLE, converted.GetTarget().GetMetaType())
	require.Equal(t, "orders", converted.GetTarget().GetPath().GetObject())
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, converted.GetState())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, converted.GetOutcome())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED, converted.GetTrigger())
	require.Empty(t, converted.GetInitiator())
	require.Equal(t, "instances/instance_1", converted.GetInstance())
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
	require.Len(t, converted.GetSamples()[0].GetColumns(), 4)
	require.NotNil(t, converted.GetSamples()[0].GetColumns()[0].IsNull)
	require.False(t, converted.GetSamples()[0].GetColumns()[0].GetIsNull(), "an explicit false marker survives the conversion")
	require.Equal(t, "amount", converted.GetSamples()[0].GetColumns()[1].GetColumn())
	require.Equal(t, "-5", converted.GetSamples()[0].GetColumns()[1].GetValue())
	require.Nil(t, converted.GetSamples()[0].GetColumns()[1].IsNull, "a legacy value stays unmarked")
	require.Equal(t, "note", converted.GetSamples()[0].GetColumns()[2].GetColumn())
	require.True(t, converted.GetSamples()[0].GetColumns()[2].GetIsNull())
	require.Nil(t, converted.GetSamples()[0].GetColumns()[3].IsNull, "a legacy row keeps its absence")
	require.Equal(t, "SELECT COUNT(*) FROM orders", converted.GetExecutedSql())
	require.Equal(t, 1500*time.Millisecond, converted.GetDuration().AsDuration())
	require.Equal(t, startedAt, converted.GetStartTime().AsTime())
	require.Equal(t, finishedAt, converted.GetFinishTime().AsTime())

	pending := convertQualityExecution("ws1", &store.QualityExecutionMessage{
		ID:          43,
		State:       storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		InitiatorID: qualityTestPtr(int32(9)),
		StartedAt:   startedAt,
	}, "")
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING, pending.GetState())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, pending.GetOutcome())
	require.Equal(t, "users/9", pending.GetInitiator())
	require.Nil(t, pending.GetFinishTime())
	require.Nil(t, pending.GetThresholds())
	// An absent observation must not invent a value; the status carries the
	// difference between "zero" and "not measured".
	require.Equal(t, 0.0, pending.GetObservedValue())
	require.Empty(t, pending.GetSamples())
	require.Nil(t, convertQualityExecution("ws1", nil, ""))
}

func TestConvertQualityExecutionSummary(t *testing.T) {
	t.Parallel()

	finishedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	execution := &store.QualityExecutionMessage{
		ID:            42,
		Status:        storepb.QualityStatus_QUALITY_STATUS_WARN,
		ObservedValue: qualityTestPtr(1.25),
		ObservedKind:  storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO,
		FailedRows:    5,
		TotalRows:     100,
		FinishedAt:    finishedAt,
	}
	converted := convertQualityExecutionSummary("ws1", execution)
	require.NotNil(t, converted)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_WARN, converted.GetStatus())
	require.Equal(t, 1.25, converted.GetObservedValue())
	require.Equal(t, v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO, converted.GetObservedKind())
	require.Equal(t, int64(5), converted.GetFailedRows())
	require.Equal(t, int64(100), converted.GetTotalRows())
	require.Equal(t, "workspaces/ws1/qualityExecutions/42", converted.GetExecution())
	require.Equal(t, finishedAt, converted.GetFinishTime().AsTime())
	require.Nil(t, convertQualityExecutionSummary("ws1", nil))
}

func TestQualityResourceNameParsing(t *testing.T) {
	t.Parallel()

	workspace, ruleID, err := parseQualityRuleName("workspaces/ws1/qualityRules/rule123")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, "rule123", ruleID)

	workspace, executionID, err := parseQualityExecutionID("workspaces/ws1/qualityExecutions/42")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, int64(42), executionID)

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
	for _, name := range []string{
		"",
		"workspaces/ws1/qualityExecutions",
		"workspaces/ws1/qualityExecutions/",
		"workspaces/ws1/qualityExecutions/0",
		"workspaces/ws1/qualityExecutions/-1",
		"workspaces/ws1/qualityExecutions/abc",
		"workspaces/ws1/qualityExecutions/42/extra",
		"workspaces/ws1/qualityRuns/run123",
	} {
		_, _, err := parseQualityExecutionID(name)
		require.Error(t, err, "name %q", name)
	}
}

func TestQualityResourceNameFormatting(t *testing.T) {
	t.Parallel()

	require.Equal(t, "workspaces/ws1/qualityRules/rule123", formatQualityRuleName("ws1", "rule123"))
	require.Equal(t, "workspaces/ws1/qualityExecutions/42", formatQualityExecutionName("ws1", 42))
	require.Equal(t, "workspaces/ws1/qualityIssues/issue123", formatQualityIssueName("ws1", "issue123"))
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
	require.Nil(t, convertQualityStatusCountsToV1(nil))
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
		ID:                   5,
		ResourceID:           "issue123",
		RuleID:               7,
		TargetGUID:           "instance_1;shop;public;orders",
		TargetType:           storepb.MetaType_TABLE,
		Status:               storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED,
		Severity:             storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		FirstSeenExecutionID: qualityTestPtr(int64(101)),
		LastSeenExecutionID:  qualityTestPtr(int64(102)),
		FirstSeenAt:          firstSeenAt,
		LastSeenAt:           lastSeenAt,
		ResolvedAt:           &resolvedAt,
		ResolutionReason:     storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED,
		ResolutionNote:       "backfilled by hand",
		AcknowledgedBy:       qualityTestPtr(int32(9)),
		AcknowledgedAt:       &acknowledgedAt,
		MutedUntil:           &mutedUntil,
		OccurrenceCount:      3,
	}
	converted := convertQualityIssue("ws1", issue, "workspaces/ws1/qualityRules/rule123")
	require.NotNil(t, converted)
	require.Equal(t, "workspaces/ws1/qualityIssues/issue123", converted.GetName())
	require.Equal(t, "workspaces/ws1/qualityRules/rule123", converted.GetRule())
	require.Equal(t, "instance_1;shop;public;orders", converted.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.MetaType_TABLE, converted.GetTarget().GetMetaType())
	require.Equal(t, v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED, converted.GetStatus())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR, converted.GetSeverity())
	require.Equal(t, "workspaces/ws1/qualityExecutions/101", converted.GetFirstSeenExecution())
	require.Equal(t, "workspaces/ws1/qualityExecutions/102", converted.GetLastSeenExecution())
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
	}, "")
	require.Empty(t, open.GetFirstSeenExecution())
	require.Empty(t, open.GetLastSeenExecution())
	require.Nil(t, open.GetResolvedTime())
	require.Nil(t, open.GetAcknowledgedTime())
	require.Nil(t, open.GetMutedUntil())
	require.Empty(t, open.GetAcknowledgedBy())
	require.Nil(t, convertQualityIssue("ws1", nil, ""))
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

func TestFillSchemaDriftSnapshot(t *testing.T) {
	t.Parallel()

	columns := []dataquality.ColumnInfo{
		{Name: "id", Type: "bigint", Kind: dataquality.ColumnKindNumeric, Nullable: false},
		{Name: "email", Type: "varchar(255)", Kind: dataquality.ColumnKindString, Nullable: true},
		{Name: "amount", Type: "decimal(10,2)", Kind: dataquality.ColumnKindNumeric, Nullable: true},
	}

	t.Run("fills an empty snapshot in registry order", func(t *testing.T) {
		t.Parallel()
		config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{}}}
		require.True(t, fillSchemaDriftSnapshot(config, columns))

		expected := config.GetSchemaDrift().GetExpectedColumns()
		require.Len(t, expected, 3)
		require.Equal(t, "id", expected[0].GetName())
		require.Equal(t, "bigint", expected[0].GetType())
		// An API-filled snapshot records nullability explicitly: an absent field
		// would tell the evaluator not to judge it.
		require.NotNil(t, expected[0].Nullable)
		require.False(t, expected[0].GetNullable())
		require.Equal(t, "email", expected[1].GetName())
		require.NotNil(t, expected[1].Nullable)
		require.True(t, expected[1].GetNullable())
		require.Equal(t, "amount", expected[2].GetName())
		require.NotNil(t, expected[2].Nullable)
		require.True(t, expected[2].GetNullable())
	})

	t.Run("keeps a user-provided snapshot untouched", func(t *testing.T) {
		t.Parallel()
		config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{
			ExpectedColumns: []*storepb.SchemaDriftColumn{{Name: "legacy_id", Type: "int"}},
		}}}
		require.False(t, fillSchemaDriftSnapshot(config, columns))
		require.Len(t, config.GetSchemaDrift().GetExpectedColumns(), 1)
		require.Equal(t, "legacy_id", config.GetSchemaDrift().GetExpectedColumns()[0].GetName())
	})

	t.Run("no-op without a drift config or columns", func(t *testing.T) {
		t.Parallel()
		require.False(t, fillSchemaDriftSnapshot(nil, columns))

		otherArm := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: qualityTestPtr(int64(1))}}}
		require.False(t, fillSchemaDriftSnapshot(otherArm, columns))

		emptyTarget := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{}}}
		require.False(t, fillSchemaDriftSnapshot(emptyTarget, nil))
		require.Empty(t, emptyTarget.GetSchemaDrift().GetExpectedColumns())
	})
}

func TestSchemaDriftOptionalCheckTypesRoundTrip(t *testing.T) {
	t.Parallel()

	// An unset check_types means "true" semantically, so the conversion must keep
	// it unset rather than materializing a false the evaluator would read as
	// "ignore type changes".
	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{
		ExpectedColumns: []*storepb.SchemaDriftColumn{{Name: "id", Type: "bigint"}},
	}}}
	require.Nil(t, config.GetSchemaDrift().CheckTypes)

	roundTripped := convertQualityRuleConfigFromV1(convertQualityRuleConfigToV1(config))
	require.Nil(t, roundTripped.GetSchemaDrift().CheckTypes)
	require.True(t, proto.Equal(config, roundTripped))
}

// Nullability presence is part of the snapshot contract: unset means "do not
// judge", while an explicit false is a real NOT NULL expectation. The v1<->store
// conversion must not collapse the two.
func TestSchemaDriftNullablePresenceRoundTrip(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{
		ExpectedColumns: []*storepb.SchemaDriftColumn{
			{Name: "unjudged", Type: "text"},
			{Name: "required", Type: "bigint", Nullable: qualityTestPtr(false)},
			{Name: "optional", Type: "text", Nullable: qualityTestPtr(true)},
		},
	}}}

	public := convertQualityRuleConfigToV1(config)
	require.Len(t, public.GetConfig().(*v1pb.QualityRuleConfig_SchemaDrift).SchemaDrift.GetExpectedColumns(), 3)
	publicColumns := public.GetConfig().(*v1pb.QualityRuleConfig_SchemaDrift).SchemaDrift.GetExpectedColumns()
	require.Nil(t, publicColumns[0].Nullable, "an unset snapshot column must stay unset over the wire")
	require.NotNil(t, publicColumns[1].Nullable)
	require.False(t, publicColumns[1].GetNullable())
	require.NotNil(t, publicColumns[2].Nullable)
	require.True(t, publicColumns[2].GetNullable())

	roundTripped := convertQualityRuleConfigFromV1(public)
	require.True(t, proto.Equal(config, roundTripped))
	stored := roundTripped.GetSchemaDrift().GetExpectedColumns()
	require.Nil(t, stored[0].Nullable)
	require.NotNil(t, stored[1].Nullable)
	require.False(t, stored[1].GetNullable())
	require.True(t, stored[2].GetNullable())
}
