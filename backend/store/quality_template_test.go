package store

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityRuleTemplateRowStub produces a quality_rule_template row for
// scanQualityRuleTemplate without a database. The destination order is
// qualityRuleTemplateColumns.
type qualityRuleTemplateRowStub struct {
	id          int64
	resourceID  string
	title       string
	description string
	dimension   storepb.QualityDimension
	ruleType    storepb.QualityRuleType
	config      []byte
	severity    storepb.QualitySeverity
	createdBy   sql.NullInt32
	updatedBy   sql.NullInt32
	createdAt   time.Time
	updatedAt   time.Time
}

func (s qualityRuleTemplateRowStub) Scan(dest ...any) error {
	if len(dest) != 12 {
		return fmt.Errorf("quality rule template stub got %d destinations, want 12", len(dest))
	}
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.resourceID
	*(dest[2].(*string)) = s.title
	*(dest[3].(*string)) = s.description
	*(dest[4].(*storepb.QualityDimension)) = s.dimension
	*(dest[5].(*storepb.QualityRuleType)) = s.ruleType
	*(dest[6].(*[]byte)) = s.config
	*(dest[7].(*storepb.QualitySeverity)) = s.severity
	*(dest[8].(*sql.NullInt32)) = s.createdBy
	*(dest[9].(*sql.NullInt32)) = s.updatedBy
	*(dest[10].(*time.Time)) = s.createdAt
	*(dest[11].(*time.Time)) = s.updatedAt
	return nil
}

// The row mapping is where the JSONB payload and the nullable principal columns
// get their Go shape. A template has no history, so a scan mistake would corrupt
// the only copy of its definition.
func TestScanQualityRuleTemplateReadsJSONBAndNullableColumns(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 2, 9, 30, 0, 0, time.UTC)
	updatedAt := createdAt.Add(2 * time.Hour)
	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{
			Unique: &storepb.UniqueConfig{FailThreshold: proto.Float64(10)},
		},
		OnEmpty:         storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS,
		TimeoutSeconds:  120,
		ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_COUNT,
	}
	configJSON, err := marshalQualityProto(config)
	require.NoError(t, err)

	msg, err := scanQualityRuleTemplate(qualityRuleTemplateRowStub{
		id:          7,
		resourceID:  "template-7",
		title:       "unique id",
		description: "checks one identifier column for duplicates",
		dimension:   storepb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
		ruleType:    storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		config:      configJSON,
		severity:    storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		createdBy:   sql.NullInt32{Int32: 3, Valid: true},
		updatedBy:   sql.NullInt32{},
		createdAt:   createdAt,
		updatedAt:   updatedAt,
	})
	require.NoError(t, err)

	require.Equal(t, int64(7), msg.ID)
	require.Equal(t, "template-7", msg.ResourceID)
	require.Equal(t, "unique id", msg.Title)
	require.Equal(t, "checks one identifier column for duplicates", msg.Description)
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS, msg.Dimension)
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, msg.RuleType)
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_WARNING, msg.Severity)
	require.Equal(t, createdAt, msg.CreatedAt)
	require.Equal(t, updatedAt, msg.UpdatedAt)
	require.NotNil(t, msg.CreatedBy)
	require.Equal(t, int32(3), *msg.CreatedBy)
	require.Nil(t, msg.UpdatedBy, "a NULL updated_by stays unset")
	require.Equal(t, 10.0, msg.Config.GetUnique().GetFailThreshold())
	require.Equal(t, storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS, msg.Config.GetOnEmpty())
	require.Equal(t, int32(120), msg.Config.GetTimeoutSeconds())
}

// An empty or "null" payload leaves an empty config rather than an error: the
// JSONB column has a default, and a row written by an older build still reads.
func TestScanQualityRuleTemplateAcceptsEmptyConfig(t *testing.T) {
	t.Parallel()

	for _, raw := range [][]byte{nil, []byte("null"), []byte("{}")} {
		msg, err := scanQualityRuleTemplate(qualityRuleTemplateRowStub{
			resourceID: "template-empty",
			config:     raw,
		})
		require.NoError(t, err)
		require.NotNil(t, msg.Config)
		require.Nil(t, msg.Config.GetConfig())
	}
}

// The resource id is the API-facing identity; a filter on it is the only scoping
// a template list has, and an empty one must not narrow the query.
func TestBuildQualityRuleTemplateWhereMatchesResourceID(t *testing.T) {
	t.Parallel()

	where, args := buildQualityRuleTemplateWhere(nil)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)

	id := "  template-1  "
	where, args = buildQualityRuleTemplateWhere(&FindQualityRuleTemplateMessage{ResourceID: &id})
	require.Equal(t, "TRUE AND resource_id = $1", where)
	require.Equal(t, []any{"template-1"}, args)

	blank := "   "
	where, args = buildQualityRuleTemplateWhere(&FindQualityRuleTemplateMessage{ResourceID: &blank})
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)
}

// The list is newest first with id breaking timestamp ties, and the limit/offset
// parameters follow the predicate so a page boundary is stable.
func TestQualityRuleTemplateListQueryOrdersNewestFirst(t *testing.T) {
	t.Parallel()

	query, args := qualityRuleTemplateListQuery(nil)
	require.Contains(t, query, `FROM quality_rule_template WHERE TRUE ORDER BY created_at DESC, id DESC`)
	require.Empty(t, args)

	limit, offset := 51, 100
	query, args = qualityRuleTemplateListQuery(&FindQualityRuleTemplateMessage{Limit: &limit, Offset: &offset})
	require.Contains(t, query, "ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2")
	require.Equal(t, []any{51, 100}, args)

	negative := -5
	query, args = qualityRuleTemplateListQuery(&FindQualityRuleTemplateMessage{Limit: &negative})
	require.Contains(t, query, "LIMIT $1")
	require.Equal(t, []any{0}, args, "a negative limit must not reach the database")
}

// A template without its identity fields would store a row no editor can apply,
// so create and update refuse it before the insert.
func TestValidateQualityRuleTemplateIdentity(t *testing.T) {
	t.Parallel()

	valid := func() *QualityRuleTemplateMessage {
		return &QualityRuleTemplateMessage{
			ResourceID: "template-1",
			Dimension:  storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Severity:   storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		}
	}
	require.NoError(t, validateQualityRuleTemplateIdentity(valid()))

	tests := []struct {
		name      string
		mutate    func(*QualityRuleTemplateMessage)
		wantError string
	}{
		{name: "nil message", mutate: nil, wantError: "a quality rule template is required"},
		{name: "blank resource id", mutate: func(m *QualityRuleTemplateMessage) { m.ResourceID = "  " }, wantError: "resource id is required"},
		{name: "unspecified dimension", mutate: func(m *QualityRuleTemplateMessage) {
			m.Dimension = storepb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED
		}, wantError: "dimension is required"},
		{name: "unspecified rule type", mutate: func(m *QualityRuleTemplateMessage) {
			m.RuleType = storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED
		}, wantError: "rule type is required"},
		{name: "unspecified severity", mutate: func(m *QualityRuleTemplateMessage) {
			m.Severity = storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED
		}, wantError: "severity is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			msg := valid()
			if test.mutate != nil {
				test.mutate(msg)
			} else {
				msg = nil
			}
			err := validateQualityRuleTemplateIdentity(msg)
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantError)
		})
	}
}
