package v1

import (
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	celoperators "github.com/google/cel-go/common/operators"
	celoverloads "github.com/google/cel-go/common/overloads"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// filterField maps one CEL filter variable onto SQL. Every predicate is built
// through *filterArgs, the only place a placeholder can be allocated, so no
// field handler can splice a client value into the WHERE clause.
type filterField struct {
	// compare renders `variable <op> value`, where op is one of the
	// ComparatorType constants.
	compare func(args *filterArgs, variable string, op OperatorType, value any) (string, error)
	// matches renders `variable.matches("pattern")`; pattern is as written.
	matches func(args *filterArgs, variable, pattern string) (string, error)
	// in renders `variable in [...]` (negate false) and `!(variable in [...])`.
	in func(args *filterArgs, variable string, values []string, negate bool) (string, error)
}

// filterArgs accumulates the positional arguments of a translated filter.
type filterArgs struct {
	args []any
}

// add records a value and returns its placeholder.
func (a *filterArgs) add(value any) string {
	a.args = append(a.args, value)
	return fmt.Sprintf("$%d", len(a.args))
}

func invalidFilter(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, errors.Errorf(format, args...))
}

// translateFilter parses a CEL filter and renders it into a parameterized SQL
// predicate. Every list method shares this grammar: `&&`, `||`, `==`, `>=`,
// `<=`, `field.matches("...")` and `field in [...]` / `!(field in [...])`.
// Variables are resolved through fields; an unsupported variable or operator is
// InvalidArgument rather than a partially built predicate.
func translateFilter(filter string, fields map[string]filterField) (*store.ListResourceFilter, error) {
	if strings.TrimSpace(filter) == "" {
		return nil, nil
	}

	env, err := cel.NewEnv()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create cel env"))
	}
	ast, iss := env.Parse(filter)
	if iss != nil {
		return nil, invalidFilter("failed to parse filter %q, error: %v", filter, iss.String())
	}

	args := &filterArgs{}
	var walk func(expr celast.Expr) (string, error)
	walk = func(expr celast.Expr) (string, error) {
		if expr.Kind() != celast.CallKind {
			return "", invalidFilter("unexpected filter expr kind %v", expr.Kind())
		}
		call := expr.AsCall()
		switch call.FunctionName() {
		case celoperators.LogicalOr:
			return walkSubConditions(expr, walk, "OR")
		case celoperators.LogicalAnd:
			return walkSubConditions(expr, walk, "AND")
		case celoperators.Equals:
			return walkCompare(expr, args, fields, ComparatorTypeEqual)
		case celoperators.GreaterEquals:
			return walkCompare(expr, args, fields, ComparatorTypeGreaterEqual)
		case celoperators.LessEquals:
			return walkCompare(expr, args, fields, ComparatorTypeLessEqual)
		case celoverloads.Matches:
			variable, value, err := matchArgs(expr)
			if err != nil {
				return "", err
			}
			pattern, err := filterString(variable, value)
			if err != nil {
				return "", err
			}
			if pattern == "" {
				return "", invalidFilter("empty value for %q", variable)
			}
			field, ok := fields[variable]
			if !ok || field.matches == nil {
				return "", invalidFilter("%q does not support the %q operator", variable, celoverloads.Matches)
			}
			return field.matches(args, variable, pattern)
		case celoperators.In:
			return walkIn(expr, args, fields, false)
		case celoperators.LogicalNot:
			operand := call.Args()
			if len(operand) != 1 || operand[0].Kind() != celast.CallKind || operand[0].AsCall().FunctionName() != celoperators.In {
				return "", invalidFilter(`only "!(field in [...])" is supported with "!"`)
			}
			return walkIn(operand[0], args, fields, true)
		default:
			return "", invalidFilter("unexpected filter function %q", call.FunctionName())
		}
	}

	where, err := walk(ast.NativeRep().Expr())
	if err != nil {
		return nil, err
	}
	return &store.ListResourceFilter{Args: args.args, Where: "(" + where + ")"}, nil
}

func walkCompare(expr celast.Expr, args *filterArgs, fields map[string]filterField, op OperatorType) (string, error) {
	variable, value, err := getVariableAndValueFromExpr(expr)
	if err != nil {
		return "", err
	}
	field, ok := fields[variable]
	if !ok || field.compare == nil {
		return "", invalidFilter("unsupported filter variable %q", variable)
	}
	return field.compare(args, variable, op, value)
}

