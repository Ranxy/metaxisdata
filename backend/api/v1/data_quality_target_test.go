package v1

import (
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/common/schedule"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// An unknown or unspecified order falls back to attention: the directory always
// has a deterministic order, even for an older client.
func TestQualityTargetOrderFromV1(t *testing.T) {
	t.Parallel()

	require.Equal(t, store.QualityTargetOrderAttention, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_UNSPECIFIED))
	require.Equal(t, store.QualityTargetOrderAttention, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_ATTENTION))
	require.Equal(t, store.QualityTargetOrderName, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_NAME))
	require.Equal(t, store.QualityTargetOrderRuleCount, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_RULE_COUNT))
	require.Equal(t, store.QualityTargetOrderLastRun, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_LAST_RUN))
	require.Equal(t, store.QualityTargetOrderAttention, qualityTargetOrderFromV1(v1pb.QualityTargetOrder(99)))
}

// The target header's worst status must agree with the store aggregate: ERROR
// outranks FAIL, and a status that observed nothing ranks below PASS.
func TestWorstQualityStatus(t *testing.T) {
	t.Parallel()

	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_ERROR,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_FAIL, v1pb.QualityStatus_QUALITY_STATUS_ERROR))
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_PASS, v1pb.QualityStatus_QUALITY_STATUS_FAIL))
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_WARN,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, v1pb.QualityStatus_QUALITY_STATUS_WARN))
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_SKIPPED, v1pb.QualityStatus_QUALITY_STATUS_PASS))
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED))
	// NOT_EVALUATED outranks SKIPPED: the check ran and had nothing to judge,
	// whereas a skipped check never ran.
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_SKIPPED, v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED))
}

