package postgresql

import (
	"strings"

	pgast "github.com/bytebase/omni/pg/ast"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// exprText returns the exact source slice for a node span, trimmed. omni's Loc is
// a byte range absolute into the parsed SQL and may include trailing whitespace,
// so trimming yields the exact source slice (source text, inner whitespace kept).
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
// name before it is the table qualifier, and a further name before that is the
// schema qualifier, which the resolver needs to tell two same-named relations
// apart (s1.t from s2.t). A trailing A_Star becomes the wildcard column.
func (*Analyzer) columnRefFromFields(fields *pgast.List) scope.ColumnRef {
	names := stringList(fields)

	ref := scope.ColumnRef{}
	if fieldsContainStar(fields) {
		ref.Column = wildcardColumn
		switch len(names) {
		case 0:
		case 1:
			ref.Table = names[0]
		default:
			ref.Schema = names[len(names)-2]
			ref.Table = names[len(names)-1]
		}
		return ref
	}

	switch len(names) {
	case 0:
	case 1:
		ref.Column = names[0]
	default:
		ref.Column = names[len(names)-1]
		ref.Table = names[len(names)-2]
		if len(names) >= 3 {
			ref.Schema = names[len(names)-3]
		}
	}
	return ref
}

// extractColumnsFromNode collects the column references an expression depends
// on. A subquery is opaque here: its columns belong to its own scope and are
// resolved by subquerySources, so they are not attributed to the relations of
// the enclosing query. A SubLink's Testexpr still belongs to the enclosing
// expression and is collected.
func (a *Analyzer) extractColumnsFromNode(node pgast.Node, sp *scope.Scope) []scope.ColumnRef {
	columns := make([]scope.ColumnRef, 0)
	if node == nil {
		return columns
	}
	var subqueries []*pgast.SelectStmt
	pgast.Inspect(node, func(n pgast.Node) bool {
		switch x := n.(type) {
		case *pgast.SubLink:
			if x.Testexpr != nil {
				columns = append(columns, a.extractColumnsFromNode(x.Testexpr, sp)...)
			}
			if sel, ok := x.Subselect.(*pgast.SelectStmt); ok {
				subqueries = append(subqueries, sel)
			}
			return false // the subselect is analyzed as a unit below
		case *pgast.FuncCall:
			// `OVER w` keeps the whole window definition — PARTITION BY, ORDER BY
			// and the frame bounds — in the statement's WINDOW clause, which the
			// walk over this expression cannot reach. The columns those clauses
			// name decide the window as much as the aggregate's own arguments do.
			if windowDef, ok := x.Over.(*pgast.WindowDef); ok {
				for _, named := range a.namedWindowDefinitions(windowDef) {
					columns = append(columns, a.windowClauseColumns(named.PartitionClause, sp, false)...)
					columns = append(columns, a.windowClauseColumns(named.OrderClause, sp, true)...)
					if named.StartOffset != nil {
						columns = append(columns, a.extractColumnsFromNode(named.StartOffset, sp)...)
					}
					if named.EndOffset != nil {
						columns = append(columns, a.extractColumnsFromNode(named.EndOffset, sp)...)
					}
				}
			}
		case *pgast.ColumnRef:
			colRef := a.columnRefFromFields(x.Fields)
			if colRef.Column != "" && colRef.Column != wildcardColumn {
				columns = append(columns, colRef)
			}
		default:
			// Other nodes are traversed for the column references they contain.
		}
		return true
	})
	for _, sel := range subqueries {
		columns = append(columns, a.subquerySources(sel, sp)...)
	}
	return columns
}

// subquerySources analyzes a subquery in its own scope and returns the sources of
// its output columns, resolved and marked so the enclosing scope can use them
// without seeing the subquery's relations.
func (a *Analyzer) subquerySources(sel *pgast.SelectStmt, sp *scope.Scope) []scope.ColumnRef {
	if sel == nil {
		return nil
	}
	a.scopeStack = append(a.scopeStack, scope.NewScope(sp))
	a.processSelectStmt(sel)
	subScope := a.popScope()
	if subScope == nil {
		return nil
	}
	// The subquery's rows decide the value the enclosing expression reads, so the
	// predicates that shaped them belong to that expression's query.
	a.influences.Inherit(subScope, sp)
	var out []scope.ColumnRef
	for _, col := range a.resolveOutputColumns(subScope, subScope.GetOutputColumns()) {
		out = append(out, scope.Refs(col.Sources)...)
	}
	return out
}

// resolveOutputColumns resolves each output column's source references against
// the scope the column was collected in and marks them resolved. Without it a
// later resolution in the enclosing scope would fail, because the subquery's
// relations are not visible there, and the lineage would be dropped. Each
// source keeps the transformation that produced it, and a source that resolved
// to a query-local relation is flattened into the stored relations its lineage
// came from, so the reference never carries an alias into a scope that does not
// define it.
func (a *Analyzer) resolveOutputColumns(sp *scope.Scope, cols []scope.OutputColumn) []scope.OutputColumn {
	out := make([]scope.OutputColumn, len(cols))
	copy(out, cols)
	for i := range out {
		if len(out[i].Sources) == 0 {
			continue
		}
		resolved := make([]scope.ColumnSource, 0, len(out[i].Sources))
		for _, source := range out[i].Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				resolved = append(resolved, source)
				continue
			}
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					resolved = append(resolved, a.flattenTempSources(sp, res.Ref, res.Relation, source.Transform)...)
					continue
				}
				columnRef := res.Ref
				columnRef.Resolved = true
				resolved = append(resolved, scope.ColumnSource{Ref: columnRef, Transform: source.Transform})
			}
		}
		out[i].Sources = resolved
	}
	return out
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

