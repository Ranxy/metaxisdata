package mssql

import (
	"reflect"
	"strings"

	nodes "github.com/bytebase/omni/mssql/ast"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// ---------------------------------------------------------------------------
// Names
// ---------------------------------------------------------------------------

// nodeOrNil returns nil for a node that is nil, including a *typed* nil pointer. An
// interface holding a nil *nodes.List is not equal to nil, and omni's walker
// dereferences it rather than stopping, so every entry point that can be handed an
// optional AST field normalizes it here first.
func nodeOrNil(node nodes.Node) nodes.Node {
	if node == nil {
		return nil
	}
	value := reflect.ValueOf(node)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil
		}
	default:
	}
	return node
}

// columnSpelling returns the name a relation gives a column.
//
// SQL Server compares identifiers case-insensitively under its default collation, so
// `SELECT id FROM Users` reads the column the registry knows as `ID`. Recording the
// statement's spelling would name a column that object does not have, and the graph would
// show the edge's two ends as belonging to different fields. The registered spelling wins
// for a relation whose columns are known; an exact match is preferred, because a
// case-sensitive collation can hold both `id` and `ID` as two columns, and an unknown
// relation keeps the statement's own spelling.
func columnSpelling(relation *scope.TableRef, column string) string {
	if relation == nil || column == "" {
		return column
	}
	return spellingIn(relation.ColumnNames(), column)
}

// spellingIn returns the spelling a column list gives a name, preferring an exact match and
// falling back to a case-insensitive one. An exact match wins because a case-sensitive
// collation can hold both `id` and `ID` as two columns.
func spellingIn(names []string, column string) string {
	for _, name := range names {
		if name == column {
			return name
		}
	}
	for _, name := range names {
		if strings.EqualFold(name, column) {
			return name
		}
	}
	return column
}

// canonicalResolution returns a resolved column under the spelling its relation is
// registered with, so that every edge this analyzer builds names a column the registry's
// object actually has.
func (*Analyzer) canonicalResolution(res scope.ResolvedColumn) scope.ResolvedColumn {
	res.Ref.Column = columnSpelling(res.Relation, res.Ref.Column)
	return res
}

// canonicalPredicateRef spells a predicate's column the way its relation is registered,
// as canonicalResolution does for a value edge. The influence machinery resolves the
// reference itself at emission time, so the spelling has to be settled before it is handed
// over; a reference that resolves to nothing — a select-list alias in HAVING, for one — is
// passed through untouched and left to that machinery.
func (*Analyzer) canonicalPredicateRef(ref scope.ColumnRef, sp *scope.Scope) scope.ColumnRef {
	if ref.Column == "" || ref.Resolved || sp == nil {
		return ref
	}
	resolutions, err := sp.ResolveColumnRefs(ref)
	if err != nil {
		return ref
	}
	for _, res := range resolutions {
		if canonical := columnSpelling(res.Relation, res.Ref.Column); canonical != ref.Column {
			ref.Column = canonical
			return ref
		}
	}
	return ref
}

// identifierOf maps an omni column reference onto the scope's. T-SQL addresses
// an object as [server.][database.][schema.]table.column; the metadata registry
// has no server dimension, so a linked-server qualifier is dropped.
func identifierOf(ref *nodes.ColumnRef) scope.ColumnRef {
	if ref == nil || ref.Column == "" {
		return scope.ColumnRef{}
	}
	return scope.ColumnRef{Schema: ref.Schema, Table: ref.Table, Column: ref.Column}
}

