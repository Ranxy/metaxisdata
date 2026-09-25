// Package mysql provides direct lineage analysis for MySQL queries.
//
// This implementation is built on github.com/bytebase/omni's MySQL parser and
// typed AST. It replaces the legacy ANTLR implementation, which was
// parity-verified against it over the golden corpus and then removed.
//
// Known gaps and their intended resolution are tracked in
// plan/mysql_lineage_optimization_plan.md; defects in omni itself are recorded
// in docs/omni_upstream_defects.md.
package mysql

//go:generate go run ./gen

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/mysql/ast"
	mysqlparser "github.com/bytebase/omni/mysql/parser"
)

// Constants for special table/column markers.
const (
	resultTableName   = model.ResultTableName
	deletionFieldName = model.DeletionColumnName
	wildcardColumn    = model.WildcardColumn
	fileSourceMarker  = model.FileSourceName
)

// Registration binds MySQL to the analyzer this package provides. The process
// assembles the registered engines where it is built, so this package does not
// register itself into package state.
//
// MariaDB and TiDB live in their own packages and provide their own analyzers.
// OceanBase is deliberately unsupported: the runner records a per-object skip for
// it.
func Registration() lineage.EngineRegistration {
	return lineage.EngineRegistration{
		Engine:  storepb.Engine_MYSQL,
		Analyze: Analyze,
		Split:   SplitStatements,
	}
}

// valuesQueryPrimary returns the VALUES query primary of a select statement
// (`SELECT * FROM (VALUES ROW(1)) v`), or nil when the statement has none. This
// dialect's AST models the form as a field of SelectStmt.
func valuesQueryPrimary(stmt *nodes.SelectStmt) *nodes.ValuesStmt {
	return stmt.ValuesSource
}

// rowAliasNames returns the row alias an INSERT's VALUES row declares together
// with its optional column list (`INSERT ... VALUES (...) AS new(a) ...`), which
// an upsert assignment may then use to name the proposed row. Both are empty when
// the statement declares none.
func rowAliasNames(stmt *nodes.InsertStmt) (string, []string) {
	return stmt.RowAlias, stmt.ColAliases
}

// The line below separates this dialect's header from the traversal the MySQL
// family shares. Everything from it onward is copied byte for byte into
// tidb/analyzer_body_gen.go and mariadb/analyzer_body_gen.go by
// `go generate ./backend/plugin/lineage/mysql`, so the two siblings are edited
// here and never in a generated copy. Only the marker constants and the two
// hooks above it are taken back out of the header; the TiDB and MariaDB headers
// live in their own dialect.go.
// == MYSQL-FAMILY SHARED BODY ==

// Analyzer performs direct lineage analysis on MySQL queries.
type Analyzer struct {
	ctx context.Context
	sql string
	// tokens backs whitespace-free expression text reconstruction.
	tokens []mysqlparser.Token
	// Current scope stack
	scopeStack []*scope.Scope
	// Collected column relations
	edges *algorithm.EdgeSet
	// Optional catalog for wildcard expansion and metadata lookup, memoized for
	// the whole analysis so a relation named twice is read once. It is nil when no
	// provider was configured, which is why every use is guarded.
	catalog *catalog.Cache
	// realTarget is set while analyzing the query of a statement that writes to a
	// real object (INSERT/REPLACE/CREATE TABLE AS/CREATE VIEW), so the query
	// result edges are not emitted alongside the real target edges.
	realTarget bool
	// inSetOpArm is set while analyzing one arm of a set operation, so only the
	// merged set-operation result emits edges.
	inSetOpArm bool
	// influences holds the row-set influences collected while a scope was
	// current. They belong to the rows that scope produces and are inherited by
	// whatever consumes them, so a scope whose rows reach no output cannot
	// influence the statement's result.
	influences *algorithm.Influences
	// diagnostics collects what the analysis could not represent: a reference
	// that resolved to nothing, and a clause whose target it could not choose.
	// They turn the result into a partial one that says what is missing.
	diagnostics *algorithm.Diagnostics
	// namedWindows maps the WINDOW clause in effect to its definitions. A window
	// function may name the window it uses (`OVER w`) instead of spelling its
	// clauses out, and the definition is a sibling of the select list rather than
	// a child of the expression, so the walk over the expression cannot reach it.
	namedWindows map[string]*nodes.WindowDef
}

// Analyze parses a single MySQL statement and returns its column relations.
func Analyze(ctx context.Context, sql string, cat catalog.Provide) ([]model.ColumnRelation, error) {
	return NewAnalyzer(ctx, sql, cat).AnalyzeRelations()
}

// SplitStatements splits a script into its individual statements, dropping the
// ones that carry no SQL. The caller feeds them to Analyze one at a time: this
// analyzer is defined for exactly one statement.
func SplitStatements(sql string) []string {
	segments := mysqlparser.Split(sql)
	statements := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment.Empty() {
			continue
		}
		statements = append(statements, segment.Text)
	}
	return statements
}

// NewAnalyzer creates a new MySQL lineage analyzer.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	diagnostics := algorithm.NewDiagnostics()
	return &Analyzer{
		ctx:         ctx,
		sql:         sql,
		tokens:      mysqlparser.Tokenize(sql),
		scopeStack:  []*scope.Scope{scope.NewScope(nil)}, // Root scope
		edges:       algorithm.NewEdgeSet(),
		catalog:     catalog.NewCache(catalogProvide),
		influences:  algorithm.NewInfluences(diagnostics),
		diagnostics: diagnostics,
	}
}

// AnalyzeRelations parses the SQL and returns column relations.
//
// Parsing is strict: a statement omni cannot parse is an error, never a partial
// result. Multi-statement input is rejected because the analyzer is defined for
// exactly one statement; the caller splits a script and analyzes it statement by
// statement. A statement shape the analyzer cannot represent is reported as a gap
// beside the edges it found, never as a silent empty result.
func (a *Analyzer) AnalyzeRelations() ([]model.ColumnRelation, error) {
	list, err := mysqlparser.Parse(a.sql)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse MySQL SQL")
	}
	if list.Len() != 1 {
		return nil, errors.Errorf("expected exactly 1 statement, got %d", list.Len())
	}

	switch stmt := list.Items[0].(type) {
	case *nodes.SelectStmt:
		a.processSelectStatement(stmt)
	case *nodes.TableStmt:
		a.processTableStmt(stmt)
	case *nodes.ValuesStmt:
		a.processValuesStatement(stmt)
	case *nodes.InsertStmt:
		if !a.skipLeadingWith("INSERT") {
			a.processInsertStatement(stmt)
		}
	case *nodes.CreateTableStmt:
		a.processCreateTable(stmt)
	case *nodes.CreateViewStmt:
		a.processCreateView(stmt)
	case *nodes.AlterViewStmt:
		a.processAlterView(stmt)
	case *nodes.UpdateStmt:
		if !a.skipLeadingWith("UPDATE") {
			a.processUpdateStatement(stmt)
		}
	case *nodes.DeleteStmt:
		if !a.skipLeadingWith("DELETE") {
			a.processDeleteStatement(stmt)
		}
	case *nodes.LoadDataStmt:
		a.processLoadStatement(stmt)
	default:
		// Statement kinds with no column lineage (DDL, maintenance, transactions)
		// are ignored; there is nothing to resolve.
	}

	// What the analysis could not represent is reported beside the edges it did
	// find: a statement shape it does not model, and a reference that resolved to
	// nothing. A parse error is the only thing that fails an analysis whole, and it
	// has already returned above.
	if notes, omitted := a.diagnostics.Notes(); len(notes) > 0 {
		return a.edges.Edges(), &lineage.UnsupportedStatementError{Diagnostics: notes, Omitted: omitted}
	}
	return a.edges.Edges(), nil
}

// skipLeadingWith reports whether a DML statement is prefixed with a WITH clause,
// and records the gap when it is. omni parses such statements but its
// InsertStmt/UpdateStmt/DeleteStmt carry no CTEs, so the WITH clause is dropped and
// the CTE name would be treated as a real table — a wrong edge is worse than a
// reported gap, so the statement is not analyzed. The note is what keeps the skip
// from reading as "this statement has no lineage" (see docs/omni_upstream_defects.md).
func (a *Analyzer) skipLeadingWith(statement string) bool {
	if !hasLeadingWith(a.sql) {
		return false
	}
	a.diagnostics.NotModelled("WITH before "+statement,
		"the parser drops the CTE, so its sources cannot be resolved")
	return true
}

// hasLeadingWith reports whether sql starts with a WITH clause, skipping leading
// whitespace and comments.
func hasLeadingWith(sql string) bool {
	s := sql
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		switch {
		case strings.HasPrefix(s, "/*"):
			end := strings.Index(s[2:], "*/")
			if end < 0 {
				return false
			}
			s = s[2+end+2:]
		case strings.HasPrefix(s, "--"), strings.HasPrefix(s, "#"):
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return false
			}
			s = s[i+1:]
		default:
			if len(s) < 5 || !strings.EqualFold(s[:4], "WITH") {
				return false
			}
			return s[4] == ' ' || s[4] == '\t' || s[4] == '\n' || s[4] == '\r'
		}
	}
}

// ---------------------------------------------------------------------------
// Source text
// ---------------------------------------------------------------------------

// locFieldCache caches the struct field index of the Loc field per node type,
// so reading a node's location does not repeat the name lookup.
var locFieldCache sync.Map // map[reflect.Type]int (-1 when absent)

// nodeLoc reads the Loc field every omni AST node carries. omni exposes no
// location interface: Loc is a named field, so its methods are not promoted and
// an interface assertion fails (docs/omni_upstream_defects.md item 2).
// Reflection is therefore required; the field index is cached per type.
func nodeLoc(n nodes.Node) nodes.Loc {
	if n == nil {
		return nodes.Loc{}
	}
	v := reflect.ValueOf(n)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nodes.Loc{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nodes.Loc{}
	}
	idx, ok := locFieldIndex(v.Type())
	if !ok || idx >= v.NumField() {
		return nodes.Loc{}
	}
	loc, ok := v.Field(idx).Interface().(nodes.Loc)
	if !ok {
		return nodes.Loc{}
	}
	return loc
}