// groupByKeys renders the GROUP BY items of a query specification in source
// order, the way this analyzer renders PARTITION BY and ORDER BY. A positional
// key stays "1" and an alias stays the alias, because that is what the query
// wrote. GROUP BY DISTINCT has no representation here; the flag is not part of a
// key list.
func (a *Analyzer) groupByKeys(items *pgast.List) []string {
	if items == nil || len(items.Items) == 0 {
		return nil
	}
	return a.nodeTexts(items)
}

// containsGroupAggregate reports whether the expression contains an aggregate
// call that GROUP BY governs, i.e. one without an OVER clause. A windowed
// aggregate follows its OVER clause instead. Traversal stops at a subquery: an
// aggregate inside one is grouped by that query's own GROUP BY, never by the
// enclosing statement's.
func containsGroupAggregate(node pgast.Node) bool {
	found := false
	if node == nil {
		return false
	}
	pgast.Inspect(node, func(n pgast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*pgast.SubLink); ok {
			return false
		}
		if fc, ok := n.(*pgast.FuncCall); ok && fc.Over == nil && aggregateFunctions[strings.ToUpper(funcName(fc))] {
			found = true
			return false
		}
		return true
	})
	return found
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

// maxNamedWindowChain bounds the walk over a chain of named windows. PostgreSQL
// rejects a cycle, so the bound only keeps a malformed tree from looping.
const maxNamedWindowChain = 16

// namedWindowsOf indexes a query's WINDOW clause by the name each definition is
// referenced by.
func namedWindowsOf(list *pgast.List) map[string]*pgast.WindowDef {
	if list == nil || len(list.Items) == 0 {
		return nil
	}
	out := make(map[string]*pgast.WindowDef, len(list.Items))
	for _, item := range list.Items {
		if def, ok := item.(*pgast.WindowDef); ok && def.Name != "" {
			out[def.Name] = def
		}
	}
	return out
}

// namedWindowDefinitions returns the window definitions a use of a window
// reaches by name, in the order it reaches them. A use names the definition it
// refers to, and PostgreSQL lets one named window be defined over another
// (`WINDOW w2 AS (w1 ORDER BY x)`), so the chain is followed. The definition the
// use itself carries is never returned: the walk over the expression already
// reaches its own clauses.
func (a *Analyzer) namedWindowDefinitions(use *pgast.WindowDef) []*pgast.WindowDef {
	if use == nil || len(a.namedWindows) == 0 {
		return nil
	}
	// `OVER w` names the definition in Name; `OVER (w PARTITION BY x)` refers to
	// it through Refname and adds clauses of its own.
	name := use.Refname
	if name == "" {
		name = use.Name
	}
	var out []*pgast.WindowDef
	seen := make(map[string]struct{})
	for depth := 0; name != "" && depth < maxNamedWindowChain; depth++ {
		if _, ok := seen[name]; ok {
			break
		}
		seen[name] = struct{}{}
		def, ok := a.namedWindows[name]
		if !ok || def == nil {
			break
		}
		out = append(out, def)
		name = def.Refname
	}
	return out
}

