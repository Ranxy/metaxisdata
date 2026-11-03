package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// acceptedValuesQueryConfig builds one accepted-values rule whose set comes from
// a query.
func acceptedValuesQueryConfig(sql, valueColumn string) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
		AcceptedValues: &storepb.AcceptedValuesConfig{
			Column: "name",
			Query:  &storepb.AcceptedValuesQuery{Sql: sql, ValueColumn: valueColumn},
		},
	}}
}

// TestValidateAcceptedValuesQueryOnMSSQL pins that a query source which SQL Server
// cannot embed as a derived table is refused at save time: T-SQL rejects a CTE and
// a top-level ORDER BY inside a derived table, so saving one would only produce a
// check that always errors. PostgreSQL accepts the same shapes.
func TestValidateAcceptedValuesQueryOnMSSQL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		sql          string
		wantMSSQLErr bool
	}{
		{name: "plain select", sql: "SELECT name FROM orders"},
		{name: "cte", sql: "WITH x AS (SELECT name FROM orders) SELECT name FROM x", wantMSSQLErr: true},
		{name: "order by", sql: "SELECT name FROM orders ORDER BY name", wantMSSQLErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := acceptedValuesQueryConfig(test.sql, "name")
			mssqlErr := ValidateConfig(validationTarget(storepb.Engine_MSSQL), storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, config)
			if test.wantMSSQLErr {
				require.Error(t, mssqlErr)
				require.Equal(t, common.Invalid, common.ErrorCode(mssqlErr))
				return
			}
			require.NoError(t, mssqlErr)
			require.NoError(t, ValidateConfig(validationTarget(storepb.Engine_POSTGRES), storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, config),
				"PostgreSQL accepts a CTE or ORDER BY in a derived table")
		})
	}
}

// TestCompileAcceptedValuesQueryWithTrailingComment pins that a trailing line
// comment in the user statement cannot swallow the wrapper's closing parenthesis
// and WHERE clause.
func TestCompileAcceptedValuesQueryWithTrailingComment(t *testing.T) {
	t.Parallel()

	config := acceptedValuesQueryConfig("SELECT name FROM orders -- keep in sync", "name")
	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		Config:  config,
	})
	require.NoError(t, err)
	require.Contains(t, query.SQL, "orders -- keep in sync\n) dq_values")
}

// TestCompileAcceptedValuesFieldRejectsTheCheckedColumn pins that a reference
// column source reads a different column: the same column would never fail a row.
func TestCompileAcceptedValuesFieldRejectsTheCheckedColumn(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
		AcceptedValues: &storepb.AcceptedValuesConfig{Column: "name", Field: "name"},
	}}
	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	_, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		Config:  config,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "differ from the checked column")
}
