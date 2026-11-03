package dataquality

import (
	"slices"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// acceptedValuesSource names which of the three value sources an accepted-values
// config selects: the fixed list, another column of the same target, or a
// single-column query.
type acceptedValuesSource int

const (
	acceptedValuesList acceptedValuesSource = iota + 1
	acceptedValuesField
	acceptedValuesQuery
)

// acceptedValuesSourceOf reports the config's source and refuses a config that
// names none of them or more than one. Validation, the aggregate compiler and the
// failure sample all call it, so the three cannot disagree about what the rule
// means.
func acceptedValuesSourceOf(accepted *storepb.AcceptedValuesConfig) (acceptedValuesSource, error) {
	if accepted == nil {
		return 0, invalidConfig("the accepted-values config is missing")
	}
	list := len(accepted.GetValues()) > 0
	field := strings.TrimSpace(accepted.GetField()) != ""
	query := accepted.GetQuery() != nil
	sources := 0
	for _, present := range []bool{list, field, query} {
		if present {
			sources++
		}
	}
	if sources > 1 {
		return 0, invalidConfig("an accepted-values rule takes one value source: a value list, another column, or a query, not several")
	}
	switch {
	case list:
		return acceptedValuesList, nil
	case field:
		return acceptedValuesField, nil
	case query:
		return acceptedValuesQuery, nil
	default:
		return 0, invalidConfig("an accepted-values rule needs a value source: a value list, another column, or a query")
	}
}

// acceptedValuesColumn resolves the column the rule checks. A fixed list binds
// scalar values, so the column must also accept them as parameters; a dynamic
// source only compares the column with the set, so comparison is enough.
func acceptedValuesColumn(target ResolvedTarget, accepted *storepb.AcceptedValuesConfig, source acceptedValuesSource) (ColumnInfo, error) {
	if source == acceptedValuesList {
		return requireValueComparisonColumn(target, accepted.GetColumn())
	}
	return requireComparisonColumn(target, accepted.GetColumn())
}

// validateAcceptedValuesSource checks the source's own parameters: the list is
// bounded and convertible, the field names a different comparable column of the
// same target, and the query is a bounded read-only statement that names the
// column it returns.
func validateAcceptedValuesSource(target ResolvedTarget, accepted *storepb.AcceptedValuesConfig, source acceptedValuesSource, column ColumnInfo) error {
	switch source {
	case acceptedValuesList:
		values := accepted.GetValues()
		if len(values) > compileMaxAcceptedValues {
			return invalidConfig("an accepted-values rule allows at most %d values, got %d", compileMaxAcceptedValues, len(values))
		}
		for _, value := range values {
			if _, err := convertFilterValue(column, value); err != nil {
				return err
			}
		}
	case acceptedValuesField:
		if _, err := acceptedValuesFieldColumn(target, accepted, column); err != nil {
			return err
		}
	case acceptedValuesQuery:
		return validateAcceptedValuesQuery(target.Engine, accepted.GetQuery())
	default:
		return invalidConfig("unknown accepted-values value source %d", source)
	}
	return nil
}

// acceptedValuesFieldColumn resolves the reference column of a field source and
// checks the set membership is expressible: the column exists, differs from the
// checked column, and the two types compare. Kind equality alone is not enough —
// the "string" kind holds text, jsonb, uuid and user-defined types alike.
func acceptedValuesFieldColumn(target ResolvedTarget, accepted *storepb.AcceptedValuesConfig, column ColumnInfo) (ColumnInfo, error) {
	reference, err := requireComparisonColumn(target, strings.TrimSpace(accepted.GetField()))
	if err != nil {
		return ColumnInfo{}, err
	}
	if reference.Name == column.Name {
		return ColumnInfo{}, invalidConfig("the reference column must differ from the checked column %q", column.Name)
	}
	if !comparableColumnTypes(column, reference, target.Engine) {
		return ColumnInfo{}, invalidConfig("columns %q (%s) and %q (%s) are not comparable; an accepted-values rule needs both sides to be text or the same type", column.Name, column.Type, reference.Name, reference.Type)
	}
	return reference, nil
}

// validateAcceptedValuesQuery checks a query source without a dialect: the
// statement is a single read-only SELECT/WITH this build's parser accepts for the
// target engine, within the custom-SQL size bound, and it names the column it
// returns. The returned column's type is only knowable when the statement runs,
// so a wrong type is a runtime error (Preview proves it before saving).
func validateAcceptedValuesQuery(engine storepb.Engine, query *storepb.AcceptedValuesQuery) error {
	if query == nil {
		return invalidConfig("the accepted-values query source is missing")
	}
	if strings.TrimSpace(query.GetValueColumn()) == "" {
		return invalidConfig("an accepted-values query source needs the column its query returns")
	}
	if err := ValidateCustomSQL(engine, query.GetSql()); err != nil {
		return invalidConfig("the accepted-values query is not a usable read-only statement: %v", err)
	}
	// The query is embedded as a derived table; SQL Server rejects two otherwise
	// valid read-only shapes there, so refuse them at save time.
	if engine == storepb.Engine_MSSQL {
		parseable, err := customSQLForParsing(query.GetSql())
		if err != nil {
			return err
		}
		return validateMSSQLDerivedQuery(parseable)
	}
	return nil
}

// acceptedValuesCondition renders the predicate that is true for a row outside
// the accepted set. A fixed list binds its values through placeholder in the
// order they appear in the SQL text; a dynamic source compiles to a subquery that
// filters NULL out of the set, because a NULL in the set would make NOT IN
// unknown for every row and silently pass the whole table. allow_null controls
// the outer NULL arm only, as it always has.
func acceptedValuesCondition(input CompileInput, accepted *storepb.AcceptedValuesConfig, column ColumnInfo, placeholder func(any) string) (string, error) {
	source, err := acceptedValuesSourceOf(accepted)
	if err != nil {
		return "", err
	}
	var set string
	switch source {
	case acceptedValuesList:
		values := accepted.GetValues()
		if len(values) > compileMaxAcceptedValues {
			return "", invalidConfig("an accepted-values rule allows at most %d values, got %d", compileMaxAcceptedValues, len(values))
		}
		placeholders := make([]string, 0, len(values))
		for _, value := range values {
			converted, err := compileValueArg(column, value)
			if err != nil {
				return "", err
			}
			placeholders = append(placeholders, placeholder(converted))
		}
		set = strings.Join(placeholders, ", ")
	case acceptedValuesField:
		set, err = acceptedValuesFieldSet(input, accepted, column)
	case acceptedValuesQuery:
		set, err = acceptedValuesQuerySet(input, accepted.GetQuery())
	default:
		return "", invalidConfig("unknown accepted-values value source %d", source)
	}
	if err != nil {
		return "", err
	}
	qualified := qualify(input.Dialect, column.Name)
	condition := qualified + " NOT IN (" + set + ")"
	if !accepted.GetAllowNull() {
		condition = qualified + " IS NULL OR " + condition
	}
	return condition, nil
}

// acceptedValuesFieldSet renders the subquery that reads one column of the
// target's own table. The rule's filter and the contract filter bound the checked
// rows, not the accepted set: the set is what the column holds, the same way a
// referential rule reads its whole parent relation.
func acceptedValuesFieldSet(input CompileInput, accepted *storepb.AcceptedValuesConfig, column ColumnInfo) (string, error) {
	reference, err := acceptedValuesFieldColumn(input.Target, accepted, column)
	if err != nil {
		return "", err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return "", err
	}
	quoted := qualify(input.Dialect, reference.Name)
	return "SELECT " + quoted + " FROM " + table + " WHERE " + quoted + " IS NOT NULL", nil
}

// acceptedValuesQuerySet renders the subquery a query source names. The values
// are read over the target's read-only data source, and the time variables are
// rendered with the same rendering a custom SQL check uses, so the statement the
// save path parsed is the statement that runs.
func acceptedValuesQuerySet(input CompileInput, query *storepb.AcceptedValuesQuery) (string, error) {
	if input.Target.Engine != storepb.Engine_ENGINE_UNSPECIFIED {
		// A row written before this check existed must not run a non-SELECT
		// statement: compile re-validates the stored text, like custom SQL does.
		if err := validateAcceptedValuesQuery(input.Target.Engine, query); err != nil {
			return "", err
		}
	}
	if query == nil {
		return "", invalidConfig("the accepted-values query source is missing")
	}
	statement := strings.TrimSpace(query.GetSql())
	statement = strings.TrimRight(statement, "; \t\r\n")
	rendered, err := RenderCustomSQLVariables(statement, input.Variables)
	if err != nil {
		return "", err
	}
	// The derived table is aliased differently from scanRelation's bounded form
	// ("dq") so neither wrapper can shadow the other.
	quoted := qualify(input.Dialect, strings.TrimSpace(query.GetValueColumn()))
	// The newline keeps a trailing line comment in the user statement from
	// swallowing the closing parenthesis and the wrapper's WHERE clause.
	return "SELECT dq_values." + quoted + " FROM (" + rendered + "\n) dq_values WHERE dq_values." + quoted + " IS NOT NULL", nil
}

// acceptedValuesSourceDiscriminator renders the value source as part of a
// suggestion's identity: a fixed list keeps its sorted, de-duplicated values, a
// reference column is named, and a query is hashed with the column it returns.
func acceptedValuesSourceDiscriminator(accepted *storepb.AcceptedValuesConfig) string {
	source, err := acceptedValuesSourceOf(accepted)
	if err != nil {
		return "values="
	}
	switch source {
	case acceptedValuesList:
		values := append([]string(nil), accepted.GetValues()...)
		slices.Sort(values)
		return "values=" + suggestionSignatureHash(strings.Join(slices.Compact(values), "\x00"))
	case acceptedValuesField:
		return "field=" + strings.TrimSpace(accepted.GetField())
	default:
		query := accepted.GetQuery()
		return "query=" + suggestionSignatureHash(strings.TrimSpace(query.GetSql())+"\x00"+strings.TrimSpace(query.GetValueColumn()))
	}
}
