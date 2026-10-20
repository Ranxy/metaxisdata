package dataquality

import (
	"strconv"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Compiler-side constants. They mirror the limits the config proto documents and
// are enforced here because a compiler is the last place a bad value can still be
// rejected before it reaches SQL.
const (
	// compileMaxAcceptedValues bounds one accepted_values list.
	compileMaxAcceptedValues = 200
	// compileMaxUniqueColumns bounds one unique_combination column list.
	compileMaxUniqueColumns = 10
	// compileMaxCustomSQLLength bounds a custom SQL definition (8 KiB), matching
	// the design's storage limit.
	compileMaxCustomSQLLength = 8 * 1024
	// maxGroupCheckColumns bounds a group-check rule's grouping list.
	maxGroupCheckColumns = 5
	// maxNullValues bounds the custom missing values a null_percent rule may name.
	maxNullValues = 20
)

// compileTable renders the target relation for the dialect. The foundation
// dialects report a target that cannot be qualified as a plain error, so the
// compiler re-codes it as a validation error.
func compileTable(dialect Dialect, target ResolvedTarget) (string, error) {
	table, err := dialect.QuoteTable(target)
	if err != nil {
		return "", invalidConfig("cannot render the rule's target table: %v", err)
	}
	return table, nil
}

// compileAppendWhere adds one condition to a WHERE clause produced by whereClause.
// The clause already carries the WHERE keyword when it is non-empty.
func compileAppendWhere(where, condition string) string {
	if where == "" {
		return " WHERE " + condition
	}
	return where + " AND " + condition
}

// compileArgs collects the bound arguments of one statement. The filter compiles
// on its own and numbers its placeholders from 1, but several rules bind config
// values in the SELECT list, before the WHERE clause appears in the SQL text.
// That gives two different orders to reconcile: numbered dialects ($n, @pN) bind
// by number, while positional dialects (?) bind in the order the ?s appear in the
// text. The filter arguments therefore stay first for numbered dialects and move
// last for positional ones; add hands out each config argument's placeholder.
type compileArgs struct {
	dialect    Dialect
	filterArgs []any
	configArgs []any
}

// newCompileArgs starts a statement's argument list from the filter's arguments.
func newCompileArgs(dialect Dialect, filterArgs []any) *compileArgs {
	return &compileArgs{dialect: dialect, filterArgs: filterArgs}
}

// add appends one config argument and returns the placeholder that binds it.
func (a *compileArgs) add(value any) string {
	a.configArgs = append(a.configArgs, value)
	if a.positional() {
		return a.dialect.Placeholder(len(a.configArgs))
	}
	return a.dialect.Placeholder(len(a.filterArgs) + len(a.configArgs))
}

// positional reports whether the dialect binds arguments by their order in the
// SQL text rather than by an explicit number.
func (a *compileArgs) positional() bool {
	return compilePositional(a.dialect)
}

// compilePositional reports whether a dialect binds arguments by their order in
// the SQL text (?) instead of by an explicit number ($n / @pN). A statement that
// repeats a predicate must pass a positional dialect's arguments once per
// occurrence but may reuse a numbered placeholder.
func compilePositional(dialect Dialect) bool {
	return dialect.Placeholder(1) == dialect.Placeholder(2)
}

// values returns the argument slice in the order the driver expects.
func (a *compileArgs) values() []any {
	if len(a.filterArgs) == 0 && len(a.configArgs) == 0 {
		return nil
	}
	values := make([]any, 0, len(a.filterArgs)+len(a.configArgs))
	if a.positional() {
		values = append(values, a.configArgs...)
		return append(values, a.filterArgs...)
	}
	values = append(values, a.filterArgs...)
	return append(values, a.configArgs...)
}

// groupCheckColumns resolves and de-duplicates a group-check rule's grouping
// columns, preserving the configured order. The same function is used by
// validation, the aggregate compiler and the sample compiler so all three reject
// the same config with the same message.
func groupCheckColumns(target ResolvedTarget, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, invalidConfig("a group-check rule needs at least one column")
	}
	if len(names) > maxGroupCheckColumns {
		return nil, invalidConfig("a group-check rule allows at most %d group columns, got %d", maxGroupCheckColumns, len(names))
	}
	columns := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		column, err := requireEqualityColumn(target, strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		if _, ok := seen[column.Name]; ok {
			return nil, invalidConfig("group column %q is named more than once", column.Name)
		}
		seen[column.Name] = struct{}{}
		columns = append(columns, column.Name)
	}
	return columns, nil
}

