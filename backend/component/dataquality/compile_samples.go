package dataquality

import (
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CompileFailureRows renders the statement that returns the rows a rule counts
// as failures, for the review queue's "show me the bad rows" step. It is a
// separate statement from the observation because the aggregate only counts: the
// sample must select the offending rows themselves and stay bounded.
//
// The rule's configuration is validated by Compile first, so every builder below
// runs on a config that already passed the rule's own checks; this file only
// assembles SQL. Rule types whose failure is a measurement rather than a row
// (row_count, row_count_change, freshness) have no failing rows and return a zero
// CompiledQuery with a nil error, which tells the caller to skip the sample
// (there is no row that "violated" a freshness threshold — the newest row is
// simply too old). A limit of 0 or less reads as the default, and a large one is
// clamped to the maximum.
func CompileFailureRows(input CompileInput, limit int) (CompiledQuery, error) {
	if input.Dialect == nil {
		return CompiledQuery{}, invalidConfig("a dialect is required")
	}
	if input.Config == nil {
		return CompiledQuery{}, invalidConfig("a rule config is required")
	}
	if _, err := Compile(input); err != nil {
		return CompiledQuery{}, err
	}
	limit = normalizeSampleLimit(limit)
	switch input.Type {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:
		return sampleNullPercent(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		return sampleUnique(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:
		return sampleAcceptedValues(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:
		return sampleRange(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
		return sampleRegex(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:
		return sampleStringLength(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY:
		return sampleReferentialIntegrity(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:
		return sampleCrossColumn(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:
		return sampleCustomSQL(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:
		return sampleGroupCheck(input, limit)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:
		// A drift sample is the comparison result itself, built by the executor: it
		// is a list of columns and reasons, not rows of the target database.
		return CompiledQuery{}, nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		// An aggregate or reconciliation result is a value, not a failing row, and a
		// CEL rule's failing rows are computed in process from the sample it already
		// evaluated: none of them needs a separate failed-row statement.
		return CompiledQuery{}, nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		// No failing row exists: the observation is a property of the whole table
		// or of its newest row, so there is nothing to show.
		return CompiledQuery{}, nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return CompiledQuery{}, invalidConfig("rule type %s has no failed-row sample", input.Type)
	default:
		return CompiledQuery{}, invalidConfig("rule type %s has no failed-row sample", input.Type)
	}
}

// sampleArgs collects the arguments of one sample statement in the order their
// placeholders appear in its SQL text. It is deliberately not compileArgs: in a
// sample the filter (which appears first, in the WHERE clause) precedes every
// config condition, so no dialect needs the filter-last reordering the aggregate
// SELECT list forces.
type sampleArgs struct {
	dialect Dialect
	values  []any
}

// newSampleArgs starts the argument list from the filter's arguments. The slice
// is copied, so appending a config argument cannot write into the caller's
// filter arguments, and it stays nil when there is nothing to bind.
func newSampleArgs(dialect Dialect, filterArgs []any) *sampleArgs {
	var values []any
	if len(filterArgs) > 0 {
		values = append(values, filterArgs...)
	}
	return &sampleArgs{dialect: dialect, values: values}
}

// add appends one argument and returns its placeholder.
func (a *sampleArgs) add(value any) string {
	a.values = append(a.values, value)
	return a.dialect.Placeholder(len(a.values))
}

// sampleSelect renders the columns a sample returns, quoted for the dialect.
func sampleSelect(dialect Dialect, names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, qualify(dialect, name))
	}
	return strings.Join(quoted, ", ")
}

// sampleLimitClause appends the row bound. T-SQL only accepts OFFSET/FETCH after
// an ORDER BY, and the sample has no natural order, so a constant ordering is
// emitted there; the other engines take a plain LIMIT.
func sampleLimitClause(dialect Dialect, limit int) string {
	if _, ok := dialect.(mssqlDialect); ok {
		return " ORDER BY (SELECT NULL) " + dialect.LimitClause(limit)
	}
	return " " + dialect.LimitClause(limit)
}

// sampleQuery finishes one sample statement with its bound and argument list.
func sampleQuery(input CompileInput, sql string, args []any, columns []string, limit int) CompiledQuery {
	return CompiledQuery{
		SQL:     sql + sampleLimitClause(input.Dialect, limit),
		Args:    args,
		Kind:    QuerySample,
		Columns: columns,
	}
}

// sampleNullPercent selects the rows whose column is NULL.
func sampleNullPercent(input CompileInput, limit int) (CompiledQuery, error) {
	config := input.Config.GetNullPercent()
	// A plain null check works on every type; only the custom missing values compare
	// the column, and only they need comparison support. The aggregate compiler
	// applies the same gate, so this sample can never be stricter than the query it
	// is evidence for.
	column, err := requireColumn(input.Target, config.GetColumn())
	if err == nil && len(config.GetNullValues()) > 0 {
		_, err = requireComparisonColumn(input.Target, config.GetColumn())
	}
	if err != nil {
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
	values, err := nullPercentValues(column, config.GetNullValues())
	if err != nil {
		return CompiledQuery{}, err
	}
	args := newSampleArgs(input.Dialect, filterArgs)
	qualified := qualify(input.Dialect, column.Name)
	condition := qualified + " IS NULL"
	if len(values) > 0 {
		placeholders := make([]string, 0, len(values))
		for _, value := range values {
			placeholders = append(placeholders, args.add(value))
		}
		condition = qualified + " IS NULL OR " + qualified + " IN (" + strings.Join(placeholders, ", ") + ")"
	}
	return sampleQuery(input,
		"SELECT "+sampleSelect(input.Dialect, []string{column.Name})+" FROM "+table+
			compileAppendWhere(where, "("+condition+")"),
		args.values, []string{column.Name}, limit), nil
}

// sampleAcceptedValues selects the rows whose value is outside the accepted set.
// The predicate is the aggregate's failing condition, parenthesized so its OR
// cannot absorb the filter's AND.
func sampleAcceptedValues(input CompileInput, limit int) (CompiledQuery, error) {
	config := input.Config.GetAcceptedValues()
	column, err := requireComparisonColumn(input.Target, config.GetColumn())
	if err != nil {
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
	args := newSampleArgs(input.Dialect, filterArgs)
	placeholders := make([]string, 0, len(config.GetValues()))
	for _, value := range config.GetValues() {
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
	return sampleQuery(input,
		"SELECT "+sampleSelect(input.Dialect, []string{column.Name})+" FROM "+table+
			compileAppendWhere(where, "("+condition+")"),
		args.values, []string{column.Name}, limit), nil
}

// sampleRange selects the rows outside the configured interval.
func sampleRange(input CompileInput, limit int) (CompiledQuery, error) {
	config := input.Config.GetRange()
	column, err := compileComparableColumn(input, config.GetColumn())
	if err != nil {
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
	args := newSampleArgs(input.Dialect, filterArgs)
	qualified := qualify(input.Dialect, column.Name)
	conditions := make([]string, 0, 2)
	if config.Min != nil {
		value, err := compileValueArg(column, *config.Min)
		if err != nil {
			return CompiledQuery{}, err
		}
		operator := "<"
		if !config.GetInclusive() {
			operator = "<="
		}
		conditions = append(conditions, qualified+" "+operator+" "+args.add(value))
	}
	if config.Max != nil {
		value, err := compileValueArg(column, *config.Max)
		if err != nil {
			return CompiledQuery{}, err
		}
		operator := ">"
		if !config.GetInclusive() {
			operator = ">="
		}
		conditions = append(conditions, qualified+" "+operator+" "+args.add(value))
	}
	return sampleQuery(input,
		"SELECT "+sampleSelect(input.Dialect, []string{column.Name})+" FROM "+table+
			compileAppendWhere(where, "("+strings.Join(conditions, " OR ")+")"),
		args.values, []string{column.Name}, limit), nil
}

// sampleRegex selects the rows whose value does not match the pattern. A NULL
// does not match, so it is sampled as a failure too, mirroring the aggregate.
func sampleRegex(input CompileInput, limit int) (CompiledQuery, error) {
	config := input.Config.GetRegex()
	column, err := compileRegexColumn(input, config.GetColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	operator, err := compileRegexNotMatch(input.Dialect)
	if err != nil {
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
	args := newSampleArgs(input.Dialect, filterArgs)
	qualified := qualify(input.Dialect, column.Name)
	condition := qualified + " IS NULL OR " + qualified + " " + operator + " " + args.add(strings.TrimSpace(config.GetPattern()))
	return sampleQuery(input,
		"SELECT "+sampleSelect(input.Dialect, []string{column.Name})+" FROM "+table+
			compileAppendWhere(where, "("+condition+")"),
		args.values, []string{column.Name}, limit), nil
}

// sampleStringLength selects the rows whose text length is outside the bounds.
func sampleStringLength(input CompileInput, limit int) (CompiledQuery, error) {
	config := input.Config.GetStringLength()
	column, err := compileLengthColumn(input, config.GetColumn())
	if err != nil {
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
	args := newSampleArgs(input.Dialect, filterArgs)
	length := input.Dialect.LengthExpr(qualify(input.Dialect, column.Name))
	conditions := make([]string, 0, 2)
	if config.MinLen != nil {
		conditions = append(conditions, length+" < "+args.add(*config.MinLen))
	}
	if config.MaxLen != nil {
		conditions = append(conditions, length+" > "+args.add(*config.MaxLen))
	}
	return sampleQuery(input,
		"SELECT "+sampleSelect(input.Dialect, []string{column.Name})+" FROM "+table+
			compileAppendWhere(where, "("+strings.Join(conditions, " OR ")+")"),
		args.values, []string{column.Name}, limit), nil
}

// sampleReferentialIntegrity selects the child rows whose foreign-key value has
// no parent. The filter stays inside the derived child table, exactly as in the
// aggregate, so the predicate keeps referring to the child when the parent uses
// the same column names.
func sampleReferentialIntegrity(input CompileInput, limit int) (CompiledQuery, error) {
	config := input.Config.GetReferentialIntegrity()
	childColumn, err := requireComparisonColumn(input.Target, config.GetChildColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	parentTable, err := compileParentTable(input.Dialect, input.Target, config.GetParentTable())
	if err != nil {
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
	child := "c." + qualify(input.Dialect, childColumn.Name)
	parent := "p." + qualify(input.Dialect, config.GetParentColumn())
	condition := child + " IS NULL OR " + parent + " IS NULL"
	if config.GetAllowNull() {
		condition = child + " IS NOT NULL AND " + parent + " IS NULL"
	}
	// The sample shows the child row itself, so every child column is selected
	// and qualified: the join's parent side must not shadow a same-named column.
	childColumns := input.Target.ColumnNames()
	selectList := make([]string, 0, len(childColumns))
	for _, name := range childColumns {
		selectList = append(selectList, "c."+qualify(input.Dialect, name))
	}
	return sampleQuery(input,
		"SELECT "+strings.Join(selectList, ", ")+" FROM (SELECT * FROM "+table+where+") c"+
			" LEFT JOIN "+parentTable+" p ON "+child+" = "+parent+compileAppendWhere("", "("+condition+")"),
		filterArgs, childColumns, limit), nil
}

// sampleCrossColumn selects the rows whose two columns violate the comparison.
func sampleCrossColumn(input CompileInput, limit int) (CompiledQuery, error) {
	config := input.Config.GetCrossColumn()
	left, err := compileComparableColumn(input, config.GetLeftColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	right, err := compileComparableColumn(input, config.GetRightColumn())
	if err != nil {
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
	args := newSampleArgs(input.Dialect, filterArgs)
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
	columns := []string{left.Name, right.Name}
	return sampleQuery(input,
		"SELECT "+sampleSelect(input.Dialect, columns)+" FROM "+table+
			compileAppendWhere(where, "("+condition+")"),
		args.values, columns, limit), nil
}

// sampleUnique selects the duplicate keys rather than the duplicated rows: the
// grouped statement returns one row per duplicated value, which is what the
// reviewer checks. A value that repeats three times is one group, not three rows,
// so the sample length is a count of duplicates, not of affected rows.
func sampleUnique(input CompileInput, limit int) (CompiledQuery, error) {
	var names []string
	var countNulls bool
	switch input.Type {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:
		config := input.Config.GetUnique()
		names = []string{config.GetColumn()}
		countNulls = config.GetCountNulls()
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		config := input.Config.GetUniqueCombination()
		names = config.GetColumns()
		if config.GetAllColumns() {
			names = input.Target.ColumnNames()
		}
		countNulls = config.GetCountNulls()
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		return CompiledQuery{}, invalidConfig("rule type %s has no duplicate sample", input.Type)
	default:
		return CompiledQuery{}, invalidConfig("rule type %s has no duplicate sample", input.Type)
	}
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, qualify(input.Dialect, name))
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
		for _, column := range quoted {
			innerWhere = compileAppendWhere(innerWhere, column+" IS NOT NULL")
		}
	}
	selectList := strings.Join(quoted, ", ")
	return sampleQuery(input,
		"SELECT "+selectList+" FROM "+table+innerWhere+
			" GROUP BY "+selectList+" HAVING COUNT(*) > 1",
		filterArgs, names, limit), nil
}

// sampleCustomSQL selects the rows the user's query returns: that query is the
// definition of a failing row, so it is never rewritten and the rule's filter
// does not apply to it.
func sampleCustomSQL(input CompileInput, limit int) (CompiledQuery, error) {
	userSQL := strings.TrimSpace(input.Config.GetCustomSql().GetSql())
	userSQL = strings.TrimRight(userSQL, "; \t\r\n")
	// The result columns of an arbitrary query are not known before it runs, so
	// the sample keeps whatever the user's SELECT returns.
	return sampleQuery(input,
		"SELECT * FROM ("+userSQL+") dq", nil, nil, limit), nil
}

// sampleGroupCheck selects the groups whose row count is outside the bounds,
// largest first. The grouped statement is a derived table so the outer WHERE can
// test the group size without repeating the aggregate in HAVING, which the
// numbered dialects name differently (PostgreSQL rejects a select alias there).
//
// The filter sits inside the derived table, before the bounds in the SQL text, so
// sampleArgs binds its arguments first — exactly the order the text presents.
func sampleGroupCheck(input CompileInput, limit int) (CompiledQuery, error) {
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
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}

	args := newSampleArgs(input.Dialect, filterArgs)
	conditions := make([]string, 0, 2)
	if hasMin {
		conditions = append(conditions, "group_rows < "+args.add(minValue))
	}
	if hasMax {
		conditions = append(conditions, "group_rows > "+args.add(maxValue))
	}
	groupList := qualifyColumns(input.Dialect, columns)
	query := "SELECT " + groupList + ", group_rows FROM (SELECT " + groupList +
		", COUNT(*) AS group_rows FROM " + table + where + " GROUP BY " + groupList +
		") dq WHERE " + strings.Join(conditions, " OR ") +
		" ORDER BY group_rows DESC, " + groupList + " " + input.Dialect.LimitClause(limit)
	resultColumns := make([]string, 0, len(columns)+1)
	resultColumns = append(resultColumns, columns...)
	resultColumns = append(resultColumns, "group_rows")
	return CompiledQuery{
		SQL:     query,
		Args:    args.values,
		Kind:    QuerySample,
		Columns: resultColumns,
	}, nil
}