func walkIn(expr celast.Expr, args *filterArgs, fields map[string]filterField, negate bool) (string, error) {
	variable, value, err := getVariableAndValueFromExpr(expr)
	if err != nil {
		return "", err
	}
	field, ok := fields[variable]
	if !ok || field.in == nil {
		return "", invalidFilter("%q does not support the %q operator", variable, celoperators.In)
	}
	values, err := filterStringList(variable, value)
	if err != nil {
		return "", err
	}
	return field.in(args, variable, values, negate)
}

func walkSubConditions(expr celast.Expr, walk func(celast.Expr) (string, error), join string) (string, error) {
	var conditions []string
	for _, arg := range expr.AsCall().Args() {
		sub, err := walk(arg)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, "("+sub+")")
	}
	return strings.Join(conditions, fmt.Sprintf(" %s ", join)), nil
}

// equalField maps `variable == "v"` to `column = $n` and rejects every other
// operator.
func equalField(column string) filterField {
	return filterField{compare: func(args *filterArgs, variable string, op OperatorType, value any) (string, error) {
		if op != ComparatorTypeEqual {
			return "", invalidFilter("%q only supports equality", variable)
		}
		v, err := filterString(variable, value)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s = %s", column, args.add(v)), nil
	}}
}

// likeField maps `variable == "v"` to `column = $n` and `variable.matches("p")`
// to a LIKE predicate. foldMatches additionally lowercases both sides, which is
// how the title/name/resource_id filters behave.
func likeField(column string, foldMatches bool) filterField {
	field := equalField(column)
	field.matches = func(args *filterArgs, _ string, pattern string) (string, error) {
		if foldMatches {
			return fmt.Sprintf("LOWER(%s) LIKE %s", column, args.add(likePattern(strings.ToLower(pattern)))), nil
		}
		return fmt.Sprintf("%s LIKE %s", column, args.add(likePattern(pattern))), nil
	}
	return field
}

// resourceField maps `variable == "instances/x"` to `column = $n`, converting
// the resource name with parse first.
func resourceField(column string, parse func(string) (string, error)) filterField {
	return filterField{compare: func(args *filterArgs, variable string, op OperatorType, value any) (string, error) {
		if op != ComparatorTypeEqual {
			return "", invalidFilter("%q only supports equality", variable)
		}
		v, err := filterString(variable, value)
		if err != nil {
			return "", err
		}
		id, err := parse(v)
		if err != nil {
			return "", invalidFilter("invalid %s filter %q", variable, v)
		}
		return fmt.Sprintf("%s = %s", column, args.add(id)), nil
	}}
}

// enumField maps an enum-valued variable onto a column. toStore converts the v1
// enum name, and allowIn additionally enables `in [...]` / `!(in [...])`.
func enumField(column string, toStore func(string) (any, error), allowIn bool) filterField {
	field := filterField{compare: func(args *filterArgs, variable string, op OperatorType, value any) (string, error) {
		if op != ComparatorTypeEqual {
			return "", invalidFilter("%q only supports equality", variable)
		}
		name, err := filterString(variable, value)
		if err != nil {
			return "", err
		}
		converted, err := toStore(name)
		if err != nil {
			return "", invalidFilter("invalid %s filter %q", variable, name)
		}
		return fmt.Sprintf("%s = %s", column, args.add(converted)), nil
	}}
	if allowIn {
		field.in = func(args *filterArgs, variable string, values []string, negate bool) (string, error) {
			placeholders := make([]string, 0, len(values))
			for _, name := range values {
				converted, err := toStore(name)
				if err != nil {
					return "", invalidFilter("invalid %s filter %q", variable, name)
				}
				placeholders = append(placeholders, args.add(converted))
			}
			relation := "IN"
			if negate {
				relation = "NOT IN"
			}
			return fmt.Sprintf("%s %s (%s)", column, relation, strings.Join(placeholders, ", ")), nil
		}
	}
	return field
}

// boolField maps a bool literal onto a predicate chosen by that literal.
func boolField(predicate func(args *filterArgs, value bool) (string, error)) filterField {
	return filterField{compare: func(args *filterArgs, variable string, op OperatorType, value any) (string, error) {
		if op != ComparatorTypeEqual {
			return "", invalidFilter("%q only supports equality", variable)
		}
		v, err := filterBool(variable, value)
		if err != nil {
			return "", err
		}
		return predicate(args, v)
	}}
}

