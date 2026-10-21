package v1

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/state"
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
	require.False(t, find.OwnerUnassigned, "an owner filter never sets the unassigned flag")
	require.Equal(t, "orders", *find.Search)

	err = applyQualityRuleListFilters(&store.FindQualityRuleMessage{}, &v1pb.ListQualityRulesRequest{Owner: "nobody"})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// A raw instance id is accepted too: the filter is a GUID prefix, and the
	// instance name is merely a convenience spelling of it.
	raw := &store.FindQualityRuleMessage{}
	require.NoError(t, applyQualityRuleListFilters(raw, &v1pb.ListQualityRulesRequest{Instance: "instance_1"}))
	require.Equal(t, "instance_1", raw.Instance)

	// owner_unassigned selects only rules with no owner: the complement of an
	// owner filter, never a second condition.
	unassigned := &store.FindQualityRuleMessage{}
	require.NoError(t, applyQualityRuleListFilters(unassigned, &v1pb.ListQualityRulesRequest{OwnerUnassigned: true}))
	require.True(t, unassigned.OwnerUnassigned)
	require.Nil(t, unassigned.OwnerID)

	// A whitespace-only owner is no owner, so it may accompany the flag.
	blank := &store.FindQualityRuleMessage{}
	require.NoError(t, applyQualityRuleListFilters(blank, &v1pb.ListQualityRulesRequest{Owner: "  ", OwnerUnassigned: true}))
	require.True(t, blank.OwnerUnassigned)
	require.Nil(t, blank.OwnerID)

	err = applyQualityRuleListFilters(&store.FindQualityRuleMessage{}, &v1pb.ListQualityRulesRequest{
		Owner:           "users/7",
		OwnerUnassigned: true,
	})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
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
	validated := &dataquality.ValidatedRule{Compiled: dataquality.CompiledQuery{
		SQL:  "SELECT COUNT(*) AS total_rows FROM orders WHERE dt >= $1",
		Args: []any{"2026-01-01"},
	}}
	unreachable := qualityProbeError(errors.New("dial tcp 127.0.0.1:3306: connect: connection refused"), validated)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(unreachable))
	require.Contains(t, unreachable.Error(), "unreachable")

	// The SQL text a user wrote is theirs to fix.
	invalid := qualityProbeError(errors.New("the server rejected the statement"), validated)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(invalid))
	// The message names the statement and its bound values so the author can see
	// which parameter the database refused.
	require.Contains(t, invalid.Error(), "SELECT COUNT(*) AS total_rows FROM orders")
	require.Contains(t, invalid.Error(), "2026-01-01")
}

func TestApplyQualityIssueListFilters(t *testing.T) {
	t.Parallel()

	startAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	endAt := startAt.Add(7 * 24 * time.Hour)
	find := &store.FindQualityIssueMessage{}
	err := applyQualityIssueListFilters(find, &v1pb.ListQualityIssuesRequest{
		Parent:     "workspaces/ws1",
		Status:     v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		Severity:   v1pb.QualitySeverity_QUALITY_SEVERITY_CRITICAL,
		TargetGuid: "instance_1;shop;public;orders",
		StartTime:  timestamppb.New(startAt),
		EndTime:    timestamppb.New(endAt),
	})
	require.NoError(t, err)
	require.Equal(t, []storepb.QualityIssueStatus{storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN}, find.Statuses)
	require.Equal(t, []storepb.QualitySeverity{storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL}, find.Severities)
	require.Equal(t, "instance_1;shop;public;orders", *find.TargetGUID)
	require.Equal(t, startAt, *find.StartAt)
	require.Equal(t, endAt, *find.EndAt)

	empty := &store.FindQualityIssueMessage{}
	require.NoError(t, applyQualityIssueListFilters(empty, &v1pb.ListQualityIssuesRequest{Parent: "workspaces/ws1"}))
	require.Empty(t, empty.Statuses)
	require.Empty(t, empty.Severities)
	require.Nil(t, empty.TargetGUID)
	require.Nil(t, empty.StartAt)

	err = applyQualityIssueListFilters(&store.FindQualityIssueMessage{}, &v1pb.ListQualityIssuesRequest{
		Status: v1pb.QualityIssueStatus(99),
	})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	err = applyQualityIssueListFilters(&store.FindQualityIssueMessage{}, &v1pb.ListQualityIssuesRequest{
		Severity: v1pb.QualitySeverity(99),
	})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestParseQualityIssueUpdate(t *testing.T) {
	t.Parallel()

	t.Run("resolve via status", func(t *testing.T) {
		t.Parallel()
		update, err := parseQualityIssueUpdate(&v1pb.QualityIssue{
			Status: v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED,
		}, false, []string{"status"})
		require.NoError(t, err)
		require.True(t, update.resolve)
		require.False(t, update.acknowledge)
	})

	t.Run("resolve via reason and note", func(t *testing.T) {
		t.Parallel()
		update, err := parseQualityIssueUpdate(&v1pb.QualityIssue{
			ResolutionReason: v1pb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_EXPECTED,
			ResolutionNote:   "load test window",
		}, false, []string{"resolution_reason", "resolution_note"})
		require.NoError(t, err)
		require.True(t, update.resolve)
		require.Equal(t, storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_EXPECTED, update.reason)
		require.Equal(t, "load test window", update.note)
	})

	t.Run("acknowledge", func(t *testing.T) {
		t.Parallel()
		update, err := parseQualityIssueUpdate(&v1pb.QualityIssue{}, true, []string{"acknowledged"})
		require.NoError(t, err)
		require.True(t, update.acknowledge)
	})

	t.Run("mute and clear mute", func(t *testing.T) {
		t.Parallel()
		mutedUntil := time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)
		update, err := parseQualityIssueUpdate(&v1pb.QualityIssue{
			MutedUntil: timestamppb.New(mutedUntil),
		}, false, []string{"muted_until"})
		require.NoError(t, err)
		require.NotNil(t, update.mutedUntil)
		require.Equal(t, mutedUntil, *update.mutedUntil)

		update, err = parseQualityIssueUpdate(&v1pb.QualityIssue{}, false, []string{"muted_until"})
		require.NoError(t, err)
		require.NotNil(t, update.mutedUntil)
		require.True(t, update.mutedUntil.IsZero(), "an unset mute clears the column")
	})

	t.Run("empty mask is a no-op", func(t *testing.T) {
		t.Parallel()
		update, err := parseQualityIssueUpdate(&v1pb.QualityIssue{}, false, nil)
		require.NoError(t, err)
		require.False(t, update.resolve)
		require.False(t, update.acknowledge)
		require.Nil(t, update.mutedUntil)
	})
}