// groupCheckBounds validates the per-group row bounds shared by validation, the
// aggregate compiler and the sample compiler: at least one bound, neither
// negative, and the lower bound not above the upper one.
func groupCheckBounds(config *storepb.GroupCheckConfig) (minValue, maxValue int64, hasMin, hasMax bool, err error) {
	if config.MinRowsPerGroup == nil && config.MaxRowsPerGroup == nil {
		return 0, 0, false, false, invalidConfig("a group-check rule needs min_rows_per_group or max_rows_per_group")
	}
	if config.MinRowsPerGroup != nil {
		if *config.MinRowsPerGroup < 0 {
			return 0, 0, false, false, invalidConfig("min_rows_per_group must not be negative")
		}
		minValue, hasMin = *config.MinRowsPerGroup, true
	}
	if config.MaxRowsPerGroup != nil {
		if *config.MaxRowsPerGroup < 0 {
			return 0, 0, false, false, invalidConfig("max_rows_per_group must not be negative")
		}
		maxValue, hasMax = *config.MaxRowsPerGroup, true
	}
	if hasMin && hasMax && minValue > maxValue {
		return 0, 0, false, false, invalidConfig("min_rows_per_group %d exceeds max_rows_per_group %d", minValue, maxValue)
	}
	return minValue, maxValue, hasMin, hasMax, nil
}

// compileCountSelect renders the two-column shape shared by the column-level
// count rules: the filtered row total and the rows the failure condition selects.
// The evaluator reads both columns by name, so they are a contract.
func compileCountSelect(table, where, failureCondition string, args []any) CompiledQuery {
	return CompiledQuery{
		SQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN " + failureCondition +
			" THEN 1 ELSE 0 END) AS failed_rows FROM " + table + where,
		Args:    args,
		Kind:    QueryAggregate,
		Columns: []string{"total_rows", "failed_rows"},
	}
}

// compileValueArg converts one wire value to a typed argument using the column's
// kind. The engine casts temporal strings itself; reformatting a date here could
// change the comparison. A value the column cannot represent is a save-time
// validation error, never a runtime surprise.
func compileValueArg(column ColumnInfo, value string) (any, error) {
	switch column.Kind {
	case ColumnKindNumeric:
		if integer, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return integer, nil
		}
		number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, invalidConfig("value %q is not a number for column %q", value, column.Name)
		}
		return number, nil
	case ColumnKindTemporal, ColumnKindString:
		return value, nil
	case ColumnKindBoolean:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, invalidConfig("value %q is not a boolean for column %q", value, column.Name)
		}
		return parsed, nil
	case ColumnKindUnknown:
		return nil, invalidConfig("column %q has a type this build cannot compare values against", column.Name)
	}
	return nil, invalidConfig("column %q has an unclassified type", column.Name)
}

// numericValue reports the numeric reading of a converted argument. It is used to
// check that a range's lower bound does not exceed its upper bound.
func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

// compileStringColumn resolves a column and requires text operations on its type,
// mirroring validateStringColumn.
func compileStringColumn(input CompileInput, name string) (ColumnInfo, error) {
	return requireTextColumn(input.Target, name)
}