// columnList reads a declared column list — a CTE, a view, an INSERT, a derived
// table's alias. Every one of them is a List of String nodes whose Loc is not
// set, so the name is read from the node and never from the source.
func columnList(list *nodes.List) []string {
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		if name, ok := item.(*nodes.String); ok && name.Str != "" {
			out = append(out, name.Str)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Expression text
// ---------------------------------------------------------------------------

// exprTextOf renders an expression the way the statement wrote it.
//
// Most T-SQL nodes carry exact byte offsets, so the text is the source slice with
// whitespace runs collapsed — spaces inside a string literal are content, so they
// are never removed. A few nodes carry no end offset at all (a scalar subquery,
// an aliased table reference, every declared name list), and for those the text
// is rebuilt from the node they wrap.
func (a *Analyzer) exprTextOf(node nodes.Node) string {
	node = nodeOrNil(node)
	if node == nil {
		return ""
	}
	if loc := nodes.NodeLoc(node); loc.IsValid() && loc.Start < loc.End && loc.End <= len(a.sql) {
		return normalizeExpressionText(a.sql[loc.Start:loc.End])
	}
	return a.rebuiltText(node)
}

// rebuiltText is exprTextOf for a node whose own Loc has no end offset. Only the
// shapes whose text a consumer reads are rebuilt; anything else contributes no
// text rather than a wrong one.
func (a *Analyzer) rebuiltText(node nodes.Node) string {
	switch n := node.(type) {
	case *nodes.SubqueryExpr:
		if inner := a.exprTextOf(n.Query); inner != "" {
			return "(" + inner + ")"
		}
	case *nodes.SubqueryComparisonExpr:
		left, inner := a.exprTextOf(n.Left), a.exprTextOf(n.Subquery)
		if left != "" && inner != "" {
			return left + " " + n.Quantifier + " (" + inner + ")"
		}
	case *nodes.ParenExpr:
		if inner := a.exprTextOf(n.Expr); inner != "" {
			return "(" + inner + ")"
		}
	default:
	}
	return ""
}

// normalizeExpressionText collapses whitespace runs in expression text.
func normalizeExpressionText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// inferredColumnAlias names an unaliased select item the way SQL Server reports
// it: a column reference contributes its own name, a scalar subquery the name of
// the column it selects, and anything else its source text.
func (*Analyzer) inferredColumnAlias(expr nodes.Node, exprText string) string {
	switch x := unwrapParens(expr).(type) {
	case *nodes.ColumnRef:
		return x.Column
	case *nodes.SubqueryExpr:
		if name := subqueryColumnName(x.Query); name != "" {
			return name
		}
	default:
	}
	if exprText == "" {
		return "column"
	}
	return exprText
}

// subqueryColumnName names an unaliased scalar subquery after the column it
// selects, which is what SQL Server's own naming comes closest to (it reports
// "(No column name)", a name no registry object can have).
func subqueryColumnName(query nodes.Node) string {
	selectStmt, ok := query.(*nodes.SelectStmt)
	if !ok || selectStmt == nil {
		return ""
	}
	if selectStmt.Op != nodes.SetOpNone {
		selectStmt = selectStmt.Larg
	}
	if selectStmt == nil || selectStmt.TargetList == nil || len(selectStmt.TargetList.Items) == 0 {
		return ""
	}
	target, ok := selectStmt.TargetList.Items[0].(*nodes.ResTarget)
	if !ok || target == nil {
		return ""
	}
	if target.Name != "" {
		return target.Name
	}
	if ref, ok := unwrapParens(target.Val).(*nodes.ColumnRef); ok {
		return ref.Column
	}
	return ""
}

// ---------------------------------------------------------------------------
// Columns of an expression
// ---------------------------------------------------------------------------

// extractColumnsFromNode collects the column references an expression depends on.
//
// A subquery is opaque here: its columns were resolved in its own scope and are
// returned by subquerySources, so they are not attributed to the relations of the
// enclosing query. omni's walker descends into every query body, so the boundary
// has to be declared explicitly — otherwise a correlated subquery would report the
// inner table's columns as if the outer statement named them.
func (a *Analyzer) extractColumnsFromNode(node nodes.Node, sp *scope.Scope) []scope.ColumnRef {
	columns := make([]scope.ColumnRef, 0)
	node = nodeOrNil(node)
	if node == nil {
		return columns
	}
	var subqueries []nodes.Node
	nodes.Inspect(node, func(n nodes.Node) bool {
		switch x := n.(type) {
		case *nodes.SubqueryExpr:
			subqueries = append(subqueries, x.Query)
			return false
		case *nodes.ExistsExpr:
			subqueries = append(subqueries, x.Subquery)
			return false
		case *nodes.SubqueryComparisonExpr:
			columns = append(columns, a.extractColumnsFromNode(x.Left, sp)...)
			subqueries = append(subqueries, x.Subquery)
			return false
		case *nodes.InExpr:
			columns = append(columns, a.extractColumnsFromNode(x.Expr, sp)...)
			columns = append(columns, a.extractColumnsFromNode(x.List, sp)...)
			subqueries = append(subqueries, x.Subquery)
			return false
		case *nodes.ColumnRef:
			if ref := identifierOf(x); ref.Column != "" && ref.Column != wildcardColumn {
				columns = append(columns, ref)
			}
			return false
		case *nodes.StarExpr:
			// A star inside an expression (`COUNT(*)`) names no column of its
			// own; the aggregate fallback in the caller is what turns it into a
			// dependency on the relation's rows.
			return false
		default:
		}
		return true
	})
	for _, query := range subqueries {
		columns = append(columns, a.subquerySources(query, sp)...)
	}
	return columns
}

// queryOf unwraps the SubqueryExpr an expression holds. An IN list and a comparison
// against ALL carry one, and analyzing the wrapper as a query expression would report
// the statement as unmodelled while its body is right there.
func queryOf(node nodes.Node) nodes.Node {
	if sub, ok := node.(*nodes.SubqueryExpr); ok && sub != nil {
		return nodeOrNil(sub.Query)
	}
	return node
}

// subquerySources analyzes a subquery in a scope of its own and returns the base
// columns its output columns came from, marked so the enclosing scope can use
// them without seeing the subquery's relations.
func (a *Analyzer) subquerySources(query nodes.Node, sp *scope.Scope) []scope.ColumnRef {
	query = queryOf(nodeOrNil(query))
	if query == nil {
		return nil
	}
	a.pushScope()
	a.processQueryNode(query)
	subScope := a.popScope()
	if subScope == nil {
		return nil
	}
	// The subquery's rows decide the value the enclosing expression reads, so the
	// predicates that shaped them belong to that expression's query.
	a.influences.Inherit(subScope, sp)

	var out []scope.ColumnRef
	for _, col := range algorithm.ResolveOutputColumns(subScope, subScope.GetOutputColumns()) {
		out = append(out, scope.Refs(col.Sources)...)
	}
	return out
}

// ---------------------------------------------------------------------------
// Classification
// ---------------------------------------------------------------------------

// isPlainColumnRef reports whether an expression is a bare (possibly
// parenthesized) column reference and therefore a direct projection rather than a
// transformation. Deciding it from the AST keeps a column whose name merely looks
// like an expression from being classified as one.
func isPlainColumnRef(expr nodes.Node) bool {
	ref, ok := unwrapParens(expr).(*nodes.ColumnRef)
	return ok && ref.Column != ""
}

// isExpressionDerived reports whether an expression derives from its sources via
// an operation, i.e. its governing node is an operation rather than a leaf. It is
// an explicit allow-list: a source-less leaf such as GETDATE() stays non-derived
// so no wildcard fallback can fabricate a dependency from it.
func isExpressionDerived(expr nodes.Node) bool {
	switch unwrapParens(expr).(type) {
	case *nodes.FuncCallExpr,
		*nodes.CaseExpr,
		*nodes.CoalesceExpr,
		*nodes.NullifExpr,
		*nodes.IifExpr,
		*nodes.CastExpr,
		*nodes.ConvertExpr,
		*nodes.TryCastExpr,
		*nodes.TryConvertExpr,
		*nodes.BinaryExpr,
		*nodes.UnaryExpr,
		*nodes.BetweenExpr,
		*nodes.InExpr,
		*nodes.LikeExpr,
		*nodes.IsExpr,
		*nodes.SubqueryExpr,
		*nodes.SubqueryComparisonExpr,
		*nodes.ExistsExpr,
		*nodes.CollateExpr,
		*nodes.AtTimeZoneExpr,
		*nodes.MethodCallExpr,
		*nodes.FullTextPredicate:
		return true
	default:
		return false
	}
}

// unwrapParens removes redundant parentheses around an expression. T-SQL
// materialises them as a node, unlike PostgreSQL, so the classification below
// would otherwise see every parenthesized operand as a PROJECT.
func unwrapParens(expr nodes.Node) nodes.Node {
	for {
		paren, ok := expr.(*nodes.ParenExpr)
		if !ok || paren.Expr == nil {
			return expr
		}
		expr = paren.Expr
	}
}

// classifyExpression describes how one expression produced its output column.
//
// The governing top-level node decides, rather than the first function call found
// in the tree: the MySQL family reads the latter and reports `SUM(x) + 1` as an
// aggregate, which is why a wrapper's own operation has to win here. A windowed
// call is a WINDOW before it is an aggregate whatever the function is called; a
// group aggregate is an AGGREGATE; a set-returning subquery is a PROJECT.
func (a *Analyzer) classifyExpression(expr nodes.Node) model.Transformation {
	exprText := a.exprTextOf(expr)
	switch x := unwrapParens(expr).(type) {
	case *nodes.CaseExpr:
		return model.NewCaseTransformation(exprText)
	case *nodes.IifExpr:
		return model.NewFunctionTransformation("IIF", exprText, a.argumentTexts(x.Condition, x.TrueVal, x.FalseVal))
	case *nodes.CoalesceExpr:
		return model.NewFunctionTransformation("COALESCE", exprText, a.listTexts(x.Args))
	case *nodes.NullifExpr:
		return model.NewFunctionTransformation("NULLIF", exprText, a.argumentTexts(x.Left, x.Right))
	case *nodes.CastExpr:
		return model.NewFunctionTransformation("CAST", exprText, a.argumentTexts(x.Expr))
	case *nodes.TryCastExpr:
		return model.NewFunctionTransformation("TRY_CAST", exprText, a.argumentTexts(x.Expr))
	case *nodes.ConvertExpr:
		return model.NewFunctionTransformation("CONVERT", exprText, a.argumentTexts(x.Expr, x.Style))
	case *nodes.TryConvertExpr:
		return model.NewFunctionTransformation("TRY_CONVERT", exprText, a.argumentTexts(x.Expr, x.Style))
	case *nodes.FuncCallExpr:
		return a.classifyFuncCall(x, exprText)
	case *nodes.BinaryExpr:
		if name, ok := binaryOperatorName(x.Op); ok {
			return model.NewOperatorTransformation(name, exprText)
		}
	case *nodes.UnaryExpr:
		if name, ok := unaryOperatorName(x.Op); ok {
			return model.NewOperatorTransformation(name, exprText)
		}
	case *nodes.BetweenExpr:
		return model.NewOperatorTransformation("BETWEEN", exprText)
	case *nodes.InExpr:
		return model.NewOperatorTransformation("IN", exprText)
	case *nodes.LikeExpr:
		return model.NewOperatorTransformation("LIKE", exprText)
	case *nodes.IsExpr:
		return model.NewOperatorTransformation(isTestName(x.TestType), exprText)
	case *nodes.AtTimeZoneExpr:
		return model.NewOperatorTransformation("AT TIME ZONE", exprText)
	case *nodes.SubqueryExpr, *nodes.SubqueryComparisonExpr, *nodes.ExistsExpr, *nodes.FullTextPredicate:
		return model.NewProjectTransformation(exprText)
	default:
	}
	return model.NewProjectTransformation(exprText)
}

// classifyFuncCall classifies a function call: a windowed call is a WINDOW, an
// aggregate is an AGGREGATE, anything else is a FUNCTION.
func (a *Analyzer) classifyFuncCall(call *nodes.FuncCallExpr, exprText string) model.Transformation {
	name := strings.ToUpper(funcCallName(call))
	if call.Over != nil {
		partitionBy, orderBy := a.windowKeys(call.Over)
		return model.NewWindowTransformation(name, exprText, partitionBy, orderBy)
	}
	if call.Star || aggregateFunctions[name] {
		return model.NewAggregateTransformation(name, exprText, nil)
	}
	return model.NewFunctionTransformation(funcCallName(call), exprText, a.listTexts(call.Args))
}

// funcCallName returns a function call's bare name, ignoring any qualifier.
func funcCallName(call *nodes.FuncCallExpr) string {
	if call == nil || call.Name == nil {
		return ""
	}
	return call.Name.Object
}

// argumentTexts renders each non-nil expression of a call's argument list.
func (a *Analyzer) argumentTexts(exprs ...nodes.Node) []string {
	out := make([]string, 0, len(exprs))
	for _, expr := range exprs {
		if nodeOrNil(expr) == nil {
			continue
		}
		out = append(out, a.exprTextOf(expr))
	}
	return out
}

// listTexts renders every item of a node list.
func (a *Analyzer) listTexts(list *nodes.List) []string {
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		if nodeOrNil(item) == nil {
			continue
		}
		out = append(out, a.exprTextOf(item))
	}
	return out
}

// windowKeys renders the PARTITION BY and ORDER BY keys of an OVER clause.
// `OVER w` names a window defined in the statement's WINDOW clause, whose own
// keys are what decide the window, so they are read too.
func (a *Analyzer) windowKeys(over *nodes.OverClause) (partitionBy, orderBy []string) {
	if over == nil {
		return nil, nil
	}
	partitionBy = a.listTexts(over.PartitionBy)
	orderBy = a.orderByTexts(over.OrderBy)
	if over.WindowName != "" {
		if named, ok := a.namedWindows[over.WindowName]; ok && named != nil {
			partitionBy = append(a.listTexts(named.PartitionBy), partitionBy...)
			orderBy = append(a.orderByTexts(named.OrderBy), orderBy...)
		}
	}
	return partitionBy, orderBy
}

// orderByTexts renders the expression of every ORDER BY item, dropping the sort
// direction: a direction is not a column and the same key written either way
// names one dependency.
func (a *Analyzer) orderByTexts(list *nodes.List) []string {
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		orderBy, ok := item.(*nodes.OrderByItem)
		if !ok || orderBy == nil || orderBy.Expr == nil {
			continue
		}
		out = append(out, a.exprTextOf(orderBy.Expr))
	}
	return out
}