// The worst-status filter accepts the six real verdicts, keeps UNSPECIFIED as "no
// filter", and refuses a value outside the enum instead of silently matching the
// "never run" rank (the ListQualityExecutions list already behaves this way).
func TestQualityTargetWorstStatusFromV1(t *testing.T) {
	t.Parallel()

	for _, status := range []v1pb.QualityStatus{
		v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		v1pb.QualityStatus_QUALITY_STATUS_PASS,
		v1pb.QualityStatus_QUALITY_STATUS_WARN,
		v1pb.QualityStatus_QUALITY_STATUS_FAIL,
		v1pb.QualityStatus_QUALITY_STATUS_ERROR,
		v1pb.QualityStatus_QUALITY_STATUS_SKIPPED,
		v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
	} {
		mapped, err := qualityTargetWorstStatusFromV1(status)
		require.NoError(t, err, status.String())
		require.Equal(t, storepb.QualityStatus(status), mapped)
	}

	_, err := qualityTargetWorstStatusFromV1(v1pb.QualityStatus(99))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// The list filters map resource names and raw ids alike; the rule-only filters
// (owner, rule type) have no target meaning and stay unset.
func TestApplyQualityTargetListFilters(t *testing.T) {
	t.Parallel()

	find := &store.FindQualityTargetMessage{}
	applyQualityTargetListFilters(find, &v1pb.ListQualityTargetsRequest{
		Instance: "instances/instance-1",
		Database: "shop",
		Schema:   "public",
		Table:    "orders",
		Search:   "  orders  ",
	})
	require.Equal(t, "instance-1", find.Instance)
	require.Equal(t, "shop", find.Database)
	require.Equal(t, "public", find.Schema)
	require.Equal(t, "orders", find.TablePrefix)
	require.NotNil(t, find.Search)
	require.Equal(t, "orders", *find.Search)
}

// A summary keeps the snapshot path, the counts the directory renders, and the
// target's newest run slice.
func TestConvertQualityTargetSummary(t *testing.T) {
	t.Parallel()

	finishedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	nextRun := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	qualityScore := 83.5
	passRate := 0.75
	summary := convertQualityTargetSummary("ws1", &store.QualityTargetMessage{
		GUID:                 "instance-1;shop;public;orders",
		Type:                 storepb.MetaType_TABLE,
		Path:                 &storepb.QualityTargetPath{Instance: "instance-1", Database: "shop", Schema: "public", Object: "orders"},
		RuleCount:            3,
		EnabledCount:         2,
		PausedCount:          1,
		ColumnCount:          2,
		OpenIssueCount:       1,
		PendingDraftCount:    4,
		PendingLabelCount:    5,
		WorstStatus:          storepb.QualityStatus_QUALITY_STATUS_FAIL,
		SchemaChanged:        true,
		Orphaned:             true,
		RunCount:             2,
		ScheduleInterval:     2 * time.Hour,
		NextRunAt:            &nextRun,
		QualityScore:         &qualityScore,
		ScoredRuleCount:      2,
		RecentPassRate:       &passRate,
		RecentExecutionCount: 3,
		LatestRun: &store.QualityTargetRunSummaryMessage{
			TargetGUID: "instance-1;shop;public;orders",
			TargetType: storepb.MetaType_TABLE,
			Run: &store.QualityRunMessage{
				ID:              42,
				TargetGUID:      "instance-1;shop;public;orders",
				TargetType:      storepb.MetaType_TABLE,
				Trigger:         storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
				Status:          storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
				Outcome:         storepb.QualityStatus_QUALITY_STATUS_FAIL,
				Counts:          &storepb.QualityStatusCounts{Total: 3, Fail: 1, Pass: 2},
				RuleRevisionMin: 1,
				RuleRevisionMax: 2,
				StartedAt:       finishedAt.Add(-time.Minute),
			},
		},
	})
	require.Equal(t, "orders", summary.GetTarget().GetPath().GetObject())
	require.Equal(t, v1pb.MetaType_TABLE, summary.GetTarget().GetMetaType())
	require.Equal(t, int32(3), summary.GetRuleCount())
	require.Equal(t, int32(2), summary.GetEnabledRuleCount())
	require.Equal(t, int32(1), summary.GetPausedRuleCount())
	require.Equal(t, int32(2), summary.GetColumnCount())
	require.Equal(t, int32(1), summary.GetOpenIssueCount())
	require.Equal(t, int32(4), summary.GetPendingDraftCount())
	require.Equal(t, int32(5), summary.GetPendingLabelCount())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, summary.GetWorstStatus())
	require.True(t, summary.GetSchemaChanged())
	require.True(t, summary.GetOrphaned())
	require.Equal(t, int32(2), summary.GetRunCount())
	require.NotNil(t, summary.GetLatestRun())
	require.Equal(t, "workspaces/ws1/qualityRuns/42", summary.GetLatestRun().GetName())
	require.Equal(t, "instance-1;shop;public;orders", summary.GetLatestRun().GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL, summary.GetLatestRun().GetTrigger())
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, summary.GetLatestRun().GetState())
	require.Equal(t, int32(3), summary.GetLatestRun().GetCounts().GetTotal())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, summary.GetLatestRun().GetOutcome())
	require.Equal(t, int32(1), summary.GetLatestRun().GetRuleRevisionMin())
	require.Equal(t, int32(2), summary.GetLatestRun().GetRuleRevisionMax())
	require.Equal(t, 2*time.Hour, summary.GetSchedule().GetScheduleInterval().AsDuration())
	require.Equal(t, nextRun, summary.GetSchedule().GetNextRunTime().AsTime())
	require.InDelta(t, 83.5, summary.GetQualityScore(), 1e-9)
	require.Equal(t, int32(2), summary.GetScoredRuleCount())
	require.InDelta(t, 0.75, summary.GetRecentPassRate(), 1e-9, "the pass rate is a 0-1 ratio")
	require.Equal(t, int32(3), summary.GetRecentExecutionCount())

	// A target that has never run carries zero values rather than an empty slice.
	never := convertQualityTargetSummary("ws1", &store.QualityTargetMessage{
		GUID: "instance-1;shop;public;orders",
	})
	require.Equal(t, int32(0), never.GetRunCount())
	require.Nil(t, never.GetLatestRun())
	require.Nil(t, never.GetSchedule(), "a target without a schedule row has no schedule")
}

// A cron target's directory row carries the expression and its resolved zone, and
// still names the nominal cadence the store derived from the expression.
func TestConvertQualityTargetSummaryCronSchedule(t *testing.T) {
	t.Parallel()

	nextRun := time.Date(2026, time.October, 8, 2, 0, 0, 0, time.UTC)
	summary := convertQualityTargetSummary("ws1", &store.QualityTargetMessage{
		GUID:             "instance-1;shop;public;orders",
		Type:             storepb.MetaType_TABLE,
		ScheduleInterval: 24 * time.Hour,
		ScheduleCron:     "0 2 * * *",
		ScheduleTimeZone: "Asia/Shanghai",
		NextRunAt:        &nextRun,
	})
	scheduleRow := summary.GetSchedule()
	require.NotNil(t, scheduleRow)
	require.Equal(t, 24*time.Hour, scheduleRow.GetScheduleInterval().AsDuration())
	require.Equal(t, "0 2 * * *", scheduleRow.GetCron())
	require.Equal(t, "Asia/Shanghai", scheduleRow.GetTimezone())
	require.Equal(t, nextRun, scheduleRow.GetNextRunTime().AsTime())
}

