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

// compileStringColumn resolves a column and requires it to be text.
func compileStringColumn(input CompileInput, name string) (ColumnInfo, error) {
	column, err := requireColumn(input.Target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind != ColumnKindString {
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a string column", column.Name, column.Kind)
	}
	return column, nil
}

// compileTemporalColumn resolves a column and requires it to be a date/time.
func compileTemporalColumn(input CompileInput, name string) (ColumnInfo, error) {
	column, err := requireColumn(input.Target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind != ColumnKindTemporal {
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a temporal column", column.Name, column.Kind)
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
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a numeric or temporal column", column.Name, column.Kind)
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

// compileParentTable renders the parent side of a referential-integrity rule.
// The config names the parent as "table" or "container.table": one segment is
// qualified with the child's schema (PostgreSQL/MSSQL) or database (MySQL), while
// two segments must name the child's own schema/database — a rule never reaches
// across containers.
func compileParentTable(dialect Dialect, target ResolvedTarget, spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", invalidConfig("a referential-integrity rule needs a parent table")
	}
	parts := strings.Split(spec, ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", invalidConfig("parent table %q is not a valid table name", spec)
		}
	}

	parent := ResolvedTarget{Target: target.Target}
	switch dialect.(type) {
	case mysqlDialect:
		switch len(parts) {
		case 1:
			parent.Database, parent.Table = target.Database, parts[0]
		case 2:
			if parts[0] != target.Database {
				return "", invalidConfig("parent table %q must be in the rule's database %q", spec, target.Database)
			}
			parent.Database, parent.Table = parts[0], parts[1]
		default:
			return "", invalidConfig("parent table %q must be \"table\" or \"database.table\"", spec)
		}
	case postgresDialect, mssqlDialect:
		switch len(parts) {
		case 1:
			parent.Schema, parent.Table = target.Schema, parts[0]
		case 2:
			if parts[0] != target.Schema {
				return "", invalidConfig("parent table %q must be in the rule's schema %q", spec, target.Schema)
			}
			parent.Schema, parent.Table = parts[0], parts[1]
		default:
			return "", invalidConfig("parent table %q must be \"table\" or \"schema.table\"", spec)
		}
	default:
		return "", invalidConfig("%s does not support referential-integrity rules", dialect.Name())
	}
	return compileTable(dialect, parent)
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
