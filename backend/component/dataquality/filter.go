package dataquality

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// FilterOp is one comparison in a rule's AND filter.
type FilterOp int

const (
	FilterEQ FilterOp = iota + 1
	FilterNE
	FilterLT
	FilterLTE
	FilterGT
	FilterGTE
	FilterIN
	FilterNOTIN
	FilterISNull
	FilterISNotNull
)

// String renders the operator for error messages.
func (op FilterOp) String() string {
	switch op {
	case FilterEQ:
		return "="
	case FilterNE:
		return "!="
	case FilterLT:
		return "<"
	case FilterLTE:
		return "<="
	case FilterGT:
		return ">"
	case FilterGTE:
		return ">="
	case FilterIN:
		return "IN"
	case FilterNOTIN:
		return "NOT IN"
	case FilterISNull:
		return "IS NULL"
	case FilterISNotNull:
		return "IS NOT NULL"
	}
	return "unknown"
}

// FilterClause is one column comparison. Values are strings on the wire and are
// converted using the column's type before they become parameters.
type FilterClause struct {
	Column string
	Op     FilterOp
	Values []string
}

// Filter is an AND of clauses. The empty filter matches every row.
type Filter struct {
	Clauses []FilterClause
}

// MaxFilterClauses bounds a rule's filter; a longer one is better expressed as
// custom SQL.
const MaxFilterClauses = 10

// MaxFilterINValues bounds the value list of one IN / NOT IN clause.
const MaxFilterINValues = 100

// ParseFilter converts the stored protobuf filter into the compiler's model. It
// rejects operators with the wrong number of values and oversized filters; column
// existence and value conversion happen when the filter compiles against a
// resolved target.
func ParseFilter(filter *storepb.QualityFilter) (Filter, error) {
	if filter == nil {
		return Filter{}, nil
	}
	clauses := filter.GetClauses()
	if len(clauses) > MaxFilterClauses {
		return Filter{}, common.Errorf(common.Invalid, "a filter allows at most %d clauses, got %d", MaxFilterClauses, len(clauses))
	}
	parsed := Filter{Clauses: make([]FilterClause, 0, len(clauses))}
	for _, clause := range clauses {
		if clause == nil {
			return Filter{}, common.Errorf(common.Invalid, "a filter clause is required")
		}
		column := strings.TrimSpace(clause.GetColumn())
		if column == "" {
			return Filter{}, common.Errorf(common.Invalid, "a filter clause needs a column")
		}
		op, err := parseFilterOp(clause.GetOp())
		if err != nil {
			return Filter{}, err
		}
		values := clause.GetValues()
		switch op {
		case FilterISNull, FilterISNotNull:
			if len(values) != 0 {
				return Filter{}, common.Errorf(common.Invalid, "filter %s on %q takes no values", op, column)
			}
		case FilterIN, FilterNOTIN:
			if len(values) == 0 {
				return Filter{}, common.Errorf(common.Invalid, "filter %s on %q needs at least one value", op, column)
			}
			if len(values) > MaxFilterINValues {
				return Filter{}, common.Errorf(common.Invalid, "filter %s on %q allows at most %d values, got %d", op, column, MaxFilterINValues, len(values))
			}
		default:
			if len(values) != 1 {
				return Filter{}, common.Errorf(common.Invalid, "filter %s on %q takes exactly one value, got %d", op, column, len(values))
			}
		}
		parsed.Clauses = append(parsed.Clauses, FilterClause{Column: column, Op: op, Values: values})
	}
	return parsed, nil
}

// parseFilterOp maps the stored enum to the compiler's operator, rejecting
// UNSPECIFIED.
func parseFilterOp(op storepb.QualityFilterOperator) (FilterOp, error) {
	switch op {
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ:
		return FilterEQ, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_NE:
		return FilterNE, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_LT:
		return FilterLT, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_LTE:
		return FilterLTE, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GT:
		return FilterGT, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE:
		return FilterGTE, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IN:
		return FilterIN, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_NOT_IN:
		return FilterNOTIN, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NULL:
		return FilterISNull, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NOT_NULL:
		return FilterISNotNull, nil
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_UNSPECIFIED:
		return 0, common.Errorf(common.Invalid, "a filter clause needs an operator")
	default:
		return 0, common.Errorf(common.Invalid, "unknown filter operator %s", op)
	}
}

