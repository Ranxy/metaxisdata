package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// variableInstant is a fixed execution start: 17:12:49 UTC on 2026-11-01, which is
// already 2026-11-02 in Asia/Shanghai. That makes a zone mistake visible as a
// one-day shift instead of a hidden same-day value.
var variableInstant = time.Date(2026, 11, 1, 17, 12, 49, 0, time.UTC)

func utcVariables() TimeVariables {
	return NewTimeVariables(variableInstant, time.UTC)
}

func TestResolveTimeVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		zone  *time.Location
		want  string
	}{
		{name: "now", token: "${now}", zone: time.UTC, want: "2026-11-01 17:12:49"},
		{name: "today is a date", token: "${today}", zone: time.UTC, want: "2026-11-01"},
		{name: "yesterday is a date", token: "${yesterday}", zone: time.UTC, want: "2026-10-31"},
		{name: "day offset stays a date", token: "${today-7d}", zone: time.UTC, want: "2026-10-25"},
		{name: "hour offset leaves midnight so renders a timestamp", token: "${today+2h}", zone: time.UTC, want: "2026-11-01 02:00:00"},
		{name: "minute offset on now", token: "${now-30m}", zone: time.UTC, want: "2026-11-01 16:42:49"},
		{name: "surrounding spaces are trimmed", token: " ${today} ", zone: time.UTC, want: "2026-11-01"},
		{name: "today follows the workspace zone", token: "${today}", zone: mustLocation(t, "Asia/Shanghai"), want: "2026-11-02"},
		{name: "yesterday follows the workspace zone", token: "${yesterday}", zone: mustLocation(t, "Asia/Shanghai"), want: "2026-11-01"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveTimeVariable(test.token, NewTimeVariables(variableInstant, test.zone))
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestResolveTimeVariableRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		code  common.Code
	}{
		{name: "unknown name", token: "${week}", code: common.Invalid},
		{name: "names are lower case", token: "${Today}", code: common.Invalid},
		{name: "unknown unit", token: "${today-7w}", code: common.Invalid},
		{name: "missing amount", token: "${today-d}", code: common.Invalid},
		{name: "offset above the cap", token: "${today-999d}", code: common.Invalid},
		{name: "no token at all", token: "today", code: common.Invalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ResolveTimeVariable(test.token, utcVariables())
			require.Error(t, err)
			require.Equal(t, test.code, common.ErrorCode(err))
		})
	}

	// Without an execution instant the token is a caller defect, not a user error.
	_, err := ResolveTimeVariable("${today}", TimeVariables{})
	require.Error(t, err)
	require.Equal(t, common.Internal, common.ErrorCode(err))
}

func TestResolveFilterVariables(t *testing.T) {
	t.Parallel()

	filter := Filter{Clauses: []FilterClause{
		{Column: "created_at", Op: FilterGTE, Values: []string{"${today-7d}"}},
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
		{Column: "amount", Op: FilterIN, Values: []string{"${yesterday}", "10"}},
	}}
	resolved, err := ResolveFilterVariables(filter, utcVariables())
	require.NoError(t, err)
	require.Equal(t, []string{"2026-10-25"}, resolved.Clauses[0].Values)
	require.Equal(t, []string{"paid"}, resolved.Clauses[1].Values)
	require.Equal(t, []string{"2026-10-31", "10"}, resolved.Clauses[2].Values)
	// The caller's filter is not modified.
	require.Equal(t, "${today-7d}", filter.Clauses[0].Values[0])
}

func TestResolveFilterVariablesRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "a token mixed with text", value: "prefix-${today}"},
		{name: "a malformed token", value: "${today"},
		{name: "an unknown variable", value: "${week}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ResolveFilterVariables(Filter{Clauses: []FilterClause{
				{Column: "created_at", Op: FilterGTE, Values: []string{test.value}},
			}}, utcVariables())
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
		})
	}
}

