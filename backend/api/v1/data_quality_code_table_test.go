package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestQualityCodeTableNameRoundTrip(t *testing.T) {
	t.Parallel()

	name := qualityCodeTableName("ws1", "abc123")
	require.Equal(t, "workspaces/ws1/qualityCodeTables/abc123", name)
	workspace, resourceID, err := parseQualityCodeTableName(name)
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, "abc123", resourceID)

	_, _, err = parseQualityCodeTableName("workspaces/ws1/qualityRules/abc123")
	require.Error(t, err)
}

func TestConvertQualityCodeTableToV1(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	author := int32(3)
	table := &store.QualityCodeTableMessage{
		ResourceID:  "abc123",
		Title:       "Enterprise status",
		Description: "SG 3-2021 status codes",
		Values:      []string{"01", "02"},
		ValueCount:  2,
		CreatedBy:   &author,
		UpdatedBy:   &author,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	listed := convertQualityCodeTableToV1("ws1", table, false)
	require.Equal(t, "workspaces/ws1/qualityCodeTables/abc123", listed.GetName())
	require.Equal(t, "Enterprise status", listed.GetTitle())
	require.Empty(t, listed.GetValues(), "a list response omits the values")
	require.Equal(t, int32(2), listed.GetValueCount())
	require.Equal(t, common.FormatUserUID(3), listed.GetCreatedBy())
	require.NotNil(t, listed.GetCreateTime())

	loaded := convertQualityCodeTableToV1("ws1", table, true)
	require.Equal(t, []string{"01", "02"}, loaded.GetValues())
	require.True(t, proto.Equal(listed.GetUpdateTime(), loaded.GetUpdateTime()))

	require.Nil(t, convertQualityCodeTableToV1("ws1", nil, true))
}

func TestQualityCodeTableInput(t *testing.T) {
	t.Parallel()

	title, description, values, err := qualityCodeTableInput(&v1pb.QualityCodeTable{
		Title:       "  Statuses ",
		Description: "  ",
		Values:      []string{"02", "01", "01", " "},
	})
	require.NoError(t, err)
	require.Equal(t, "Statuses", title)
	require.Empty(t, description)
	require.Equal(t, []string{"01", "02"}, values)

	_, _, _, err = qualityCodeTableInput(&v1pb.QualityCodeTable{})
	require.Error(t, err, "a title is required")

	_, _, _, err = qualityCodeTableInput(&v1pb.QualityCodeTable{
		Title:  string(make([]rune, qualityCodeTableTitleMaximum+1)),
		Values: []string{"01"},
	})
	require.Error(t, err)
}

// The rule config conversion preserves the code table reference in both directions.
func TestQualityRuleConfigStandardCodeTableRoundTrip(t *testing.T) {
	t.Parallel()

	storeConfig := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StandardCode{StandardCode: &storepb.StandardCodeConfig{
		Column:    "status",
		Standard:  storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE,
		CodeTable: "workspaces/ws1/qualityCodeTables/abc123",
	}}}
	converted := convertQualityRuleConfigToV1(storeConfig)
	require.Equal(t, "workspaces/ws1/qualityCodeTables/abc123", converted.GetStandardCode().GetCodeTable())
	back := convertQualityRuleConfigFromV1(converted)
	require.True(t, proto.Equal(storeConfig, back))
}