// A schedule update accepts exactly one of an interval and a cron expression. An
// empty zone resolves to the workspace's check zone and the resolved name is what
// gets stored, while a too-frequent expression or an unknown IANA zone is refused.
func TestQualityScheduleSpecFromV1(t *testing.T) {
	t.Parallel()

	// The old interval-only request keeps working and needs no zone.
	spec, err := qualityScheduleSpecFromV1(&v1pb.UpdateQualityTargetScheduleRequest{
		ScheduleInterval: durationpb.New(2 * time.Hour),
	}, "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, schedule.Spec{Interval: 2 * time.Hour}, spec)

	spec, err = qualityScheduleSpecFromV1(&v1pb.UpdateQualityTargetScheduleRequest{
		Cron: "0 2 * * *",
	}, "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, schedule.Spec{Cron: "0 2 * * *", TimeZone: "Asia/Shanghai"}, spec,
		"an empty timezone resolves to the workspace's check zone")

	spec, err = qualityScheduleSpecFromV1(&v1pb.UpdateQualityTargetScheduleRequest{
		Cron: "0 2 * * *", Timezone: "UTC",
	}, "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, schedule.Spec{Cron: "0 2 * * *", TimeZone: "UTC"}, spec,
		"an explicit zone wins over the workspace default")

	rejected := map[string]*v1pb.UpdateQualityTargetScheduleRequest{
		"both modes":         {ScheduleInterval: durationpb.New(2 * time.Hour), Cron: "0 2 * * *"},
		"neither mode":       {},
		"interval too short": {ScheduleInterval: durationpb.New(14 * time.Minute)},
		"interval too long":  {ScheduleInterval: durationpb.New(8 * 24 * time.Hour)},
		"negative interval":  {ScheduleInterval: durationpb.New(-time.Hour)},
		"cron too frequent":  {Cron: "* * * * *", Timezone: "UTC"},
		"cron malformed":     {Cron: "0 2 * *", Timezone: "UTC"},
		"unknown IANA zone":  {Cron: "0 2 * * *", Timezone: "Mars/Olympus"},
	}
	for name, request := range rejected {
		_, err := qualityScheduleSpecFromV1(request, "Asia/Shanghai")
		require.Error(t, err, name)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), name)
	}
}

// qualityTargetTestRule builds one rule in the given lifecycle state with the
// type and severity the score reads.
func qualityTargetTestRule(id int64, state storepb.QualityRuleState, ruleType storepb.QualityRuleType,
	severity storepb.QualitySeverity) *store.QualityRuleMessage {
	return &store.QualityRuleMessage{
		ID:         id,
		TargetGUID: "instance-1;shop;;orders",
		TargetType: storepb.MetaType_TABLE,
		State:      state,
		RuleType:   ruleType,
		Severity:   severity,
	}
}

// A target's score is the weighted mean of its enabled rules' latest judged
// results: a paused rule, a never-run rule and an operational error are not
// scored, and a row-level check scores by its passed rows.
func TestQualityTargetScores(t *testing.T) {
	t.Parallel()

	rules := []*store.QualityRuleMessage{
		qualityTargetTestRule(1, storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, storepb.QualitySeverity_QUALITY_SEVERITY_WARNING),
		qualityTargetTestRule(2, storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR),
		qualityTargetTestRule(3, storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL),
		qualityTargetTestRule(4, storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR),
		qualityTargetTestRule(5, storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, storepb.QualitySeverity_QUALITY_SEVERITY_WARNING),
		qualityTargetTestRule(6, storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, storepb.QualitySeverity_QUALITY_SEVERITY_WARNING),
	}
	latest := map[int64]*store.QualityExecutionMessage{
		1: qualityTargetTestExecution(1, storepb.QualityStatus_QUALITY_STATUS_PASS),
		2: qualityTargetTestExecution(2, storepb.QualityStatus_QUALITY_STATUS_FAIL),
		3: qualityTargetTestExecution(3, storepb.QualityStatus_QUALITY_STATUS_PASS),
		4: qualityTargetTestExecution(4, storepb.QualityStatus_QUALITY_STATUS_ERROR),
	}
	latest[2].FailedRows, latest[2].TotalRows = 1, 10

	scores := qualityTargetScores(rules, latest)
	key := store.MetaGUIDKey{GUID: "instance-1;shop;;orders", ObjectType: storepb.MetaType_TABLE}
	require.Contains(t, scores, key)
	require.Equal(t, int32(3), scores[key].scored, "the paused, error and never-run rules are unscored")
	require.NotNil(t, scores[key].score)
	// (1*1 + 2*0.9 + 3*1) / 6 = 0.9667.
	require.InDelta(t, 100*5.8/6, *scores[key].score, 1e-9)

	none := qualityTargetScores(rules, map[int64]*store.QualityExecutionMessage{})
	require.Empty(t, none, "a target with no judged result has no score at all")
}