// groupByKeys renders the GROUP BY items of a query in source order, the way this
// analyzer renders PARTITION BY and ORDER BY. GROUP BY ALL carries no key list.
func (a *Analyzer) groupByKeys(list *nodes.List) []string {
	return a.listTexts(list)
}

// binaryOperatorName maps a T-SQL binary operator onto the recorded operator type.
// The vocabulary is the MySQL family's word set rather than PostgreSQL's source
// token, because this AST is enum-driven the way MySQL's is.
func binaryOperatorName(op nodes.BinaryOp) (string, bool) {
	switch op {
	case nodes.BinOpAdd:
		return "ADDITION", true
	case nodes.BinOpSub:
		return "SUBTRACTION", true
	case nodes.BinOpMul:
		return "MULTIPLICATION", true
	case nodes.BinOpDiv:
		return "DIVISION", true
	case nodes.BinOpMod:
		return "MODULO", true
	case nodes.BinOpBitAnd:
		return "BIT_AND", true
	case nodes.BinOpBitOr:
		return "BIT_OR", true
	case nodes.BinOpBitXor:
		return "BIT_XOR", true
	case nodes.BinOpEq:
		return "EQUALS", true
	case nodes.BinOpNeq:
		return "NOT_EQUALS", true
	case nodes.BinOpLt:
		return "LESS_THAN", true
	case nodes.BinOpGt:
		return "GREATER_THAN", true
	case nodes.BinOpLte:
		return "LESS_OR_EQUAL", true
	case nodes.BinOpGte:
		return "GREATER_OR_EQUAL", true
	case nodes.BinOpNotLt:
		return "NOT_LESS_THAN", true
	case nodes.BinOpNotGt:
		return "NOT_GREATER_THAN", true
	case nodes.BinOpAnd:
		return "LOGICAL_AND", true
	case nodes.BinOpOr:
		return "LOGICAL_OR", true
	default:
		return "", false
	}
}

