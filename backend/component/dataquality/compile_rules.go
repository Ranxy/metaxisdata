package dataquality

import (
	"slices"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The compilers turn one validated rule into the single read-only statement that
// observes it. Three rules hold for every compiler here: user values are bound
// through compileArgs (never concatenated into SQL), identifiers go through
// Dialect.QuoteIdentifier, and the result column names are fixed in this file
// because the evaluator scans them by name. Validation that needs the resolved
// target (column existence, column kind) happens before SQL is assembled, so a
// rule that cannot be observed is refused rather than compiled into a bad query.

// compileRowCount observes the table's size. The verdict is decided in Go from the
// same count, so no threshold appears in the SQL.
func compileRowCount(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetRowCount()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the row-count config is missing")
	}
	if err := compileValidateRowCount(config); err != nil {
		return CompiledQuery{}, err
	}
	return compileTotalRows(input)
}

// compileRowCountChange observes the same count as row_count; the change is judged
// in Go against the rule's last successful observation, not in SQL.
func compileRowCountChange(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetRowCountChange()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the row-count-change config is missing")
	}
	if err := compileValidateRowCountChange(config); err != nil {
		return CompiledQuery{}, err
	}
	return compileTotalRows(input)
}

// compileTotalRows renders the one-column aggregate row_count and
// row_count_change share.
func compileTotalRows(input CompileInput) (CompiledQuery, error) {
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, args, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}
	return CompiledQuery{
		SQL:     "SELECT COUNT(*) AS total_rows FROM " + table + where,
		Args:    args,
		Kind:    QueryAggregate,
		Columns: []string{"total_rows"},
	}, nil
}

// compileNullPercent counts a column's NULLs. Count and percentage thresholds are
// exclusive in the config; the SQL is the same for both because the evaluator
// divides by total_rows for the percentage form.
func compileNullPercent(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetNullPercent()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the null-percent config is missing")
	}
	column, err := requireColumn(input.Target, config.GetColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	if err := compileValidateNullPercent(config); err != nil {
		return CompiledQuery{}, err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	// The custom missing values appear in the SELECT list, before the filter's
	// WHERE clause, so compileArgs reconciles the argument order per dialect. They
	// compare the column with bound values, which a type without equality cannot.
	if len(config.GetNullValues()) > 0 {
		if _, err := requireComparisonColumn(input.Target, column.Name); err != nil {
			return CompiledQuery{}, err
		}
	}
	values, err := nullPercentValues(column, config.GetNullValues())
	if err != nil {
		return CompiledQuery{}, err
	}
	args := newCompileArgs(input.Dialect, filterArgs)
	qualified := qualify(input.Dialect, column.Name)
	condition := qualified + " IS NULL"
	if len(values) > 0 {
		placeholders := make([]string, 0, len(values))
		for _, value := range values {
			placeholders = append(placeholders, args.add(value))
		}
		condition = qualified + " IS NULL OR " + qualified + " IN (" + strings.Join(placeholders, ", ") + ")"
	}
	return compileCountSelect(table, where, condition, args.values()), nil
}

// compileUnique counts the rows that duplicate a non-NULL value. The derived table
// collapses each value once (SUM(cnt - 1) is the duplicate row count) and the
// outer COUNT(*) is the number of duplicated groups. NULLs are equal to each other
// in GROUP BY, so they are excluded unless the rule asks to count them.
func compileUnique(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetUnique()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the unique config is missing")
	}
	column, err := requireGroupingColumn(input.Target, config.GetColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	columns := []string{column.Name}
	return compileUniqueQuery(input, columns, config.GetCountNulls())
}

// compileUniqueCombination counts duplicate composite keys. all_columns makes the
// check a whole-row duplicate test over every column of the target.
func compileUniqueCombination(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetUniqueCombination()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the unique-combination config is missing")
	}
	names := config.GetColumns()
	if config.GetAllColumns() {
		names = input.Target.ColumnNames()
		if len(names) == 0 {
			return CompiledQuery{}, invalidConfig("the target %s has no columns for an all-columns rule", input.Target.GUID)
		}
	}
	if len(names) == 0 {
		return CompiledQuery{}, invalidConfig("a unique-combination rule needs at least one column")
	}
	if len(names) > compileMaxUniqueColumns {
		return CompiledQuery{}, invalidConfig("a unique-combination rule allows at most %d columns, got %d", compileMaxUniqueColumns, len(names))
	}
	columns := make([]string, 0, len(names))
	for _, name := range names {
		column, err := requireGroupingColumn(input.Target, name)
		if err != nil {
			return CompiledQuery{}, err
		}
		columns = append(columns, column.Name)
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	return compileUniqueQuery(input, columns, config.GetCountNulls())
}

// compileUniqueQuery renders the grouped duplicate-count query unique and
// unique_combination share. It also reports the filtered table's row count so a
// PERCENT threshold has the same denominator the other count rules use.
func compileUniqueQuery(input CompileInput, columns []string, countNulls bool) (CompiledQuery, error) {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, qualify(input.Dialect, column))
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}
	innerWhere := where
	if !countNulls {
		// A duplicate of NULL is not a duplicate in SQL uniqueness, so the grouped
		// input drops NULL keys unless the rule opts in.
		for _, column := range quoted {
			innerWhere = compileAppendWhere(innerWhere, column+" IS NOT NULL")
		}
	}
	selectList := strings.Join(quoted, ", ")
	// The filter predicate appears twice: once in the total_rows scalar subquery and
	// once inside the grouped input. Numbered dialects can reuse the same $n/@pN
	// for both occurrences, but a positional dialect binds one ? per occurrence, so
	// its arguments are passed once per appearance in the SQL text.
	query := "SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS groups, " +
		"(SELECT COUNT(*) FROM " + table + where + ") AS total_rows FROM (" +
		"SELECT " + selectList + ", COUNT(*) AS cnt FROM " + table + innerWhere +
		" GROUP BY " + selectList + " HAVING COUNT(*) > 1) dq"
	args := filterArgs
	if compilePositional(input.Dialect) && len(filterArgs) > 0 {
		args = slices.Concat(filterArgs, filterArgs)
	}
	return CompiledQuery{
		SQL:     query,
		Args:    args,
		Kind:    QueryAggregate,
		Columns: []string{"failed_rows", "groups", "total_rows"},
	}, nil
}