// qualityTargetTestExecution builds one completed result for the target the test
// rules watch; the score helper only trusts an execution that judged the rule's
// current target.
func qualityTargetTestExecution(ruleID int64, status storepb.QualityStatus) *store.QualityExecutionMessage {
	return &store.QualityExecutionMessage{
		RuleID:     ruleID,
		TargetGUID: "instance-1;shop;;orders",
		TargetType: storepb.MetaType_TABLE,
		Status:     status,
	}
}

// A moved rule's newest execution judged its old target, so it contributes no
// score to the target it now watches.
func TestQualityTargetScoresSkipsMovedRuleExecution(t *testing.T) {
	t.Parallel()

	rules := []*store.QualityRuleMessage{
		qualityTargetTestRule(1, storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, storepb.QualitySeverity_QUALITY_SEVERITY_WARNING),
	}
	execution := qualityTargetTestExecution(1, storepb.QualityStatus_QUALITY_STATUS_PASS)
	execution.TargetGUID = "instance-1;shop;;previous"
	require.Empty(t, qualityTargetScores(rules, map[int64]*store.QualityExecutionMessage{1: execution}),
		"an execution against the previous target is not a score")
}

// The recent pass rate is PASS over the judged verdicts in the window: WARN and
// FAIL count in the denominator. The store's tally carries only those three, so
// ERROR, SKIPPED and NOT_EVALUATED never reach the ratio.
func TestQualityRecentPassRate(t *testing.T) {
	t.Parallel()

	rate, judgedCount := qualityRecentPassRate(store.QualityTargetExecutionTally{Pass: 1, Warn: 1, Fail: 1})
	require.NotNil(t, rate)
	require.InDelta(t, 1.0/3.0, *rate, 1e-9)
	require.Equal(t, int32(3), judgedCount)

	// All-pass and all-fail boundaries.
	rate, judgedCount = qualityRecentPassRate(store.QualityTargetExecutionTally{Pass: 4})
	require.NotNil(t, rate)
	require.InDelta(t, 1.0, *rate, 1e-9)
	require.Equal(t, int32(4), judgedCount)
	rate, judgedCount = qualityRecentPassRate(store.QualityTargetExecutionTally{Fail: 2})
	require.NotNil(t, rate)
	require.InDelta(t, 0.0, *rate, 1e-9)
	require.Equal(t, int32(2), judgedCount)

	rate, judgedCount = qualityRecentPassRate(store.QualityTargetExecutionTally{})
	require.Nil(t, rate, "no judged execution means no pass rate")
	require.Zero(t, judgedCount)
}