// unaryOperatorName maps a T-SQL unary operator onto the recorded operator type.
func unaryOperatorName(op nodes.UnaryOp) (string, bool) {
	switch op {
	case nodes.UnaryPlus:
		return "UNARY_PLUS", true
	case nodes.UnaryMinus:
		return "NEGATION", true
	case nodes.UnaryBitNot:
		return "BIT_NOT", true
	case nodes.UnaryNot:
		return "NOT", true
	default:
		return "", false
	}
}

// isTestName names an IS test the way the statement wrote it. The NOT forms are
// part of the operator's identity, unlike BETWEEN / IN / LIKE, whose NOT the
// MySQL family folds away as well.
func isTestName(test nodes.IsTestType) string {
	switch test {
	case nodes.IsNull:
		return "IS NULL"
	case nodes.IsNotNull:
		return "IS NOT NULL"
	case nodes.IsTrue:
		return "IS TRUE"
	case nodes.IsNotTrue:
		return "IS NOT TRUE"
	case nodes.IsFalse:
		return "IS FALSE"
	case nodes.IsNotFalse:
		return "IS NOT FALSE"
	default:
		return "IS"
	}
}

// aggregateFunctions are the T-SQL aggregate functions: a call to one of them
// depends on the rows of its relation even when it names no column, as COUNT(*)
// does.
var aggregateFunctions = map[string]bool{
	"APPROX_COUNT_DISTINCT": true,
	"AVG":                   true,
	"CHECKSUM_AGG":          true,
	"COUNT":                 true,
	"COUNT_BIG":             true,
	"GROUPING":              true,
	"GROUPING_ID":           true,
	"MAX":                   true,
	"MIN":                   true,
	"STDEV":                 true,
	"STDEVP":                true,
	"STRING_AGG":            true,
	"SUM":                   true,
	"VAR":                   true,
	"VARP":                  true,
}
