package store

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityRuleRowStub produces a quality_rule row for scanQualityRule without a
// database. The destination order is qualityRuleColumns.
type qualityRuleRowStub struct {
	id              int64
	resourceID      string
	title           string
	description     string
	dimension       storepb.QualityDimension
	ruleType        storepb.QualityRuleType
	targetGUID      string
	targetType      storepb.MetaType
	targetPath      []byte
	targetMetaHash  []byte
	config          []byte
	severity        storepb.QualitySeverity
	state           storepb.QualityRuleState
	ownerID         sql.NullInt32
	tags            pq.StringArray
	intervalSeconds int32
	nextRunAt       sql.NullTime
	muteUntil       sql.NullTime
	revision        int32
	createdBy       sql.NullInt32
	updatedBy       sql.NullInt32
	createdAt       time.Time
	updatedAt       time.Time
	deleted         bool
}

func (s qualityRuleRowStub) Scan(dest ...any) error {
	if len(dest) != 24 {
		return fmt.Errorf("quality rule stub got %d destinations, want 24", len(dest))
	}
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.resourceID
	*(dest[2].(*string)) = s.title
	*(dest[3].(*string)) = s.description
	*(dest[4].(*storepb.QualityDimension)) = s.dimension
	*(dest[5].(*storepb.QualityRuleType)) = s.ruleType
	*(dest[6].(*string)) = s.targetGUID
	*(dest[7].(*storepb.MetaType)) = s.targetType
	*(dest[8].(*[]byte)) = s.targetPath
	*(dest[9].(*[]byte)) = s.targetMetaHash
	*(dest[10].(*[]byte)) = s.config
	*(dest[11].(*storepb.QualitySeverity)) = s.severity
	*(dest[12].(*storepb.QualityRuleState)) = s.state
	*(dest[13].(*sql.NullInt32)) = s.ownerID
	*(dest[14].(*pq.StringArray)) = s.tags
	*(dest[15].(*int32)) = s.intervalSeconds
	*(dest[16].(*sql.NullTime)) = s.nextRunAt
	*(dest[17].(*sql.NullTime)) = s.muteUntil
	*(dest[18].(*int32)) = s.revision
	*(dest[19].(*sql.NullInt32)) = s.createdBy
	*(dest[20].(*sql.NullInt32)) = s.updatedBy
	*(dest[21].(*time.Time)) = s.createdAt
	*(dest[22].(*time.Time)) = s.updatedAt
	*(dest[23].(*bool)) = s.deleted
	return nil
}

// qualityRuleRevisionRowStub produces a quality_rule_revision row for
// scanQualityRuleRevision. The destination order is qualityRuleRevisionColumns.
type qualityRuleRevisionRowStub struct {
	id             int64
	ruleID         int64
	revision       int32
	targetGUID     string
	targetType     storepb.MetaType
	targetPath     []byte
	targetMetaHash []byte
	dimension      storepb.QualityDimension
	ruleType       storepb.QualityRuleType
	severity       storepb.QualitySeverity
	config         []byte
	createdBy      sql.NullInt32
	createdAt      time.Time
}

func (s qualityRuleRevisionRowStub) Scan(dest ...any) error {
	if len(dest) != 13 {
		return fmt.Errorf("quality rule revision stub got %d destinations, want 13", len(dest))
	}
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*int64)) = s.ruleID
	*(dest[2].(*int32)) = s.revision
	*(dest[3].(*string)) = s.targetGUID
	*(dest[4].(*storepb.MetaType)) = s.targetType
	*(dest[5].(*[]byte)) = s.targetPath
	*(dest[6].(*[]byte)) = s.targetMetaHash
	*(dest[7].(*storepb.QualityDimension)) = s.dimension
	*(dest[8].(*storepb.QualityRuleType)) = s.ruleType
	*(dest[9].(*storepb.QualitySeverity)) = s.severity
	*(dest[10].(*[]byte)) = s.config
	*(dest[11].(*sql.NullInt32)) = s.createdBy
	*(dest[12].(*time.Time)) = s.createdAt
	return nil
}

