package dataquality

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	celoperators "github.com/google/cel-go/common/operators"
	celoverloads "github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// CEL compile-time bounds. They mirror the config proto's documented limits and
// are enforced here because validation and compilation are the last places a bad
// value can be refused before it reaches a sample query.
const (
	// maxCELExpressionLength bounds one expression (4 KiB), the same order as the
	// custom-SQL definition limit.
	maxCELExpressionLength = 4 * 1024
	// maxCELColumns bounds how many target columns one expression may read.
	maxCELColumns = 20
	// defaultCELSampleRows is the sample size when the config leaves it unset.
	// CEL is evaluated in process, so the sample is the execution budget.
	defaultCELSampleRows = 10000
	// maxCELSampleRows bounds one sample query and therefore the memory one
	// execution can spend on rows.
	maxCELSampleRows = 100000
)

// celIntegerTypes are the engine type names whose values are whole numbers.
// ClassifyColumnType already reduced the spelling to its base name.
var celIntegerTypes = map[string]bool{
	"int": true, "integer": true, "tinyint": true, "smallint": true,
	"mediumint": true, "bigint": true, "serial": true, "smallserial": true,
	"bigserial": true,
}

// celIdentifierPattern is the CEL variable grammar. A column whose name is not an
// identifier cannot be referenced by an expression, so the rule is refused at
// validation instead of failing the compile with a syntax error.
var celIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// compileCelExpression builds the sample-row statement a CEL rule reads. The
// expression itself is not compiled here: it runs in Go against the returned
// rows, and Compile is the statement-shape contract used by the preview path.
func compileCelExpression(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetCelExpression()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the CEL expression config is missing")
	}
	columns, err := celColumns(input.Target, config.GetColumns())
	if err != nil {
		return CompiledQuery{}, err
	}
	sampleRows, err := celSampleRows(config.GetSampleRows())
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
	names := celColumnNames(columns)
	return sampleQuery(input,
		"SELECT "+sampleSelect(input.Dialect, names)+" FROM "+table+where,
		args.values, names, sampleRows), nil
}

// celColumns resolves the columns an expression may read: they must exist on the
// target, their names must be usable as CEL variables, and there must be between
// one and maxCELColumns of them. Duplicates are collapsed preserving order, so a
// config that names the same column twice still selects it once.
func celColumns(target ResolvedTarget, names []string) ([]ColumnInfo, error) {
	if len(names) == 0 {
		return nil, invalidConfig("a CEL expression needs at least one column")
	}
	columns := make([]ColumnInfo, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		column, err := requireColumn(target, strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		if !celIdentifierPattern.MatchString(column.Name) {
			return nil, invalidConfig("column %q cannot be referenced from a CEL expression: its name is not a CEL identifier", column.Name)
		}
		if _, ok := seen[column.Name]; ok {
			continue
		}
		seen[column.Name] = struct{}{}
		columns = append(columns, column)
	}
	if len(columns) > maxCELColumns {
		return nil, invalidConfig("a CEL expression reads at most %d columns, got %d", maxCELColumns, len(columns))
	}
	return columns, nil
}

// celColumnNames returns the resolved column names in order.
func celColumnNames(columns []ColumnInfo) []string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names
}

// celSampleRows maps the configured sample size to the number of rows the sample
// query asks for: unset reads as the default, and the maximum is refused rather
// than silently clamped so the operator sees the limit they crossed.
func celSampleRows(configured int32) (int, error) {
	if configured < 0 {
		return 0, invalidConfig("sample_rows must not be negative")
	}
	if configured == 0 {
		return defaultCELSampleRows, nil
	}
	if int(configured) > maxCELSampleRows {
		return 0, invalidConfig("sample_rows %d is above the maximum of %d", configured, maxCELSampleRows)
	}
	return int(configured), nil
}