// CompileFilter renders the filter's WHERE predicate (without the WHERE keyword)
// and its arguments. The predicate is empty when the filter has no clauses.
//
// Every value is bound as a parameter; the column is quoted from the resolved
// target, never from user input.
func CompileFilter(dialect Dialect, target ResolvedTarget, filter Filter) (string, []any, error) {
	if len(filter.Clauses) == 0 {
		return "", nil, nil
	}
	conditions := make([]string, 0, len(filter.Clauses))
	args := make([]any, 0, len(filter.Clauses))
	for _, clause := range filter.Clauses {
		column, ok := target.Column(clause.Column)
		if !ok {
			return "", nil, common.Errorf(common.Invalid, "filter column %q does not exist on %s", clause.Column, target.GUID)
		}
		quoted := dialect.QuoteIdentifier(column.Name)
		switch clause.Op {
		case FilterISNull, FilterISNotNull:
			if !supportsNullTest(column, target.Engine) {
				return "", nil, invalidConfig("filter column %q is %s; the engine cannot test values of this type for NULL", column.Name, column.Type)
			}
			if clause.Op == FilterISNull {
				conditions = append(conditions, quoted+" IS NULL")
			} else {
				conditions = append(conditions, quoted+" IS NOT NULL")
			}
		default:
			// A value comparison needs equality for the column's type. IS NULL and
			// IS NOT NULL above work for every type, so they are checked first.
			if column.Kind == ColumnKindUnknown {
				return "", nil, invalidConfig("filter column %q has a type this build cannot filter on", column.Name)
			}
			if !supportsComparison(column, target.Engine) {
				return "", nil, invalidConfig("filter column %q is %s; the engine cannot compare values of this type", column.Name, column.Type)
			}
			values, err := convertFilterValues(column, clause.Values)
			if err != nil {
				return "", nil, err
			}
			conditions = append(conditions, renderFilterComparison(dialect, quoted, clause.Op, values, &args))
		}
	}
	return strings.Join(conditions, " AND "), args, nil
}

// renderFilterComparison renders one operator's predicate and appends its
// arguments.
func renderFilterComparison(dialect Dialect, quoted string, op FilterOp, values []any, args *[]any) string {
	if op == FilterIN || op == FilterNOTIN {
		placeholders := make([]string, 0, len(values))
		for _, value := range values {
			*args = append(*args, value)
			placeholders = append(placeholders, dialect.Placeholder(len(*args)))
		}
		keyword := "IN"
		if op == FilterNOTIN {
			keyword = "NOT IN"
		}
		return fmt.Sprintf("%s %s (%s)", quoted, keyword, strings.Join(placeholders, ", "))
	}
	*args = append(*args, values[0])
	return quoted + " " + op.String() + " " + dialect.Placeholder(len(*args))
}

// convertFilterValues converts the wire strings to typed arguments using the
// column's kind. A value the column's type cannot represent is a validation
// error, not a runtime surprise.
func convertFilterValues(column ColumnInfo, values []string) ([]any, error) {
	converted := make([]any, 0, len(values))
	for _, value := range values {
		parsed, err := convertFilterValue(column, value)
		if err != nil {
			return nil, err
		}
		converted = append(converted, parsed)
	}
	return converted, nil
}

// convertFilterValue converts one value. Temporal and text values stay strings:
// the engine casts them, and reformatting a date would change the comparison.
func convertFilterValue(column ColumnInfo, value string) (any, error) {
	switch column.Kind {
	case ColumnKindNumeric:
		if integer, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return integer, nil
		}
		number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, common.Errorf(common.Invalid, "filter value %q is not a number for column %q", value, column.Name)
		}
		return number, nil
	case ColumnKindBoolean:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, common.Errorf(common.Invalid, "filter value %q is not a boolean for column %q", value, column.Name)
		}
		return parsed, nil
	case ColumnKindString, ColumnKindTemporal:
		return value, nil
	case ColumnKindUnknown:
		return nil, common.Errorf(common.Invalid, "column %q has a type this build cannot filter on", column.Name)
	}
	return nil, errors.Errorf("unclassified column kind %d", column.Kind)
}
