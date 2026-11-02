package qualityrunner

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestCodeTableResourceID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc123", codeTableResourceID("workspaces/ws1/qualityCodeTables/abc123"))
	require.Equal(t, "abc123", codeTableResourceID("workspaces/-/qualityCodeTables/abc123"))
	require.Empty(t, codeTableResourceID(""))
	require.Empty(t, codeTableResourceID("workspaces/ws1/qualityRules/abc123"))
}

func TestBatchCodeTablesReadsOncePerReference(t *testing.T) {
	t.Parallel()

	calls := 0
	cache := newBatchCodeTables(func(_ context.Context, _ string) ([]string, error) {
		calls++
		return []string{"A", "B"}, nil
	})

	first, err := cache.forRule(context.Background(), "workspaces/ws1/qualityCodeTables/t1")
	require.NoError(t, err)
	require.Equal(t, []string{"A", "B"}, first)
	second, err := cache.forRule(context.Background(), "workspaces/ws1/qualityCodeTables/t1")
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, calls)

	empty, err := cache.forRule(context.Background(), "")
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Equal(t, 1, calls, "a rule without a reference costs no read")
}

func TestBatchCodeTablesDoesNotCacheErrors(t *testing.T) {
	t.Parallel()

	calls := 0
	cache := newBatchCodeTables(func(_ context.Context, _ string) ([]string, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("store unavailable")
		}
		return []string{"A"}, nil
	})

	_, err := cache.forRule(context.Background(), "workspaces/ws1/qualityCodeTables/t1")
	require.Error(t, err)
	values, err := cache.forRule(context.Background(), "workspaces/ws1/qualityCodeTables/t1")
	require.NoError(t, err)
	require.Equal(t, []string{"A"}, values)
	require.Equal(t, 2, calls)
}

func TestCodeTableReference(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StandardCode{StandardCode: &storepb.StandardCodeConfig{
		Column: "code", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE,
		CodeTable: "workspaces/ws1/qualityCodeTables/t1",
	}}}
	require.Equal(t, "workspaces/ws1/qualityCodeTables/t1",
		codeTableReference(storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE, config))
	require.Empty(t, codeTableReference(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config))
}
