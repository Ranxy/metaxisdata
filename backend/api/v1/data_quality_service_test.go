package v1

import (
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestValidateQualityScheduleInterval(t *testing.T) {
	t.Parallel()

	accepted := []time.Duration{
		15 * time.Minute,
		time.Hour,
		7 * 24 * time.Hour,
	}
	for _, interval := range accepted {
		require.NoError(t, validateQualityScheduleInterval(interval), "interval %s", interval)
	}
	rejected := []time.Duration{
		0,
		14*time.Minute + 59*time.Second,
		-15 * time.Minute,
		7*24*time.Hour + time.Second,
	}
	for _, interval := range rejected {
		err := validateQualityScheduleInterval(interval)
		require.Error(t, err, "interval %s", interval)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}
}

func TestQualityOwnerID(t *testing.T) {
	t.Parallel()

	ownerID, err := qualityOwnerID("users/7")
	require.NoError(t, err)
	require.NotNil(t, ownerID)
	require.Equal(t, int32(7), *ownerID)

	ownerID, err = qualityOwnerID("")
	require.NoError(t, err)
	require.Nil(t, ownerID, "an empty owner is unassigned, not principal 0")

	_, err = qualityOwnerID("groups/team@example.com")
	require.Error(t, err)

	require.Equal(t, "users/7", formatQualityOwner(qualityTestPtr(int32(7))))
	require.Empty(t, formatQualityOwner(nil))
	require.Empty(t, formatQualityOwner(qualityTestPtr(int32(0))))
}

func TestParseQualityRuleUpdateFieldMapping(t *testing.T) {
	t.Parallel()

	mutedUntil := time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)
	rule := &v1pb.QualityRule{
		Title:            "new title",
		Description:      "new description",
		Dimension:        v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
		RuleType:         v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Target:           &v1pb.QualityTarget{MetaGuid: "instance_1;shop;public;orders", MetaType: v1pb.MetaType_TABLE},
		Config:           &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_NullPercent{NullPercent: &v1pb.NullPercentConfig{Column: "email", MaxNullPct: qualityTestPtr(0.5)}}},
		Severity:         v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		State:            v1pb.QualityRuleState_QUALITY_RULE_STATE_PAUSED,
		Owner:            "users/7",
		Tags:             []string{"pii"},
		ScheduleInterval: durationpb.New(2 * time.Hour),
		MuteUntil:        timestamppb.New(mutedUntil),
	}

	update, err := parseQualityRuleUpdate(rule, []string{
		"title", "description", "dimension", "rule_type", "target", "config",
		"severity", "state", "owner", "tags", "schedule_interval", "mute_until",
	})
	require.NoError(t, err)
	require.True(t, update.DefinitionChanged)
	require.True(t, update.ScheduleChanged)
	require.True(t, update.StateChanged)

	require.Equal(t, "new title", *update.Patch.Title)
	require.Equal(t, "new description", *update.Patch.Description)
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, *update.Patch.Dimension)
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, *update.Patch.RuleType)
	require.Equal(t, "instance_1;shop;public;orders", *update.Patch.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, *update.Patch.TargetType)
	require.NotNil(t, update.Patch.Config)
	require.Equal(t, "email", update.Patch.Config.GetNullPercent().GetColumn())
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, *update.Patch.Severity)
	require.Equal(t, storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED, *update.Patch.State)
	require.Equal(t, int32(7), *update.Patch.OwnerID)
	require.Equal(t, []string{"pii"}, *update.Patch.Tags)
	require.Equal(t, 2*time.Hour, *update.Patch.ScheduleInterval)
	require.Equal(t, mutedUntil, *update.Patch.MuteUntil)
}

func TestParseQualityRuleUpdateEmptyMaskChangesNothing(t *testing.T) {
	t.Parallel()

	update, err := parseQualityRuleUpdate(&v1pb.QualityRule{Name: "workspaces/ws1/qualityRules/rule1"}, nil)
	require.NoError(t, err)
	require.False(t, update.DefinitionChanged)
	require.False(t, update.ScheduleChanged)
	require.False(t, update.StateChanged)
	require.Nil(t, update.Patch.Title)
	require.Nil(t, update.Patch.Config)
}