func TestRenderCustomSQLVariables(t *testing.T) {
	t.Parallel()

	statement := "SELECT id FROM orders WHERE created_at >= ${today-7d} AND updated_at < ${now-1h}"
	tests := []struct {
		engine   storepb.Engine
		wantSQL  string
		wantArgs []any
	}{
		{
			engine:   storepb.Engine_MYSQL,
			wantSQL:  "SELECT id FROM orders WHERE created_at >= ? AND updated_at < ?",
			wantArgs: []any{"2026-10-25", "2026-11-01 16:12:49"},
		},
		{
			engine:   storepb.Engine_POSTGRES,
			wantSQL:  "SELECT id FROM orders WHERE created_at >= $1 AND updated_at < $2",
			wantArgs: []any{"2026-10-25", "2026-11-01 16:12:49"},
		},
		{
			engine:   storepb.Engine_MSSQL,
			wantSQL:  "SELECT id FROM orders WHERE created_at >= @p1 AND updated_at < @p2",
			wantArgs: []any{"2026-10-25", "2026-11-01 16:12:49"},
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			rendered, args, err := RenderCustomSQLVariables(statement, utcVariables(), dialect)
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, rendered)
			require.Equal(t, test.wantArgs, args)
		})
	}
}

func TestRenderCustomSQLVariablesWithoutTokens(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	statement := "SELECT id FROM orders WHERE status = 'paid'"
	rendered, args, err := RenderCustomSQLVariables(statement, utcVariables(), dialect)
	require.NoError(t, err)
	require.Equal(t, statement, rendered)
	require.Empty(t, args)
}

// A stored custom-SQL rule keeps the token; the compiler renders it as a bound
// argument like any other value.
func TestCompileCustomSQLRendersTimeVariables(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_POSTGRES),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
			CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT id FROM orders WHERE created_at >= ${today-7d}"},
		}},
		Variables: utcVariables(),
	})
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) AS failed_rows FROM (SELECT id FROM orders WHERE created_at >= $1) dq`, query.SQL)
	require.Equal(t, []any{"2026-10-25"}, query.Args)
}

// A filter value may be one whole time variable; the compiled statement binds its
// rendering as a parameter.
func TestCompileFilterRendersTimeVariables(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_POSTGRES),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Config:  &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: validationPtr(int64(1))}}},
		Filter: Filter{Clauses: []FilterClause{
			{Column: "created_at", Op: FilterGTE, Values: []string{"${today-7d}"}},
		}},
		Variables: utcVariables(),
	})
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) AS total_rows FROM "public"."orders" WHERE "created_at" >= $1`, query.SQL)
	require.Equal(t, []any{"2026-10-25"}, query.Args)
}

// A rule that uses a variable but is compiled without an execution instant is a
// caller defect rather than a silently wrong window.
func TestCompileTimeVariablesRequireAnInstant(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	_, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Config:  &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: validationPtr(int64(1))}}},
		Filter: Filter{Clauses: []FilterClause{
			{Column: "created_at", Op: FilterGTE, Values: []string{"${today}"}},
		}},
	})
	require.Error(t, err)
	require.Equal(t, common.Internal, common.ErrorCode(err))
}

// The custom-SQL parser sees a variable rendered as a quoted literal: the
// statement's shape is what is being checked, and a variable inside quotes cannot
// become a placeholder surrounded by quotes.
func TestValidateCustomSQLAcceptsTimeVariables(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateCustomSQL(storepb.Engine_MYSQL,
		"SELECT id FROM orders WHERE created_at >= ${today-7d} AND status = 'paid'"))
	require.NoError(t, ValidateCustomSQL(storepb.Engine_POSTGRES,
		"WITH recent AS (SELECT id FROM orders WHERE created_at >= ${today-7d}) SELECT id FROM recent"))

	err := ValidateCustomSQL(storepb.Engine_MYSQL, "SELECT id FROM orders WHERE created_at >= '${today}'")
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	err = ValidateCustomSQL(storepb.Engine_MYSQL, "SELECT id FROM orders WHERE created_at >= ${week}")
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// Variables resolve only in the filter and custom SQL; a token in any other value
// field is refused instead of compared literally.
func TestValidateConfigRefusesTimeVariablesElsewhere(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_MYSQL)
	err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
			AcceptedValues: &storepb.AcceptedValuesConfig{Column: "status", Values: []string{"${today}"}},
		}})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	// The same text is fine in custom SQL.
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		&storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
			CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT id FROM orders WHERE created_at >= ${today}"},
		}}))
}

// A filter value that a variable renders to must still fit the column's type: a
// rendered date cannot be compared with a numeric column.
func TestValidateConfigRefusesVariableAgainstNumericFilter(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_MYSQL)
	err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		&storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: validationPtr(int64(1))}},
			Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
				{Column: "amount", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE, Values: []string{"${today}"}},
			}},
		})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	require.NoError(t, err)
	return location
}