// CompileCEL compiles one CEL predicate against the target's columns. The
// environment declares one variable per referenced column, with the CEL type that
// matches the column's kind, plus the helpers the rule language promises
// (is_null, coalesce, to_lower, to_upper); everything else is the CEL standard
// library. The expression must produce a bool: anything else would have no
// verdict to return.
func CompileCEL(columns []ColumnInfo, expression string) (cel.Program, error) {
	trimmed := strings.TrimSpace(expression)
	if trimmed == "" {
		return nil, invalidConfig("a CEL expression is required")
	}
	if len(trimmed) > maxCELExpressionLength {
		return nil, invalidConfig("the CEL expression is longer than %d bytes", maxCELExpressionLength)
	}
	env, err := newCELEnv(columns)
	if err != nil {
		return nil, err
	}
	parsed, issues := env.Parse(trimmed)
	if issues != nil && issues.Err() != nil {
		return nil, invalidConfig("failed to parse the CEL expression: %v", issues.Err())
	}
	if err := requireConstantMatchesPattern(parsed); err != nil {
		return nil, err
	}
	checked, issues := env.Check(parsed)
	if issues != nil && issues.Err() != nil {
		return nil, invalidConfig("failed to compile the CEL expression: %v", issues.Err())
	}
	if checked.OutputType() == nil || !checked.OutputType().IsExactType(cel.BoolType) {
		return nil, invalidConfig("a CEL expression must produce a bool, not %s", checked.OutputType())
	}
	program, err := env.Program(checked)
	if err != nil {
		return nil, invalidConfig("failed to build the CEL program: %v", err)
	}
	return program, nil
}

// newCELEnv builds the CEL environment one rule's expression compiles against.
// Declaring every referenced column is also the unknown-variable check: an
// expression that names anything else fails to parse.
func newCELEnv(columns []ColumnInfo) (*cel.Env, error) {
	if len(columns) == 0 {
		return nil, invalidConfig("a CEL expression needs at least one column")
	}
	options := make([]cel.EnvOption, 0, len(columns)+8)
	for _, column := range columns {
		columnType, err := celColumnType(column)
		if err != nil {
			return nil, err
		}
		options = append(options, cel.Variable(column.Name, columnType))
	}
	options = append(options,
		cel.Function("is_null",
			cel.Overload("is_null_dyn", []*cel.Type{cel.DynType}, cel.BoolType,
				cel.UnaryBinding(func(value ref.Val) ref.Val {
					if value == types.NullValue {
						return types.True
					}
					return types.False
				}))),
		// coalesce is a macro rather than a function: a typed function overload
		// refuses a NULL argument at run time (CEL dispatches on the argument's
		// runtime type), which is the exact case coalesce exists for. The expansion
		// is a conditional that only evaluates the branch it needs.
		cel.Macros(cel.GlobalMacro("coalesce", 2, expandCelCoalesce)),
		cel.Function("to_lower",
			cel.MemberOverload("string_to_lower", []*cel.Type{cel.StringType}, cel.StringType,
				cel.UnaryBinding(celToLower))),
		cel.Function("to_upper",
			cel.MemberOverload("string_to_upper", []*cel.Type{cel.StringType}, cel.StringType,
				cel.UnaryBinding(celToUpper))),
	)
	env, err := cel.NewEnv(options...)
	if err != nil {
		return nil, common.Errorf(common.Internal, "failed to build the CEL environment: %v", err)
	}
	return env, nil
}

// celBaseTypeName strips the precision and modifier suffixes from an engine type
// name, so "bigint unsigned" and "decimal(10,2)" are recognized the way
// ClassifyColumnType classifies them.
func celBaseTypeName(columnType string) string {
	name := strings.ToLower(strings.TrimSpace(columnType))
	if index := strings.IndexAny(name, "( "); index >= 0 {
		name = name[:index]
	}
	return name
}

// celColumnType maps one column kind onto the CEL type its values are bound as.
// A kind this build cannot classify is refused: the expression would otherwise
// compare a value the CEL adapter cannot represent.
func celColumnType(column ColumnInfo) (*cel.Type, error) {
	switch column.Kind {
	case ColumnKindNumeric:
		if celIntegerTypes[celBaseTypeName(column.Type)] {
			return cel.IntType, nil
		}
		return cel.DoubleType, nil
	case ColumnKindString:
		return cel.StringType, nil
	case ColumnKindBoolean:
		return cel.BoolType, nil
	case ColumnKindTemporal:
		return cel.TimestampType, nil
	case ColumnKindUnknown:
		return nil, invalidConfig("column %q has a type this build cannot evaluate in CEL", column.Name)
	default:
		return nil, invalidConfig("column %q has a type this build cannot evaluate in CEL", column.Name)
	}
}