// compileTemporalColumn resolves a column and requires it to be a date/time.
func compileTemporalColumn(input CompileInput, name string) (ColumnInfo, error) {
	column, err := requireColumn(input.Target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind != ColumnKindTemporal {
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a temporal column", column.Name, column.Type)
	}
	return column, nil
}

// compileComparableColumn resolves a column whose values support ordering, for
// range and cross-column rules. Text columns are excluded: a range over text is
// a collation question, not a data quality check.
func compileComparableColumn(input CompileInput, name string) (ColumnInfo, error) {
	column, err := requireColumn(input.Target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind != ColumnKindNumeric && column.Kind != ColumnKindTemporal {
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a numeric or temporal column", column.Name, column.Type)
	}
	return column, nil
}

// compileComparisonOperator maps the cross-column operator enum to SQL.
func compileComparisonOperator(op storepb.QualityComparisonOperator) (string, error) {
	switch op {
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ:
		return "=", nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_NE:
		return "<>", nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LT:
		return "<", nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LTE:
		return "<=", nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GT:
		return ">", nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GTE:
		return ">=", nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_UNSPECIFIED:
		return "", invalidConfig("a cross-column rule needs a comparison operator")
	default:
		return "", invalidConfig("unknown comparison operator %s", op)
	}
}

// compileCrossColumnFailure renders the predicate that selects rows violating the
// comparison. Without a tolerance that is the negation of the comparison; with
// one the tolerance is applied on the failing side of the comparison so floating
// point noise does not register as a violation.
func compileCrossColumnFailure(left, right string, op storepb.QualityComparisonOperator, tolerance *float64, placeholder string) (string, error) {
	if tolerance == nil {
		operator, err := compileComparisonOperator(op)
		if err != nil {
			return "", err
		}
		return "NOT (" + left + " " + operator + " " + right + ")", nil
	}
	switch op {
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ:
		return "ABS(" + left + " - " + right + ") > " + placeholder, nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_NE:
		return "ABS(" + left + " - " + right + ") <= " + placeholder, nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LT:
		return left + " >= " + right + " - " + placeholder, nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LTE:
		return left + " > " + right + " + " + placeholder, nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GT:
		return left + " <= " + right + " + " + placeholder, nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GTE:
		return left + " < " + right + " - " + placeholder, nil
	case storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_UNSPECIFIED:
		return "", invalidConfig("a cross-column rule needs a comparison operator")
	default:
		return "", invalidConfig("unknown comparison operator %s", op)
	}
}

// compileRegexNotMatch returns the dialect's not-matches operator. MSSQL has no
// regular-expression operator at all, so it is rejected even though the support
// table already refuses regex rules for it: the compiler must not silently emit a
// wrong query if it is called directly.
func compileRegexNotMatch(dialect Dialect) (string, error) {
	if !dialect.SupportsRegex() {
		return "", invalidConfig("%s has no regular-expression operator; use custom SQL", dialect.Name())
	}
	switch dialect.(type) {
	case postgresDialect:
		return "!~", nil
	case mysqlDialect:
		return "NOT REGEXP", nil
	default:
		return "", invalidConfig("%s has no regular-expression operator; use custom SQL", dialect.Name())
	}
}

// compileFreshnessLagExpr renders how stale the newest timestamp is, in seconds.
// The comparison happens inside the target database so a clock difference between
// the server and the engine cannot skew the verdict; a NULL timestamp stays NULL.
func compileFreshnessLagExpr(dialect Dialect, maxTimestamp string) (string, error) {
	switch dialect.(type) {
	case postgresDialect:
		return "EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - " + maxTimestamp + "))", nil
	case mysqlDialect:
		return "TIMESTAMPDIFF(SECOND, " + maxTimestamp + ", NOW())", nil
	case mssqlDialect:
		return "DATEDIFF(SECOND, " + maxTimestamp + ", SYSUTCDATETIME())", nil
	default:
		return "", invalidConfig("%s has no freshness expression", dialect.Name())
	}
}

// compileRelatedTable renders a second table that must live in the target's own
// database (MySQL family) or schema (PostgreSQL/MSSQL). role names the table in
// error messages ("parent table", "right table"); ruleName names the rule kind.
func compileRelatedTable(dialect Dialect, target ResolvedTarget, spec, role, ruleName string) (string, error) {
	schema, table, err := relatedTableName(target, spec, role, ruleName)
	if err != nil {
		return "", err
	}
	related := ResolvedTarget{
		Target:   target.Target,
		Database: target.Database,
		Schema:   schema,
		Table:    table,
	}
	return compileTable(dialect, related)
}

// compileParentTable renders the parent side of a referential-integrity rule.
func compileParentTable(dialect Dialect, target ResolvedTarget, spec string) (string, error) {
	return compileRelatedTable(dialect, target, spec, "parent table", "referential-integrity")
}

// compileRightTable renders the reference side of a cross-table reconciliation
// rule. It must live in the target's own database/schema: v1.1 reconciles tables
// that share a connection, not tables across instances.
func compileRightTable(dialect Dialect, target ResolvedTarget, spec string) (string, error) {
	return compileRelatedTable(dialect, target, spec, "right table", "cross-table")
}

// aggregateExpression renders one aggregate over a quoted column. The percentile
// placeholder binds the 0–1 fraction a PERCENTILE needs; it is empty for every
// other function. The engine spellings differ only for standard deviation
// (PostgreSQL/MySQL STDDEV, SQL Server STDEV) and for the percentile-cont form
// (T-SQL requires OVER ()).
func aggregateExpression(dialect Dialect, function storepb.QualityAggregateFunction, quotedColumn, percentilePlaceholder string) (string, error) {
	switch function {
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT:
		return "COUNT(*)", nil
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM:
		return "SUM(" + quotedColumn + ")", nil
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG:
		return "AVG(" + quotedColumn + ")", nil
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN:
		return "MIN(" + quotedColumn + ")", nil
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MAX:
		return "MAX(" + quotedColumn + ")", nil
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_STDDEV:
		switch dialect.(type) {
		case mssqlDialect:
			return "STDEV(" + quotedColumn + ")", nil
		case mysqlDialect, postgresDialect:
			return "STDDEV(" + quotedColumn + ")", nil
		default:
			return "", invalidConfig("%s has no standard-deviation aggregate", dialect.Name())
		}
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE:
		if percentilePlaceholder == "" {
			return "", invalidConfig("an aggregate PERCENTILE needs a percentile")
		}
		switch dialect.(type) {
		case postgresDialect:
			return "PERCENTILE_CONT(" + percentilePlaceholder + ") WITHIN GROUP (ORDER BY " + quotedColumn + ")", nil
		case mssqlDialect:
			return "PERCENTILE_CONT(" + percentilePlaceholder + ") WITHIN GROUP (ORDER BY " + quotedColumn + ") OVER ()", nil
		default:
			return "", invalidConfig("%s has no percentile aggregate", dialect.Name())
		}
	case storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED:
		return "", invalidConfig("an aggregate function is required")
	default:
		return "", invalidConfig("unknown aggregate function %d", function)
	}
}

// nullPercentValues converts a null_percent rule's custom missing values with the
// column's type, in configured order. The empty list is the plain SQL-NULL rule.
func nullPercentValues(column ColumnInfo, values []string) ([]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > maxNullValues {
		return nil, invalidConfig("null_values allows at most %d values, got %d", maxNullValues, len(values))
	}
	converted := make([]any, 0, len(values))
	for _, value := range values {
		argument, err := compileValueArg(column, value)
		if err != nil {
			return nil, err
		}
		converted = append(converted, argument)
	}
	return converted, nil
}

// compileValidateCountThresholds checks the shared fail/warn pair of a count-type
// rule. A negative threshold, a percentage above 100, or a warn level above the
// fail level would make the verdict meaningless.
func compileValidateCountThresholds(config *storepb.QualityRuleConfig, fail, warn *float64) error {
	percent := thresholdMetric(config) == storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	failValue := 0.0
	if fail != nil {
		failValue = *fail
	}
	if failValue < 0 {
		return invalidConfig("failure threshold must not be negative")
	}
	if percent && failValue > 100 {
		return invalidConfig("failure threshold %v is above 100 percent", failValue)
	}
	if warn != nil {
		if *warn < 0 {
			return invalidConfig("warning threshold must not be negative")
		}
		if percent && *warn > 100 {
			return invalidConfig("warning threshold %v is above 100 percent", *warn)
		}
		if *warn > failValue {
			return invalidConfig("warning threshold %v exceeds failure threshold %v", *warn, failValue)
		}
	}
	return nil
}

// compileValidateRowCount checks the row_count bounds: at least one bound, exact
// is exclusive with min/max, and the warn bounds sit inside the fail bounds.
func compileValidateRowCount(config *storepb.RowCountConfig) error {
	if config.MinRows == nil && config.MaxRows == nil && config.ExactRows == nil {
		return invalidConfig("a row-count rule needs at least one of min_rows, max_rows or exact_rows")
	}
	if config.ExactRows != nil && (config.MinRows != nil || config.MaxRows != nil) {
		return invalidConfig("exact_rows cannot be combined with min_rows or max_rows")
	}
	if config.MinRows != nil && *config.MinRows < 0 {
		return invalidConfig("min_rows must not be negative")
	}
	if config.MaxRows != nil && *config.MaxRows < 0 {
		return invalidConfig("max_rows must not be negative")
	}
	if config.MinRows != nil && config.MaxRows != nil && *config.MinRows > *config.MaxRows {
		return invalidConfig("min_rows %d exceeds max_rows %d", *config.MinRows, *config.MaxRows)
	}
	if config.WarnMinRows != nil {
		if *config.WarnMinRows < 0 {
			return invalidConfig("warn_min_rows must not be negative")
		}
		if config.MinRows != nil && *config.WarnMinRows < *config.MinRows {
			return invalidConfig("warn_min_rows %d is below min_rows %d", *config.WarnMinRows, *config.MinRows)
		}
	}
	if config.WarnMaxRows != nil {
		if *config.WarnMaxRows < 0 {
			return invalidConfig("warn_max_rows must not be negative")
		}
		if config.MaxRows != nil && *config.WarnMaxRows > *config.MaxRows {
			return invalidConfig("warn_max_rows %d is above max_rows %d", *config.WarnMaxRows, *config.MaxRows)
		}
	}
	if config.WarnMinRows != nil && config.WarnMaxRows != nil && *config.WarnMinRows > *config.WarnMaxRows {
		return invalidConfig("warn_min_rows %d exceeds warn_max_rows %d", *config.WarnMinRows, *config.WarnMaxRows)
	}
	return nil
}

// compileValidateRowCountChange checks the change bounds: at least one bound, each
// within 0–1000 percent, and warn inside fail.
func compileValidateRowCountChange(config *storepb.RowCountChangeConfig) error {
	if config.MaxIncreasePct == nil && config.MaxDecreasePct == nil &&
		config.WarnIncreasePct == nil && config.WarnDecreasePct == nil {
		return invalidConfig("a row-count-change rule needs at least one change bound")
	}
	validate := func(name string, value *float64) error {
		if value == nil {
			return nil
		}
		if *value < 0 || *value > 1000 {
			return invalidConfig("%s %v is outside the accepted 0–1000 percent range", name, *value)
		}
		return nil
	}
	for _, bound := range []struct {
		name  string
		value *float64
	}{
		{name: "max_increase_pct", value: config.MaxIncreasePct},
		{name: "max_decrease_pct", value: config.MaxDecreasePct},
		{name: "warn_increase_pct", value: config.WarnIncreasePct},
		{name: "warn_decrease_pct", value: config.WarnDecreasePct},
	} {
		if err := validate(bound.name, bound.value); err != nil {
			return err
		}
	}
	if config.MaxIncreasePct != nil && config.WarnIncreasePct != nil && *config.WarnIncreasePct > *config.MaxIncreasePct {
		return invalidConfig("warn_increase_pct %v exceeds max_increase_pct %v", *config.WarnIncreasePct, *config.MaxIncreasePct)
	}
	if config.MaxDecreasePct != nil && config.WarnDecreasePct != nil && *config.WarnDecreasePct > *config.MaxDecreasePct {
		return invalidConfig("warn_decrease_pct %v exceeds max_decrease_pct %v", *config.WarnDecreasePct, *config.MaxDecreasePct)
	}
	return nil
}

// compileValidateNullPercent checks that exactly one threshold form is set (count
// or percentage) and that the warn level is inside the fail level.
func compileValidateNullPercent(config *storepb.NullPercentConfig) error {
	countForm := config.MaxNullCount != nil || config.WarnNullCount != nil
	percentForm := config.MaxNullPct != nil || config.WarnNullPct != nil
	switch {
	case countForm && percentForm:
		return invalidConfig("null_percent uses max_null_count or max_null_pct, not both")
	case !countForm && !percentForm:
		return invalidConfig("null_percent needs max_null_count or max_null_pct")
	}
	if countForm {
		if config.MaxNullCount == nil {
			return invalidConfig("warn_null_count needs max_null_count")
		}
		if *config.MaxNullCount < 0 {
			return invalidConfig("max_null_count must not be negative")
		}
		if config.WarnNullCount != nil {
			if *config.WarnNullCount < 0 {
				return invalidConfig("warn_null_count must not be negative")
			}
			if *config.WarnNullCount > *config.MaxNullCount {
				return invalidConfig("warn_null_count %d exceeds max_null_count %d", *config.WarnNullCount, *config.MaxNullCount)
			}
		}
		return nil
	}
	if config.MaxNullPct == nil {
		return invalidConfig("warn_null_pct needs max_null_pct")
	}
	if *config.MaxNullPct < 0 || *config.MaxNullPct > 100 {
		return invalidConfig("max_null_pct %v is outside the accepted 0–100 range", *config.MaxNullPct)
	}
	if config.WarnNullPct != nil {
		if *config.WarnNullPct < 0 || *config.WarnNullPct > 100 {
			return invalidConfig("warn_null_pct %v is outside the accepted 0–100 range", *config.WarnNullPct)
		}
		if *config.WarnNullPct > *config.MaxNullPct {
			return invalidConfig("warn_null_pct %v exceeds max_null_pct %v", *config.WarnNullPct, *config.MaxNullPct)
		}
	}
	return nil
}