// windowClauseColumns collects the columns one window clause depends on. orderBy
// unwraps the SortBy wrapper the ORDER BY list uses.
func (a *Analyzer) windowClauseColumns(list *pgast.List, sp *scope.Scope, orderBy bool) []scope.ColumnRef {
	if list == nil {
		return nil
	}
	var out []scope.ColumnRef
	for _, item := range list.Items {
		expr := item
		if orderBy {
			sortBy, ok := item.(*pgast.SortBy)
			if !ok {
				continue
			}
			expr = sortBy.Node
		}
		out = append(out, a.extractColumnsFromNode(expr, sp)...)
	}
	return out
}

// windowClauses extracts PARTITION BY and ORDER BY expressions from a window
// definition, following a `OVER w` reference into the WINDOW clause that defines
// it. Sort direction is deliberately dropped, matching the MySQL analyzer's
// extractWindowClauses.
func (a *Analyzer) windowClauses(over pgast.Node) (partitionBy, orderBy []string) {
	windowDef, ok := over.(*pgast.WindowDef)
	if !ok || windowDef == nil {
		return nil, nil
	}
	// The clauses the use carries itself come first, then the named definitions
	// it is defined over.
	definitions := append([]*pgast.WindowDef{windowDef}, a.namedWindowDefinitions(windowDef)...)
	for _, def := range definitions {
		if def.PartitionClause != nil {
			for _, expr := range def.PartitionClause.Items {
				partitionBy = append(partitionBy, a.exprTextOf(expr))
			}
		}
		if def.OrderClause != nil {
			for _, item := range def.OrderClause.Items {
				if sortBy, ok := item.(*pgast.SortBy); ok {
					orderBy = append(orderBy, a.exprTextOf(sortBy.Node))
				}
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

// unaliasedColumnName is the name PostgreSQL gives an output column whose
// expression it cannot name.
const unaliasedColumnName = "?column?"

// maxColumnNameDepth bounds the walk into a nested subquery when naming an
// unaliased target. A statement cannot nest deeper than the parser allows, so
// the bound only keeps a malformed tree from looping.
const maxColumnNameDepth = 16

// inferColumnAlias returns the name PostgreSQL gives an unaliased target
// expression, so a stored target column matches the column the object actually
// exposes. A view's definition is stored with those names written out by
// pg_get_viewdef, but a manually entered statement is not, and the expression
// text this used to return (`count(*)`, `a + b`) is not a column name at all.
//
// The rules, verified against PostgreSQL 16: a column reference is named by the
// column, a function call by its unqualified function name, a cast by whatever
// the cast names and otherwise by its target type, and CASE, COALESCE,
// GREATEST/LEAST, NULLIF, ARRAY[...], ROW(...) and the SQL value functions by
// their own names. Anything PostgreSQL does not name — a constant, a parameter,
// an operator — becomes "?column?".
func (a *Analyzer) inferColumnAlias(node pgast.Node) string {
	if name := a.columnNameOf(node, 0); name != "" {
		return name
	}
	return unaliasedColumnName
}

// columnNameOf returns the name PostgreSQL derives from an expression, or "" when
// it derives none. A cast is the one node that uses the empty answer: it falls
// back to its target type's name.
func (a *Analyzer) columnNameOf(node pgast.Node, depth int) string {
	if node == nil || depth > maxColumnNameDepth {
		return ""
	}
	switch n := node.(type) {
	case *pgast.ColumnRef:
		return a.columnRefFromFields(n.Fields).Column
	case *pgast.FuncCall:
		return funcName(n)
	case *pgast.TypeCast:
		if name := a.columnNameOf(n.Arg, depth+1); name != "" {
			return name
		}
		return typeNameOf(n.TypeName)
	case *pgast.CollateClause:
		// `a COLLATE "C"` is named after the expression it collates.
		return a.columnNameOf(n.Arg, depth+1)
	case *pgast.A_Indirection:
		// A subscript or a field selection is named after its base: `arr[1]` is
		// `arr`.
		return a.columnNameOf(n.Arg, depth+1)
	case *pgast.SubLink:
		// A scalar subquery takes the name of its first output column. An output
		// column the subquery cannot name makes the whole expression "?column?"
		// rather than falling through to an enclosing cast's type name, which is
		// what PostgreSQL reports for `(SELECT 1)::int`.
		inner := a.subqueryFirstName(n, depth)
		if inner != "" {
			return inner
		}
		return unaliasedColumnName
	case *pgast.CaseExpr:
		return "case"
	case *pgast.CoalesceExpr:
		return "coalesce"
	case *pgast.MinMaxExpr:
		if n.Op == pgast.IS_GREATEST {
			return "greatest"
		}
		return "least"
	case *pgast.NullIfExpr:
		return "nullif"
	case *pgast.A_Expr:
		if n.Kind == pgast.AEXPR_NULLIF {
			return "nullif"
		}
	case *pgast.A_ArrayExpr:
		return "array"
	case *pgast.RowExpr:
		return "row"
	case *pgast.GroupingFunc:
		return "grouping"
	case *pgast.SQLValueFunction:
		return sqlValueFunctionName(n.Op)
	default:
		// A constant, a parameter, an operator, a test: PostgreSQL derives no
		// name from them.
	}
	return ""
}

// subqueryFirstName returns the name of a scalar subquery's first output column,
// which is the name the subquery itself gives it.
func (a *Analyzer) subqueryFirstName(sub *pgast.SubLink, depth int) string {
	sel, ok := sub.Subselect.(*pgast.SelectStmt)
	if !ok || sel.TargetList == nil || len(sel.TargetList.Items) == 0 {
		return ""
	}
	rt, ok := sel.TargetList.Items[0].(*pgast.ResTarget)
	if !ok {
		return ""
	}
	if rt.Name != "" {
		return rt.Name
	}
	// The target's own expression decides; "" means the subquery cannot name it.
	return a.columnNameOf(rt.Val, depth+1)
}

// typeNameOf returns the unqualified name of a cast's target type.
func typeNameOf(typeName *pgast.TypeName) string {
	if typeName == nil {
		return ""
	}
	names := stringList(typeName.Names)
	if len(names) == 0 {
		return ""
	}
	return names[len(names)-1]
}

// sqlValueFunctionName returns the name PostgreSQL gives a SQL value function
// (`CURRENT_DATE`, `USER`, …). A precision-carrying spelling shares the name of
// the plain one, as PostgreSQL reports it.
func sqlValueFunctionName(op pgast.SVFOp) string {
	switch op {
	case pgast.SVFOP_CURRENT_DATE:
		return "current_date"
	case pgast.SVFOP_CURRENT_TIME, pgast.SVFOP_CURRENT_TIME_N:
		return "current_time"
	case pgast.SVFOP_CURRENT_TIMESTAMP, pgast.SVFOP_CURRENT_TIMESTAMP_N:
		return "current_timestamp"
	case pgast.SVFOP_LOCALTIME, pgast.SVFOP_LOCALTIME_N:
		return "localtime"
	case pgast.SVFOP_LOCALTIMESTAMP, pgast.SVFOP_LOCALTIMESTAMP_N:
		return "localtimestamp"
	case pgast.SVFOP_CURRENT_ROLE:
		return "current_role"
	case pgast.SVFOP_CURRENT_USER:
		return "current_user"
	case pgast.SVFOP_USER:
		return "user"
	case pgast.SVFOP_SESSION_USER:
		return "session_user"
	case pgast.SVFOP_CURRENT_CATALOG:
		return "current_catalog"
	case pgast.SVFOP_CURRENT_SCHEMA:
		return "current_schema"
	default:
		return ""
	}
}
