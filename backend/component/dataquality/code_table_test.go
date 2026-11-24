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

// A NUL byte is valid UTF-8 but JSONB cannot store it; refuse it as a bad value, not
// as a failed write.
func TestNormalizeCodeTableValuesRejectsNUL(t *testing.T) {
	t.Parallel()

	_, err := NormalizeCodeTableValues([]string{"ab\x00cd"})
	require.Error(t, err)
}

// A custom-code-table rule runs through the executor like any other standard code:
// membership is the only criterion, and the table values arrive through ExecOptions.
// The workspace's codes are authoritative as written, so the match is case-sensitive
// and only trailing CHAR padding is tolerated.
func TestExecuteCustomCodeTableRule(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE, 10)
	config.GetStandardCode().CodeTable = "workspaces/ws1/qualityCodeTables/t1"
	execution, _, err := runStandardCodeExecutionWithOptions(t, config, &db.QueryResult{
		Columns: []string{"id", "name"},
		Rows: [][]any{
			{int64(1), "open"},
			{int64(2), "closed"},
			{int64(3), "retired"},
			{int64(4), "OPEN"},
			{int64(5), "closed "},
		},
	}, ExecOptions{CodeTableValues: []string{"open", "closed"}})
	require.NoError(t, err)
	require.Equal(t, int64(2), execution.Observation.FailedRows, "a case variant and an unknown code fail")
	require.Equal(t, int64(5), execution.Observation.TotalRows)
}

// A built-in standard keeps its own check-digit algorithm; a workspace code table
// only narrows it. Table entries and sampled values are normalized the way the
// validator is, and trailing CHAR padding is tolerated.
func TestExecuteBuiltInStandardNarrowedByCodeTable(t *testing.T) {
	t.Parallel()

	uscc := storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE
	config := standardCodeRuleConfig("name", uscc, 10)
	config.GetStandardCode().CodeTable = "workspaces/ws1/qualityCodeTables/t1"
	execution, _, err := runStandardCodeExecutionWithOptions(t, config, &db.QueryResult{
		Columns: []string{"id", "name"},
		Rows: [][]any{
			{int64(1), "91350100M000100Y43"},  // valid and in the table
			{int64(2), "91350100M000100Y43 "}, // trailing padding is tolerated
			{int64(3), "91350100m000100y43"},  // lower case normalizes like the validator
			{int64(4), "91440300708461136T"},  // valid, but outside the narrowing table
			{int64(5), "91350100M000100Y44"},  // in the table, but the check digit is bad
		},
	}, ExecOptions{CodeTableValues: []string{"91350100m000100y43", "91350100m000100y44"}})
	require.NoError(t, err)
	require.Equal(t, int64(2), execution.Observation.FailedRows,
		"the narrowing table keeps the standard's algorithm instead of replacing it")
	require.Equal(t, int64(5), execution.Observation.TotalRows)
}

// A code table the rule names but cannot use is a stored-config defect: the
// executor must not judge every value against an empty set.
func TestExecuteCodeTableDefectsFail(t *testing.T) {
	t.Parallel()

	custom := storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE
	uscc := storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE
	cases := []struct {
		name      string
		standard  storepb.QualityStandardCode
		reference string
	}{
		{name: "custom standard with no reference", standard: custom},
		{name: "custom table with no values", standard: custom, reference: "workspaces/ws1/qualityCodeTables/t1"},
		{name: "built-in narrowing table with no values", standard: uscc, reference: "workspaces/ws1/qualityCodeTables/t1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			config := standardCodeRuleConfig("name", testCase.standard, 10)
			config.GetStandardCode().CodeTable = testCase.reference
			_, _, err := runStandardCodeExecution(t, config, &db.QueryResult{Columns: []string{"id", "name"}}, ExecOptions{})
			require.Error(t, err)
		})
	}
}