// The exact-target selection is the metadata browser's batch read. It accepts only
// keys a rule could name, deduplicates them and keeps the request order, because
// the client zips the answer back onto the rows it is showing.
func TestQualityTargetSelectionFromV1(t *testing.T) {
	t.Parallel()

	selection, err := qualityTargetSelectionFromV1([]*v1pb.QualityTargetKey{
		{TargetGuid: "instance-1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
		{TargetGuid: "  instance-1;shop;;order_items  ", MetaType: v1pb.MetaType_VIEW},
		// A duplicate key is one target, not two rows.
		{TargetGuid: "instance-1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
	})
	require.NoError(t, err)
	require.Equal(t, []store.MetaGUIDKey{
		{GUID: "instance-1;shop;;orders", ObjectType: storepb.MetaType_TABLE},
		{GUID: "instance-1;shop;;order_items", ObjectType: storepb.MetaType_VIEW},
	}, selection, "duplicates collapse and the request order is kept")

	// A GUID alone does not identify the registry row, so the type is required.
	_, err = qualityTargetSelectionFromV1([]*v1pb.QualityTargetKey{
		{TargetGuid: "instance-1;shop;;orders"},
	})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = qualityTargetSelectionFromV1([]*v1pb.QualityTargetKey{
		{TargetGuid: "  ", MetaType: v1pb.MetaType_TABLE},
	})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Only the four object types a rule can watch carry a signal; a database or
	// column selection is a client bug, not an empty answer.
	_, err = qualityTargetSelectionFromV1([]*v1pb.QualityTargetKey{
		{TargetGuid: "instance-1;shop", MetaType: v1pb.MetaType_DATABASE},
	})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// The selection is bounded: one request cannot ask for the whole registry.
func TestQualityTargetSelectionFromV1Limit(t *testing.T) {
	t.Parallel()

	keys := make([]*v1pb.QualityTargetKey, 0, qualityTargetSelectionLimit+1)
	for i := 0; i <= qualityTargetSelectionLimit; i++ {
		keys = append(keys, &v1pb.QualityTargetKey{
			TargetGuid: "instance-1;shop;;orders" + strconv.Itoa(i),
			MetaType:   v1pb.MetaType_TABLE,
		})
	}
	_, err := qualityTargetSelectionFromV1(keys)
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Exactly at the bound is allowed, so a page of 200 still answers in one call.
	selection, err := qualityTargetSelectionFromV1(keys[:qualityTargetSelectionLimit])
	require.NoError(t, err)
	require.Len(t, selection, qualityTargetSelectionLimit)
}

// The answer is reordered onto the request so the caller can zip each signal back
// onto its row. A selected target with no live rule produces no row: it is not
// monitored, not "failed to load".
func TestOrderQualityTargetSelection(t *testing.T) {
	t.Parallel()

	selection := []store.MetaGUIDKey{
		{GUID: "a", ObjectType: storepb.MetaType_TABLE},
		{GUID: "unmonitored", ObjectType: storepb.MetaType_TABLE},
		{GUID: "b", ObjectType: storepb.MetaType_VIEW},
	}
	ordered := orderQualityTargetSelection([]*store.QualityTargetMessage{
		{GUID: "b", Type: storepb.MetaType_VIEW},
		{GUID: "a", Type: storepb.MetaType_TABLE},
	}, selection)
	require.Len(t, ordered, 2)
	require.Equal(t, "a", ordered[0].GUID)
	require.Equal(t, "b", ordered[1].GUID)

	require.Empty(t, orderQualityTargetSelection(nil, selection))
}

// Column coverage counts from one registry read: the denominator is every column
// the registry holds for the target, the numerator only the enabled checks' columns
// that still exist there. A dropped or renamed column therefore covers nothing, so
// the numerator can never exceed the denominator.
func TestQualityTargetColumnCoverage(t *testing.T) {
	t.Parallel()

	registry := map[string]struct{}{"id": {}, "email": {}, "created_at": {}}
	checked := map[string]struct{}{"id": {}, "email": {}}
	covered, total := qualityTargetColumnCoverage(registry, checked)
	require.Equal(t, int32(2), covered)
	require.Equal(t, int32(3), total)

	// A check that still names a column the schema sync dropped covers nothing.
	covered, total = qualityTargetColumnCoverage(registry, map[string]struct{}{"dropped": {}})
	require.Equal(t, int32(0), covered)
	require.Equal(t, int32(3), total)

	// No registry row (an orphaned target) is 0/0, not 0/100.
	covered, total = qualityTargetColumnCoverage(nil, checked)
	require.Zero(t, covered)
	require.Zero(t, total)

	// A target with columns but no column-level check is covered 0 of N.
	covered, total = qualityTargetColumnCoverage(registry, nil)
	require.Zero(t, covered)
	require.Equal(t, int32(3), total)
}

// The conversion carries the coverage pair through to the API message, so the
// directory cell and the asset badge read the same two numbers.
func TestConvertQualityTargetSummaryColumnCoverage(t *testing.T) {
	t.Parallel()

	summary := convertQualityTargetSummary("ws1", &store.QualityTargetMessage{
		GUID:               "instance-1;shop;;orders",
		Type:               storepb.MetaType_TABLE,
		CoveredColumnCount: 2,
		TotalColumnCount:   7,
	})
	require.Equal(t, int32(2), summary.GetCoveredColumnCount())
	require.Equal(t, int32(7), summary.GetTotalColumnCount())

	// A target that never resolved reports 0/0 rather than a full denominator.
	orphan := convertQualityTargetSummary("ws1", &store.QualityTargetMessage{
		GUID: "instance-1;shop;;orders",
		Type: storepb.MetaType_TABLE,
	})
	require.Zero(t, orphan.GetCoveredColumnCount())
	require.Zero(t, orphan.GetTotalColumnCount())
}