// locFieldIndex returns the index of the Loc field of a node type.
func locFieldIndex(t reflect.Type) (int, bool) {
	if cached, ok := locFieldCache.Load(t); ok {
		idx, ok := cached.(int)
		if !ok {
			return -1, false
		}
		return idx, idx >= 0
	}
	idx := -1
	if field, ok := t.FieldByName("Loc"); ok && field.Type == reflect.TypeOf(nodes.Loc{}) {
		idx = field.Index[0]
	}
	locFieldCache.Store(t, idx)
	return idx, idx >= 0
}

// exprSpan returns the source span of a node, widening an infix node's span to
// the earliest child offset because omni anchors its Loc at the operator
// (docs/omni_upstream_defects.md item 1). ok is false when the span is unusable.
func (a *Analyzer) exprSpan(n nodes.Node) (start, end int, ok bool) {
	if n == nil {
		return 0, 0, false
	}
	loc := nodeLoc(n)
	if loc.Start < 0 || loc.End > len(a.sql) || loc.Start >= loc.End {
		return 0, 0, false
	}
	start = loc.Start
	if isInfixNode(n) {
		if earliest := earliestStart(n); earliest >= 0 && earliest < start {
			start = earliest
		}
	}
	return start, loc.End, true
}

// exprTextOf reconstructs a node's source text with inter-token whitespace
// removed.
func (a *Analyzer) exprTextOf(n nodes.Node) string {
	start, end, ok := a.exprSpan(n)
	if !ok {
		return ""
	}
	// Tokens are in source order, so the first token inside the span is found by
	// binary search instead of scanning from the start of the statement.
	from, _ := slices.BinarySearchFunc(a.tokens, start, func(t mysqlparser.Token, target int) int {
		return cmp.Compare(t.Loc, target)
	})
	var b strings.Builder
	for i := from; i < len(a.tokens); i++ {
		t := a.tokens[i]
		if t.Loc >= end {
			break
		}
		if t.End <= end {
			_, _ = b.WriteString(a.sql[t.Loc:t.End])
		}
	}
	if b.Len() == 0 {
		return strings.TrimSpace(a.sql[start:end])
	}
	return b.String()
}

// exprSourceText returns a node's raw source text, preserving the spacing the
// engine uses when it names an unaliased expression output column.
func (a *Analyzer) exprSourceText(n nodes.Node) string {
	start, end, ok := a.exprSpan(n)
	if !ok {
		return ""
	}
	return strings.TrimSpace(a.sql[start:end])
}

// isInfixNode reports whether omni anchors the node's Loc at its operator.
func isInfixNode(n nodes.Node) bool {
	switch n.(type) {
	case *nodes.BinaryExpr, *nodes.BetweenExpr, *nodes.InExpr:
		return true
	default:
		return false
	}
}

// earliestStart returns the smallest source offset in the node's subtree.
func earliestStart(n nodes.Node) int {
	earliest := -1
	nodes.Inspect(n, func(c nodes.Node) bool {
		l := nodeLoc(c)
		if l.Start <= 0 || l.End <= l.Start {
			return true
		}
		if earliest < 0 || l.Start < earliest {
			earliest = l.Start
		}
		return true
	})
	return earliest
}

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// processSelectStatement processes a SELECT statement, set operation or
// parenthesized query.
func (a *Analyzer) processSelectStatement(stmt *nodes.SelectStmt) {
	if stmt == nil {
		return
	}
	if len(stmt.CTEs) > 0 {
		a.processCTEs(stmt.CTEs)
	}
	switch {
	case stmt.SetOp != nodes.SetOpNone:
		a.processSetOperation(stmt)
	case stmt.ParenSource != nil:
		a.processSelectStatement(stmt.ParenSource)
	default:
		a.processQuerySpecification(stmt)
	}
}

// processQuerySpecification processes FROM, the SELECT list, then emits edges.
func (a *Analyzer) processQuerySpecification(stmt *nodes.SelectStmt) {
	sp := a.currentScope()
	if values := valuesQueryPrimary(stmt); values != nil {
		// A VALUES query primary (`SELECT * FROM (VALUES ROW(1)) v`) has rows in
		// place of a select list, and those rows are the query's output.
		a.processValuesRows(values.Rows)
		a.generateEdges(sp)
		return
	}
	previousWindows := a.namedWindows
	a.namedWindows = namedWindowsOf(stmt.WindowClause)
	defer func() { a.namedWindows = previousWindows }()
	a.processFromClause(stmt.From)
	// A WHERE or HAVING predicate decides which rows the query emits without its
	// value reaching any output column, so it is recorded as an influence on the
	// statement's target rows rather than on a column.
	a.collectPredicates(stmt.Where, sp, model.NewFilterTransformation(a.exprTextOf(stmt.Where)), false)
	a.processSelectItemList(stmt.TargetList, sp, a.groupByKeys(stmt.GroupBy))
	// HAVING is the one clause that may name a select-list alias, so it resolves
	// one before falling back to the scope.
	a.collectPredicates(stmt.Having, sp, model.NewFilterTransformation(a.exprTextOf(stmt.Having)), true)
	a.generateEdges(sp)
}

// groupByKeys renders the GROUP BY items of a query specification in source
// order, the same way PartitionBy and OrderBy are rendered. A positional key
// stays "1" and an alias stays the alias, because that is what the query wrote.
// GROUP BY ... WITH ROLLUP has no representation here; the flag is not part of a
// key list.
func (a *Analyzer) groupByKeys(items []nodes.ExprNode) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, a.exprTextOf(item))
	}
	return keys
}

// processTableStmt processes a TABLE statement, which is a whole-table select.
func (a *Analyzer) processTableStmt(stmt *nodes.TableStmt) {
	if stmt == nil || stmt.Table == nil {
		return
	}
	sp := a.currentScope()
	tableRef := &scope.TableRef{
		Schema: stmt.Table.Schema,
		Table:  stmt.Table.Name,
		Alias:  stmt.Table.Alias,
	}
	a.attachColumnLookup(tableRef)
	sp.AddTable(tableRef)
	a.processStar(sp)
	a.generateEdges(sp)
}

// processCTEs processes a WITH clause.
func (a *Analyzer) processCTEs(ctes []*nodes.CommonTableExpr) {
	for _, cte := range ctes {
		a.processCTE(cte)
	}
}

// processCTE processes a single CTE.
func (a *Analyzer) processCTE(cte *nodes.CommonTableExpr) {
	if cte == nil {
		return
	}
	cteName := cte.Name
	definition := &scope.CTEDefinition{Name: cteName, Columns: cte.Columns}

	var cteLineage []model.ColumnRelation
	if cte.Select != nil {
		a.pushScope()
		if cte.Recursive {
			// In a recursive CTE the name refers to the rows the CTE has produced
			// so far, not to a stored relation. Registering it with no lineage —
			// the fixpoint is not modelled — makes every reference to it inside
			// its own body opaque, so neither a source nor a predicate resolves to
			// a base table that merely shares the name.
			a.currentScope().AddCTE(definition)
		}
		a.processSelectStatement(cte.Select)
		cteScope := a.popScope()

		// The body's influences belong to the CTE's rows, so they are held
		// against the definition and reach the statement only if it references
		// the CTE. An unreferenced CTE filters rows nobody reads.
		a.influences.BindCTE(definition, cteScope)

		outputColumns := cteScope.GetOutputColumns()
		names := scope.ExposedColumnNames(cte.Columns, outputColumns)
		for i, outputCol := range outputColumns {
			targetName := names[i]
			for _, source := range outputCol.Sources {
				resolutions, err := cteScope.ResolveColumnRefs(source.Ref)
				if err != nil {
					a.diagnostics.Unresolved("a CTE body", source.Ref)
					continue
				}
				for _, res := range resolutions {
					if a.flattenTempSourceLineage(cteScope, res.Relation, res.Ref.Column, cteName, targetName, source.Transform, &cteLineage) {
						continue
					}
					cteLineage = append(cteLineage, scope.NewLineageEdge(
						res.Ref.Schema, res.Ref.Table, res.Ref.Column,
						"", cteName, targetName,
						source.Transform,
						true, // CTE is temporary
					))
				}
			}
		}
	}

	definition.Lineage = cteLineage
	a.currentScope().AddCTE(definition)
}

// setOpArm is one leaf SELECT of a set-operation tree together with the chain of
// set-operation transformations that combine it into the statement's result,
// outermost first, so a nested operation is not labelled with only the outermost
// one.
type setOpArm struct {
	stmt  *nodes.SelectStmt
	chain []model.Transformation
}

// flattenSetOpArms flattens a set-operation tree into its leaf SELECTs in order.
func flattenSetOpArms(stmt *nodes.SelectStmt, chain []model.Transformation) []setOpArm {
	if stmt == nil {
		return nil
	}
	if stmt.SetOp != nodes.SetOpNone {
		inner := chain
		if transform, ok := setOpTransformation(stmt.SetOp, stmt.SetAll); ok {
			inner = algorithm.ArmChain(chain, transform)
		}
		return append(flattenSetOpArms(stmt.Left, inner), flattenSetOpArms(stmt.Right, inner)...)
	}
	if stmt.ParenSource != nil {
		return flattenSetOpArms(stmt.ParenSource, chain)
	}
	return []setOpArm{{stmt: stmt, chain: chain}}
}

