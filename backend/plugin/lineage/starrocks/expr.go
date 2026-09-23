package starrocks

import (
	"strings"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/starrocks/ast"
)

// ---------------------------------------------------------------------------
// Identifier helpers
// ---------------------------------------------------------------------------

// columnRefFromObjectName maps an omni ObjectName onto a scope column
// reference. StarRocks addresses objects as [catalog.]database.table.column;
// the metadata registry has no catalog dimension, so a catalog qualifier is
// dropped.
func columnRefFromObjectName(name *nodes.ObjectName) (scope.ColumnRef, bool) {
	if name == nil || len(name.Parts) == 0 {
		return scope.ColumnRef{}, false
	}
	parts := name.Parts
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			return scope.ColumnRef{}, false
		}
		return scope.ColumnRef{Column: parts[0]}, true
	case 2:
		return scope.ColumnRef{Table: parts[0], Column: parts[1]}, true
	default:
		n := len(parts)
		return scope.ColumnRef{Schema: parts[n-3], Table: parts[n-2], Column: parts[n-1]}, true
	}
}

// tableRefFromObjectName maps an omni ObjectName onto a table reference's
// database qualifier and table name. As in columnRefFromObjectName, a catalog
// qualifier is dropped.
func tableRefFromObjectName(name *nodes.ObjectName) (schema, table string, ok bool) {
	if name == nil || len(name.Parts) == 0 {
		return "", "", false
	}
	parts := name.Parts
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			return "", "", false
		}
		return "", parts[0], true
	default:
		n := len(parts)
		return parts[n-2], parts[n-1], true
	}
}

// normalizeIdentifier strips surrounding backticks or double quotes.
func normalizeIdentifier(text string) string {
	text = strings.TrimSpace(text)
	if len(text) < 2 {
		return text
	}
	quote := text[0]
	if (quote != '`' && quote != '"') || text[len(text)-1] != quote {
		return text
	}
	inner := text[1 : len(text)-1]
	escapedQuote := strings.Repeat(string(quote), 2)
	return strings.ReplaceAll(inner, escapedQuote, string(quote))
}

// splitQualifiedIdentifier splits a dotted identifier into normalized parts.
func splitQualifiedIdentifier(fullText string) []string {
	parts := strings.Split(fullText, ".")
	for i, part := range parts {
		parts[i] = normalizeIdentifier(part)
	}
	return parts
}

// inferColumnAlias infers an alias from an expression text.
func inferColumnAlias(exprText string) string {
	if strings.Contains(exprText, ".") && !strings.Contains(exprText, "(") {
		parts := splitQualifiedIdentifier(exprText)
		return parts[len(parts)-1]
	}
	if !strings.ContainsAny(exprText, "() +-*/%<>=,!?") {
		return normalizeIdentifier(exprText)
	}
	return exprText
}

// normalizeExpressionText collapses whitespace runs in expression text. Spaces
// inside a string literal are content, so they are only collapsed, never
// removed.
func normalizeExpressionText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// ---------------------------------------------------------------------------
// Expression analysis
// ---------------------------------------------------------------------------

// collectColumns recursively collects column references from an expression.
func collectColumns(expr nodes.Node) []scope.ColumnRef {
	columns := make([]scope.ColumnRef, 0)
	if expr == nil {
		return columns
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		cr, ok := n.(*nodes.ColumnRef)
		if !ok {
			return true
		}
		if ref, ok := columnRefFromObjectName(cr.Name); ok {
			columns = append(columns, ref)
		}
		return true
	})
	return columns
}

// isPlainColumnRef reports whether an expression is a bare (possibly
// parenthesized) column reference and therefore a direct projection rather than
// a transformation. Deciding it from the AST keeps a quoted identifier that
// contains an operator character — `created-at` — from being mistaken for an
// expression.
func isPlainColumnRef(expr nodes.Node) bool {
	switch x := expr.(type) {
	case *nodes.ColumnRef:
		return true
	case *nodes.ParenExpr:
		return isPlainColumnRef(x.Expr)
	default:
		return false
	}
}

// containsAggregateCall reports whether an expression contains an aggregate
// function call and therefore depends on the rows of its FROM relations even
// when it names no column, as COUNT(*) does. A constant or a NOW() has no such
// dependency and must not invent one.
func containsAggregateCall(expr nodes.Node) bool {
	if expr == nil {
		return false
	}
	found := false
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if found {
			return false
		}
		if fc, ok := n.(*nodes.FuncCallExpr); ok && aggregateFunctions[strings.ToUpper(funcCallName(fc))] {
			found = true
			return false
		}
		return true
	})
	return found
}