// engineFilterValue maps a v1 engine name to the store engine recorded in the
// instance metadata.
func engineFilterValue(name string) (any, error) {
	value, ok := v1pb.Engine_value[name]
	if !ok {
		return nil, errors.Errorf("unknown engine %q", name)
	}
	return convertEngine(v1pb.Engine(value)), nil
}

// deletedFilterValue maps a v1 State name to the `deleted` column value.
func deletedFilterValue(name string) (any, error) {
	value, ok := v1pb.State_value[name]
	if !ok {
		return nil, errors.Errorf("unknown state %q", name)
	}
	return v1pb.State(value) == v1pb.State_DELETED, nil
}

// principalTypeFilterValue maps a v1 user type name to the store principal type.
func principalTypeFilterValue(name string) (any, error) {
	value, ok := v1pb.UserType_value[name]
	if !ok {
		return nil, errors.Errorf("unknown user type %q", name)
	}
	return convertToPrincipalType(v1pb.UserType(value))
}

func parseListInstanceFilter(filter string) (*store.ListResourceFilter, error) {
	return translateFilter(filter, map[string]filterField{
		"name":        likeField("instance.metadata->>'title'", true),
		"resource_id": likeField("instance.resource_id", true),
		"host":        likeField("ds ->> 'host'", false),
		"port":        likeField("ds ->> 'port'", false),
		"environment": resourceField("instance.environment", common.GetEnvironmentID),
		"state":       enumField("instance.deleted", deletedFilterValue, false),
		"engine":      enumField("instance.metadata->>'engine'", engineFilterValue, true),
	})
}

func parseListUserFilter(find *store.FindUserMessage, filter string) error {
	fields := map[string]filterField{
		"email":     likeField("principal.email", true),
		"name":      likeField("principal.name", true),
		"user_type": enumField("principal.type", principalTypeFilterValue, true),
		"state":     enumField("principal.deleted", deletedFilterValue, false),
	}

	translated, err := translateFilter(filter, fields)
	if err != nil {
		return err
	}
	find.Filter = translated
	return nil
}

func getListDatabaseFilter(filter string) (*store.ListResourceFilter, error) {
	return translateFilter(filter, map[string]filterField{
		"instance":    resourceField("db.instance", common.GetInstanceID),
		"environment": resourceField("COALESCE(db.environment, instance.environment)", common.GetEnvironmentID),
		"engine":      enumField("instance.metadata->>'engine'", engineFilterValue, true),
		"name":        likeField("db.name", true),
		"label": filterField{compare: func(args *filterArgs, variable string, op OperatorType, value any) (string, error) {
			if op != ComparatorTypeEqual {
				return "", invalidFilter("%q only supports equality", variable)
			}
			v, err := filterString(variable, value)
			if err != nil {
				return "", err
			}
			keyValue := strings.Split(v, ":")
			if len(keyValue) != 2 {
				return "", invalidFilter(`invalid label filter %q, should be in "{label key}:{label value}" format`, v)
			}
			key := args.add(keyValue[0])
			values := args.add(strings.Split(keyValue[1], ","))
			return fmt.Sprintf("db.metadata->'labels'->>%s = ANY(%s)", key, values), nil
		}},
		"drifted": boolField(func(_ *filterArgs, value bool) (string, error) {
			condition := "IS"
			if !value {
				condition = "IS NOT"
			}
			return fmt.Sprintf("(db.metadata->>'drifted')::boolean %s TRUE", condition), nil
		}),
	})
}

func parseAuditLogFilter(filter string) (*store.ListResourceFilter, error) {
	fields := map[string]filterField{}
	for _, name := range []string{"resource", "method", "user"} {
		fields[name] = equalField(fmt.Sprintf("payload->>'%s'", name))
	}
	fields["severity"] = enumField("payload->>'severity'", func(name string) (any, error) {
		if _, ok := v1pb.AuditLogSeverity_value[name]; !ok {
			return nil, errors.Errorf("unknown severity %q", name)
		}
		return name, nil
	}, false)
	fields["create_time"] = filterField{compare: func(args *filterArgs, variable string, op OperatorType, value any) (string, error) {
		v, err := filterString(variable, value)
		if err != nil {
			return "", err
		}
		if op != ComparatorTypeEqual && op != ComparatorTypeGreaterEqual && op != ComparatorTypeLessEqual {
			return "", invalidFilter("%q only supports =, >=, <= operators", variable)
		}
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return "", invalidFilter("invalid %s filter %q: %v", variable, v, err)
		}
		return fmt.Sprintf("created_at %s %s", op, args.add(parsed)), nil
	}}

	return translateFilter(filter, fields)
}