// processSetOperation handles UNION/INTERSECT/EXCEPT by processing every arm in
// its own scope and merging their output columns positionally. Each arm's source
// references are resolved in the scope they were collected in and marked
// resolved, because the arm scope is gone by the time the enclosing CTE,
// derived table or statement resolves the merged columns.
func (a *Analyzer) processSetOperation(stmt *nodes.SelectStmt) {
	arms := flattenSetOpArms(stmt, nil)
	if len(arms) == 0 {
		return
	}

	baseScope := a.currentScope()
	var (
		allOutputColumns [][]scope.OutputColumn
		armTransforms    [][]model.Transformation
	)

	for i, arm := range arms {
		armTransforms = append(armTransforms, arm.chain)
		if i == 0 {
			a.processSetOpArm(arm.stmt)
			allOutputColumns = append(allOutputColumns, algorithm.ResolveOutputColumns(baseScope, baseScope.GetOutputColumns()))
			continue
		}
		// The arm starts a relation namespace of its own — the first arm's
		// relations are not visible in it — while still reading the CTEs the
		// statement declared.
		tempScope := scope.NewScopeWithDefinitions(baseScope.Parent(), baseScope)
		originalScope := a.currentScope()
		a.scopeStack[len(a.scopeStack)-1] = tempScope
		a.processSetOpArm(arm.stmt)
		a.scopeStack[len(a.scopeStack)-1] = originalScope
		// The arm's rows are part of the merged result, so its predicates reach
		// the operation's output.
		a.influences.Inherit(tempScope, baseScope)
		allOutputColumns = append(allOutputColumns, algorithm.ResolveOutputColumns(tempScope, tempScope.GetOutputColumns()))
	}

	algorithm.MergeSetOpColumns(baseScope, allOutputColumns, armTransforms)
	// A set operation nested in a CTE or derived table contributes its merged
	// columns to the parent; only a root-level operation emits result edges.
	a.generateEdges(baseScope)
}

// processSetOpArm processes one leaf arm of a set operation. The arm itself must
// not emit result edges; the merged operation does.
func (a *Analyzer) processSetOpArm(arm *nodes.SelectStmt) {
	previous := a.inSetOpArm
	a.inSetOpArm = true
	a.processSelectStatement(arm)
	a.inSetOpArm = previous
}

// setOpTransformation maps a set operation to its transformation.
func setOpTransformation(setOp nodes.SetOperation, all bool) (model.Transformation, bool) {
	switch setOp {
	case nodes.SetOpUnion:
		return model.NewUnionTransformation(all), true
	case nodes.SetOpIntersect:
		return model.NewIntersectTransformation(all), true
	case nodes.SetOpExcept:
		return model.NewExceptTransformation(all), true
	case nodes.SetOpNone:
		return model.Transformation{}, false
	default:
		return model.Transformation{}, false
	}
}

// ---------------------------------------------------------------------------
// FROM clause
// ---------------------------------------------------------------------------

// processFromClause processes the FROM clause.
func (a *Analyzer) processFromClause(from []nodes.TableExpr) {
	for _, te := range from {
		a.processTableExpr(te)
		// The relations have to be in scope before a join condition's columns
		// can be resolved.
		a.collectJoinPredicates(te)
	}
}

// collectJoinPredicates records the columns a join condition depends on. ON is
// recorded from its expression; USING names the shared column, which is a join
// key of every relation the two sides introduce.
func (a *Analyzer) collectJoinPredicates(te nodes.TableExpr) {
	join, ok := te.(*nodes.JoinClause)
	if !ok {
		return
	}
	a.collectJoinPredicates(join.Left)
	a.collectJoinPredicates(join.Right)

	sp := a.currentScope()
	if on, ok := join.Condition.(*nodes.OnCondition); ok {
		a.collectPredicates(on.Expr, sp, model.NewJoinTransformation(a.exprTextOf(on)), false)
		return
	}
	if using, ok := join.Condition.(*nodes.UsingCondition); ok {
		transform := model.NewJoinTransformation(a.exprTextOf(using))
		for _, side := range []nodes.TableExpr{join.Left, join.Right} {
			for _, ref := range joinSideRefs(side) {
				for _, column := range using.Columns {
					a.influences.Resolve(sp, scope.ColumnRef{Schema: ref.Schema, Table: ref.Table, Column: column}, transform, false)
				}
			}
		}
	}
}

// joinSideRefs returns the relation a join operand introduces, addressed the way
// a SQL reference addresses it: its alias when it has one, otherwise its name.
func joinSideRefs(te nodes.TableExpr) []scope.ColumnRef {
	switch t := te.(type) {
	case *nodes.TableRef:
		name := t.Alias
		if name == "" {
			name = t.Name
		}
		return []scope.ColumnRef{{Schema: t.Schema, Table: name}}
	case *nodes.JoinClause:
		return append(joinSideRefs(t.Left), joinSideRefs(t.Right)...)
	default:
		// A derived table's columns are traced, not addressed by name here.
		return nil
	}
}

// collectPredicates resolves every column a predicate depends on and records it
// as an influence on the rows the current scope produces. resolveAliases is set
// for a clause that may name a select-list alias, as HAVING does.
func (a *Analyzer) collectPredicates(expr nodes.ExprNode, sp *scope.Scope, transform model.Transformation, resolveAliases bool) {
	if expr == nil {
		return
	}
	for _, ref := range a.collectExprColumns(expr, sp) {
		a.influences.Resolve(sp, ref, transform, resolveAliases)
	}
}

// emitPredicateInfluences adds one edge per predicate column of sp to the rows
// the statement produces. The target column is empty: a predicate decides which
// rows are emitted, not the value of any one column.
func (a *Analyzer) emitPredicateInfluences(sp *scope.Scope, targetSchema, targetTable string) {
	a.influences.Emit(sp, targetSchema, targetTable, targetTable == resultTableName, algorithm.Emitter{
		Trace:   a.traceThroughTableLineage,
		AddEdge: a.addRelation,
		NewEdge: scope.NewLineageEdge,
	})
}

// processTableExpr processes a table reference, join, derived table or table
// function.
func (a *Analyzer) processTableExpr(te nodes.TableExpr) {
	switch t := te.(type) {
	case *nodes.TableRef:
		a.processSingleTableRef(t)
	case *nodes.JoinClause:
		a.processTableExpr(t.Left)
		a.processTableExpr(t.Right)
	case *nodes.SubqueryExpr:
		a.processDerivedTable(t)
	case *nodes.JsonTableExpr:
		a.processJSONTable(t)
	default:
		// Function-in-FROM and other table expressions carry no lineage.
	}
}

// processJsonTable registers JSON_TABLE in FROM as a query-local relation. Its
// columns are the ones the COLUMNS clause declares and every value is extracted
// from the JSON document expression, so the columns that expression reads are the
// source of each of them: `SELECT * FROM t, JSON_TABLE(t.doc, '$[*]' COLUMNS (x
// INT PATH '$.x')) jt` reports `t.doc`.
func (a *Analyzer) processJSONTable(jt *nodes.JsonTableExpr) {
	if jt == nil {
		return
	}
	alias := jt.Alias
	if alias == "" {
		alias = "json_table"
	}
	sp := a.currentScope()
	columns := jsonTableColumnNames(jt.Columns)
	var sources []scope.ColumnRef
	if jt.Expr != nil {
		sources = a.collectExprColumns(jt.Expr, sp)
	}
	lineage := make([]model.ColumnRelation, 0, len(columns)*len(sources))
	for _, column := range columns {
		for _, ref := range sources {
			resolutions, err := sp.ResolveColumnRefs(ref)
			if err != nil {
				a.diagnostics.Unresolved("a JSON_TABLE document expression", ref)
				continue
			}
			for _, res := range resolutions {
				if a.flattenTempSourceLineage(sp, res.Relation, res.Ref.Column, alias, column, nil, &lineage) {
					continue
				}
				lineage = append(lineage, scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					"", alias, column,
					nil,
					true, // the table function is not a stored object
				))
			}
		}
	}

	tableRef := &scope.TableRef{
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		Lineage:    lineage,
	}
	scope.AttachTempColumnLookup(tableRef, columns)
	sp.AddTable(tableRef)
}

// jsonTableColumnNames collects the names a COLUMNS clause declares, including the
// ones a NESTED PATH contributes.
func jsonTableColumnNames(columns []*nodes.JsonTableColumn) []string {
	out := make([]string, 0, len(columns))
	for _, col := range columns {
		if col == nil || col.Name == "" {
			continue
		}
		out = append(out, col.Name)
		out = append(out, jsonTableColumnNames(col.NestedCols)...)
	}
	return out
}

// processSingleTableRef adds a base table or CTE reference to the current scope.
func (a *Analyzer) processSingleTableRef(ref *nodes.TableRef) {
	if ref == nil {
		return
	}
	tableName := ref.Name
	alias := ref.Alias

	// A CTE is query-local and is never qualified, so a qualified reference
	// always names a real table: `db1.t` must not be mistaken for a CTE named t.
	if ref.Schema == "" {
		if cte, ok := a.currentScope().FindCTE(tableName); ok {
			if alias == "" {
				alias = tableName
			}
			tableRef := &scope.TableRef{
				Table:   tableName,
				Alias:   alias,
				IsCTE:   true,
				Lineage: cte.Lineage,
			}
			scope.AttachTempColumnLookup(tableRef, cte.Columns)
			a.currentScope().AddTable(tableRef)
			// Reading the CTE's rows carries the predicates that shaped them.
			a.influences.InheritCTE(a.currentScope(), cte)
			return
		}
	}

	if alias == "" {
		alias = tableName
	}
	tableRef := &scope.TableRef{
		Schema: ref.Schema,
		Table:  tableName,
		Alias:  alias,
	}
	a.attachColumnLookup(tableRef)
	a.currentScope().AddTable(tableRef)
}