// expandCelCoalesce rewrites coalesce(value, fallback) to
// is_null(value) ? fallback : value. Both branches keep their concrete type, so
// the result is the type the surrounding expression needs, and only the chosen
// branch is evaluated: a NULL value never reaches an operator that cannot accept
// it.
func expandCelCoalesce(mef cel.MacroExprFactory, target celast.Expr, args []celast.Expr) (celast.Expr, *cel.Error) {
	if target != nil {
		return nil, mef.NewError(args[0].ID(), "coalesce is a global function")
	}
	isNull := mef.NewCall("is_null", mef.Copy(args[0]))
	return mef.NewCall(celoperators.Conditional, isNull, mef.Copy(args[1]), mef.Copy(args[0])), nil
}

// celToLower lowercases a string value.
func celToLower(value ref.Val) ref.Val {
	text, ok := value.Value().(string)
	if !ok {
		return types.NewErr("to_lower expects a string, got %T", value.Value())
	}
	return types.String(strings.ToLower(text))
}

// celToUpper uppercases a string value.
func celToUpper(value ref.Val) ref.Val {
	text, ok := value.Value().(string)
	if !ok {
		return types.NewErr("to_upper expects a string, got %T", value.Value())
	}
	return types.String(strings.ToUpper(text))
}

// requireConstantMatchesPattern enforces the design's rule that a matches() pattern
// is a literal: a pattern built from row data would be a run-time regex compiler
// driven by the data, and it would also be invisible to the save-time check below.
// A literal pattern is compiled once here, so an invalid RE2 expression is a save
// error instead of a per-row failure.
func requireConstantMatchesPattern(parsed *cel.Ast) error {
	var firstErr error
	celast.PreOrderVisit(parsed.NativeRep().Expr(), celast.NewExprVisitor(func(expr celast.Expr) {
		if firstErr != nil || expr.Kind() != celast.CallKind {
			return
		}
		call := expr.AsCall()
		if call.FunctionName() != celoverloads.Matches {
			return
		}
		// A member call (email.matches(pattern)) excludes its target from Args;
		// the global spelling would carry the value first.
		var pattern celast.Expr
		if call.IsMemberFunction() {
			if len(call.Args()) != 1 {
				return
			}
			pattern = call.Args()[0]
		} else {
			if len(call.Args()) != 2 {
				return
			}
			pattern = call.Args()[1]
		}
		if pattern.Kind() != celast.LiteralKind {
			firstErr = invalidConfig("the matches() pattern must be a constant string")
			return
		}
		literal, ok := pattern.AsLiteral().Value().(string)
		if !ok {
			firstErr = invalidConfig("the matches() pattern must be a constant string")
			return
		}
		if _, err := regexp.Compile(literal); err != nil {
			firstErr = invalidConfig("the matches() pattern is not a valid RE2 expression: %v", err)
		}
	}))
	return firstErr
}

// EvalCELRows evaluates one compiled predicate over the sample rows and returns
// how many rows violated it and how many were evaluated. It stops at the first row
// whose value cannot be represented or whose evaluation errors: a silent skip
// would turn a broken rule into a passing one.
func EvalCELRows(program cel.Program, columns []ColumnInfo, rows [][]any) (violations int64, evaluated int64, err error) {
	violations, evaluated, _, err = evaluateCELRows(program, columns, rows, 0)
	return violations, evaluated, err
}