// compileAcceptedValues counts rows whose value is outside the accepted set. A
// NULL counts as a violation unless allow_null is set; allowing it is expressed
// by omitting the IS NULL arm, because NOT IN over NULL stays unknown and the
// CASE falls through to zero.
func compileAcceptedValues(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetAcceptedValues()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the accepted-values config is missing")
	}
	column, err := requireComparisonColumn(input.Target, config.GetColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	values := config.GetValues()
	if len(values) == 0 {
		return CompiledQuery{}, invalidConfig("an accepted-values rule needs at least one value")
	}
	if len(values) > compileMaxAcceptedValues {
		return CompiledQuery{}, invalidConfig("an accepted-values rule allows at most %d values, got %d", compileMaxAcceptedValues, len(values))
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	args := newCompileArgs(input.Dialect, filterArgs)
	placeholders := make([]string, 0, len(values))
	for _, value := range values {
		converted, err := compileValueArg(column, value)
		if err != nil {
			return CompiledQuery{}, err
		}
		placeholders = append(placeholders, args.add(converted))
	}
	qualified := qualify(input.Dialect, column.Name)
	condition := qualified + " NOT IN (" + strings.Join(placeholders, ", ") + ")"
	if !config.GetAllowNull() {
		condition = qualified + " IS NULL OR " + condition
	}
	return compileCountSelect(table, where, condition, args.values()), nil
}

// compileRange counts rows outside the configured interval. inclusive decides
// whether the bounds themselves are allowed, so the failing comparison is <= and
// >= for an open interval and < and > for a closed one.
func compileRange(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetRange()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the range config is missing")
	}
	column, err := compileComparableColumn(input, config.GetColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	if config.Min == nil && config.Max == nil {
		return CompiledQuery{}, invalidConfig("a range rule needs min or max")
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	var minValue, maxValue any
	if config.Min != nil {
		minValue, err = compileValueArg(column, *config.Min)
		if err != nil {
			return CompiledQuery{}, err
		}
	}
	if config.Max != nil {
		maxValue, err = compileValueArg(column, *config.Max)
		if err != nil {
			return CompiledQuery{}, err
		}
	}
	if minValue != nil && maxValue != nil {
		// Only numeric bounds have a defined order in Go; a date bound stays a string
		// and the engine's cast decides the comparison.
		if lower, ok := numericValue(minValue); ok {
			if upper, ok := numericValue(maxValue); ok && lower > upper {
				return CompiledQuery{}, invalidConfig("min %q exceeds max %q", *config.Min, *config.Max)
			}
		}
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	args := newCompileArgs(input.Dialect, filterArgs)
	qualified := qualify(input.Dialect, column.Name)
	conditions := make([]string, 0, 2)
	if config.Min != nil {
		operator := "<"
		if !config.GetInclusive() {
			operator = "<="
		}
		conditions = append(conditions, qualified+" "+operator+" "+args.add(minValue))
	}
	if config.Max != nil {
		operator := ">"
		if !config.GetInclusive() {
			operator = ">="
		}
		conditions = append(conditions, qualified+" "+operator+" "+args.add(maxValue))
	}
	return compileCountSelect(table, where, strings.Join(conditions, " OR "), args.values()), nil
}

// compileRegex counts rows that do not match the pattern. A NULL does not match,
// so it counts as a violation; the evaluator never sees the pattern itself.
func compileRegex(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetRegex()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the regex config is missing")
	}
	column, err := compileRegexColumn(input, config.GetColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	pattern := strings.TrimSpace(config.GetPattern())
	if pattern == "" {
		return CompiledQuery{}, invalidConfig("a regex rule needs a pattern")
	}
	operator, err := compileRegexNotMatch(input.Dialect)
	if err != nil {
		return CompiledQuery{}, err
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	args := newCompileArgs(input.Dialect, filterArgs)
	placeholder := args.add(pattern)
	qualified := qualify(input.Dialect, column.Name)
	condition := qualified + " IS NULL OR " + qualified + " " + operator + " " + placeholder
	return compileCountSelect(table, where, condition, args.values()), nil
}

// compileStringLength counts rows whose text length is outside the configured
// bounds. The length function is the dialect's (CHAR_LENGTH vs LEN), so a
// multibyte character counts once on every engine.
func compileStringLength(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetStringLength()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the string-length config is missing")
	}
	column, err := compileLengthColumn(input, config.GetColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	if config.MinLen == nil && config.MaxLen == nil {
		return CompiledQuery{}, invalidConfig("a string-length rule needs min_len or max_len")
	}
	if config.MinLen != nil && *config.MinLen < 0 {
		return CompiledQuery{}, invalidConfig("min_len must not be negative")
	}
	if config.MaxLen != nil && *config.MaxLen < 0 {
		return CompiledQuery{}, invalidConfig("max_len must not be negative")
	}
	if config.MinLen != nil && config.MaxLen != nil && *config.MinLen > *config.MaxLen {
		return CompiledQuery{}, invalidConfig("min_len %d exceeds max_len %d", *config.MinLen, *config.MaxLen)
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	args := newCompileArgs(input.Dialect, filterArgs)
	length := input.Dialect.LengthExpr(qualify(input.Dialect, column.Name))
	conditions := make([]string, 0, 2)
	if config.MinLen != nil {
		conditions = append(conditions, length+" < "+args.add(*config.MinLen))
	}
	if config.MaxLen != nil {
		conditions = append(conditions, length+" > "+args.add(*config.MaxLen))
	}
	return compileCountSelect(table, where, strings.Join(conditions, " OR "), args.values()), nil
}

// compileReferentialIntegrity counts child rows whose foreign-key value has no
// parent. The child table is narrowed by the filter in a derived table so the
// predicate keeps referring to the child when the parent happens to use the same
// column names. The parent's columns are not part of the resolved target, so the
// parent column's existence and type are checked by the SQL engine at run time.
func compileReferentialIntegrity(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetReferentialIntegrity()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the referential-integrity config is missing")
	}
	childColumn, err := requireComparisonColumn(input.Target, config.GetChildColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	parentColumn := strings.TrimSpace(config.GetParentColumn())
	if parentColumn == "" {
		return CompiledQuery{}, invalidConfig("a referential-integrity rule needs a parent column")
	}
	parentTable, err := compileParentTable(input.Dialect, input.Target, config.GetParentTable())
	if err != nil {
		return CompiledQuery{}, err
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, args, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	child := "c." + qualify(input.Dialect, childColumn.Name)
	parent := "p." + qualify(input.Dialect, parentColumn)
	condition := child + " IS NULL OR " + parent + " IS NULL"
	if config.GetAllowNull() {
		condition = child + " IS NOT NULL AND " + parent + " IS NULL"
	}
	return CompiledQuery{
		SQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN " + condition +
			" THEN 1 ELSE 0 END) AS failed_rows FROM (SELECT * FROM " + table + where +
			") c LEFT JOIN " + parentTable + " p ON " + child + " = " + parent,
		Args:    args,
		Kind:    QueryAggregate,
		Columns: []string{"total_rows", "failed_rows"},
	}, nil
}

// compileCrossColumn counts rows where the two columns do not satisfy the
// configured comparison. A NULL on either side makes the comparison unknown and
// is not counted: a null check is a separate rule.
func compileCrossColumn(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetCrossColumn()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the cross-column config is missing")
	}
	left, err := compileComparableColumn(input, config.GetLeftColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	right, err := compileComparableColumn(input, config.GetRightColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	if left.Kind != right.Kind {
		return CompiledQuery{}, invalidConfig("columns %q (%s) and %q (%s) are not comparable", left.Name, left.Kind, right.Name, right.Kind)
	}
	if config.GetOp() == storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_UNSPECIFIED {
		return CompiledQuery{}, invalidConfig("a cross-column rule needs a comparison operator")
	}
	if config.Tolerance != nil {
		if *config.Tolerance < 0 {
			return CompiledQuery{}, invalidConfig("a cross-column tolerance must not be negative")
		}
		if left.Kind != ColumnKindNumeric {
			return CompiledQuery{}, invalidConfig("a cross-column tolerance needs numeric columns")
		}
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	args := newCompileArgs(input.Dialect, filterArgs)
	placeholder := ""
	if config.Tolerance != nil {
		placeholder = args.add(*config.Tolerance)
	}
	condition, err := compileCrossColumnFailure(
		qualify(input.Dialect, left.Name),
		qualify(input.Dialect, right.Name),
		config.GetOp(),
		config.Tolerance,
		placeholder,
	)
	if err != nil {
		return CompiledQuery{}, err
	}
	return compileCountSelect(table, where, condition, args.values()), nil
}

// compileFreshness measures how old the newest timestamp is. The lag is computed
// inside the target engine from its own clock so the verdict does not depend on
// the server's time zone or clock skew; a NULL maximum stays NULL and the
// evaluator follows on_empty.
func compileFreshness(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetFreshness()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the freshness config is missing")
	}
	column, err := compileTemporalColumn(input, config.GetTimestampColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	if config.GetMaxLagSeconds() < 0 {
		return CompiledQuery{}, invalidConfig("max_lag_seconds must not be negative")
	}
	if config.WarnLagSeconds != nil {
		if *config.WarnLagSeconds < 0 {
			return CompiledQuery{}, invalidConfig("warn_lag_seconds must not be negative")
		}
		if *config.WarnLagSeconds > config.GetMaxLagSeconds() {
			return CompiledQuery{}, invalidConfig("warn_lag_seconds %d exceeds max_lag_seconds %d", *config.WarnLagSeconds, config.GetMaxLagSeconds())
		}
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, args, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}
	maximum := "MAX(" + qualify(input.Dialect, column.Name) + ")"
	lag, err := compileFreshnessLagExpr(input.Dialect, maximum)
	if err != nil {
		return CompiledQuery{}, err
	}
	return CompiledQuery{
		SQL: "SELECT COUNT(*) AS total_rows, " + maximum + " AS max_ts, " + lag +
			" AS lag_seconds FROM " + table + where,
		Args:    args,
		Kind:    QueryAggregate,
		Columns: []string{"total_rows", "max_ts", "lag_seconds"},
	}, nil
}

// compileCustomSQL counts the rows the user's query returns: the query is the
// definition of a failing row, so DQ never rewrites it and the rule's filter does
// not apply to it. The full single-statement/SELECT parser check belongs to the
// validation pipeline; here the cheap shape checks keep a multi-statement string
// from being wrapped into a surprising query. The wrapper already aggregates, so
// the kind is QueryAggregate: the executor must not count rows a second time.
func compileCustomSQL(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetCustomSql()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the custom-SQL config is missing")
	}
	userSQL := strings.TrimSpace(config.GetSql())
	userSQL = strings.TrimRight(userSQL, "; \t\r\n")
	if userSQL == "" {
		return CompiledQuery{}, invalidConfig("a custom-SQL rule needs SQL")
	}
	if len(userSQL) > compileMaxCustomSQLLength {
		return CompiledQuery{}, invalidConfig("custom SQL is longer than %d bytes", compileMaxCustomSQLLength)
	}
	// The statement shape is the parser's business, not a text scan: a semicolon
	// inside a string literal or a leading comment would trip a naive check. The
	// validation pipeline runs ValidateCustomSQL on every save; compiling an
	// already-stored rule runs it once more so a row written before that check
	// existed cannot execute a non-SELECT statement.
	if input.Target.Engine != storepb.Engine_ENGINE_UNSPECIFIED {
		if err := ValidateCustomSQL(input.Target.Engine, userSQL); err != nil {
			return CompiledQuery{}, err
		}
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	return CompiledQuery{
		SQL:     "SELECT COUNT(*) AS failed_rows FROM (" + userSQL + ") dq",
		Kind:    QueryAggregate,
		Columns: []string{"failed_rows"},
	}, nil
}

// compileSchemaDrift observes the metadata registry, not the target database: the
// executor compares the live column list with the saved snapshot, so there is no
// SQL to run. QueryMetadata tells the executor and the SQL preview that the empty
// statement is deliberate rather than a compiler failure.
func compileSchemaDrift(input CompileInput) (CompiledQuery, error) {
	if input.Config.GetSchemaDrift() == nil {
		return CompiledQuery{}, invalidConfig("the schema-drift config is missing")
	}
	return CompiledQuery{Kind: QueryMetadata}, nil
}

// compileGroupCheck counts the groups whose row count is outside the configured
// bounds. The grouped input is a derived table so the outer aggregate can test
// the group size; the group columns and the filter are read from the target, so
// no caller text reaches the SQL unquoted.
//
// The bounds appear in the SELECT list, before the filter's WHERE clause in the
// SQL text. compileArgs reconciles that: numbered dialects bind the filter first
// and number the bounds after it, while a positional dialect gets its arguments
// in text order (bounds first).
func compileGroupCheck(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetGroupCheck()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the group-check config is missing")
	}
	columns, err := groupCheckColumns(input.Target, config.GetColumns())
	if err != nil {
		return CompiledQuery{}, err
	}
	minValue, maxValue, hasMin, hasMax, err := groupCheckBounds(config)
	if err != nil {
		return CompiledQuery{}, err
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	args := newCompileArgs(input.Dialect, filterArgs)
	conditions := make([]string, 0, 2)
	if hasMin {
		conditions = append(conditions, "cnt < "+args.add(minValue))
	}
	if hasMax {
		conditions = append(conditions, "cnt > "+args.add(maxValue))
	}
	groupList := qualifyColumns(input.Dialect, columns)
	query := "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN " + strings.Join(conditions, " OR ") +
		" THEN 1 ELSE 0 END) AS failed_groups FROM (SELECT " + groupList +
		", COUNT(*) AS cnt FROM " + table + where + " GROUP BY " + groupList + ") dq"
	return CompiledQuery{
		SQL:     query,
		Args:    args.values(),
		Kind:    QueryAggregate,
		Columns: []string{"total_groups", "failed_groups"},
	}, nil
}

// qualifyColumns quotes a column list for one statement.
func qualifyColumns(dialect Dialect, columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, qualify(dialect, column))
	}
	return strings.Join(quoted, ", ")
}

// compileAggregate bounds one aggregate of a single column. The observed value is
// the aggregate itself and total_rows is the scanned row count; the evaluator
// judges the value against an interval, and a NULL aggregate (empty table or an
// all-NULL column) follows on_empty.
func compileAggregate(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetAggregate()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the aggregate config is missing")
	}
	function := config.GetFunction()
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	args := newCompileArgs(input.Dialect, filterArgs)
	quotedColumn := ""
	if function != storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT {
		column, err := requireColumn(input.Target, config.GetColumn())
		if err != nil {
			return CompiledQuery{}, err
		}
		quotedColumn = qualify(input.Dialect, column.Name)
	}
	percentilePlaceholder := ""
	if function == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE {
		if config.Percentile == nil {
			return CompiledQuery{}, invalidConfig("an aggregate PERCENTILE needs a percentile")
		}
		percentile := *config.Percentile
		if percentile < 0 || percentile > 100 {
			return CompiledQuery{}, invalidConfig("percentile %v is outside the accepted 0–100 range", percentile)
		}
		// PERCENTILE_CONT takes a fraction: 50th percentile is 0.5.
		percentilePlaceholder = args.add(percentile / 100)
	}
	expression, err := aggregateExpression(input.Dialect, function, quotedColumn, percentilePlaceholder)
	if err != nil {
		return CompiledQuery{}, err
	}
	return CompiledQuery{
		SQL:     "SELECT COUNT(*) AS total_rows, " + expression + " AS observed FROM " + table + where,
		Args:    args.values(),
		Kind:    QueryAggregate,
		Columns: []string{"total_rows", "observed"},
	}, nil
}

// compileCrossTable reconciles one aggregate of the rule's target with one
// aggregate of another table in the same database. Both sides are scalar
// subqueries in one statement; the filter applies to the left (target) side only,
// which is the v1.1 contract.
func compileCrossTable(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetCrossTable()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the cross-table config is missing")
	}
	leftFunction := config.GetLeftFunction()
	if ok, reason := crossTableFunctionSupported(leftFunction); !ok {
		return CompiledQuery{}, invalidConfig("%s", reason)
	}
	rightFunction := config.GetRightFunction()
	if ok, reason := crossTableFunctionSupported(rightFunction); !ok {
		return CompiledQuery{}, invalidConfig("%s", reason)
	}
	leftTable, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	rightTable, err := compileRightTable(input.Dialect, input.Target, config.GetRightTable())
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	leftExpression, err := crossTableExpression(input.Dialect, leftFunction, config.GetLeftColumn(), input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	// The right table is not resolved here (the API resolves it when the rule is
	// saved), so the right column is only quoted; its existence is checked there.
	rightExpression, err := crossTableExpression(input.Dialect, rightFunction, config.GetRightColumn(), ResolvedTarget{})
	if err != nil {
		return CompiledQuery{}, err
	}
	return CompiledQuery{
		SQL: "SELECT (SELECT " + leftExpression + " FROM " + leftTable + where + ") AS left_value, " +
			"(SELECT " + rightExpression + " FROM " + rightTable + ") AS right_value",
		Args:    filterArgs,
		Kind:    QueryAggregate,
		Columns: []string{"left_value", "right_value"},
	}, nil
}

// crossTableExpression renders one side of a reconciliation. COUNT ignores the
// column; every other function aggregates it. When the target is resolved the
// left column is proven to exist; the right side passes an empty target because
// its table is not part of the rule's resolution.
func crossTableExpression(dialect Dialect, function storepb.QualityAggregateFunction, column string, target ResolvedTarget) (string, error) {
	if function == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT {
		return "COUNT(*)", nil
	}
	column = strings.TrimSpace(column)
	if column == "" {
		return "", invalidConfig("a cross-table %s check needs a column", strings.TrimPrefix(function.String(), "QUALITY_AGGREGATE_FUNCTION_"))
	}
	if target.GUID != "" {
		resolved, err := requireColumn(target, column)
		if err != nil {
			return "", err
		}
		column = resolved.Name
	}
	return aggregateExpression(dialect, function, qualify(dialect, column), "")
}