// processDerivedTable processes a derived table (subquery in FROM).
func (a *Analyzer) processDerivedTable(sub *nodes.SubqueryExpr) {
	if sub == nil || sub.Select == nil {
		return
	}
	alias := sub.Alias

	a.pushScope()
	a.processSelectStatement(sub.Select)
	subqueryScope := a.popScope()
	// The derived table's rows are part of the enclosing query's rows, so the
	// predicates that shaped them reach its output.
	a.influences.Inherit(subqueryScope, a.currentScope())

	outputColumns := subqueryScope.GetOutputColumns()
	names := scope.ExposedColumnNames(sub.Columns, outputColumns)
	derivedLineage := make([]model.ColumnRelation, 0)
	for i, col := range outputColumns {
		colName := names[i]
		if colName == "" {
			colName = "column"
		}
		for _, source := range col.Sources {
			resolutions, err := subqueryScope.ResolveColumnRefs(source.Ref)
			if err != nil {
				a.diagnostics.Unresolved("a derived table", source.Ref)
				continue
			}
			for _, res := range resolutions {
				if a.flattenTempSourceLineage(subqueryScope, res.Relation, res.Ref.Column, alias, colName, source.Transform, &derivedLineage) {
					continue
				}
				derivedLineage = append(derivedLineage, scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					"", alias, colName,
					source.Transform,
					true, // Subquery is temporary
				))
			}
		}
	}

	tableRef := &scope.TableRef{
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		Lineage:    derivedLineage,
	}
	scope.AttachTempColumnLookup(tableRef, names)
	a.currentScope().AddTable(tableRef)
}

// ---------------------------------------------------------------------------
// SELECT list
// ---------------------------------------------------------------------------

// processSelectItemList processes the SELECT item list. groupKeys are the GROUP
// BY keys of the query specification these items belong to; they are recorded on
// the transformations of the items that aggregate.
func (a *Analyzer) processSelectItemList(items []nodes.ExprNode, sp *scope.Scope, groupKeys []string) {
	for _, item := range items {
		switch it := item.(type) {
		case *nodes.StarExpr:
			a.processStar(sp)
		case *nodes.ResTarget:
			a.processSelectExpr(it.Val, it.Name, sp, groupKeys)
		case *nodes.ColumnRef:
			if it.Star {
				a.processTableWildcard(it, sp)
				continue
			}
			a.processSelectExpr(it, "", sp, groupKeys)
		default:
			a.processSelectExpr(item, "", sp, groupKeys)
		}
	}
}

// processStar expands SELECT * against every table in scope. Every relation is
// expanded, so two same-named tables from different databases each contribute.
func (a *Analyzer) processStar(sp *scope.Scope) {
	for _, tableRef := range sp.Tables() {
		if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
			if a.expandWildcardWithCatalog(tableRef, sp) {
				continue
			}
		}
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:   wildcardColumn,
			Sources: scope.NewColumnSources([]scope.ColumnRef{scope.WildcardSourceRef(tableRef)}, nil),
		})
	}
}

// processTableWildcard expands table.* against the table the reference names,
// including the database or schema qualifier it carries.
func (a *Analyzer) processTableWildcard(cr *nodes.ColumnRef, sp *scope.Scope) {
	tableRef, ok := sp.FindRelation(scope.RelationKey{Qualifier: cr.Schema, Name: cr.Table})
	if !ok {
		// A wildcard whose qualifier names no relation has no source to expand;
		// the note is what keeps the empty result from reading as a complete one.
		a.diagnostics.UnresolvedQualifier("a wildcard qualifier", cr.Table)
		return
	}
	if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
		if a.expandWildcardWithCatalog(tableRef, sp) {
			return
		}
	}
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:   wildcardColumn,
		Sources: scope.NewColumnSources([]scope.ColumnRef{scope.WildcardSourceRef(tableRef)}, nil),
	})
}

// processSelectExpr turns one select expression into an output column.
func (a *Analyzer) processSelectExpr(expr nodes.ExprNode, alias string, sp *scope.Scope, groupKeys []string) {
	if expr == nil {
		return
	}
	sp.AddOutputColumn(a.outputColumnFor(expr, alias, sp, groupKeys))
}

// outputColumnFor builds the output column one expression produces. It is separate
// from processSelectExpr because a VALUES row needs the column without adding it:
// several rows derive the same column and have to merge into it.
func (a *Analyzer) outputColumnFor(expr nodes.ExprNode, alias string, sp *scope.Scope, groupKeys []string) scope.OutputColumn {
	exprText := a.exprTextOf(expr)
	if alias == "" {
		alias = a.inferredColumnAlias(expr, exprText)
	}
	sourceColumns := a.collectExprColumns(expr, sp)
	isDerived := !isPlainColumnRef(expr)

	// A table-wide aggregate such as COUNT(*) depends on the rows of every table
	// in scope even though it names no column. Any other source-less expression
	// (a literal, NOW(), a source-less function) depends on no column at all and
	// must not invent a dependency.
	if isDerived && len(sourceColumns) == 0 && containsAggregateCall(expr) {
		for _, tableRef := range sp.Tables() {
			sourceColumns = append(sourceColumns, scope.WildcardSourceRef(tableRef))
		}
	}

	outputCol := scope.OutputColumn{
		Alias:     alias,
		Sources:   scope.NewColumnSources(sourceColumns, nil),
		IsDerived: isDerived,
	}
	if isDerived {
		transform := a.analyzeExpressionOperator(expr)
		// The GROUP BY keys describe how an aggregate in this select item was
		// computed, so they ride on every transformation the item produced: the
		// outermost node is often not the aggregate itself, since SUM(x) + 1 is
		// an operator and a CASE or a function can wrap one too. An expression
		// with no group aggregate of its own records nothing, which also keeps a
		// windowed aggregate's OVER clause from being confused with GROUP BY.
		if len(groupKeys) > 0 && containsGroupAggregate(expr) {
			for i := range transform {
				transform[i].GroupKeys = groupKeys
			}
		}
		outputCol.SetTransform(transform)
	}
	return outputCol
}

// ---------------------------------------------------------------------------
// Edge generation
// ---------------------------------------------------------------------------

// generateEdges creates ColumnRelation objects from the scope's output columns.
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	if a.realTarget || a.inSetOpArm {
		return
	}
	// Only the root query emits edges to the final result; subqueries/CTEs rely
	// on their lineage being traced when the parent references them.
	if sp == nil || sp.Parent() != nil {
		return
	}

	for _, outputCol := range sp.GetOutputColumns() {
		a.emitSources(sp, outputCol.Sources, "", resultTableName, outputCol.Alias)
	}
	a.emitPredicateInfluences(sp, "", resultTableName)
}

// generateEdgesForDataModification maps SELECT output columns onto the target
// table columns of an INSERT/REPLACE.
func (a *Analyzer) generateEdgesForDataModification(targetSchema, targetTable string, targetColumns []string) {
	sp := a.currentScope()
	for i, outputCol := range sp.GetOutputColumns() {
		// A SELECT with more columns than the insert column list writes only the
		// listed ones; the extra outputs are discarded.
		if len(targetColumns) > 0 && i >= len(targetColumns) {
			break
		}
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			targetColName = targetColumns[i]
		}
		a.emitSources(sp, outputCol.Sources, targetSchema, targetTable, targetColName)
	}
	a.emitPredicateInfluences(sp, targetSchema, targetTable)
}

// emitSources resolves source columns and adds one relation per source. An
// unqualified name that several relations in scope own contributes one relation
// per owner: each of them really is a source of the value, which is what a
// coalesced USING or NATURAL JOIN column looks like.
func (a *Analyzer) emitSources(sp *scope.Scope, sources []scope.ColumnSource, targetSchema, targetTable, targetColumn string) {
	for _, source := range sources {
		resolutions, err := sp.ResolveColumnRefs(source.Ref)
		if err != nil {
			a.diagnostics.Unresolved("an output column", source.Ref)
			continue
		}
		for _, res := range resolutions {
			// A CTE or derived table contributes the lineage of its own columns:
			// there is no stored relation to point an edge at.
			if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
				a.traceThroughTableLineage(res.Relation, res.Ref.Column, targetSchema, targetTable, targetColumn, source.Transform)
				continue
			}
			isTemp := targetTable == resultTableName
			a.addRelation(scope.NewLineageEdge(
				res.Ref.Schema, res.Ref.Table, res.Ref.Column,
				targetSchema, targetTable, targetColumn,
				source.Transform,
				isTemp,
			))
		}
	}
}

// traceThroughTableLineage traces lineage through a CTE or subquery to a target.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName string, targetSchema, targetTable, targetColumn string, transform []model.Transformation) {
	algorithm.TraceThroughTableLineageToTarget(scope.NewLineageEdge, a.addRelation, tableRef, columnName, targetSchema, targetTable, targetColumn, transform)
}

// flattenTempSourceLineage expands a source column that belongs to a query-local
// relation into the base-table lineage behind it. It reports whether the source
// was query-local and has been handled.
func (*Analyzer) flattenTempSourceLineage(sp *scope.Scope, relation *scope.TableRef, columnName, targetTable, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	return algorithm.FlattenTempSourceLineage(scope.NewLineageEdge, sp, relation, columnName, targetTable, targetColumn, transform, lineage)
}

// ---------------------------------------------------------------------------
// INSERT / REPLACE
// ---------------------------------------------------------------------------

