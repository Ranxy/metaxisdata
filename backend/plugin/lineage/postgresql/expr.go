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

// isExpressionDerivedText checks whether an expression text involves a
// transformation operation. It stays text-based so the emitted Transformation
// content matches the legacy analyzer byte for byte; see PG-FU-1 in
// plan/postgresql_omni_parser_migration_plan.md for the structured upgrade.
func (*Analyzer) isExpressionDerivedText(text string) bool {
	upperText := strings.ToUpper(text)

	if strings.Contains(text, "(") {
		return true
	}
	for _, op := range arithmeticOperators {
		if strings.Contains(text, op) {
			return true
		}
	}
	return strings.Contains(upperText, "CASE") || strings.Contains(upperText, "WHEN")
}

// analyzeExpressionOperator analyzes an expression to determine its
// transformation type. Detection is text-based on purpose (legacy parity); the
// omni AST carries structured FuncCall/CaseExpr/A_Expr that PG-FU-1 will use.
func (a *Analyzer) analyzeExpressionOperator(node pgast.Node) []model.Transformation {
	if node == nil {
		return nil
	}

	exprText := a.exprTextOf(node)
	upperText := strings.ToUpper(exprText)

	if fnName := a.findFunctionInExpression(upperText, aggregateFunctions); fnName != "" {
		return []model.Transformation{model.NewAggregateTransformation(fnName, exprText, nil)}
	}

	if fnName := a.findFunctionInExpression(upperText, windowFunctions); fnName != "" {
		if strings.Contains(upperText, "OVER") {
			// TODO(PG-FU-1): extract WindowDef.PartitionClause / .OrderClause from FuncCall.Over.
			return []model.Transformation{model.NewWindowTransformation(fnName, exprText, nil, nil)}
		}
	}

	if a.isCaseExpression(upperText) {
		return []model.Transformation{model.NewCaseTransformation(exprText)}
	}

	if strings.Contains(exprText, "(") {
		return []model.Transformation{model.NewFunctionTransformation("", exprText, nil)}
	}

	if a.containsArithmeticOperator(exprText) {
		return []model.Transformation{model.NewOperatorTransformation("ARITHMETIC", exprText)}
	}

	return []model.Transformation{model.NewProjectTransformation(exprText)}
}

// findFunctionInExpression checks if any function from the list appears in the expression.
func (*Analyzer) findFunctionInExpression(upperText string, functions []string) string {
	for _, fn := range functions {
		if strings.Contains(upperText, fn+"(") {
			return fn
		}
	}
	return ""
}

// isCaseExpression checks if the expression is a CASE expression.
func (*Analyzer) isCaseExpression(upperText string) bool {
	return strings.Contains(upperText, "CASE") && strings.Contains(upperText, "WHEN")
}

// containsArithmeticOperator checks if the expression contains arithmetic operators.
func (*Analyzer) containsArithmeticOperator(text string) bool {
	for _, op := range arithmeticOperators {
		if strings.Contains(text, op) {
			return true
		}
	}
	return false
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