// The row mapping is where the JSONB payloads and the nullable columns get their
// Go shape; a mistake here silently corrupts an edit because the head and its
// revision are read through the same function.
func TestScanQualityRuleReadsJSONBAndNullableColumns(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 19, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	nextRunAt := updatedAt.Add(time.Hour)
	muteUntil := updatedAt.Add(2 * time.Hour)

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_NullPercent{
			NullPercent: &storepb.NullPercentConfig{
				Column:     "email",
				MaxNullPct: proto.Float64(5),
			},
		},
		ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT,
		OnEmpty:         storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS,
	}
	configJSON, err := marshalQualityProto(config)
	require.NoError(t, err)
	targetPath := &storepb.QualityTargetPath{
		Instance: "instances/inst-1",
		Database: "shop",
		Schema:   "public",
		Object:   "users",
		Column:   "email",
	}
	targetPathJSON, err := marshalQualityProto(targetPath)
	require.NoError(t, err)

	msg, err := scanQualityRule(qualityRuleRowStub{
		id:              101,
		resourceID:      "quality-rule-1",
		title:           "email null percent",
		description:     "keeps the email column populated",
		dimension:       storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
		ruleType:        storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		targetGUID:      "instances/inst-1;shop;public;users",
		targetType:      storepb.MetaType_COLUMN,
		targetPath:      targetPathJSON,
		targetMetaHash:  []byte{0x01, 0x02},
		config:          configJSON,
		severity:        storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		state:           storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED,
		ownerID:         sql.NullInt32{Int32: 7, Valid: true},
		tags:            pq.StringArray{"pii", "core"},
		intervalSeconds: 3600,
		nextRunAt:       sql.NullTime{Time: nextRunAt, Valid: true},
		muteUntil:       sql.NullTime{Time: muteUntil, Valid: true},
		revision:        3,
		createdBy:       sql.NullInt32{Int32: 4, Valid: true},
		updatedBy:       sql.NullInt32{Int32: 5, Valid: true},
		createdAt:       createdAt,
		updatedAt:       updatedAt,
		deleted:         true,
	})
	require.NoError(t, err)
	require.Equal(t, int64(101), msg.ID)
	require.Equal(t, "quality-rule-1", msg.ResourceID)
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, msg.Dimension)
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, msg.RuleType)
	require.Equal(t, storepb.MetaType_COLUMN, msg.TargetType)
	require.True(t, proto.Equal(targetPath, msg.TargetPath))
	require.Equal(t, []byte{0x01, 0x02}, msg.TargetMetaHash)
	require.True(t, proto.Equal(config, msg.Config))
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, msg.Severity)
	require.Equal(t, storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED, msg.State)
	require.Equal(t, int32(7), *msg.OwnerID)
	require.Equal(t, []string{"pii", "core"}, msg.Tags)
	require.Equal(t, time.Hour, msg.ScheduleInterval)
	require.Equal(t, nextRunAt, *msg.NextRunAt)
	require.Equal(t, muteUntil, *msg.MuteUntil)
	require.Equal(t, int32(3), msg.Revision)
	require.Equal(t, int32(4), *msg.CreatedBy)
	require.Equal(t, int32(5), *msg.UpdatedBy)
	require.Equal(t, createdAt, msg.CreatedAt)
	require.Equal(t, updatedAt, msg.UpdatedAt)
	require.True(t, msg.Deleted)
}