// groupByKeys renders the GROUP BY items of a query specification in source
// order, the way this analyzer renders PARTITION BY and ORDER BY. A positional
// key stays "1" and an alias stays the alias, because that is what the query
// wrote. GROUP BY ... WITH ROLLUP has no representation here; the flag is not
// part of a key list.
func (a *Analyzer) groupByKeys(items []nodes.Node) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, a.exprTextOf(item))
	}
	return keys
}

// containsGroupAggregate reports whether the expression contains an aggregate
// call that GROUP BY governs, i.e. one without an OVER clause. A windowed
// aggregate follows its OVER clause instead. Traversal stops at a subquery: an
// aggregate inside one is grouped by that query's own GROUP BY, never by the
// enclosing statement's.
func containsGroupAggregate(expr nodes.Node) bool {
	if expr == nil {
		return false
	}
	found := false
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*nodes.SubqueryExpr); ok {
			return false
		}
		if fc, ok := n.(*nodes.FuncCallExpr); ok && fc.Over == nil && aggregateFunctions[strings.ToUpper(funcCallName(fc))] {
			found = true
			return false
		}
		return true
	})
	return found
}

// analyzeExpressionOperator identifies the operation kind of an expression and
// returns its transformation metadata. What the expression *is* decides it: a
// CASE or an operator at the outermost node first, then the first function call
// it contains — checked for OVER before the aggregate name set, because a call
// with one is a window function whatever its name. Classifying by the first call
// alone reported `SUM(x) + 1` as an aggregate and `SUM(x) OVER (…)` as an
// aggregate rather than a window, which is the order the MySQL analyzer uses.
func (a *Analyzer) analyzeExpressionOperator(expr nodes.Node) []model.Transformation {
	if expr == nil {
		return nil
	}
	exprText := a.exprTextOf(expr)

	top := unwrapParens(expr)
	if _, ok := top.(*nodes.CaseExpr); ok {
		return []model.Transformation{createCaseOperatorInfo(exprText)}
	}
	if opInfo, ok := a.detectInfixOperator(top); ok {
		return []model.Transformation{opInfo}
	}
	if fc := firstFuncCall(expr); fc != nil {
		name := strings.ToUpper(funcCallName(fc))
		if fc.Over != nil {
			partitionBy, orderBy := a.extractWindowClauses(fc)
			return []model.Transformation{createWindowOperatorInfo(name, exprText, partitionBy, orderBy)}
		}
		if aggregateFunctions[name] {
			return []model.Transformation{createAggregateOperatorInfo(name, exprText, nil)}
		}
		if funcCallName(fc) == "" {
			return []model.Transformation{model.NewProjectTransformation(exprText)}
		}
		args := make([]string, 0, len(fc.Args))
		for _, arg := range fc.Args {
			args = append(args, a.exprTextOf(arg))
		}
		return []model.Transformation{createFunctionOperatorInfo(funcCallName(fc), exprText, args)}
	}
	return []model.Transformation{model.NewProjectTransformation(exprText)}
}

// unwrapParens removes redundant parentheses around an expression.
func unwrapParens(expr nodes.Node) nodes.Node {
	for {
		p, ok := expr.(*nodes.ParenExpr)
		if !ok || p.Expr == nil {
			return expr
		}
		expr = p.Expr
	}
}

// detectInfixOperator reports the operator transformation of the outermost
// operator expression, read from the AST rather than from the text: an operator
// character inside a string literal or a quoted identifier must not decide it.
func (a *Analyzer) detectInfixOperator(expr nodes.Node) (model.Transformation, bool) {
	switch x := expr.(type) {
	case *nodes.BinaryExpr:
		if name, ok := binaryOperatorName(x.Op); ok {
			return createOperatorExprInfo(name, a.exprTextOf(x)), true
		}
	case *nodes.UnaryExpr:
		if name, ok := unaryOperatorName(x.Op); ok {
			return createOperatorExprInfo(name, a.exprTextOf(x)), true
		}
	case *nodes.BetweenExpr:
		return createOperatorExprInfo("BETWEEN", a.exprTextOf(x)), true
	case *nodes.InExpr:
		return createOperatorExprInfo("IN", a.exprTextOf(x)), true
	case *nodes.LikeExpr:
		return createOperatorExprInfo("LIKE", a.exprTextOf(x)), true
	default:
		return model.Transformation{}, false
	}
	return model.Transformation{}, false
}

