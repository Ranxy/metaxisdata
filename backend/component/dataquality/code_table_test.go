package dataquality

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

func TestNormalizeCodeTableValues(t *testing.T) {
	t.Parallel()

	values, err := NormalizeCodeTableValues([]string{" B ", "A", "", "  ", "A", "C"})
	require.NoError(t, err)
	require.Equal(t, []string{"A", "B", "C"}, values, "trimmed, de-duplicated and sorted")

	values, err = NormalizeCodeTableValues(nil)
	require.NoError(t, err)
	require.Empty(t, values)

	// Over-long values and invalid UTF-8 are refused, not truncated.
	_, err = NormalizeCodeTableValues([]string{string(make([]byte, maxCodeTableValueBytes+1))})
	require.Error(t, err)
	_, err = NormalizeCodeTableValues([]string{string([]byte{0xff})})
	require.Error(t, err)

	tooMany := make([]string, 0, maxCodeTableValues+1)
	for index := 0; index <= maxCodeTableValues; index++ {
		tooMany = append(tooMany, fmt.Sprintf("code-%05d", index))
	}
	_, err = NormalizeCodeTableValues(tooMany)
	require.Error(t, err)
}

func TestStandardCodeTableSetAndAccepts(t *testing.T) {
	t.Parallel()

	custom := storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE
	uscc := storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE

	// A custom code table requires a reference; without one there is no algorithm
	// and no membership set.
	_, err := standardCodeTableSet(custom, "", nil)
	require.Error(t, err)
	_, err = standardCodeTableSet(uscc, "", nil)
	require.NoError(t, err)

	// A referenced table with no values is a defect: it would reject every value.
	_, err = standardCodeTableSet(custom, "workspaces/ws1/qualityCodeTables/t1", nil)
	require.Error(t, err)
	table, err := standardCodeTableSet(custom, "workspaces/ws1/qualityCodeTables/t1", []string{"A", "B"})
	require.NoError(t, err)

	require.True(t, standardCodeAccepts(custom, "A", table))
	require.False(t, standardCodeAccepts(custom, "C", table))
	require.False(t, standardCodeAccepts(custom, "a", table), "a workspace code table is case-sensitive")
	require.True(t, standardCodeAccepts(custom, "A ", table), "trailing CHAR padding is tolerated")

	// A built-in standard keeps its own algorithm and the table narrows it.
	builtIn, err := standardCodeTableSet(uscc, "workspaces/ws1/qualityCodeTables/t1",
		[]string{"91350100M000100Y43", "91330100799655058B"})
	require.NoError(t, err)
	require.True(t, standardCodeAccepts(uscc, "91350100M000100Y43", builtIn))
	require.False(t, standardCodeAccepts(uscc, "91440300708461136T", builtIn),
		"a valid code outside the narrowing table must fail")
	require.False(t, standardCodeAccepts(uscc, "91350100M000100Y44", builtIn),
		"a bad check digit fails even when the string is in the table")
}

// A built-in standard normalizes case in its own validator, so its narrowing table
// must be normalized too; otherwise a lowercase imported table silently rejects every
// valid uppercase code.
func TestStandardCodeTableNormalizesBuiltInEntries(t *testing.T) {
	t.Parallel()

	uscc := storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE
	table, err := standardCodeTableSet(uscc, "workspaces/ws1/qualityCodeTables/t1",
		[]string{"91350100m000100y43"})
	require.NoError(t, err)
	require.True(t, standardCodeAccepts(uscc, "91350100M000100Y43", table))
	require.False(t, standardCodeAccepts(uscc, "91330100799655058B", table))

	// The custom table stays case-sensitive: its values are authoritative as written.
	custom := storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE
	customTable, err := standardCodeTableSet(custom, "workspaces/ws1/qualityCodeTables/t1", []string{"open"})
	require.NoError(t, err)
	require.True(t, standardCodeAccepts(custom, "open", customTable))
	require.False(t, standardCodeAccepts(custom, "OPEN", customTable))
}

// A NUL byte is valid UTF-8 but JSONB cannot store it; refuse it as a bad value, not
// as a failed write.
func TestNormalizeCodeTableValuesRejectsNUL(t *testing.T) {
	t.Parallel()

	_, err := NormalizeCodeTableValues([]string{"ab\x00cd"})
	require.Error(t, err)
}

// A custom-code-table rule runs through the executor like any other standard code:
// membership is the only criterion, and the table values arrive through ExecOptions.
func TestExecuteCustomCodeTableRule(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE, 10)
	config.GetStandardCode().CodeTable = "workspaces/ws1/qualityCodeTables/t1"
	execution, _, err := runStandardCodeExecutionWithOptions(t, config, &db.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(1), "open"}, {int64(2), "closed"}, {int64(3), "retired"}},
	}, ExecOptions{CodeTableValues: []string{"open", "closed"}})
	require.NoError(t, err)
	require.Equal(t, int64(1), execution.Observation.FailedRows)
	require.Equal(t, int64(3), execution.Observation.TotalRows)
}

// A missing reference is a stored-config defect: the executor must not judge every
// value against an empty table.
func TestExecuteCustomCodeTableRuleWithoutReferenceFails(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE, 10)
	_, _, err := runStandardCodeExecution(t, config, &db.QueryResult{Columns: []string{"id", "name"}}, ExecOptions{})
	require.Error(t, err)
}

// A referenced table with no loaded values is likewise a defect.
func TestExecuteCustomCodeTableRuleWithEmptyTableFails(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE, 10)
	config.GetStandardCode().CodeTable = "workspaces/ws1/qualityCodeTables/t1"
	_, _, err := runStandardCodeExecution(t, config, &db.QueryResult{Columns: []string{"id", "name"}}, ExecOptions{})
	require.Error(t, err)
}