// NULL next_run_at/mute_until/owner are meaningful states (not scheduled,
// not muted, unassigned), so the scan must keep them as nil pointers.
func TestScanQualityRuleKeepsNullColumnsNil(t *testing.T) {
	t.Parallel()

	msg, err := scanQualityRule(qualityRuleRowStub{
		id:         102,
		resourceID: "quality-rule-2",
		targetPath: []byte("{}"),
		config:     []byte("{}"),
	})
	require.NoError(t, err)
	require.Nil(t, msg.OwnerID)
	require.Nil(t, msg.NextRunAt)
	require.Nil(t, msg.MuteUntil)
	require.Nil(t, msg.CreatedBy)
	require.Nil(t, msg.UpdatedBy)
	require.Nil(t, msg.TargetMetaHash)
	require.Empty(t, msg.Tags)
	require.NotNil(t, msg.TargetPath)
	require.NotNil(t, msg.Config)
}

func TestScanQualityRuleRevision(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 19, 11, 0, 0, 0, time.UTC)
	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{
			RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(100)},
		},
	}
	configJSON, err := marshalQualityProto(config)
	require.NoError(t, err)

	msg, err := scanQualityRuleRevision(qualityRuleRevisionRowStub{
		id:             9,
		ruleID:         101,
		revision:       4,
		targetGUID:     "instances/inst-1;shop;public;users",
		targetType:     storepb.MetaType_TABLE,
		targetPath:     []byte("{}"),
		targetMetaHash: []byte{0x0a},
		dimension:      storepb.QualityDimension_QUALITY_DIMENSION_TIMELINESS,
		ruleType:       storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		severity:       storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL,
		config:         configJSON,
		createdBy:      sql.NullInt32{Int32: 5, Valid: true},
		createdAt:      createdAt,
	})
	require.NoError(t, err)
	require.Equal(t, int64(9), msg.ID)
	require.Equal(t, int64(101), msg.RuleID)
	require.Equal(t, int32(4), msg.Revision)
	require.Equal(t, storepb.MetaType_TABLE, msg.TargetType)
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_TIMELINESS, msg.Dimension)
	require.True(t, proto.Equal(config, msg.Config))
	require.Equal(t, int32(5), *msg.CreatedBy)
	require.Equal(t, createdAt, msg.CreatedAt)
}