// evalCELRows is EvalCELRows plus the rows that violated the predicate, up to
// failingLimit. The executor needs them for the failure sample; the public wrapper
// does not expose them because the row count is the observation.
func evaluateCELRows(program cel.Program, columns []ColumnInfo, rows [][]any, failingLimit int) (violations int64, evaluated int64, failing [][]any, err error) {
	for rowIndex, row := range rows {
		activation := make(map[string]any, len(columns))
		for columnIndex, column := range columns {
			if columnIndex >= len(row) {
				return 0, 0, nil, badData("sample row %d has %d values, expect %d", rowIndex, len(row), len(columns))
			}
			value, err := celRowValue(column, row[columnIndex])
			if err != nil {
				return 0, 0, nil, err
			}
			activation[column.Name] = value
		}
		result, _, err := program.Eval(activation)
		if err != nil {
			return 0, 0, nil, badData("failed to evaluate the CEL expression on row %d: %v", rowIndex, err)
		}
		matched, ok := result.Value().(bool)
		if !ok {
			return 0, 0, nil, badData("the CEL expression returned %T on row %d, expect bool", result.Value(), rowIndex)
		}
		evaluated++
		if !matched {
			violations++
			if failingLimit > 0 && len(failing) < failingLimit {
				failing = append(failing, row)
			}
		}
	}
	return violations, evaluated, failing, nil
}

// celRowValue converts one normalized sample value into the Go value the CEL
// environment declared for that column. A SQL NULL becomes a nil interface, which
// CEL adapts to its null value: is_null() is how an expression asks about it.
func celRowValue(column ColumnInfo, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch column.Kind {
	case ColumnKindNumeric:
		if celIntegerTypes[celBaseTypeName(column.Type)] {
			return celIntValue(column, value)
		}
		return celDoubleValue(column, value)
	case ColumnKindString:
		return celStringValue(column, value)
	case ColumnKindBoolean:
		return celBoolValue(column, value)
	case ColumnKindTemporal:
		return celTimestampValue(column, value)
	case ColumnKindUnknown:
		return nil, badData("column %q has a type this build cannot evaluate in CEL", column.Name)
	default:
		return nil, badData("column %q has a type this build cannot evaluate in CEL", column.Name)
	}
}

// celIntValue converts one sample value to the int64 a CEL int variable holds.
func celIntValue(column ColumnInfo, value any) (any, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case float64:
		if typed != math.Trunc(typed) {
			return nil, badData("column %q is not an integer (%v)", column.Name, typed)
		}
		return int64(typed), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return nil, badData("column %q is not an integer (%q)", column.Name, typed)
		}
		return parsed, nil
	case []byte:
		return celIntValue(column, string(typed))
	default:
		return nil, badData("column %q has an unexpected %T value", column.Name, value)
	}
}

// celDoubleValue converts one sample value to the float64 a CEL double variable
// holds. A string is parsed by celParseNumber: PostgreSQL money reaches a loosely
// typed driver as decorated currency text ("$1,234.50"), and the parser accepts
// that only when the rendering is unambiguous.
func celDoubleValue(column ColumnInfo, value any) (any, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case int64:
		return float64(typed), nil
	case string:
		parsed, err := celParseNumber(typed)
		if err != nil {
			return nil, badData("column %q is not a number (%q)", column.Name, typed)
		}
		return parsed, nil
	case []byte:
		return celDoubleValue(column, string(typed))
	default:
		return nil, badData("column %q has an unexpected %T value", column.Name, value)
	}
}

// celCurrencySymbols are the currency signs a money rendering may carry. They are
// removed wherever they appear: they never separate digits, and PostgreSQL writes a
// negative amount as "-$1.23".
const celCurrencySymbols = "$€£¥₹₩₽¢"

// celGroupedCommaNumber matches a comma-grouped number with an optional decimal
// point: 1,234,567.89. A lone comma outside this shape (1,5) is ambiguous and is
// refused rather than guessed.
var celGroupedCommaNumber = regexp.MustCompile(`^[+-]?\d{1,3}(,\d{3})+(\.\d+)?$`)

// celGroupedDotNumber matches a dot-grouped number with an optional comma decimal:
// 1.234.567,89.
var celGroupedDotNumber = regexp.MustCompile(`^[+-]?\d{1,3}(\.\d{3})+(,\d+)?$`)

