package doris

import (
	"strings"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/doris/ast"
)

// ---------------------------------------------------------------------------
// Identifier helpers
// ---------------------------------------------------------------------------

// columnRefFromObjectName maps an omni ObjectName onto a scope column
// reference. Doris addresses objects as [catalog.]database.table.column;
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

// inferredColumnAlias names an unaliased select item the way Doris reports it.
// A column reference contributes its own name, however it was qualified (`t.a` is
// the column a); every other expression contributes its source text. The choice is
// made on the node, never on the text: an expression that merely contains a dot —
// a CASE whose ELSE names t.b, an IS NULL test — is not a qualified identifier
// even when its concatenated text happens to look like one, which is how the name
// used to come out as `bEND`, `xISNULL` and `b]`.
func inferredColumnAlias(expr nodes.Node, exprText string) string {
	if cr, ok := expr.(*nodes.ColumnRef); ok && cr.Name != nil && len(cr.Name.Parts) > 0 {
		return model.NormalizeIdentifier(cr.Name.Parts[len(cr.Name.Parts)-1])
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

// columnSource is one column reference a statement wrote, together with the
// VARIANT subcolumn path it reads into that column when the reference was a
// dotted path (`v.user.name`).
type columnSource struct {
	ref  scope.ColumnRef
	path string
}

// collectColumnSources resolves the columns an expression wrote against the scope
// it was written in, because a dotted name has two readings and only a scope can
// tell them apart: a qualified column, or a VARIANT column plus a subcolumn path.
// A reference that resolves the way it always did is returned untouched.
func (a *Analyzer) collectColumnSources(expr nodes.Node, sp *scope.Scope) []columnSource {
	sources := make([]columnSource, 0)
	if expr == nil {
		return sources
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		cr, ok := n.(*nodes.ColumnRef)
		if !ok {
			return true
		}
		if source, ok := a.columnSource(cr.Name, sp); ok {
			sources = append(sources, source)
		}
		return true
	})
	return sources
}

// collectColumnRefs is collectColumnSources for the callers that only need the
// columns: a path adds detail to a transformation, never a second edge.
func (a *Analyzer) collectColumnRefs(expr nodes.Node, sp *scope.Scope) []scope.ColumnRef {
	sources := a.collectColumnSources(expr, sp)
	refs := make([]scope.ColumnRef, 0, len(sources))
	for _, source := range sources {
		refs = append(refs, source.ref)
	}
	return refs
}

// columnSource resolves one reference the way the statement wrote it. The
// qualified reading is tried first, exactly as this analyzer has always read it —
// as a probe, so the returned reference is still the unresolved one the callers
// resolve in their own scope, and a reference that resolved before still does.
//
// Only a reference with no qualified reading is read as a VARIANT subcolumn path,
// and only with evidence: the catalog has to say the base column is a VARIANT
// column. Without that evidence the reference keeps the unresolved gap it is
// today, so a scalar column's invented "field" (`id.name`) never becomes an edge.
func (a *Analyzer) columnSource(name *nodes.ObjectName, sp *scope.Scope) (columnSource, bool) {
	ref, ok := columnRefFromObjectName(name)
	if !ok {
		return columnSource{}, false
	}
	// A path needs at least one dot, so a plain name is never probed: the probe
	// would be a second resolution of every ordinary column reference.
	if len(name.Parts) < 2 {
		return columnSource{ref: ref}, true
	}
	if _, err := sp.ResolveColumnRefs(ref); err == nil {
		return columnSource{ref: ref}, true
	}
	base, path, ok := a.variantPathBase(name.Parts, sp)
	if !ok {
		return columnSource{ref: ref}, true
	}
	base.Ref.Resolved = true
	return columnSource{ref: base.Ref, path: path}, true
}

// variantPathBase finds the column a dotted reference reads into, and the path
// inside it, when no relation qualifies the whole name.
//
// A relation prefix keeps precedence, which is what the engine does: in
// `SELECT v.name FROM ev, ev2 v` the reference reads ev2's column `name`, not a
// path on ev's VARIANT column `v`. The prefix has to name the base column as a
// VARIANT column the catalog knows, and the path reading is tried only when no
// prefix does. An unqualified base has to be unambiguous for the same reason: two
// relations exposing a VARIANT column of one name is a statement the engine
// rejects, not a reading to guess at.
func (a *Analyzer) variantPathBase(parts []string, sp *scope.Scope) (scope.ResolvedColumn, string, bool) {
	for k := len(parts) - 2; k >= 1; k-- {
		column := parts[k]
		schema, table, ok := tableRefFromObjectName(&nodes.ObjectName{Parts: parts[:k]})
		if !ok {
			continue
		}
		resolutions, err := sp.ResolveColumnRefs(scope.ColumnRef{Schema: schema, Table: table, Column: column})
		if err != nil || len(resolutions) != 1 {
			continue
		}
		switch a.variantBase(resolutions[0].Relation, column) {
		case variantBaseYes:
			return resolutions[0], strings.Join(parts[k+1:], "."), true
		case variantBaseNo:
			// The prefix owns the name: the engine reads `v.user` as that
			// relation's column and rejects the path (`No such field 'name' in
			// 'user'`), so no path reading may be built from this reference.
			return scope.ResolvedColumn{}, "", false
		default:
			// variantBaseUnknown: the prefix may not qualify the name at all, and a
			// shorter one — or the base column alone — still might.
		}
	}
	resolutions, err := sp.ResolveColumnRefs(scope.ColumnRef{Column: parts[0]})
	if err != nil || len(resolutions) != 1 {
		return scope.ResolvedColumn{}, "", false
	}
	if a.variantBase(resolutions[0].Relation, parts[0]) != variantBaseYes {
		return scope.ResolvedColumn{}, "", false
	}
	return resolutions[0], strings.Join(parts[1:], "."), true
}

// variantBaseVerdict is what the catalog can say about one name inside one
// relation.
type variantBaseVerdict int

const (
	// variantBaseUnknown means the catalog does not list the name, or cannot answer
	// for this relation at all: a query-local relation carries no declared type, and
	// a catalog that is absent or has no column list knows nothing either way.
	variantBaseUnknown variantBaseVerdict = iota
	// variantBaseYes means the catalog lists the name as a VARIANT column, the one
	// type Doris reads a dotted path into.
	variantBaseYes
	// variantBaseNo means the catalog lists the name as a column of another type.
	variantBaseNo
)

// variantBase reports what the catalog knows about name in relation. It is the
// evidence the path reading is gated on: without it a dotted reference that no
// relation qualifies keeps the unresolved gap it is today, so a scalar column's
// invented "field" (`id.name`) never becomes an edge.
func (a *Analyzer) variantBase(relation *scope.TableRef, name string) variantBaseVerdict {
	if relation == nil || relation.IsCTE || relation.IsSubquery {
		return variantBaseUnknown
	}
	meta := a.catalogTable(relation)
	if meta == nil {
		return variantBaseUnknown
	}
	for _, column := range meta.Columns {
		if column.Name == name || strings.EqualFold(column.Name, name) {
			if isVariantType(column.Type) {
				return variantBaseYes
			}
			return variantBaseNo
		}
	}
	return variantBaseUnknown
}

// isVariantType reports whether a stored column type names Doris's VARIANT type.
// The engine reports it lowercase (`variant`), and only VARIANT takes the dot
// form: the same engine rejects `j.a` for a JSON column, so JSON is not accepted
// here either.
func isVariantType(dataType string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(dataType))
	return normalized == "VARIANT" || strings.HasPrefix(normalized, "VARIANT<")
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

// binaryOperatorName maps a Doris binary operator to the recorded operator
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

// unaryOperatorName maps a Doris unary operator to the recorded operator
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