// processInsertStatement processes INSERT and REPLACE statements.
func (a *Analyzer) processInsertStatement(stmt *nodes.InsertStmt) {
	if stmt == nil || stmt.Table == nil {
		return
	}
	targetSchema := stmt.Table.Schema
	targetTable := stmt.Table.Name
	targetColumns := columnNames(stmt.Columns)

	switch {
	case stmt.Select != nil:
		previous := a.realTarget
		a.realTarget = true
		a.processSelectStatement(stmt.Select)
		a.realTarget = previous
	case stmt.TableSource != nil && stmt.TableSource.Table != nil:
		// INSERT ... TABLE t is INSERT ... SELECT * FROM t.
		tr := stmt.TableSource.Table
		alias := tr.Alias
		if alias == "" {
			alias = tr.Name
		}
		tableRef := &scope.TableRef{Schema: tr.Schema, Table: tr.Name, Alias: alias}
		a.attachColumnLookup(tableRef)
		a.currentScope().AddTable(tableRef)
		a.processStar(a.currentScope())
	case len(stmt.Values) > 0:
		// A VALUES row can hold a subquery, and what it selects is a source of the
		// column it is written into. Each row derives the same columns, so they
		// merge column by column; a row of literals contributes nothing, which is
		// why `INSERT INTO t VALUES (1, 2)` still records no source.
		a.processValuesRows(stmt.Values)
	case len(stmt.SetList) > 0:
		// `INSERT ... SET col = expr` is the other assignment form. Each
		// assignment names the column it writes, so the value's sources belong to
		// that column rather than to a position; a value that is a subquery is a
		// source, a constant is not.
		a.processSetList(stmt.SetList)
	default:
		// A shape with nothing to resolve, such as DEFAULT VALUES.
	}

	a.generateEdgesForDataModification(targetSchema, targetTable, targetColumns)

	if !stmt.IsReplace && len(stmt.OnDuplicateKey) > 0 {
		a.processInsertUpdateList(stmt, targetSchema, targetTable, targetColumns)
	}
}

// processValuesStatement processes a standalone VALUES statement. Its rows are the
// statement's output, so a value that reads a subquery becomes lineage of the
// column that row produces.
func (a *Analyzer) processValuesStatement(stmt *nodes.ValuesStmt) {
	if stmt == nil {
		return
	}
	a.processValuesRows(stmt.Rows)
	a.generateEdges(a.currentScope())
}

// processSetList records what each assignment of an `INSERT ... SET` writes, the
// same way a VALUES row does: the assignment names its target column, and the
// value expression may hold a subquery that reads another relation.
func (a *Analyzer) processSetList(assignments []*nodes.Assignment) {
	sp := a.currentScope()
	if sp == nil {
		return
	}
	columns := make([]scope.OutputColumn, 0, len(assignments))
	for _, elem := range assignments {
		if elem == nil || elem.Column == nil || elem.Column.Column == "" {
			continue
		}
		if elem.Value == nil {
			columns = append(columns, scope.OutputColumn{Alias: elem.Column.Column})
			continue
		}
		columns = append(columns, a.outputColumnFor(elem.Value, elem.Column.Column, sp, nil))
	}
	sp.SetOutputColumns(columns)
}

// processValuesRows records what each VALUES row writes into each column of the row
// it produces, which generateEdgesForDataModification then maps onto the target's
// columns. A row's subquery resolves in the statement's own scope, so a CTE or a
// derived table it reads is traced through its lineage like any other source.
//
// The column names are positional because MySQL gives a VALUES column none; the
// target's column list decides wherever the statement writes one.
func (a *Analyzer) processValuesRows(rows [][]nodes.ExprNode) {
	sp := a.currentScope()
	if sp == nil {
		return
	}
	var columns []scope.OutputColumn
	for _, row := range rows {
		for len(columns) < len(row) {
			// MySQL names an unnamed VALUES column column_0, column_1, …; the
			// target's column list decides whenever the statement writes one.
			columns = append(columns, scope.OutputColumn{Alias: fmt.Sprintf("column_%d", len(columns))})
		}
		for i, expr := range row {
			built := a.outputColumnFor(expr, columns[i].Alias, sp, nil)
			columns[i].Sources = append(columns[i].Sources, built.Sources...)
			columns[i].IsDerived = columns[i].IsDerived || built.IsDerived
		}
	}
	sp.SetOutputColumns(columns)
}

// processInsertUpdateList processes the ON DUPLICATE KEY UPDATE clause.
func (a *Analyzer) processInsertUpdateList(stmt *nodes.InsertStmt, targetSchema, targetTable string, targetColumns []string) {
	sp := a.currentScope()
	// The VALUES(col) and row-alias sources are resolved in the scope the INSERT's
	// query ran in, which is what they mean, so the map is built before the
	// clause's own scope gains the target table below.
	insertSources := a.insertSourceMap(stmt, sp, targetColumns)
	// The clause's own scope contains the target table, which is how MySQL
	// resolves the row being updated: `a = a + 1` reads the target's own column,
	// and the statement is rejected as ambiguous (error 1052, verified on 8.3.0)
	// when the SELECT's table owns the name too. Registering it makes that
	// resolution real instead of a guess.
	a.processSingleTableRef(&nodes.TableRef{Schema: targetSchema, Name: targetTable, Alias: targetTable})
	for _, elem := range stmt.OnDuplicateKey {
		if elem == nil || elem.Column == nil || elem.Value == nil {
			continue
		}
		sourceColumns := a.collectUpsertSources(elem.Value, sp, insertSources)
		if len(sourceColumns) == 0 {
			continue
		}
		transform := a.analyzeExpressionOperator(elem.Value)
		for _, sourceCol := range sourceColumns {
			resolutions, err := sp.ResolveColumnRefs(sourceCol)
			if err != nil {
				a.diagnostics.Unresolved("an upsert assignment", sourceCol)
				continue
			}
			isTemp := targetTable == resultTableName
			for _, res := range resolutions {
				// A derived table contributes the lineage of its own columns:
				// there is no stored relation to point an edge at.
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineage(res.Relation, res.Ref.Column, targetSchema, targetTable, elem.Column.Column, transform)
					continue
				}
				a.addRelation(scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, elem.Column.Column,
					transform,
					isTemp,
				))
			}
		}
	}
}

// insertSourceMap maps every name an upsert assignment can use for the value the
// insert proposed onto the sources of that value. `VALUES(col)` names the target
// column, and a MySQL 8.0.19 row alias (`AS new`, `AS new(a, b) ON DUPLICATE KEY
// UPDATE b = new.a`) names the proposed row and its own column list. Both are keys
// of one map, looked up by the name the assignment wrote, because both mean "the
// value this insert proposed" rather than a column of a relation in scope.
//
// Each source is resolved here, in the scope the INSERT's query ran in: the
// clause's own scope also holds the target table, and an unqualified name both it
// and the query own would otherwise be attributed to whichever comes first by name.
func (a *Analyzer) insertSourceMap(stmt *nodes.InsertStmt, sp *scope.Scope, targetColumns []string) map[string][]scope.ColumnRef {
	out := make(map[string][]scope.ColumnRef)
	rowAliasName, aliasColumns := rowAliasNames(stmt)
	rowAlias := strings.ToLower(rowAliasName)
	for i, outputCol := range sp.GetOutputColumns() {
		if len(targetColumns) > 0 && i >= len(targetColumns) {
			break
		}
		name := outputCol.Alias
		if i < len(targetColumns) {
			name = targetColumns[i]
		}
		sources := a.resolveWithin(sp, scope.Refs(outputCol.Sources))
		out[strings.ToLower(name)] = append(out[strings.ToLower(name)], sources...)
		if rowAlias == "" || name == "" {
			continue
		}
		aliasColumn := name
		if i < len(aliasColumns) && aliasColumns[i] != "" {
			aliasColumn = aliasColumns[i]
		}
		key := rowAlias + "." + strings.ToLower(aliasColumn)
		out[key] = append(out[key], sources...)
	}
	return out
}

// proposedValueKey is the key an upsert assignment's column reference is looked up
// by: a row-alias reference is qualified (`new.a`), a VALUES(col) reference is not.
func proposedValueKey(table, column string) string {
	column = strings.ToLower(column)
	if table == "" {
		return column
	}
	return strings.ToLower(table) + "." + column
}

// resolveWithin resolves every reference against the scope it came from and marks
// it resolved, so a later lookup in a scope that does not share it returns the
// same column instead of guessing again. An unresolvable reference is dropped and
// reported: it is one of the sources the insert writes, and losing it silently
// would make the upsert map look complete.
func (a *Analyzer) resolveWithin(sp *scope.Scope, refs []scope.ColumnRef) []scope.ColumnRef {
	out := make([]scope.ColumnRef, 0, len(refs))
	for _, ref := range refs {
		resolutions, err := sp.ResolveColumnRefs(ref)
		if err != nil {
			a.diagnostics.Unresolved("an insert source", ref)
			continue
		}
		for _, res := range resolutions {
			resolved := res.Ref
			resolved.Resolved = true
			out = append(out, resolved)
		}
	}
	return out
}

// collectUpsertSources collects the sources of an upsert assignment value.
// VALUES(col) names the value the insert proposed for col rather than a column of
// any relation in scope, so it maps through insertSources instead of the scope;
// every other reference, subqueries included, is collected the way any other
// expression's sources are. Reading the value's subqueries as plain columns of
// the enclosing scope used to attribute their columns to whichever relation the
// fallback picked — `a = (b IN (SELECT x FROM other))` recorded `stage.x` — and
// dropped what the subquery really reads.
func (a *Analyzer) collectUpsertSources(expr nodes.ExprNode, sp *scope.Scope, insertSources map[string][]scope.ColumnRef) []scope.ColumnRef {
	return a.collectExpressionSources(expr, sp, insertSources)
}

// ---------------------------------------------------------------------------
// CREATE TABLE / VIEW
// ---------------------------------------------------------------------------

// processCreateTable processes CREATE TABLE ... AS SELECT.
func (a *Analyzer) processCreateTable(stmt *nodes.CreateTableStmt) {
	if stmt == nil || stmt.Table == nil || stmt.Select == nil {
		return
	}
	targetSchema := stmt.Table.Schema
	targetTable := stmt.Table.Name

	previous := a.realTarget
	a.realTarget = true
	a.processSelectStatement(stmt.Select)
	a.realTarget = previous

	sp := a.currentScope()
	for _, outputCol := range sp.GetOutputColumns() {
		a.emitSources(sp, outputCol.Sources, targetSchema, targetTable, outputCol.Alias)
	}
	a.emitPredicateInfluences(sp, targetSchema, targetTable)
}