// The path filters are GUID subtree prefixes, not name lookups: the deepest
// filter decides how many segments the prefix has, and an empty schema segment
// (MySQL) must still produce a valid four-segment prefix.
func TestQualityRuleGUIDPrefixBuildsTheDeepestPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		find *FindQualityRuleMessage
		want string
	}{
		{
			name: "no path filter",
			find: &FindQualityRuleMessage{},
			want: "",
		},
		{
			name: "instance only",
			find: &FindQualityRuleMessage{Instance: "instances/inst-1"},
			want: "instances/inst-1",
		},
		{
			name: "instance and database",
			find: &FindQualityRuleMessage{Instance: "instances/inst-1", Database: "shop"},
			want: "instances/inst-1;shop",
		},
		{
			name: "instance database schema",
			find: &FindQualityRuleMessage{Instance: "instances/inst-1", Database: "shop", Schema: "public"},
			want: "instances/inst-1;shop;public",
		},
		{
			name: "table with schema",
			find: &FindQualityRuleMessage{
				Instance: "instances/inst-1", Database: "shop", Schema: "public", TablePrefix: "users",
			},
			want: "instances/inst-1;shop;public;users",
		},
		{
			name: "table without schema segment",
			find: &FindQualityRuleMessage{
				Instance: "instances/inst-1", Database: "shop", TablePrefix: "users",
			},
			want: "instances/inst-1;shop;;users",
		},
		{
			name: "guid metacharacters are escaped",
			find: &FindQualityRuleMessage{Instance: "inst;1", Database: "db%2"},
			want: "inst%3B1;db%252",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := qualityRuleGUIDPrefix(tc.find)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// A bare database/schema/table name is not a GUID prefix, so a filter that
// omits its ancestors must be rejected rather than silently matching the wrong
// subtree.
func TestQualityRuleGUIDPrefixRequiresAncestors(t *testing.T) {
	t.Parallel()

	tests := []*FindQualityRuleMessage{
		{Database: "shop"},
		{Schema: "public"},
		{TablePrefix: "users"},
		{Instance: "instances/inst-1", TablePrefix: "users"},
	}
	for _, find := range tests {
		_, err := qualityRuleGUIDPrefix(find)
		require.Error(t, err)
		require.Equal(t, common.Invalid, common.ErrorCode(err))
	}
}

// Every filter must add one parameterized condition and one argument; the
// builder is where "no caller value reaches SQL text" is enforced.
func TestBuildQualityRuleWhereAppliesEveryFilter(t *testing.T) {
	t.Parallel()

	resourceID := "  quality-rule-1  "
	targetGUID := "instances/inst-1;shop;public;users"
	ownerID := int32(7)
	search := "100%_done"
	find := &FindQualityRuleMessage{
		ResourceID:  &resourceID,
		ResourceIDs: []string{"quality-rule-2", "quality-rule-3"},
		TargetGUID:  &targetGUID,
		Instance:    "instances/inst-1",
		Database:    "shop",
		Schema:      "public",
		TablePrefix: "users",
		Dimension:   storepb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
		RuleType:    storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		Severity:    storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		State:       storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		OwnerID:     &ownerID,
		Search:      &search,
	}

	where, args, err := buildQualityRuleWhere(find)
	require.NoError(t, err)
	require.Len(t, args, 13)
	require.Equal(t, "quality-rule-1", args[0])
	require.Equal(t, targetGUID, args[2])
	require.Equal(t, targetGUID+";%", args[3])
	require.Equal(t, targetGUID, args[4])
	require.Equal(t, targetGUID+";%", args[5])
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_VALIDITY, args[6])
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, args[7])
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_WARNING, args[8])
	require.Equal(t, storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED, args[9])
	require.Equal(t, ownerID, args[10])
	require.Equal(t, "%100\\%\\_done%", args[11])
	require.Equal(t, storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED, args[12])

	for _, fragment := range []string{
		"resource_id = $1",
		"resource_id = ANY($2)",
		`(target_guid = $3 OR target_guid LIKE $4 ESCAPE E'\\')`,
		`(target_guid = $5 OR target_guid LIKE $6 ESCAPE E'\\')`,
		"dimension = $7",
		"rule_type = $8",
		"severity = $9",
		"state = $10",
		"owner_id = $11",
		`(title ILIKE $12 ESCAPE E'\\' OR description ILIKE $12 ESCAPE E'\\')`,
		"deleted = FALSE",
		"state <> $13",
	} {
		require.Contains(t, where, fragment)
	}
}

// The unassigned filter is the "no owner" complement of the owner filter: it
// renders owner_id IS NULL and binds no argument.
func TestBuildQualityRuleWhereOwnerUnassigned(t *testing.T) {
	t.Parallel()

	where, args, err := buildQualityRuleWhere(&FindQualityRuleMessage{OwnerUnassigned: true})
	require.NoError(t, err)
	require.Contains(t, where, "owner_id IS NULL")
	require.Equal(t, []any{storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED}, args,
		"the unassigned predicate binds no argument")

	// The assigned-owner predicate is unchanged: equality against its bound id.
	ownerID := int32(7)
	assignedWhere, assignedArgs, err := buildQualityRuleWhere(&FindQualityRuleMessage{OwnerID: &ownerID})
	require.NoError(t, err)
	require.Contains(t, assignedWhere, "owner_id = $1")
	require.NotContains(t, assignedWhere, "owner_id IS NULL")
	require.Equal(t, []any{ownerID, storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED}, assignedArgs)
}