func TestParseQualityRuleUpdateRejectsUnknownPath(t *testing.T) {
	t.Parallel()

	_, err := parseQualityRuleUpdate(&v1pb.QualityRule{}, []string{"revision"})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestParseQualityRuleUpdateRejectsUnusableValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		rule  *v1pb.QualityRule
		paths []string
	}{
		{
			name:  "archiving is not an update",
			rule:  &v1pb.QualityRule{State: v1pb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED},
			paths: []string{"state"},
		},
		{
			name:  "unspecified state",
			rule:  &v1pb.QualityRule{State: v1pb.QualityRuleState_QUALITY_RULE_STATE_UNSPECIFIED},
			paths: []string{"state"},
		},
		{
			name:  "unspecified severity",
			rule:  &v1pb.QualityRule{Severity: v1pb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED},
			paths: []string{"severity"},
		},
		{
			name:  "unspecified dimension",
			rule:  &v1pb.QualityRule{Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED},
			paths: []string{"dimension"},
		},
		{
			name:  "missing target",
			rule:  &v1pb.QualityRule{},
			paths: []string{"target"},
		},
		{
			name:  "missing config",
			rule:  &v1pb.QualityRule{},
			paths: []string{"config"},
		},
		{
			name:  "invalid owner",
			rule:  &v1pb.QualityRule{Owner: "nobody"},
			paths: []string{"owner"},
		},
		{
			name:  "zero schedule interval",
			rule:  &v1pb.QualityRule{ScheduleInterval: durationpb.New(0)},
			paths: []string{"schedule_interval"},
		},
		{
			name:  "schedule interval too short",
			rule:  &v1pb.QualityRule{ScheduleInterval: durationpb.New(5 * time.Minute)},
			paths: []string{"schedule_interval"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseQualityRuleUpdate(testCase.rule, testCase.paths)
			require.Error(t, err)
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

func TestParseQualityRuleUpdateClearsOwnerAndMute(t *testing.T) {
	t.Parallel()

	update, err := parseQualityRuleUpdate(&v1pb.QualityRule{}, []string{"owner", "mute_until"})
	require.NoError(t, err)
	require.NotNil(t, update.Patch.OwnerID)
	require.Equal(t, int32(0), *update.Patch.OwnerID, "an empty owner clears the column")
	require.NotNil(t, update.Patch.MuteUntil)
	require.True(t, update.Patch.MuteUntil.IsZero(), "an unset mute clears the column")
}

func TestMergedQualityRuleDefinition(t *testing.T) {
	t.Parallel()

	current := &store.QualityRuleMessage{
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		TargetGUID: "instance_1;shop;public;orders",
		TargetType: storepb.MetaType_TABLE,
		Config:     &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: qualityTestPtr(int64(1))}}},
	}
	ruleType, guid, metaType, config := mergedQualityRuleDefinition(current, &store.UpdateQualityRuleMessage{})
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, ruleType)
	require.Equal(t, current.TargetGUID, guid)
	require.Equal(t, storepb.MetaType_TABLE, metaType)
	require.Equal(t, current.Config, config)

	newType := storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS
	newGUID := "instance_2;shop;public;events"
	newMetaType := storepb.MetaType_MATERIALIZED_VIEW
	newConfig := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{TimestampColumn: "updated_at", MaxLagSeconds: 60}}}
	ruleType, guid, metaType, config = mergedQualityRuleDefinition(current, &store.UpdateQualityRuleMessage{
		RuleType:   &newType,
		TargetGUID: &newGUID,
		TargetType: &newMetaType,
		Config:     newConfig,
	})
	require.Equal(t, newType, ruleType)
	require.Equal(t, newGUID, guid)
	require.Equal(t, newMetaType, metaType)
	require.Equal(t, newConfig, config)
}

func TestGroupQualityRulesByInstance(t *testing.T) {
	t.Parallel()

	rules := []*store.QualityRuleMessage{
		{ID: 1, TargetGUID: "instance_2;shop;public;orders"},
		{ID: 2, TargetGUID: "instance_1;shop;public;orders"},
		{ID: 3, TargetGUID: "instance_2;shop;public;customers"},
		{ID: 4, TargetGUID: "no-instance-segment"},
	}
	groups, order := groupQualityRulesByInstance(rules)
	require.Equal(t, []string{"instance_1", "instance_2"}, order)
	require.Len(t, groups["instance_1"], 1)
	require.Len(t, groups["instance_2"], 2)
	require.Equal(t, int64(3), groups["instance_2"][1].ID)

	groups, order = groupQualityRulesByInstance(nil)
	require.Empty(t, groups)
	require.Empty(t, order)
}

func TestDedupeInt64(t *testing.T) {
	t.Parallel()

	require.Equal(t, []int64{3, 1, 2}, dedupeInt64([]int64{3, 1, 3, 2, 1}))
	require.Empty(t, dedupeInt64(nil))
}

