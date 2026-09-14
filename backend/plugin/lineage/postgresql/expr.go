package postgresql

import (
	"strings"

	pgast "github.com/bytebase/omni/pg/ast"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// exprText returns the exact source slice for a node span, trimmed. omni's Loc is
// a byte range absolute into the parsed SQL and may include trailing whitespace,
// so trimming reproduces the legacy ANTLR
// tokens.GetTextFromInterval(...) output (source text, inner whitespace kept).
func (a *Analyzer) exprText(loc pgast.Loc) string {
	if loc.Start < 0 || loc.End < 0 || loc.End > len(a.sql) || loc.Start >= loc.End {
		return ""
	}
	return strings.TrimSpace(a.sql[loc.Start:loc.End])
}

// exprTextOf returns the reconstructed source text for a node.
func (a *Analyzer) exprTextOf(node pgast.Node) string {
	return a.exprText(pgast.NodeLoc(node))
}

// nodeTexts returns the source text of every node in a list.
func (a *Analyzer) nodeTexts(list *pgast.List) []string {
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list.Items))
	for _, node := range list.Items {
		out = append(out, a.exprTextOf(node))
	}
	return out
}

// columnRefFromFields converts a ColumnRef's field list into the scope column
// reference the algorithm layer expects. The trailing name is the column, the
// preceding name is the table qualifier, and any schema qualifier is discarded,
// matching the legacy extractColumnRef behavior. A trailing A_Star becomes the
// wildcard column.
func (*Analyzer) columnRefFromFields(fields *pgast.List) scope.ColumnRef {
	names := stringList(fields)

	ref := scope.ColumnRef{}
	if fieldsContainStar(fields) {
		ref.Column = wildcardColumn
		if len(names) > 0 {
			ref.Table = names[len(names)-1]
		}
		return ref
	}

	switch len(names) {
	case 0:
	case 1:
		ref.Column = names[0]
	default:
		ref.Table = names[len(names)-2]
		ref.Column = names[len(names)-1]
	}
	return ref
}

// extractColumnsFromNode collects every column reference in an expression,
// recursing through subqueries exactly as the legacy parse-tree walk did.
func (a *Analyzer) extractColumnsFromNode(node pgast.Node) []scope.ColumnRef {
	columns := make([]scope.ColumnRef, 0)
	if node == nil {
		return columns
	}
	pgast.Inspect(node, func(n pgast.Node) bool {
		cr, ok := n.(*pgast.ColumnRef)
		if !ok {
			return true
		}
		colRef := a.columnRefFromFields(cr.Fields)
		if colRef.Column != "" && colRef.Column != wildcardColumn {
			columns = append(columns, colRef)
		}
		return true
	})
	return columns
}

// isStarColumnRef reports whether a column reference is `*` or `table.*`.
func isStarColumnRef(cr *pgast.ColumnRef) bool {
	return cr != nil && fieldsContainStar(cr.Fields)
}

// isBareStar reports whether a column reference is exactly `*`.
func isBareStar(cr *pgast.ColumnRef) bool {
	return isStarColumnRef(cr) && len(stringList(cr.Fields)) == 0
}

func fieldsContainStar(fields *pgast.List) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.Items {
		if _, ok := field.(*pgast.A_Star); ok {
			return true
		}
	}
	return false
}