// The default list is the actionable set: soft-deleted and archived rules are
// hidden, and a caller has to opt in explicitly.
func TestBuildQualityRuleWhereHidesDeletedAndArchivedByDefault(t *testing.T) {
	t.Parallel()

	where, args, err := buildQualityRuleWhere(&FindQualityRuleMessage{})
	require.NoError(t, err)
	require.Equal(t, "TRUE AND deleted = FALSE AND state <> $1", where)
	require.Equal(t, []any{storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED}, args)

	where, args, err = buildQualityRuleWhere(&FindQualityRuleMessage{
		IncludeDeleted:  true,
		IncludeArchived: true,
	})
	require.NoError(t, err)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)
}

// Search is a substring match, so a title containing LIKE metacharacters must
// match literally instead of acting as a wildcard.
func TestBuildQualityRuleWhereSearchEscapesLikeMetacharacters(t *testing.T) {
	t.Parallel()

	search := `50%_off\deal`
	where, args, err := buildQualityRuleWhere(&FindQualityRuleMessage{Search: &search})
	require.NoError(t, err)
	require.Equal(t, `%50\%\_off\\deal%`, args[0])
	require.Contains(t, where, `title ILIKE $1 ESCAPE E'\\'`)
}

// Only a definition change bumps the revision; display metadata and scheduling
// stay on the head so a rename does not invalidate result history.
func TestQualityRuleSemanticChanged(t *testing.T) {
	t.Parallel()

	current := &QualityRuleMessage{
		Dimension:      storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
		RuleType:       storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		TargetGUID:     "instances/inst-1;shop;public;users",
		TargetType:     storepb.MetaType_TABLE,
		TargetPath:     &storepb.QualityTargetPath{Instance: "instances/inst-1", Object: "users"},
		TargetMetaHash: []byte{0x01},
		Config:         &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(10)}}},
	}
	title := "renamed"
	description := "new description"
	severity := storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL
	state := storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED
	owner := int32(9)
	tags := []string{"core"}
	interval := 2 * time.Hour
	nextRunAt := time.Now()
	updatedBy := int32(5)

	tests := []struct {
		name  string
		patch *UpdateQualityRuleMessage
		want  bool
	}{
		{name: "empty patch", patch: &UpdateQualityRuleMessage{}, want: false},
		{name: "title", patch: &UpdateQualityRuleMessage{Title: &title}, want: false},
		{name: "description", patch: &UpdateQualityRuleMessage{Description: &description}, want: false},
		{name: "severity", patch: &UpdateQualityRuleMessage{Severity: &severity}, want: false},
		{name: "state", patch: &UpdateQualityRuleMessage{State: &state}, want: false},
		{name: "owner", patch: &UpdateQualityRuleMessage{OwnerID: &owner}, want: false},
		{name: "tags", patch: &UpdateQualityRuleMessage{Tags: &tags}, want: false},
		{name: "schedule interval", patch: &UpdateQualityRuleMessage{ScheduleInterval: &interval}, want: false},
		{name: "next run at", patch: &UpdateQualityRuleMessage{NextRunAt: &nextRunAt}, want: false},
		{name: "updated by", patch: &UpdateQualityRuleMessage{UpdatedBy: &updatedBy}, want: false},
		{
			name: "dimension",
			patch: &UpdateQualityRuleMessage{
				Dimension: qualityRuleEnumPtr(storepb.QualityDimension_QUALITY_DIMENSION_VALIDITY),
			},
			want: true,
		},
		{
			name: "rule type",
			patch: &UpdateQualityRuleMessage{
				RuleType: qualityRuleEnumPtr(storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX),
			},
			want: true,
		},
		{
			name: "target guid",
			patch: &UpdateQualityRuleMessage{
				TargetGUID: proto.String("instances/inst-1;shop;public;orders"),
			},
			want: true,
		},
		{
			name: "target type",
			patch: &UpdateQualityRuleMessage{
				TargetType: qualityRuleEnumPtr(storepb.MetaType_COLUMN),
			},
			want: true,
		},
		{
			name: "target path",
			patch: &UpdateQualityRuleMessage{
				TargetPath: &storepb.QualityTargetPath{Instance: "instances/inst-1", Object: "orders"},
			},
			want: true,
		},
		{
			name: "target path unchanged",
			patch: &UpdateQualityRuleMessage{
				TargetPath: &storepb.QualityTargetPath{Instance: "instances/inst-1", Object: "users"},
			},
			want: false,
		},
		{
			name:  "target meta hash",
			patch: &UpdateQualityRuleMessage{TargetMetaHash: &[]byte{0x02}},
			want:  true,
		},
		{
			name:  "target meta hash unchanged",
			patch: &UpdateQualityRuleMessage{TargetMetaHash: &[]byte{0x01}},
			want:  false,
		},
		{
			name: "config",
			patch: &UpdateQualityRuleMessage{
				Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(100)}}},
			},
			want: true,
		},
		{
			name: "config unchanged",
			patch: &UpdateQualityRuleMessage{
				Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(10)}}},
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, qualityRuleSemanticChanged(current, tc.patch))
		})
	}
}