// processCreateView processes CREATE VIEW.
func (a *Analyzer) processCreateView(stmt *nodes.CreateViewStmt) {
	if stmt == nil {
		return
	}
	a.processViewBody(stmt.Name, stmt.Columns, stmt.Select)
}

// processAlterView processes ALTER VIEW, which replaces the view's definition and
// therefore produces the same lineage as the CREATE it stands in for. The runner
// wraps a stored view definition as CREATE VIEW, so an ALTER reaches the analyzer
// only as a MANUAL_SQL statement; it used to be dispatched nowhere and produced no
// lineage at all.
func (a *Analyzer) processAlterView(stmt *nodes.AlterViewStmt) {
	if stmt == nil {
		return
	}
	a.processViewBody(stmt.Name, stmt.Columns, stmt.Select)
}

// processViewBody emits the lineage of a view definition onto the view it defines.
func (a *Analyzer) processViewBody(name *nodes.TableRef, explicitColumnNames []string, query *nodes.SelectStmt) {
	if name == nil || query == nil {
		return
	}
	targetView := name.Name
	targetSchema := name.Schema

	previous := a.realTarget
	a.realTarget = true
	a.processSelectStatement(query)
	a.realTarget = previous

	sp := a.currentScope()
	for i, outputCol := range sp.GetOutputColumns() {
		targetColName := outputCol.Alias
		if i < len(explicitColumnNames) {
			targetColName = explicitColumnNames[i]
		}
		a.emitSources(sp, outputCol.Sources, targetSchema, targetView, targetColName)
	}
	a.emitPredicateInfluences(sp, targetSchema, targetView)
}

// ---------------------------------------------------------------------------
// UPDATE
// ---------------------------------------------------------------------------

// processUpdateStatement processes UPDATE statements.
func (a *Analyzer) processUpdateStatement(stmt *nodes.UpdateStmt) {
	if stmt == nil {
		return
	}
	for _, te := range stmt.Tables {
		a.processTableExpr(te)
	}
	a.processUpdateList(stmt.SetList)
}