// stringList extracts the identifier values from a list of String nodes.
func stringList(list *pgast.List) []string {
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		if str, ok := item.(*pgast.String); ok {
			out = append(out, str.Str)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Structural expression classification (PG-FU-1)
//
// The governing (top-level) node decides everything. PostgreSQL does not
// materialise parentheses as nodes, so an operation can never be wrapped in a
// non-operation node and the top-level switch is decisive. Classification is a
// pure function of the AST; no source text is scanned (see
// plan/postgresql_expression_transformation_plan.md).
// ---------------------------------------------------------------------------

// isExpressionDerived reports whether a target expression derives from its
// sources via an operation, i.e. its governing node is an operation rather than
// a leaf (column, constant, parameter). It is an explicit allow-list: unknown,
// source-less nodes such as CURRENT_DATE stay non-derived so the wildcard
// fallback cannot fabricate a lineage edge for them.
func isExpressionDerived(node pgast.Node) bool {
	switch node.(type) {
	case *pgast.FuncCall,
		*pgast.CaseExpr,
		*pgast.CoalesceExpr,
		*pgast.MinMaxExpr,
		*pgast.NullIfExpr,
		*pgast.TypeCast,
		*pgast.SubLink,
		*pgast.A_Expr,
		*pgast.BoolExpr,
		*pgast.NullTest,
		*pgast.BooleanTest,
		*pgast.A_ArrayExpr,
		*pgast.RowExpr,
		*pgast.A_Indirection,
		*pgast.GroupingFunc,
		*pgast.CollateClause,
		*pgast.NamedArgExpr:
		return true
	default:
		return false
	}
}

// isTableWideExpression reports whether a source-less expression may legitimately
// depend on the whole relation, which is what licenses the `table.*` fallback.
// Only function calls qualify (e.g. COUNT(*), now()); a cast of a constant or an
// array/row of constants has no source table.
func isTableWideExpression(node pgast.Node) bool {
	_, ok := node.(*pgast.FuncCall)
	return ok
}

// classifyExpression maps an expression's governing node to its transformation.
// ok is false only for nil input; callers gate it on isExpressionDerived.
func (a *Analyzer) classifyExpression(node pgast.Node) (model.Transformation, bool) {
	if node == nil {
		return model.Transformation{}, false
	}

	exprText := a.exprTextOf(node)
	switch n := node.(type) {
	case *pgast.FuncCall:
		return a.classifyFuncCall(n, exprText), true
	case *pgast.CoalesceExpr:
		return model.NewFunctionTransformation("COALESCE", exprText, a.nodeTexts(n.Args)), true
	case *pgast.MinMaxExpr:
		name := "LEAST"
		if n.Op == pgast.IS_GREATEST {
			name = "GREATEST"
		}
		return model.NewFunctionTransformation(name, exprText, a.nodeTexts(n.Args)), true
	case *pgast.NullIfExpr:
		return model.NewFunctionTransformation("NULLIF", exprText, a.nodeTexts(n.Args)), true
	case *pgast.TypeCast:
		var args []string
		if n.Arg != nil {
			args = []string{a.exprTextOf(n.Arg)}
		}
		return model.NewFunctionTransformation("CAST", exprText, args), true
	case *pgast.CaseExpr:
		return model.NewCaseTransformation(exprText), true
	case *pgast.A_Expr:
		// NULLIF is an A_Expr of kind AEXPR_NULLIF, not a NullIfExpr node, but the
		// confirmed decision classifies it as a named FUNCTION alongside COALESCE.
		if n.Kind == pgast.AEXPR_NULLIF {
			var args []string
			if n.Lexpr != nil {
				args = append(args, a.exprTextOf(n.Lexpr))
			}
			if n.Rexpr != nil {
				args = append(args, a.exprTextOf(n.Rexpr))
			}
			return model.NewFunctionTransformation("NULLIF", exprText, args), true
		}
		return model.NewOperatorTransformation(operatorToken(n), exprText), true
	case *pgast.BoolExpr:
		return model.NewOperatorTransformation(boolOpToken(n.Boolop), exprText), true
	case *pgast.NullTest:
		return model.NewOperatorTransformation(nullTestToken(n.Nulltesttype), exprText), true
	case *pgast.BooleanTest:
		return model.NewOperatorTransformation(booleanTestToken(n.Booltesttype), exprText), true
	default:
		// SubLink (scalar subquery), A_Indirection, A_ArrayExpr, RowExpr,
		// GroupingFunc, CollateClause, NamedArgExpr.
		return model.NewProjectTransformation(exprText), true
	}
}

// classifyFuncCall classifies a function call. A window clause wins over the
// aggregate name set: a windowed aggregate is a window, not a group-by aggregate.
func (a *Analyzer) classifyFuncCall(fc *pgast.FuncCall, exprText string) model.Transformation {
	name := funcName(fc)

	if fc.Over != nil {
		partitionBy, orderBy := a.windowClauses(fc.Over)
		return model.NewWindowTransformation(strings.ToUpper(name), exprText, partitionBy, orderBy)
	}

	upperName := strings.ToUpper(name)
	if aggregateFunctions[upperName] {
		return model.NewAggregateTransformation(upperName, exprText, nil)
	}

	return model.NewFunctionTransformation(name, exprText, a.funcArgTexts(fc))
}

// funcName returns the unqualified function name (the last Funcname segment), so
// pg_catalog.count(*) reports COUNT.
func funcName(fc *pgast.FuncCall) string {
	names := stringList(fc.Funcname)
	if len(names) == 0 {
		return ""
	}
	return names[len(names)-1]
}

// funcArgTexts returns the source text of each function argument. A star argument
// (COUNT(*)) is reported as "*".
func (a *Analyzer) funcArgTexts(fc *pgast.FuncCall) []string {
	args := make([]string, 0)
	if fc.Args != nil {
		for _, arg := range fc.Args.Items {
			args = append(args, a.exprTextOf(arg))
		}
	}
	if fc.AggStar {
		args = append(args, wildcardColumn)
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

// windowClauses extracts PARTITION BY and ORDER BY expressions from a window
// definition. Sort direction is deliberately dropped, matching the MySQL
// analyzer's extractWindowClauses.
func (a *Analyzer) windowClauses(over pgast.Node) (partitionBy, orderBy []string) {
	windowDef, ok := over.(*pgast.WindowDef)
	if !ok || windowDef == nil {
		return nil, nil
	}
	if windowDef.PartitionClause != nil {
		for _, expr := range windowDef.PartitionClause.Items {
			partitionBy = append(partitionBy, a.exprTextOf(expr))
		}
	}
	if windowDef.OrderClause != nil {
		for _, item := range windowDef.OrderClause.Items {
			if sortBy, ok := item.(*pgast.SortBy); ok {
				orderBy = append(orderBy, a.exprTextOf(sortBy.Node))
			}
		}
	}
	return partitionBy, orderBy
}

// operatorToken returns the source operator ("+", "||", "->>", "=", …) for a
// normal operator, or the keyword form for the special A_Expr kinds.
func operatorToken(expr *pgast.A_Expr) string {
	name := ""
	if names := stringList(expr.Name); len(names) > 0 {
		name = names[len(names)-1]
	}

	switch expr.Kind {
	case pgast.AEXPR_OP, pgast.AEXPR_OP_ANY, pgast.AEXPR_OP_ALL:
		if name != "" {
			return name
		}
		return "OP"
	case pgast.AEXPR_DISTINCT:
		return "IS DISTINCT FROM"
	case pgast.AEXPR_NOT_DISTINCT:
		return "IS NOT DISTINCT FROM"
	case pgast.AEXPR_NULLIF:
		return "NULLIF"
	case pgast.AEXPR_IN:
		if name == "<>" {
			return "NOT IN"
		}
		return "IN"
	case pgast.AEXPR_LIKE:
		if name == "!~~" {
			return "NOT LIKE"
		}
		return "LIKE"
	case pgast.AEXPR_ILIKE:
		if name == "!~~*" {
			return "NOT ILIKE"
		}
		return "ILIKE"
	case pgast.AEXPR_SIMILAR:
		if name == "!~" {
			return "NOT SIMILAR TO"
		}
		return "SIMILAR TO"
	case pgast.AEXPR_BETWEEN:
		return "BETWEEN"
	case pgast.AEXPR_NOT_BETWEEN:
		return "NOT BETWEEN"
	case pgast.AEXPR_BETWEEN_SYM:
		return "BETWEEN SYMMETRIC"
	case pgast.AEXPR_NOT_BETWEEN_SYM:
		return "NOT BETWEEN SYMMETRIC"
	case pgast.AEXPR_OVERLAPS:
		return "OVERLAPS"
	default:
		if name != "" {
			return name
		}
		return "OP"
	}
}

func boolOpToken(op pgast.BoolExprType) string {
	switch op {
	case pgast.AND_EXPR:
		return "AND"
	case pgast.OR_EXPR:
		return "OR"
	case pgast.NOT_EXPR:
		return "NOT"
	default:
		return "BOOL"
	}
}

func nullTestToken(testType pgast.NullTestType) string {
	switch testType {
	case pgast.IS_NULL:
		return "IS NULL"
	case pgast.IS_NOT_NULL:
		return "IS NOT NULL"
	default:
		return "IS NULL"
	}
}

func booleanTestToken(testType pgast.BoolTestType) string {
	switch testType {
	case pgast.IS_TRUE:
		return "IS TRUE"
	case pgast.IS_NOT_TRUE:
		return "IS NOT TRUE"
	case pgast.IS_FALSE:
		return "IS FALSE"
	case pgast.IS_NOT_FALSE:
		return "IS NOT FALSE"
	case pgast.IS_UNKNOWN:
		return "IS UNKNOWN"
	case pgast.IS_NOT_UNKNOWN:
		return "IS NOT UNKNOWN"
	default:
		return "IS TRUE"
	}
}

// inferColumnAlias infers a column alias from an expression text.
// For qualified column references (e.g., table.column), returns just the column name.
func (*Analyzer) inferColumnAlias(exprText string) string {
	if strings.Contains(exprText, ".") && !strings.Contains(exprText, "(") {
		parts := strings.Split(exprText, ".")
		return parts[len(parts)-1]
	}
	return exprText
}