// enumPtr returns a pointer to an enum value; the update patch needs pointers to
// tell "unchanged" apart from "explicitly UNSPECIFIED".
func qualityRuleEnumPtr[T ~int32](v T) *T {
	return &v
}

// The config and target path are round-tripped through the same JSONB
// representation, so a oneof and the shared threshold fields must survive.
func TestQualityRuleJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *storepb.QualityRuleConfig
	}{
		{
			name: "oneof and shared fields",
			config: &storepb.QualityRuleConfig{
				Config: &storepb.QualityRuleConfig_Range{
					Range: &storepb.RangeConfig{Column: "amount"},
				},
				ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_COUNT,
				OnEmpty:         storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_FAIL,
			},
		},
		{
			name:   "empty config",
			config: &storepb.QualityRuleConfig{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := marshalQualityProto(tc.config)
			require.NoError(t, err)
			var got storepb.QualityRuleConfig
			require.NoError(t, unmarshalQualityProto(raw, &got))
			require.True(t, proto.Equal(tc.config, &got))
		})
	}
}

// A nil message must be stored as an object, because the JSONB columns are NOT
// NULL and every reader unmarshals them into a message.
func TestMarshalQualityRuleProtoNormalizesNil(t *testing.T) {
	t.Parallel()

	raw, err := marshalQualityProto(nil)
	require.NoError(t, err)
	require.Equal(t, "{}", string(raw))

	// A typed nil pointer still reaches protojson as a nil message; its "null"
	// output must be normalized too.
	var config *storepb.QualityRuleConfig
	raw, err = marshalQualityProto(config)
	require.NoError(t, err)
	require.Equal(t, "{}", string(raw))

	// A stored "null" leaves the message empty instead of failing the read.
	var got storepb.QualityRuleConfig
	require.NoError(t, unmarshalQualityProto([]byte("null"), &got))
	require.True(t, proto.Equal(&storepb.QualityRuleConfig{}, &got))
}

// qualityRuleByIDsStatementShape pins the reverse lookup the API uses to render a
// result row's numeric rule_id as a rule resource name.
func TestQualityRuleByIDsStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityRuleByIDsStatement, "FROM quality_rule")
	require.Contains(t, qualityRuleByIDsStatement, "id = ANY($1::bigint[])")
	require.Contains(t, qualityRuleByIDsStatement, "ORDER BY id")
}

func TestListQualityRulesByIDsEmptyInputIsEmptySlice(t *testing.T) {
	t.Parallel()

	// A zero Store has no pool: reaching the database here would panic, so this
	// also pins that an empty input is a no-op.
	s := &Store{}
	rules, err := s.ListQualityRulesByIDs(t.Context(), nil)
	require.NoError(t, err)
	require.NotNil(t, rules)
	require.Empty(t, rules)

	rules, err = s.ListQualityRulesByIDs(t.Context(), []int64{})
	require.NoError(t, err)
	require.NotNil(t, rules)
	require.Empty(t, rules)
}