// binaryOperatorName maps a StarRocks binary operator to the recorded operator
// type: the same vocabulary the other analyzers record, so the engines agree on
// what an operator edge says.
func binaryOperatorName(op nodes.BinaryOp) (string, bool) {
	switch op {
	case nodes.BinAdd:
		return "ADDITION", true
	case nodes.BinSub:
		return "SUBTRACTION", true
	case nodes.BinMul:
		return "MULTIPLICATION", true
	case nodes.BinDiv:
		return "DIVISION", true
	case nodes.BinMod:
		return "MODULO", true
	case nodes.BinIntDiv:
		return "INTEGER_DIVISION", true
	case nodes.BinEq:
		return "EQUALS", true
	case nodes.BinNe:
		return "NOT_EQUALS", true
	case nodes.BinLt:
		return "LESS_THAN", true
	case nodes.BinGt:
		return "GREATER_THAN", true
	case nodes.BinLe:
		return "LESS_OR_EQUAL", true
	case nodes.BinGe:
		return "GREATER_OR_EQUAL", true
	case nodes.BinNullSafeEq:
		return "NULL_SAFE_EQUALS", true
	case nodes.BinAnd:
		return "LOGICAL_AND", true
	case nodes.BinOr:
		return "LOGICAL_OR", true
	case nodes.BinXor:
		return "LOGICAL_XOR", true
	case nodes.BinBitAnd:
		return "BIT_AND", true
	case nodes.BinBitOr:
		return "BIT_OR", true
	case nodes.BinBitXor:
		return "BIT_XOR", true
	case nodes.BinShiftLeft:
		return "SHIFT_LEFT", true
	case nodes.BinShiftRight:
		return "SHIFT_RIGHT", true
	default:
		return "", false
	}
}

// unaryOperatorName maps a StarRocks unary operator to the recorded operator
// type.
func unaryOperatorName(op nodes.UnaryOp) (string, bool) {
	switch op {
	case nodes.UnaryMinus:
		return "NEGATION", true
	case nodes.UnaryPlus:
		return "UNARY_PLUS", true
	case nodes.UnaryNot:
		return "NOT", true
	case nodes.UnaryBitNot:
		return "BIT_NOT", true
	case nodes.UnaryBinary:
		return "BINARY", true
	default:
		return "", false
	}
}

// firstFuncCall returns the first function call in pre-order.
func firstFuncCall(expr nodes.Node) *nodes.FuncCallExpr {
	var found *nodes.FuncCallExpr
	if expr == nil {
		return nil
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if found != nil {
			return false
		}
		if fc, ok := n.(*nodes.FuncCallExpr); ok {
			found = fc
			return false
		}
		return true
	})
	return found
}

// funcCallName returns a function call's bare name, ignoring any qualifier.
func funcCallName(fc *nodes.FuncCallExpr) string {
	if fc == nil || fc.Name == nil || len(fc.Name.Parts) == 0 {
		return ""
	}
	return fc.Name.Parts[len(fc.Name.Parts)-1]
}

// Common aggregate functions.
var aggregateFunctions = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MAX": true, "MIN": true,
	"GROUP_CONCAT": true, "STD": true, "STDDEV": true, "STDDEV_POP": true,
	"STDDEV_SAMP": true, "VAR_POP": true, "VAR_SAMP": true, "VARIANCE": true,
}

// extractWindowClauses extracts PARTITION BY and ORDER BY from a window call.
func (a *Analyzer) extractWindowClauses(fc *nodes.FuncCallExpr) (partitionBy []string, orderBy []string) {
	if fc == nil || fc.Over == nil {
		return nil, nil
	}
	for _, expr := range fc.Over.PartitionBy {
		partitionBy = append(partitionBy, a.exprTextOf(expr))
	}
	for _, item := range fc.Over.OrderBy {
		if item != nil && item.Expr != nil {
			orderBy = append(orderBy, a.exprTextOf(item.Expr))
		}
	}
	return partitionBy, orderBy
}

// ---------------------------------------------------------------------------
// Transformation builders
// ---------------------------------------------------------------------------

func createFunctionOperatorInfo(functionName, exprText string, args []string) model.Transformation {
	return model.NewFunctionTransformation(functionName, exprText, args)
}

func createAggregateOperatorInfo(functionName, exprText string, groupKeys []string) model.Transformation {
	return model.NewAggregateTransformation(functionName, exprText, groupKeys)
}

func createOperatorExprInfo(opType, exprText string) model.Transformation {
	return model.NewOperatorTransformation(opType, exprText)
}

func createCaseOperatorInfo(exprText string) model.Transformation {
	return model.NewCaseTransformation(exprText)
}

func createWindowOperatorInfo(functionName, exprText string, partitionBy, orderBy []string) model.Transformation {
	return model.NewWindowTransformation(functionName, exprText, partitionBy, orderBy)
}