func TestApplyQualityRuleListFilters(t *testing.T) {
	t.Parallel()

	find := &store.FindQualityRuleMessage{}
	err := applyQualityRuleListFilters(find, &v1pb.ListQualityRulesRequest{
		Parent:     "workspaces/ws1",
		Instance:   "instances/instance_1",
		Database:   "shop",
		Schema:     "public",
		Table:      "orders",
		TargetGuid: "instance_1;shop;public;orders",
		Dimension:  v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
		RuleType:   v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		State:      v1pb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED,
		Owner:      "users/7",
		Search:     "orders",
	})
	require.NoError(t, err)
	require.Equal(t, "instance_1", find.Instance)
	require.Equal(t, "shop", find.Database)
	require.Equal(t, "public", find.Schema)
	require.Equal(t, "orders", find.TablePrefix)
	require.Equal(t, "instance_1;shop;public;orders", *find.TargetGUID)
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS, find.Dimension)
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, find.RuleType)
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, find.Severity)
	require.Equal(t, storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED, find.State)
	require.True(t, find.IncludeArchived, "asking for archived rules must include them")
	require.True(t, find.IncludeDeleted, "archived rules are soft-deleted rows")
	require.Equal(t, int32(7), *find.OwnerID)
	require.Equal(t, "orders", *find.Search)

	err = applyQualityRuleListFilters(&store.FindQualityRuleMessage{}, &v1pb.ListQualityRulesRequest{Owner: "nobody"})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// A raw instance id is accepted too: the filter is a GUID prefix, and the
	// instance name is merely a convenience spelling of it.
	raw := &store.FindQualityRuleMessage{}
	require.NoError(t, applyQualityRuleListFilters(raw, &v1pb.ListQualityRulesRequest{Instance: "instance_1"}))
	require.Equal(t, "instance_1", raw.Instance)
}

func TestQualityRunListFilter(t *testing.T) {
	t.Parallel()

	startAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	endAt := startAt.Add(24 * time.Hour)
	find, err := qualityRunListFilter(&v1pb.ListQualityRunsRequest{
		Parent:    "workspaces/ws1",
		Status:    v1pb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING,
		Trigger:   v1pb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL,
		Instance:  "instances/instance_1",
		StartTime: timestamppb.New(startAt),
		EndTime:   timestamppb.New(endAt),
		Initiator: "users/7",
		Outcome:   v1pb.QualityStatus_QUALITY_STATUS_FAIL,
	})
	require.NoError(t, err)
	require.Equal(t, []storepb.QualityRunStatus{storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING}, find.Statuses)
	require.Equal(t, storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL, *find.Trigger)
	require.Equal(t, "instance_1", *find.InstanceResourceID)
	require.Equal(t, startAt, *find.StartAt)
	require.Equal(t, endAt, *find.EndAt)
	require.Equal(t, int32(7), *find.InitiatorID)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, *find.Outcome)

	empty, err := qualityRunListFilter(&v1pb.ListQualityRunsRequest{})
	require.NoError(t, err)
	require.Empty(t, empty.Statuses)
	require.Nil(t, empty.Trigger)
	require.Nil(t, empty.Outcome)

	_, err = qualityRunListFilter(&v1pb.ListQualityRunsRequest{Instance: "not-an-instance-name"})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = qualityRunListFilter(&v1pb.ListQualityRunsRequest{Initiator: "nobody"})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestQualityRuleProbeSQL(t *testing.T) {
	t.Parallel()

	compiled := dataquality.CompiledQuery{SQL: "SELECT COUNT(*) AS total_rows FROM orders"}
	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_STARROCKS} {
		probe := qualityRuleProbeSQL(&dataquality.ValidatedRule{
			Target:   dataquality.ResolvedTarget{Engine: engine},
			Compiled: compiled,
		})
		require.Contains(t, probe, compiled.SQL)
		require.Contains(t, probe, "LIMIT 0")
	}
	mssqlProbe := qualityRuleProbeSQL(&dataquality.ValidatedRule{
		Target:   dataquality.ResolvedTarget{Engine: storepb.Engine_MSSQL},
		Compiled: compiled,
	})
	require.Contains(t, mssqlProbe, "SELECT TOP 0")
	require.NotContains(t, mssqlProbe, "LIMIT")
}

func TestPreviewQualityRuleResponse(t *testing.T) {
	t.Parallel()

	validated := &dataquality.ValidatedRule{
		Compiled: dataquality.CompiledQuery{
			SQL:  "SELECT COUNT(*) AS total_rows FROM orders WHERE dt >= $1",
			Args: []any{"2026-01-01"},
		},
	}
	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{Column: "email"}},
		Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
			{Column: "dt", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE, Values: []string{"2026-01-01"}},
		}},
	}
	response := previewQualityRuleResponse(config, validated)
	require.Equal(t, validated.Compiled.SQL, response.GetCompiledSql())
	require.Equal(t, []string{"email", "dt"}, response.GetReferencedColumns())
	require.Equal(t, []string{"2026-01-01"}, response.GetParameters())
}

func TestQualityProbeError(t *testing.T) {
	t.Parallel()

	// A dead data source is a configuration problem, not an invalid rule.
	unreachable := qualityProbeError(errors.New("dial tcp 127.0.0.1:3306: connect: connection refused"))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(unreachable))
	require.Contains(t, unreachable.Error(), "unreachable")

	// The SQL text a user wrote is theirs to fix.
	invalid := qualityProbeError(errors.New("the server rejected the statement"))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(invalid))
}