// processUpdateList processes the SET clause in UPDATE statements.
func (a *Analyzer) processUpdateList(assignments []*nodes.Assignment) {
	sp := a.currentScope()
	for _, elem := range assignments {
		if elem == nil || elem.Column == nil {
			continue
		}
		targetCol := scope.ColumnRef{Schema: elem.Column.Schema, Table: elem.Column.Table, Column: elem.Column.Column}
		// A SET target has to name exactly one relation. An unqualified name two
		// relations own is one MySQL rejects (error 1052, verified on 8.3.0), so a
		// statement that reaches here with several owners is not one MySQL would
		// run; writing the value into every owner would invent edges for tables the
		// statement never updates. A qualified target names the relation it
		// updates, which is what a multi-table UPDATE setting columns of two tables
		// relies on.
		targets, err := sp.ResolveColumnRefs(targetCol)
		if err != nil {
			a.diagnostics.Unresolved("an assignment target", targetCol)
			continue
		}
		if len(targets) != 1 {
			// MySQL rejects a SET target two relations own (error 1052), so the
			// assignment has no target to write; writing it into every owner would
			// invent edges for tables the statement never updates.
			a.diagnostics.Ambiguous("an assignment target", targetCol)
			continue
		}
		resolved := targets[0].Ref

		var sourceColumns []scope.ColumnRef
		var transform []model.Transformation
		if elem.Value != nil {
			sourceColumns = a.collectExprColumns(elem.Value, sp)
		}
		if len(sourceColumns) == 0 {
			// A constant assignment has no column source; the row is still
			// rewritten, so the relation is recorded against the whole table.
			sourceColumns = []scope.ColumnRef{{
				Schema:   resolved.Schema,
				Table:    resolved.Table,
				Column:   wildcardColumn,
				Resolved: true,
			}}
			if elem.Value != nil {
				transform = a.analyzeExpressionOperator(elem.Value)
			}
		} else {
			transform = a.analyzeExpressionOperator(elem.Value)
		}

		for _, sourceCol := range sourceColumns {
			resolutions, err := sp.ResolveColumnRefs(sourceCol)
			if err != nil {
				a.diagnostics.Unresolved("an assignment", sourceCol)
				continue
			}
			isTemp := resolved.Table == resultTableName
			for _, res := range resolutions {
				// A CTE or derived table contributes the lineage of its own
				// columns: there is no stored relation to point an edge at.
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineage(res.Relation, res.Ref.Column, resolved.Schema, resolved.Table, resolved.Column, transform)
					continue
				}
				a.addRelation(scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					resolved.Schema, resolved.Table, resolved.Column,
					transform,
					isTemp,
				))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// DELETE
// ---------------------------------------------------------------------------

// processDeleteStatement processes DELETE statements (single and multi-table).
func (a *Analyzer) processDeleteStatement(stmt *nodes.DeleteStmt) {
	if stmt == nil {
		return
	}
	multi := len(stmt.Using) > 0
	if multi {
		// In a multi-table DELETE the USING clause holds the join; the entries in
		// Tables are the aliases to delete from and are not themselves tables.
		for _, te := range stmt.Using {
			a.processTableExpr(te)
		}
	} else {
		// A single-table DELETE names its target in Tables; register it so the
		// WHERE columns resolve against a real table.
		for _, te := range stmt.Tables {
			a.processTableExpr(te)
		}
	}

	sp := a.currentScope()
	var targetTables []scope.TableRef
	for _, te := range stmt.Tables {
		tr, ok := te.(*nodes.TableRef)
		if !ok {
			continue
		}
		targetTables = append(targetTables, scope.TableRef{Schema: tr.Schema, Table: tr.Name, Alias: tr.Alias})
	}
	if len(targetTables) == 0 {
		for _, tableRef := range sp.Tables() {
			targetTables = append(targetTables, *tableRef)
			break
		}
	}

	if stmt.Where == nil {
		return
	}
	conditionColumns := a.collectExprColumns(stmt.Where, sp)
	whereText := a.exprTextOf(stmt.Where)

	for _, targetTable := range targetTables {
		actualTargetTable := targetTable
		if targetTable.Alias != "" {
			if foundTable, ok := sp.FindRelation(scope.RelationKey{Qualifier: targetTable.Schema, Name: targetTable.Alias}); ok {
				actualTargetTable = *foundTable
			}
		} else if foundTable, ok := sp.FindRelation(scope.RelationKey{Qualifier: targetTable.Schema, Name: targetTable.Table}); ok {
			actualTargetTable = *foundTable
		}
		for _, condCol := range conditionColumns {
			// An unresolvable condition column used to fall back to the reference
			// itself, which emitted an edge from a relation the statement never
			// names. PostgreSQL drops it (P2-2); so does this now.
			resolutions, err := sp.ResolveColumnRefs(condCol)
			if err != nil {
				a.diagnostics.Unresolved("a DELETE condition", condCol)
				continue
			}
			transform := []model.Transformation{
				model.NewDeleteTransformation(whereText),
			}
			isTemp := actualTargetTable.Table == resultTableName
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineage(res.Relation, res.Ref.Column, actualTargetTable.Schema, actualTargetTable.Table, deletionFieldName, transform)
					continue
				}
				a.addRelation(scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					actualTargetTable.Schema, actualTargetTable.Table, deletionFieldName,
					transform,
					isTemp,
				))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// LOAD DATA
// ---------------------------------------------------------------------------

// processLoadStatement processes LOAD DATA INFILE statements.
func (a *Analyzer) processLoadStatement(stmt *nodes.LoadDataStmt) {
	if stmt == nil || stmt.Table == nil {
		return
	}
	targetTable := stmt.Table.Name
	targetSchema := stmt.Table.Schema
	sourceFile := fileSourceMarker

	targetColumns := loadDataTargetColumns(stmt.Columns)
	if len(stmt.SetList) > 0 {
		a.processLoadDataSetClause(stmt.SetList, targetSchema, targetTable, targetColumns, sourceFile)
	}

	isTemp := targetTable == resultTableName
	if len(targetColumns) == 0 {
		a.addRelation(scope.NewLineageEdge(
			"", sourceFile, wildcardColumn,
			targetSchema, targetTable, wildcardColumn,
			nil,
			isTemp,
		))
		return
	}
	for i, colName := range targetColumns {
		a.addRelation(scope.NewLineageEdge(
			"", sourceFile, fmt.Sprintf("col%d", i+1),
			targetSchema, targetTable, colName,
			nil,
			isTemp,
		))
	}
}

// processLoadDataSetClause processes the SET clause in LOAD DATA statements.
func (a *Analyzer) processLoadDataSetClause(assignments []*nodes.Assignment, targetSchema, targetTable string, loadedColumns []string, sourceFile string) {
	sp := a.currentScope()
	for _, elem := range assignments {
		if elem == nil || elem.Column == nil {
			continue
		}
		targetCol := scope.ColumnRef{Schema: elem.Column.Schema, Table: elem.Column.Table, Column: elem.Column.Column}

		var sourceColumns []scope.ColumnRef
		var transform []model.Transformation
		if elem.Value != nil {
			sourceColumns = a.collectExprColumns(elem.Value, sp)
			transform = a.analyzeExpressionOperator(elem.Value)
		}
		if len(sourceColumns) == 0 {
			sourceColumns = []scope.ColumnRef{{
				Table:  sourceFile,
				Column: wildcardColumn,
			}}
		}

		for _, sourceCol := range sourceColumns {
			// A column the file provides wins over any table that happens to
			// share its name; an unresolvable one is a file source too.
			isFromFile := slices.Contains(loadedColumns, sourceCol.Column)
			isTemp := targetTable == resultTableName
			if !isFromFile {
				if resolutions, err := sp.ResolveColumnRefs(sourceCol); err == nil {
					for _, res := range resolutions {
						a.addRelation(scope.NewLineageEdge(
							res.Ref.Schema, res.Ref.Table, res.Ref.Column,
							targetSchema, targetTable, targetCol.Column,
							transform,
							isTemp,
						))
					}
					continue
				}
			}
			a.addRelation(scope.NewLineageEdge(
				"", sourceFile, sourceCol.Column,
				targetSchema, targetTable, targetCol.Column,
				transform,
				isTemp,
			))
		}
	}
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

// collectExprColumns collects the column references an expression depends on,
// expanding subqueries into their own output columns' sources. Subquery bodies
// are analyzed in their own scope so their columns are not attributed to the
// enclosing FROM relations. A subquery's filter columns are not included, which
// matches the rule that a SELECT's WHERE clause is not lineage.
func (a *Analyzer) collectExprColumns(expr nodes.ExprNode, sp *scope.Scope) []scope.ColumnRef {
	return a.collectExpressionSources(expr, sp, nil)
}

// collectExpressionSources is collectExprColumns with an optional upsert
// mapping: when upsertValues is set, VALUES(col) reports the sources the insert
// proposed for col rather than a column of a relation in scope.
func (a *Analyzer) collectExpressionSources(expr nodes.ExprNode, sp *scope.Scope, upsertValues map[string][]scope.ColumnRef) []scope.ColumnRef {
	columns := make([]scope.ColumnRef, 0)
	if expr == nil {
		return columns
	}
	var subqueries []*nodes.SelectStmt
	nodes.Inspect(expr, func(n nodes.Node) bool {
		switch x := n.(type) {
		case *nodes.FuncCallExpr:
			// VALUES(col) only means anything in an upsert, and there it names the
			// proposed row rather than anything the scope can resolve.
			if upsertValues != nil && strings.EqualFold(x.Name, "VALUES") {
				for _, arg := range x.Args {
					if cr, ok := arg.(*nodes.ColumnRef); ok && cr.Column != "" {
						columns = append(columns, upsertValues[proposedValueKey("", cr.Column)]...)
					}
				}
				return false
			}
			// `OVER w` keeps the whole window definition — PARTITION BY, ORDER BY
			// and the frame bounds — in the statement's WINDOW clause, which the
			// walk over this expression cannot reach. The columns those clauses
			// name decide the window as much as the function's own arguments do.
			for _, def := range a.namedWindowDefinitions(x.Over) {
				for _, expr := range def.PartitionBy {
					columns = append(columns, a.collectExpressionSources(expr, sp, upsertValues)...)
				}
				for _, item := range def.OrderBy {
					if item != nil && item.Expr != nil {
						columns = append(columns, a.collectExpressionSources(item.Expr, sp, upsertValues)...)
					}
				}
				if frame := def.Frame; frame != nil {
					if frame.Start != nil {
						columns = append(columns, a.collectExpressionSources(frame.Start.Offset, sp, upsertValues)...)
					}
					if frame.End != nil {
						columns = append(columns, a.collectExpressionSources(frame.End.Offset, sp, upsertValues)...)
					}
				}
			}
		case *nodes.SubqueryExpr:
			if x.Select != nil {
				subqueries = append(subqueries, x.Select)
			}
			return false // handled as a unit below
		case *nodes.InExpr:
			// The value list and the left operand are ordinary columns; only the
			// subquery operand is a scope of its own.
			columns = append(columns, a.collectExpressionSources(x.Expr, sp, upsertValues)...)
			for _, item := range x.List {
				columns = append(columns, a.collectExpressionSources(item, sp, upsertValues)...)
			}
			if x.Select != nil {
				subqueries = append(subqueries, x.Select)
			}
			return false
		case *nodes.ExistsExpr:
			if x.Select != nil {
				subqueries = append(subqueries, x.Select)
			}
			return false
		case *nodes.ColumnRef:
			// A row alias names the proposed row, so a qualified reference to it is
			// the value the insert wrote for that column rather than a column of
			// any relation in scope.
			if upsertValues != nil && x.Table != "" {
				if sources, ok := upsertValues[proposedValueKey(x.Table, x.Column)]; ok {
					columns = append(columns, sources...)
					return true
				}
			}
			if x.Column != "" {
				columns = append(columns, scope.ColumnRef{Schema: x.Schema, Table: x.Table, Column: x.Column})
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

// subquerySources analyzes a subquery in its own scope and returns the sources
// of its output columns, resolved and marked so the enclosing scope can use them
// without seeing the subquery's tables.
func (a *Analyzer) subquerySources(sel *nodes.SelectStmt, sp *scope.Scope) []scope.ColumnRef {
	if sel == nil {
		return nil
	}
	a.scopeStack = append(a.scopeStack, scope.NewScope(sp))
	a.processSelectStatement(sel)
	subScope := a.popScope()
	if subScope == nil {
		return nil
	}
	// The subquery's rows decide which rows the enclosing expression sees, so its
	// influences belong to the enclosing scope.
	a.influences.Inherit(subScope, sp)
	var out []scope.ColumnRef
	for _, col := range algorithm.ResolveOutputColumns(subScope, subScope.GetOutputColumns()) {
		out = append(out, scope.Refs(col.Sources)...)
	}
	return out
}

// isPlainColumnRef reports whether an expression is a bare column reference and
// therefore a direct projection rather than a transformation.
func isPlainColumnRef(expr nodes.ExprNode) bool {
	switch x := expr.(type) {
	case *nodes.ColumnRef:
		return !x.Star
	case *nodes.ParenExpr:
		return isPlainColumnRef(x.Expr)
	default:
		return false
	}
}

// unwrapParens removes redundant parentheses around an expression.
func unwrapParens(expr nodes.ExprNode) nodes.ExprNode {
	for {
		p, ok := expr.(*nodes.ParenExpr)
		if !ok || p.Expr == nil {
			return expr
		}
		expr = p.Expr
	}
}

// containsAggregateCall reports whether an expression contains an aggregate
// function call, making it depend on the rows of its FROM relations.
func containsAggregateCall(expr nodes.ExprNode) bool {
	found := false
	if expr == nil {
		return false
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if fc, ok := n.(*nodes.FuncCallExpr); ok && aggregateFunctions[strings.ToUpper(fc.Name)] {
			found = true
			return false
		}
		return !found
	})
	return found
}

// containsGroupAggregate reports whether the expression contains an aggregate
// call that GROUP BY governs, i.e. one without an OVER clause. A windowed
// aggregate follows its OVER clause instead. Traversal stops at a subquery: an
// aggregate inside one is grouped by that query's own GROUP BY, never by the
// enclosing statement's.
func containsGroupAggregate(expr nodes.ExprNode) bool {
	found := false
	if expr == nil {
		return false
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*nodes.SubqueryExpr); ok {
			return false
		}
		if x, ok := n.(*nodes.FuncCallExpr); ok && x.Over == nil && x.Name != "" && aggregateFunctions[strings.ToUpper(x.Name)] {
			found = true
			return false
		}
		return true
	})
	return found
}

// firstFuncCall returns the first function call in pre-order that belongs to
// this expression. A call inside a subquery belongs to that subquery, so a
// scalar subquery is classified as the projection it is here rather than by the
// innermost call of its body.
func firstFuncCall(expr nodes.ExprNode) *nodes.FuncCallExpr {
	var found *nodes.FuncCallExpr
	if expr == nil {
		return nil
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if found != nil {
			return false
		}
		if _, ok := n.(*nodes.SubqueryExpr); ok {
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

// Common aggregate functions.
var aggregateFunctions = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MAX": true, "MIN": true,
	"GROUP_CONCAT": true, "STD": true, "STDDEV": true, "STDDEV_POP": true,
	"STDDEV_SAMP": true, "VAR_POP": true, "VAR_SAMP": true, "VARIANCE": true,
}

// analyzeExpressionOperator identifies the operation kind of an expression and
// returns its transformation metadata. The kind is decided from the typed AST,
// never from a substring of the reconstructed text.
func (a *Analyzer) analyzeExpressionOperator(expr nodes.ExprNode) []model.Transformation {
	if expr == nil {
		return nil
	}
	exprText := a.exprTextOf(expr)
	top := unwrapParens(expr)

	if _, ok := top.(*nodes.CaseExpr); ok {
		return []model.Transformation{model.NewCaseTransformation(exprText)}
	}
	if opInfo, ok := a.detectInfixOperator(top); ok {
		return []model.Transformation{opInfo}
	}
	if fc := firstFuncCall(expr); fc != nil && fc.Name != "" {
		// A call with an OVER clause is a window function, whether or not its
		// name is a known aggregate or window function.
		if fc.Over != nil {
			partitionBy, orderBy := a.extractWindowClauses(fc)
			return []model.Transformation{model.NewWindowTransformation(fc.Name, exprText, partitionBy, orderBy)}
		}
		if aggregateFunctions[strings.ToUpper(fc.Name)] {
			return []model.Transformation{model.NewAggregateTransformation(fc.Name, exprText, nil)}
		}
		args := make([]string, 0, len(fc.Args))
		for _, arg := range fc.Args {
			args = append(args, a.exprTextOf(arg))
		}
		return []model.Transformation{model.NewFunctionTransformation(fc.Name, exprText, args)}
	}
	return []model.Transformation{model.NewProjectTransformation(exprText)}
}

// detectInfixOperator reports the operator transformation of a binary or unary
// operator expression.
func (a *Analyzer) detectInfixOperator(expr nodes.ExprNode) (model.Transformation, bool) {
	switch x := expr.(type) {
	case *nodes.BinaryExpr:
		if name, ok := binaryOperatorName(x.Op); ok {
			return model.NewOperatorTransformation(name, a.exprTextOf(x)), true
		}
	case *nodes.UnaryExpr:
		if name, ok := unaryOperatorName(x.Op); ok {
			return model.NewOperatorTransformation(name, a.exprTextOf(x)), true
		}
	case *nodes.BetweenExpr:
		return model.NewOperatorTransformation("BETWEEN", a.exprTextOf(x)), true
	case *nodes.InExpr:
		return model.NewOperatorTransformation("IN", a.exprTextOf(x)), true
	default:
		return model.Transformation{}, false
	}
	return model.Transformation{}, false
}

// binaryOperatorName maps an omni binary operator to the recorded operator type.
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
	case nodes.BinOpEq:
		return "EQUALS", true
	case nodes.BinOpNe:
		return "NOT_EQUALS", true
	case nodes.BinOpLt:
		return "LESS_THAN", true
	case nodes.BinOpGt:
		return "GREATER_THAN", true
	case nodes.BinOpLe:
		return "LESS_OR_EQUAL", true
	case nodes.BinOpGe:
		return "GREATER_OR_EQUAL", true
	case nodes.BinOpAnd:
		return "LOGICAL_AND", true
	case nodes.BinOpOr:
		return "LOGICAL_OR", true
	case nodes.BinOpXor:
		return "LOGICAL_XOR", true
	case nodes.BinOpBitAnd:
		return "BIT_AND", true
	case nodes.BinOpBitOr:
		return "BIT_OR", true
	case nodes.BinOpBitXor:
		return "BIT_XOR", true
	case nodes.BinOpShiftLeft:
		return "SHIFT_LEFT", true
	case nodes.BinOpShiftRight:
		return "SHIFT_RIGHT", true
	case nodes.BinOpDivInt:
		return "INTEGER_DIVISION", true
	case nodes.BinOpRegexp:
		return "REGEXP", true
	case nodes.BinOpLikeEscape:
		return "LIKE", true
	case nodes.BinOpNullSafeEq:
		return "NULL_SAFE_EQUALS", true
	case nodes.BinOpAssign:
		return "ASSIGN", true
	case nodes.BinOpJsonExtract:
		return "JSON_EXTRACT", true
	case nodes.BinOpJsonUnquote:
		return "JSON_UNQUOTE", true
	case nodes.BinOpSoundsLike:
		return "SOUNDS_LIKE", true
	default:
		return "", false
	}
}

// unaryOperatorName maps an omni unary operator to the recorded operator type.
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

// extractWindowClauses extracts PARTITION BY and ORDER BY from a window function,
// following an `OVER w` reference into the WINDOW clause that defines it. The
// clauses the use carries itself come first, then the named definitions it is
// defined over.
func (a *Analyzer) extractWindowClauses(fc *nodes.FuncCallExpr) (partitionBy []string, orderBy []string) {
	if fc == nil || fc.Over == nil {
		return nil, nil
	}
	definitions := append([]*nodes.WindowDef{fc.Over}, a.namedWindowDefinitions(fc.Over)...)
	for _, def := range definitions {
		for _, expr := range def.PartitionBy {
			partitionBy = append(partitionBy, a.exprTextOf(expr))
		}
		for _, item := range def.OrderBy {
			if item != nil && item.Expr != nil {
				orderBy = append(orderBy, a.exprTextOf(item.Expr))
			}
		}
	}
	return partitionBy, orderBy
}

// maxNamedWindowChain bounds the walk over a chain of named windows. MySQL rejects
// a cycle, so the bound only keeps a malformed tree from looping.
const maxNamedWindowChain = 16

// namedWindowsOf indexes a query's WINDOW clause by the name each definition is
// referenced by.
func namedWindowsOf(list []*nodes.WindowDef) map[string]*nodes.WindowDef {
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]*nodes.WindowDef, len(list))
	for _, def := range list {
		if def != nil && def.Name != "" {
			out[strings.ToLower(def.Name)] = def
		}
	}
	return out
}

// namedWindowDefinitions returns the window definitions a use of a window reaches
// by name, in the order it reaches them. MySQL lets one named window be defined
// over another (`WINDOW w2 AS (w1 ORDER BY x)`), so the chain is followed. The
// definition the use itself carries is never returned: the walk over the
// expression already reaches its own clauses.
func (a *Analyzer) namedWindowDefinitions(use *nodes.WindowDef) []*nodes.WindowDef {
	if use == nil || len(a.namedWindows) == 0 {
		return nil
	}
	name := use.RefName
	if name == "" {
		name = use.Name
	}
	var out []*nodes.WindowDef
	seen := make(map[string]struct{})
	for depth := 0; name != "" && depth < maxNamedWindowChain; depth++ {
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			break
		}
		seen[key] = struct{}{}
		def, ok := a.namedWindows[key]
		if !ok || def == nil {
			break
		}
		out = append(out, def)
		name = def.RefName
	}
	return out
}

// ---------------------------------------------------------------------------
// Transformation builders
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Identifier helpers
// ---------------------------------------------------------------------------

// splitQualifiedIdentifier splits a dotted identifier into normalized parts.
func splitQualifiedIdentifier(fullText string) []string {
	parts := strings.Split(fullText, ".")
	for i, part := range parts {
		parts[i] = model.NormalizeIdentifier(part)
	}
	return parts
}

// inferredColumnAlias returns the output column name an unaliased select
// expression gets. A bare (possibly parenthesized) column reference contributes
// its own unquoted name, and anything else contributes the expression's raw
// source text: that is how the engine names such a column, so the stored target
// column matches the column the view actually exposes.
func (a *Analyzer) inferredColumnAlias(expr nodes.ExprNode, exprText string) string {
	if name := plainColumnName(expr); name != "" {
		return name
	}
	if source := a.exprSourceText(expr); source != "" {
		return source
	}
	return inferColumnAlias(exprText)
}

// plainColumnName returns the column name of a bare column reference, unwrapping
// parentheses. It returns "" for anything else, including a star.
func plainColumnName(expr nodes.ExprNode) string {
	switch x := expr.(type) {
	case *nodes.ColumnRef:
		if !x.Star {
			return x.Column
		}
	case *nodes.ParenExpr:
		return plainColumnName(x.Expr)
	default:
	}
	return ""
}

// inferColumnAlias infers an alias from an expression text. A plain (possibly
// qualified) identifier keeps its last segment; anything else keeps the whole
// expression text, because an operator or function makes the name synthetic.
func inferColumnAlias(exprText string) string {
	if !strings.ContainsAny(exprText, "() +-*/%<>=,!?") {
		parts := splitQualifiedIdentifier(exprText)
		return model.NormalizeIdentifier(parts[len(parts)-1])
	}
	return exprText
}

// columnNames extracts column names from a column reference list.
func columnNames(refs []*nodes.ColumnRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r != nil && r.Column != "" {
			out = append(out, r.Column)
		}
	}
	return out
}

// loadDataTargetColumns extracts LOAD DATA target columns. omni surfaces user
// variables (`@name`) as ColumnRefs in the target list; they are not table
// columns, so they are filtered out.
func loadDataTargetColumns(refs []*nodes.ColumnRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r != nil && r.Column != "" && !strings.HasPrefix(r.Column, "@") {
			out = append(out, r.Column)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Scope stack and edge accumulation
// ---------------------------------------------------------------------------

// pushScope creates and pushes a new scope onto the stack.
func (a *Analyzer) pushScope() {
	a.scopeStack = append(a.scopeStack, scope.NewScope(a.currentScope()))
}

// popScope removes and returns the top scope from the stack.
func (a *Analyzer) popScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	top := a.scopeStack[len(a.scopeStack)-1]
	a.scopeStack = a.scopeStack[:len(a.scopeStack)-1]
	return top
}

// currentScope returns the current (top) scope.
func (a *Analyzer) currentScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	return a.scopeStack[len(a.scopeStack)-1]
}

// addRelation adds a column relation, dropping exact duplicates. A query-local
// relation (a CTE or a derived table) is never an endpoint here: every path that
// resolves one traces through its own lineage first, so the edge already names
// the stored relation the column came from.
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	a.edges.Add(relation)
}

// attachColumnLookup lets the scope resolver disambiguate an unqualified column
// with catalog metadata for this base table, wherever the reference is resolved:
// the statement itself, a CTE body, a derived table or an expression subquery.
func (a *Analyzer) attachColumnLookup(tableRef *scope.TableRef) {
	if a.catalog == nil {
		return
	}
	var (
		names  []string
		loaded bool
	)
	tableRef.SetColumnLookup(func() []string {
		if loaded {
			return names
		}
		loaded = true
		if meta := a.catalogTable(tableRef); meta != nil {
			names = make([]string, 0, len(meta.Columns))
			for _, col := range meta.Columns {
				names = append(names, col.Name)
			}
		}
		return names
	})
}

// catalogTable looks up a base table's metadata, reading the catalog at most once
// per analysis. A lookup that fails leaves the relation's columns unknown — the
// analysis falls back to a wildcard edge — so the failure is reported as a gap:
// swallowing it made a catalog outage produce a result indistinguishable from a
// complete one.
func (a *Analyzer) catalogTable(tableRef *scope.TableRef) *catalog.TableMeta {
	if a.catalog == nil {
		return nil
	}
	id := model.ObjectIdentifier{Database: tableRef.Schema, Name: tableRef.Table}
	meta, err := a.catalog.GetTable(a.ctx, id)
	if err != nil {
		a.diagnostics.CatalogUnavailable(id.FullName(), err)
		return nil
	}
	return meta
}

// expandWildcardWithCatalog expands a wildcard using catalog metadata.
func (a *Analyzer) expandWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope) bool {
	tableMeta := a.catalogTable(tableRef)
	if tableMeta == nil {
		return false
	}
	for _, colMeta := range tableMeta.Columns {
		sp.AddOutputColumn(scope.OutputColumn{
			Alias: colMeta.Name,
			Sources: scope.NewColumnSources([]scope.ColumnRef{{
				Schema:   tableRef.Schema,
				Table:    tableRef.Table,
				Column:   colMeta.Name,
				Resolved: true,
			}}, nil),
		})
	}
	return true
}
