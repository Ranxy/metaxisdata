package dataquality

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

func TestIsPersonalSemanticType(t *testing.T) {
	t.Parallel()

	require.True(t, IsPersonalSemanticType(storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_RESIDENT_ID_CARD))
	for _, label := range []storepb.QualitySemanticType{
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ID,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_BUSINESS_KEY,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNIFIED_SOCIAL_CREDIT_CODE,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ORGANIZATION_CODE,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNKNOWN,
	} {
		require.Falsef(t, IsPersonalSemanticType(label), "%s must not be treated as personal data", label)
	}
}

func TestRedactPersonalProfile(t *testing.T) {
	t.Parallel()

	profile := &storepb.ColumnProfile{
		TotalRows:     100,
		NullCount:     2,
		DistinctCount: 98,
		MinValue:      "110105194912310021",
		MaxValue:      "110105199901010018",
		TopValues: []*storepb.ProfileValue{
			{Value: "110105194912310021", Count: 1},
		},
		Patterns: []string{"numeric"},
	}
	RedactPersonalProfile(profile)

	require.Equal(t, "110105********0021", profile.GetMinValue())
	require.Equal(t, "110105********0018", profile.GetMaxValue())
	require.Empty(t, profile.GetTopValues(), "frequent values are raw values too")
	// Counts and shapes describe the column, not its values.
	require.Equal(t, int64(100), profile.GetTotalRows())
	require.Equal(t, int64(2), profile.GetNullCount())
	require.Equal(t, int64(98), profile.GetDistinctCount())
	require.Equal(t, []string{"numeric"}, profile.GetPatterns())

	// A nil profile is a no-op, not a panic.
	RedactPersonalProfile(nil)
}

// An aggregate rule stores its failed rows through a second query; the personal
// column named by the caller is redacted there too.
func TestExecuteAggregateRedactsPersonalSampleColumns(t *testing.T) {
	t.Parallel()

	config := validationNullPercentConfig(&storepb.NullPercentConfig{Column: "email", MaxNullPct: protoFloat(1)})
	snapshot := execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config)
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		{Columns: []string{"total_rows", "failed_rows"}, Rows: [][]any{{int64(10), int64(1)}}},
		{Columns: []string{"id", "email"}, Rows: [][]any{{int64(1), "alice@example.com"}}},
	}}

	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL), snapshot,
		ExecOptions{StoreSamples: true, SampleLimit: 20, MaskColumns: []string{"email"}})
	require.NoError(t, err)
	require.NotNil(t, execution.Samples)
	cell := execution.Samples.GetSamples()[0].GetColumns()[1]
	require.Equal(t, "email", cell.GetColumn())
	require.Equal(t, MaskPersonalValue("alice@example.com"), cell.GetValue())
	require.True(t, cell.GetMasked())
	// The key column is not personal data and stays readable.
	require.Equal(t, "1", execution.Samples.GetSamples()[0].GetColumns()[0].GetValue())
}

// The CEL path builds its sample in process; masking still applies.
func TestExecuteCELRedactsPersonalSampleColumns(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("email == \"nobody@example.com\"", []string{"email"}, 100)
	queryer := &execFakeQueryer{results: []*db.QueryResult{{
		Columns: []string{"email"},
		Rows:    [][]any{{"alice@example.com"}},
	}}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, config),
		ExecOptions{StoreSamples: true, SampleLimit: 20, MaskColumns: []string{"email"}})
	require.NoError(t, err)
	require.NotNil(t, execution.Samples)
	cell := execution.Samples.GetSamples()[0].GetColumns()[0]
	require.Equal(t, MaskPersonalValue("alice@example.com"), cell.GetValue())
	require.True(t, cell.GetMasked())
}