// celParseNumber parses the string rendering of one numeric sample value. A plain
// decimal or scientific string is the common case. Money is rendered with a
// currency symbol, grouped thousands and either decimal separator, so a decorated
// value is accepted only when its separator layout is unambiguous: the last of
// "." and "," is the decimal separator and the grouping matches three digits.
// Anything else — "1,5", "1 234,50", a non-finite spelling — is an error, because
// guessing a locale would silently change the value the rule judges.
func celParseNumber(raw string) (float64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, strconv.ErrSyntax
	}
	if parsed, ok := celFiniteFloat(trimmed); ok {
		return parsed, nil
	}

	// A money rendering may be parenthesized to mean negative and may carry the
	// currency sign on either side of the number. Remove the sign wherever it
	// appears, then require no internal whitespace: that spelling ("1 234,50") is
	// ambiguous, not a separator this parser guesses at.
	core := trimmed
	negative := false
	if strings.HasPrefix(core, "(") && strings.HasSuffix(core, ")") && len(core) > 2 {
		negative = true
		core = core[1 : len(core)-1]
	}
	core = strings.Map(func(r rune) rune {
		if strings.ContainsRune(celCurrencySymbols, r) {
			return -1
		}
		return r
	}, core)
	core = strings.TrimSpace(core)
	if core == "" {
		return 0, strconv.ErrSyntax
	}
	// Internal whitespace is the ambiguous spelling "1 234,50" (thousands or a
	// stray space), never a separator this parser guesses at.
	if strings.ContainsAny(core, " \t\r\n\u00a0\u202f") {
		return 0, strconv.ErrSyntax
	}

	parsed, ok := celParseSeparatedNumber(core)
	if !ok {
		return 0, strconv.ErrSyntax
	}
	if negative {
		parsed = -parsed
	}
	return parsed, nil
}

// celParseSeparatedNumber parses a number whose groups use one or both separators.
func celParseSeparatedNumber(value string) (float64, bool) {
	switch {
	case !strings.Contains(value, ","):
		// No grouping: a plain decimal, possibly with the decoration already
		// stripped ("1.23€").
		return celFiniteFloat(value)
	case celGroupedCommaNumber.MatchString(value):
		return celFiniteFloat(strings.ReplaceAll(value, ",", ""))
	case celGroupedDotNumber.MatchString(value):
		replaced := strings.ReplaceAll(value, ".", "")
		return celFiniteFloat(strings.Replace(replaced, ",", ".", 1))
	default:
		return 0, false
	}
}

// celFiniteFloat parses one plain float and rejects NaN and the infinities: they
// are not data a quality rule can compare.
func celFiniteFloat(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

// celStringValue converts one sample value to the string a CEL string variable
// holds. A numeric or temporal value is not stringified: the column's declared
// type is text, so a non-text value is a contract violation.
func celStringValue(column ColumnInfo, value any) (any, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return nil, badData("column %q has an unexpected %T value, expect a string", column.Name, value)
	}
}

// celBoolValue converts one sample value to the bool a CEL bool variable holds.
// MySQL-wire engines report BIT as a number or a byte, so the two literal
// spellings are accepted.
func celBoolValue(column ColumnInfo, value any) (any, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case int64:
		switch typed {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
		return nil, badData("column %q is not a boolean (%d)", column.Name, typed)
	case []byte:
		return celBoolValue(column, string(typed))
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "0", "false":
			return false, nil
		case "1", "true":
			return true, nil
		}
		return nil, badData("column %q is not a boolean (%q)", column.Name, typed)
	default:
		return nil, badData("column %q has an unexpected %T value", column.Name, value)
	}
}

// celTimestampValue converts one sample value to the time.Time a CEL timestamp
// variable holds. A driver that reports a date/time type loosely leaves a string,
// which is parsed with the same layouts the aggregate path accepts.
func celTimestampValue(column ColumnInfo, value any) (any, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed, nil
	case string:
		parsed, err := parseCELTimestamp(typed)
		if err != nil {
			return nil, badData("column %q is not a timestamp (%q)", column.Name, typed)
		}
		return parsed, nil
	case []byte:
		return celTimestampValue(column, string(typed))
	default:
		return nil, badData("column %q has an unexpected %T value, expect a timestamp", column.Name, value)
	}
}

// parseCELTimestamp parses one loosely typed timestamp value.
func parseCELTimestamp(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	for _, layout := range aggregateTimestampLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable timestamp %q", value)
}