func TestParseQualityIssueUpdateRejectsIllegalMasks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		issue        *v1pb.QualityIssue
		acknowledged bool
		paths        []string
	}{
		{
			name:  "status must be RESOLVED",
			issue: &v1pb.QualityIssue{Status: v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN},
			paths: []string{"status"},
		},
		{
			name:  "unknown status",
			issue: &v1pb.QualityIssue{Status: v1pb.QualityIssueStatus(99)},
			paths: []string{"status"},
		},
		{
			name:         "acknowledged=false cannot clear an acknowledgement",
			issue:        &v1pb.QualityIssue{},
			acknowledged: false,
			paths:        []string{"acknowledged"},
		},
		{
			name:  "note requires resolve",
			issue: &v1pb.QualityIssue{ResolutionNote: "note"},
			paths: []string{"resolution_note"},
		},
		{
			name:         "resolve and acknowledge cannot be combined",
			issue:        &v1pb.QualityIssue{Status: v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED},
			acknowledged: true,
			paths:        []string{"status", "acknowledged"},
		},
		{
			name:  "unknown reason",
			issue: &v1pb.QualityIssue{ResolutionReason: v1pb.QualityResolutionReason(99)},
			paths: []string{"resolution_reason"},
		},
		{
			name:  "unknown path",
			issue: &v1pb.QualityIssue{},
			paths: []string{"occurrence_count"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseQualityIssueUpdate(testCase.issue, testCase.acknowledged, testCase.paths)
			require.Error(t, err)
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}

	_, err := parseQualityIssueUpdate(nil, false, []string{"status"})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestQualityIssueForeignKeys(t *testing.T) {
	t.Parallel()

	runs := []*store.QualityIssueMessage{
		{RuleID: 7, FirstSeenRunID: qualityTestPtr(int64(11)), LastSeenRunID: qualityTestPtr(int64(12))},
		{RuleID: 7, FirstSeenRunID: qualityTestPtr(int64(12)), LastSeenRunID: nil},
		{RuleID: 9, FirstSeenRunID: nil, LastSeenRunID: qualityTestPtr(int64(11))},
	}
	require.Equal(t, []int64{7, 9}, qualityIssueRuleIDs(runs))
	require.Equal(t, []int64{11, 12}, qualityIssueRunIDs(runs))
	require.Empty(t, qualityIssueRuleIDs(nil))
	require.Empty(t, qualityIssueRunIDs(nil))
}

func TestCheckQualityIssueAcknowledgeable(t *testing.T) {
	t.Parallel()

	require.NoError(t, checkQualityIssueAcknowledgeable(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN))
	require.NoError(t, checkQualityIssueAcknowledgeable(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED))
	err := checkQualityIssueAcknowledgeable(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED)
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func TestQualitySuggestionDecision(t *testing.T) {
	t.Parallel()

	accepted, err := qualitySuggestionDecision(v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED)
	require.NoError(t, err)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, accepted)

	rejected, err := qualitySuggestionDecision(v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED)
	require.NoError(t, err)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED, rejected)

	for _, decision := range []v1pb.QualitySuggestionStatus{
		v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED,
		v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		v1pb.QualitySuggestionStatus(99),
	} {
		_, err := qualitySuggestionDecision(decision)
		require.Error(t, err, "decision %v", decision)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}
}

func TestApplyQualitySuggestionListFilters(t *testing.T) {
	t.Parallel()

	find := &store.FindQualitySuggestionMessage{}
	err := applyQualitySuggestionListFilters(find, &v1pb.ListQualitySuggestionsRequest{
		Status:     v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		Kind:       v1pb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		Source:     v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE,
		TargetGuid: "instance_1;shop;public;orders",
	})
	require.NoError(t, err)
	require.Equal(t, []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING}, find.Statuses)
	require.Equal(t, []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT}, find.Kinds)
	require.Equal(t, []storepb.QualitySuggestionSource{storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE}, find.Sources)
	require.Equal(t, "instance_1;shop;public;orders", *find.TargetGUID)

	empty := &store.FindQualitySuggestionMessage{}
	require.NoError(t, applyQualitySuggestionListFilters(empty, &v1pb.ListQualitySuggestionsRequest{}))
	require.Empty(t, empty.Statuses)
	require.Empty(t, empty.Kinds)
	require.Empty(t, empty.Sources)
	require.Nil(t, empty.TargetGUID)

	for name, request := range map[string]*v1pb.ListQualitySuggestionsRequest{
		"status": {Status: v1pb.QualitySuggestionStatus(99)},
		"kind":   {Kind: v1pb.QualitySuggestionKind(99)},
		"source": {Source: v1pb.QualitySuggestionSource(99)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := applyQualitySuggestionListFilters(&store.FindQualitySuggestionMessage{}, request)
			require.Error(t, err)
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

func TestApplyColumnProfileAndLabelListFilters(t *testing.T) {
	t.Parallel()

	profileFind := &store.FindColumnProfileMessage{}
	applyColumnProfileListFilters(profileFind, &v1pb.ListColumnProfilesRequest{
		TableGuid: "instance_1;shop;public;orders",
		MetaType:  v1pb.MetaType_TABLE,
		Columns:   []string{" email ", "", " id "},
	})
	require.Equal(t, "instance_1;shop;public;orders", *profileFind.TableGUID)
	require.Equal(t, storepb.MetaType_TABLE, *profileFind.TableType)
	require.Equal(t, []string{"email", "id"}, profileFind.ColumnNames)

	labelFind := &store.FindColumnSemanticLabelMessage{}
	applyColumnLabelListFilters(labelFind, &v1pb.ListColumnLabelsRequest{})
	require.Nil(t, labelFind.TableGUID)
	require.Nil(t, labelFind.TableType)
	require.Empty(t, labelFind.ColumnNames)
}

func TestParseUpdateColumnLabel(t *testing.T) {
	t.Parallel()

	accepted, err := parseUpdateColumnLabel(&v1pb.UpdateColumnLabelRequest{
		Parent:    "workspaces/ws1",
		TableGuid: " instance_1;shop;public;orders ",
		MetaType:  v1pb.MetaType_TABLE,
		Column:    " email ",
		Label:     v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
		Action:    v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT,
	})
	require.NoError(t, err)
	require.Equal(t, "instance_1;shop;public;orders", accepted.tableGUID)
	require.Equal(t, storepb.MetaType_TABLE, accepted.metaType)
	require.Equal(t, "email", accepted.column)
	require.Equal(t, storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL, accepted.label)
	require.Equal(t, v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT, accepted.action)

	// CLEAR does not need a label.
	cleared, err := parseUpdateColumnLabel(&v1pb.UpdateColumnLabelRequest{
		TableGuid: "guid",
		MetaType:  v1pb.MetaType_TABLE,
		Column:    "email",
		Action:    v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_CLEAR,
	})
	require.NoError(t, err)
	require.Equal(t, v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_CLEAR, cleared.action)

	invalid := []struct {
		name    string
		request *v1pb.UpdateColumnLabelRequest
	}{
		{name: "missing table guid", request: &v1pb.UpdateColumnLabelRequest{MetaType: v1pb.MetaType_TABLE, Column: "c", Action: v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_CLEAR}},
		{name: "missing meta type", request: &v1pb.UpdateColumnLabelRequest{TableGuid: "guid", Column: "c", Action: v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_CLEAR}},
		{name: "missing column", request: &v1pb.UpdateColumnLabelRequest{TableGuid: "guid", MetaType: v1pb.MetaType_TABLE, Action: v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_CLEAR}},
		{name: "unknown action", request: &v1pb.UpdateColumnLabelRequest{TableGuid: "guid", MetaType: v1pb.MetaType_TABLE, Column: "c", Action: v1pb.ColumnLabelAction(99)}},
		{name: "accept without label", request: &v1pb.UpdateColumnLabelRequest{TableGuid: "guid", MetaType: v1pb.MetaType_TABLE, Column: "c", Action: v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT}},
	}
	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseUpdateColumnLabel(testCase.request)
			require.Error(t, err)
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

func TestUpdateQualitySuggestionRejectsInvalidName(t *testing.T) {
	t.Parallel()

	// The name is parsed before any store read, so a malformed name is refused
	// without touching the database.
	service := &DataQualityService{}
	_, err := service.UpdateQualitySuggestion(t.Context(), connect.NewRequest(&v1pb.UpdateQualitySuggestionRequest{
		Name:     "qualitySuggestions/suggestion123",
		Decision: v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestQualityLLMHandlersRequireARequest(t *testing.T) {
	t.Parallel()

	service := &DataQualityService{}

	_, err := service.SuggestQualityRule(t.Context(), nil)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = service.SuggestColumnLabels(t.Context(), nil)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	err = service.ExplainQualityResult(t.Context(), nil, nil)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// TestTrimmedQualityColumns pins the column list normalization the profile request
// applies: blank entries disappear, duplicates collapse, and the input order is
// preserved so the response order matches the request.
func TestTrimmedQualityColumns(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"a", "b", "c"}, trimmedQualityColumns([]string{" a ", "", "b", "a", " c "}))
	require.Empty(t, trimmedQualityColumns(nil))
}

func TestDryRunNeedsProbe(t *testing.T) {
	t.Parallel()

	require.False(t, dryRunNeedsProbe(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT),
		"a metadata rule has no SQL to probe and must not open the target database")
	require.True(t, dryRunNeedsProbe(storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK),
		"a group check compiles to SQL and must be probed")
	require.True(t, dryRunNeedsProbe(storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL))
	require.True(t, dryRunNeedsProbe(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
}

// TestRequireReadOnlySourceForNewRule pins the create-time configuration check: a
// query rule needs a READ_ONLY source, while a metadata rule needs none.
func TestRequireReadOnlySourceForNewRule(t *testing.T) {
	t.Parallel()

	withReadOnly := &dataquality.Resolution{Instance: &store.InstanceMessage{
		ResourceID: "instances/ins1",
		Metadata: &storepb.Instance{DataSources: []*storepb.DataSource{
			{Type: storepb.DataSourceType_ADMIN},
			{Type: storepb.DataSourceType_READ_ONLY},
		}},
	}}
	require.NoError(t, requireReadOnlySourceForNewRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, withReadOnly))

	adminOnly := &dataquality.Resolution{Instance: &store.InstanceMessage{
		ResourceID: "instances/ins1",
		Metadata: &storepb.Instance{DataSources: []*storepb.DataSource{
			{Type: storepb.DataSourceType_ADMIN},
		}},
	}}
	err := requireReadOnlySourceForNewRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, adminOnly)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "READ_ONLY")

	// A metadata rule reads no target database, so it needs no data source at all.
	require.NoError(t, requireReadOnlySourceForNewRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, adminOnly))
}

// A dry run and a profile open a target driver on the user's behalf, so they wait
// for one of the instance's connection slots instead of bypassing the budget the
// runners share.
func TestAcquireQualityConnectionSharesTheInstanceBudget(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	service := &DataQualityService{stateCfg: stateCfg}
	instance := &store.InstanceMessage{
		ResourceID: "inst-1",
		Metadata:   &storepb.Instance{MaximumConnections: 1},
	}
	request := state.InstanceConnectionRequest{
		InstanceID:         instance.ResourceID,
		MaximumConnections: 1,
		Priority:           state.ConnectionPriorityBackground,
	}
	hold, ok := stateCfg.TryAcquireInstanceConnection(request)
	require.True(t, ok)

	_, err = service.acquireQualityConnection(context.Background(), instance, "the dry run", 10*time.Millisecond)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))

	hold()
	release, err := service.acquireQualityConnection(context.Background(), instance, "the dry run", time.Second)
	require.NoError(t, err)
	release()

	_, err = service.acquireQualityConnection(context.Background(), nil, "the dry run", time.Second)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}
