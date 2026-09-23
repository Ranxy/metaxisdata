// Package mariadb provides direct lineage analysis for MariaDB queries.
//
// This is a dialect copy of the MySQL analyzer in backend/plugin/lineage/mysql,
// built on github.com/bytebase/omni's MariaDB parser and typed AST. Keep the
// traversal in sync with the MySQL copy; the shared golden corpus is the guard.
//
// Known gaps and their intended resolution are tracked in
// plan/mysql_lineage_optimization_plan.md; defects in omni itself are recorded
// in docs/omni_upstream_defects.md.
package mariadb

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
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/mariadb/ast"
	mysqlparser "github.com/bytebase/omni/mariadb/parser"
)

// Constants for special table/column markers.
const (
	resultTableName   = "__result__"
	deletionFieldName = "__deletion__"
	wildcardColumn    = model.WildcardColumn
	fileSourceMarker  = "__file__" // Special marker for LOAD DATA source
)

func init() {
	// Only this dialect is claimed here; the other MySQL-family engines register
	// their own analyzers in their own packages.
	lineage.RegisterAnalyzeRelation(storepb.Engine_MARIADB, Analyze)
}

// Analyzer performs direct lineage analysis on MySQL queries.
type Analyzer struct {
	ctx context.Context
	sql string
	// tokens backs whitespace-free expression text reconstruction.
	tokens []mysqlparser.Token
	// Current scope stack
	scopeStack []*scope.Scope
	// Collected column relations
	edges []model.ColumnRelation
	// Map for efficient edge deduplication (key: edge signature)
	edgeSet map[columnEdgeKey]struct{}
	// Errors encountered during analysis. A statement shape the analyzer cannot
	// represent is recorded here so the caller records an explicit failure
	// instead of silently returning empty lineage.
	errors []string
	// Optional catalog provider for wildcard expansion and metadata lookup
	catalog catalog.Provide
	// Per-analysis catalog memo, keyed by the identifier the catalog is queried
	// with. A nil entry records a miss so a table is looked up at most once.
	tableCache map[model.ObjectIdentifier]*catalog.TableMeta
	// realTarget is set while analyzing the query of a statement that writes to a
	// real object (INSERT/REPLACE/CREATE TABLE AS/CREATE VIEW), so the query
	// result edges are not emitted alongside the real target edges.
	realTarget bool
	// inSetOpArm is set while analyzing one arm of a set operation, so only the
	// merged set-operation result emits edges.
	inSetOpArm bool
	// Track temporary table names (derived tables, CTEs) to filter intermediate
	// results.
	tempTables map[string]struct{}
	// predicates accumulates the columns every row-set predicate of the statement
	// depends on. Which object the statement produces is only known once the
	// whole tree has been walked, so the edges are emitted by whichever emitter
	// runs last.
	predicates []predicateInfluence
}

// predicateInfluence is one column a WHERE, HAVING or ON predicate depends on,
// together with the clause that makes it an influence.
type predicateInfluence struct {
	// key is the real relation the column resolved to. It is zero when the
	// predicate is over a query-local relation, whose lineage is traced instead.
	key       predicateKey
	relation  *scope.TableRef
	column    string
	transform model.Transformation
}

// predicateKey identifies a predicate column, so a column used by several
// clauses collapses to the one edge the deduplication keeps.
type predicateKey struct {
	database string
	table    string
	column   string
}

// columnEdgeKey identifies a lineage edge for deduplication without allocating
// a formatted signature per edge.
type columnEdgeKey struct {
	sourceDatabase string
	sourceTable    string
	sourceColumn   string
	targetDatabase string
	targetTable    string
	targetColumn   string
}

// Analyze parses a single MySQL statement and returns its column relations.
func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	return NewAnalyzer(ctx, sql, lineage.GetCatalogProvide()).AnalyzeRelations()
}

// NewAnalyzer creates a new MySQL lineage analyzer.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	return &Analyzer{
		ctx:        ctx,
		sql:        sql,
		tokens:     mysqlparser.Tokenize(sql),
		scopeStack: []*scope.Scope{scope.NewScope(nil)}, // Root scope
		edges:      make([]model.ColumnRelation, 0),
		edgeSet:    make(map[columnEdgeKey]struct{}),
		errors:     make([]string, 0),
		catalog:    catalogProvide,
		tableCache: make(map[model.ObjectIdentifier]*catalog.TableMeta),
		tempTables: make(map[string]struct{}),
	}
}

// AnalyzeRelations parses the SQL and returns column relations.
//
// Parsing is strict: a statement omni cannot parse is an error, never a partial
// result. Multi-statement input is rejected because the analyzer is defined for
// exactly one statement. A statement shape the analyzer cannot represent is also
// an error, so a parseable statement never silently produces empty lineage.
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
		// A VALUES statement carries only literals; it has no lineage.
	case *nodes.InsertStmt:
		a.rejectLeadingWith("INSERT")
		a.processInsertStatement(stmt)
	case *nodes.CreateTableStmt:
		a.processCreateTable(stmt)
	case *nodes.CreateViewStmt:
		a.processCreateView(stmt)
	case *nodes.UpdateStmt:
		if a.rejectLeadingWith("UPDATE") {
			break
		}
		a.processUpdateStatement(stmt)
	case *nodes.DeleteStmt:
		if a.rejectLeadingWith("DELETE") {
			break
		}
		a.processDeleteStatement(stmt)
	case *nodes.LoadDataStmt:
		a.processLoadStatement(stmt)
	default:
		// Statement kinds with no column lineage (DDL, maintenance, transactions)
		// are ignored; there is nothing to resolve.
	}

	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
	}
	return a.edges, nil
}

// rejectLeadingWith reports whether a DML statement is prefixed with a WITH
// clause. omni parses such statements but its InsertStmt/UpdateStmt/DeleteStmt
// carry no CTEs, so the WITH clause is dropped and the CTE name would be treated
// as a real table. The statement shape is unrepresentable, so it is an error
// (see docs/omni_upstream_defects.md).
func (a *Analyzer) rejectLeadingWith(statement string) bool {
	if !hasLeadingWith(a.sql) {
		return false
	}
	a.errors = append(a.errors,
		fmt.Sprintf("WITH before %s is not supported: the parser drops the CTE, so its sources cannot be resolved", statement))
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
// Temporary table tracking
// ---------------------------------------------------------------------------

// isTableTempInCurrentScope checks if a table is a CTE or subquery. A relation
// named with a qualifier is a real table: a CTE or a derived table is
// query-local and is never qualified.
func (a *Analyzer) isTableTempInCurrentScope(qualifier, tableName string) bool {
	if qualifier != "" {
		return false
	}
	for _, sp := range a.scopeStack {
		if _, ok := sp.FindCTE(tableName); ok {
			return true
		}
		if tableRef, ok := sp.FindTable(tableName); ok {
			return tableRef.IsSubquery || tableRef.IsCTE
		}
	}
	return false
}

// markTempTable records a temporary table name (CTE or subquery) for filtering intermediate edges.
func (a *Analyzer) markTempTable(name string) {
	if name == "" {
		return
	}
	a.tempTables[name] = struct{}{}
}

// isTempRelation reports whether an endpoint names a query-local relation (a CTE
// or a derived table). Those are never qualified, so a qualified reference to a
// real table of the same name is not one.
func (a *Analyzer) isTempRelation(id model.ObjectIdentifier) bool {
	key := scope.RelationKeyOf(id)
	if key.Qualifier != "" {
		return false
	}
	_, ok := a.tempTables[key.Name]
	return ok
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
	if len(stmt.TargetList) == 0 && len(stmt.From) == 0 {
		a.errors = append(a.errors, "query form with no select list and no FROM is not supported")
		return
	}
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
	a.markTempTable(cteName)

	var cteLineage []model.ColumnRelation
	if cte.Select != nil {
		a.pushScope()
		a.processSelectStatement(cte.Select)
		cteScope := a.popScope()

		outputColumns := cteScope.GetOutputColumns()
		names := exposedColumnNames(cte.Columns, outputColumns)
		for i, outputCol := range outputColumns {
			targetName := names[i]
			for _, sourceCol := range outputCol.SourceColumns {
				resolutions, err := cteScope.ResolveColumnRefs(sourceCol)
				if err != nil {
					continue
				}
				for _, res := range resolutions {
					if a.flattenTempSourceLineage(cteScope, res.Relation, res.Ref.Column, cteName, targetName, outputCol.Transform, &cteLineage) {
						continue
					}
					cteLineage = append(cteLineage, scope.NewLineageEdge(
						res.Ref.Schema, res.Ref.Table, res.Ref.Column,
						"", cteName, targetName,
						outputCol.Transform,
						true, // CTE is temporary
					))
				}
			}
		}
	}

	a.currentScope().AddCTE(&scope.CTEDefinition{
		Name:    cteName,
		Columns: cte.Columns,
		Lineage: cteLineage,
	})
}

// flattenSetOpArms flattens a set-operation tree into its leaf SELECTs in order.
func flattenSetOpArms(stmt *nodes.SelectStmt) []*nodes.SelectStmt {
	if stmt == nil {
		return nil
	}
	if stmt.SetOp != nodes.SetOpNone {
		var out []*nodes.SelectStmt
		out = append(out, flattenSetOpArms(stmt.Left)...)
		out = append(out, flattenSetOpArms(stmt.Right)...)
		return out
	}
	if stmt.ParenSource != nil {
		return flattenSetOpArms(stmt.ParenSource)
	}
	return []*nodes.SelectStmt{stmt}
}

// processSetOperation handles UNION/INTERSECT/EXCEPT by processing every arm in
// its own scope and merging their output columns positionally. Each arm's source
// references are resolved in the scope they were collected in and marked
// resolved, because the arm scope is gone by the time the enclosing CTE,
// derived table or statement resolves the merged columns.
func (a *Analyzer) processSetOperation(stmt *nodes.SelectStmt) {
	arms := flattenSetOpArms(stmt)
	if len(arms) == 0 {
		return
	}
	setOp := stmt.SetOp

	baseScope := a.currentScope()
	var allOutputColumns [][]scope.OutputColumn

	for i, arm := range arms {
		if i == 0 {
			a.processSetOpArm(arm)
			allOutputColumns = append(allOutputColumns, resolveOutputColumns(baseScope, baseScope.GetOutputColumns()))
			continue
		}
		tempScope := scope.NewScope(baseScope.Parent())
		for _, cte := range baseScope.CTEs() {
			tempScope.AddCTE(cte)
		}
		originalScope := a.currentScope()
		a.scopeStack[len(a.scopeStack)-1] = tempScope
		a.processSetOpArm(arm)
		a.scopeStack[len(a.scopeStack)-1] = originalScope
		allOutputColumns = append(allOutputColumns, resolveOutputColumns(tempScope, tempScope.GetOutputColumns()))
	}

	mergeSetOpOutputColumns(baseScope, allOutputColumns, setOp)
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

// resolveOutputColumns resolves each output column's source references against
// the scope the arm was analyzed in and marks them resolved. Without this, an
// arm's reference would be resolved again in the enclosing scope, where the
// arm's tables no longer exist, and the arm's lineage would be silently dropped.
func resolveOutputColumns(sp *scope.Scope, cols []scope.OutputColumn) []scope.OutputColumn {
	out := make([]scope.OutputColumn, len(cols))
	copy(out, cols)
	for i := range out {
		if len(out[i].SourceColumns) == 0 {
			continue
		}
		resolved := make([]scope.ColumnRef, 0, len(out[i].SourceColumns))
		for _, ref := range out[i].SourceColumns {
			resolutions, err := sp.ResolveColumnRefs(ref)
			if err != nil {
				resolved = append(resolved, ref)
				continue
			}
			for _, res := range resolutions {
				columnRef := res.Ref
				columnRef.Resolved = true
				resolved = append(resolved, columnRef)
			}
		}
		out[i].SourceColumns = resolved
	}
	return out
}

// mergeSetOpOutputColumns merges output columns from multiple set-operation arms
// positionally and records the set operation as the leading transformation of
// every merged column.
func mergeSetOpOutputColumns(baseScope *scope.Scope, allOutputColumns [][]scope.OutputColumn, setOp nodes.SetOperation) {
	if len(allOutputColumns) == 0 || len(allOutputColumns[0]) == 0 {
		return
	}
	firstQueryOutputs := allOutputColumns[0]
	for colIdx := 0; colIdx < len(firstQueryOutputs); colIdx++ {
		firstCol := firstQueryOutputs[colIdx]
		var mergedSources []scope.ColumnRef
		for queryIdx := 0; queryIdx < len(allOutputColumns); queryIdx++ {
			if colIdx < len(allOutputColumns[queryIdx]) {
				mergedSources = append(mergedSources, allOutputColumns[queryIdx][colIdx].SourceColumns...)
			}
		}
		firstCol.SourceColumns = mergedSources
		if transform, ok := setOpTransformation(setOp); ok {
			firstCol.Transform = append([]model.Transformation{transform}, firstCol.Transform...)
		}
		baseScope.SetOutputColumn(colIdx, firstCol)
	}
}

// setOpTransformation maps a set operation to its transformation.
func setOpTransformation(setOp nodes.SetOperation) (model.Transformation, bool) {
	switch setOp {
	case nodes.SetOpUnion:
		return model.NewUnionTransformation(), true
	case nodes.SetOpIntersect:
		return model.NewIntersectTransformation(), true
	case nodes.SetOpExcept:
		return model.NewExceptTransformation(), true
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
					a.recordPredicate(sp, scope.ColumnRef{Schema: ref.Schema, Table: ref.Table, Column: column}, transform, false)
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
// as an influence on the statement's target rows. resolveAliases is set for a
// clause that may name a select-list alias, as HAVING does.
func (a *Analyzer) collectPredicates(expr nodes.ExprNode, sp *scope.Scope, transform model.Transformation, resolveAliases bool) {
	if expr == nil {
		return
	}
	for _, ref := range a.collectExprColumns(expr, sp) {
		a.recordPredicate(sp, ref, transform, resolveAliases)
	}
}

// recordPredicate resolves one predicate column. An unresolvable column is
// dropped, the same way an unresolvable source column is, so a predicate never
// invents a relation.
func (a *Analyzer) recordPredicate(sp *scope.Scope, ref scope.ColumnRef, transform model.Transformation, resolveAliases bool) {
	// A select-list alias is not a column of any relation. The clause influences
	// the rows the aggregate behind the alias produced, so the influence belongs
	// to that output column's own sources.
	if resolveAliases && ref.Table == "" && ref.Column != "" {
		for _, output := range sp.GetOutputColumns() {
			if output.Alias != ref.Column {
				continue
			}
			for _, source := range output.SourceColumns {
				a.recordPredicate(sp, source, transform, false)
			}
			return
		}
	}

	resolutions, err := sp.ResolveColumnRefs(ref)
	if err != nil {
		return
	}
	for _, res := range resolutions {
		influence := predicateInfluence{transform: transform}
		if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
			influence.relation = res.Relation
			influence.column = res.Ref.Column
		} else {
			influence.key = predicateKey{database: res.Ref.Schema, table: res.Ref.Table, column: res.Ref.Column}
		}
		a.predicates = append(a.predicates, influence)
	}
}

// emitPredicateInfluences adds one edge per predicate column to the rows the
// statement produces. The target column is empty: a predicate decides which rows
// are emitted, not the value of any one column.
func (a *Analyzer) emitPredicateInfluences(targetSchema, targetTable string) {
	if len(a.predicates) == 0 {
		return
	}
	isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
	for _, influence := range a.predicates {
		if influence.relation != nil {
			a.traceThroughTableLineage(influence.relation, influence.column, targetSchema, targetTable, "", []model.Transformation{influence.transform})
			continue
		}
		a.addRelation(scope.NewLineageEdge(
			influence.key.database, influence.key.table, influence.key.column,
			targetSchema, targetTable, "",
			[]model.Transformation{influence.transform},
			isTemp,
		))
	}
	a.predicates = nil
}

// processTableExpr processes a table reference, join or derived table.
func (a *Analyzer) processTableExpr(te nodes.TableExpr) {
	switch t := te.(type) {
	case *nodes.TableRef:
		a.processSingleTableRef(t)
	case *nodes.JoinClause:
		a.processTableExpr(t.Left)
		a.processTableExpr(t.Right)
	case *nodes.SubqueryExpr:
		a.processDerivedTable(t)
	default:
		// Function-in-FROM and other table expressions carry no lineage.
	}
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
			attachTempColumnLookup(tableRef, cte.Columns)
			a.currentScope().AddTable(tableRef)
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
	a.markTempTable(alias)

	a.pushScope()
	a.processSelectStatement(sub.Select)
	subqueryScope := a.popScope()

	outputColumns := subqueryScope.GetOutputColumns()
	names := exposedColumnNames(sub.Columns, outputColumns)
	derivedLineage := make([]model.ColumnRelation, 0)
	for i, col := range outputColumns {
		colName := names[i]
		if colName == "" {
			colName = "column"
		}
		for _, sourceCol := range col.SourceColumns {
			resolutions, err := subqueryScope.ResolveColumnRefs(sourceCol)
			if err != nil {
				continue
			}
			for _, res := range resolutions {
				if a.flattenTempSourceLineage(subqueryScope, res.Relation, res.Ref.Column, alias, colName, col.Transform, &derivedLineage) {
					continue
				}
				derivedLineage = append(derivedLineage, scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					"", alias, colName,
					col.Transform,
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
	attachTempColumnLookup(tableRef, names)
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
			Alias:         wildcardColumn,
			SourceColumns: []scope.ColumnRef{wildcardSourceRef(tableRef)},
		})
	}
}

// processTableWildcard expands table.* against the table the reference names,
// including the database or schema qualifier it carries.
func (a *Analyzer) processTableWildcard(cr *nodes.ColumnRef, sp *scope.Scope) {
	tableRef, ok := sp.FindRelation(scope.RelationKey{Qualifier: cr.Schema, Name: cr.Table})
	if !ok {
		return
	}
	if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
		if a.expandWildcardWithCatalog(tableRef, sp) {
			return
		}
	}
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:         wildcardColumn,
		SourceColumns: []scope.ColumnRef{wildcardSourceRef(tableRef)},
	})
}

// wildcardSourceRef builds the source reference for a wildcard. It is marked
// resolved because the scope is keyed by alias while the reference carries the
// real table name, so resolving it again by name would fail and drop the edge.
func wildcardSourceRef(tableRef *scope.TableRef) scope.ColumnRef {
	return scope.ColumnRef{
		Schema:   tableRef.Schema,
		Table:    tableRef.Table,
		Column:   wildcardColumn,
		Resolved: true,
	}
}

// processSelectExpr turns one select expression into an output column.
func (a *Analyzer) processSelectExpr(expr nodes.ExprNode, alias string, sp *scope.Scope, groupKeys []string) {
	if expr == nil {
		return
	}
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
			sourceColumns = append(sourceColumns, wildcardSourceRef(tableRef))
		}
	}

	outputCol := scope.OutputColumn{
		Alias:         alias,
		SourceColumns: sourceColumns,
		IsDerived:     isDerived,
	}
	if isDerived {
		outputCol.Transform = a.analyzeExpressionOperator(expr)
		// The GROUP BY keys describe how an aggregate in this select item was
		// computed, so they ride on every transformation the item produced: the
		// outermost node is often not the aggregate itself, since SUM(x) + 1 is
		// an operator and a CASE or a function can wrap one too. An expression
		// with no group aggregate of its own records nothing, which also keeps a
		// windowed aggregate's OVER clause from being confused with GROUP BY.
		if len(groupKeys) > 0 && containsGroupAggregate(expr) {
			for i := range outputCol.Transform {
				outputCol.Transform[i].GroupKeys = groupKeys
			}
		}
	}
	sp.AddOutputColumn(outputCol)
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
		a.emitSources(sp, outputCol.SourceColumns, "", resultTableName, outputCol.Alias, outputCol.Transform)
	}
	a.emitPredicateInfluences("", resultTableName)
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
		a.emitSources(sp, outputCol.SourceColumns, targetSchema, targetTable, targetColName, outputCol.Transform)
	}
	a.emitPredicateInfluences(targetSchema, targetTable)
}

// emitSources resolves source columns and adds one relation per source. An
// unqualified name that several relations in scope own contributes one relation
// per owner: each of them really is a source of the value, which is what a
// coalesced USING or NATURAL JOIN column looks like.
func (a *Analyzer) emitSources(sp *scope.Scope, sourceColumns []scope.ColumnRef, targetSchema, targetTable, targetColumn string, transform []model.Transformation) {
	for _, sourceCol := range sourceColumns {
		resolutions, err := sp.ResolveColumnRefs(sourceCol)
		if err != nil {
			continue
		}
		for _, res := range resolutions {
			// A CTE or derived table contributes the lineage of its own columns:
			// there is no stored relation to point an edge at.
			if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
				a.traceThroughTableLineage(res.Relation, res.Ref.Column, targetSchema, targetTable, targetColumn, transform)
				continue
			}
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
			a.addRelation(scope.NewLineageEdge(
				res.Ref.Schema, res.Ref.Table, res.Ref.Column,
				targetSchema, targetTable, targetColumn,
				transform,
				isTemp,
			))
		}
	}
}

// traceThroughTableLineage traces lineage through a CTE or subquery to a target.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName string, targetSchema, targetTable, targetColumn string, transform []model.Transformation) {
	for _, edge := range model.AnsweringLineage(tableRef.Lineage, columnName) {
		actualTarget := targetColumn
		if columnName == wildcardColumn && targetColumn == wildcardColumn {
			actualTarget = edge.Target.Name
		}
		isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
		a.addRelation(scope.NewLineageEdge(
			edge.Source.Table.Database, edge.Source.Table.Name, edge.Source.Name,
			targetSchema, targetTable, actualTarget,
			combineTransformations(edge.Transformation, transform),
			isTemp,
		))
	}
}

// flattenTempSourceLineage expands a source column that belongs to a query-local
// relation into the base-table lineage behind it. It reports whether the source
// was query-local and has been handled.
func (a *Analyzer) flattenTempSourceLineage(sp *scope.Scope, relation *scope.TableRef, columnName, targetTable, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	if relation == nil || (!relation.IsSubquery && !relation.IsCTE) {
		return false
	}
	a.appendFlattenedLineage(lineage, sp, relation, columnName, targetTable, targetColumn, transform)
	return true
}

// appendFlattenedLineage traces through nested temporary tables to real tables.
func (a *Analyzer) appendFlattenedLineage(lineage *[]model.ColumnRelation, sp *scope.Scope, tableRef *scope.TableRef, columnName, targetTable, targetColumn string, transform []model.Transformation) {
	for _, edge := range model.AnsweringLineage(tableRef.Lineage, columnName) {
		actualTarget := targetColumn
		if columnName == wildcardColumn && targetColumn == wildcardColumn {
			actualTarget = edge.Target.Name
		}
		combinedTransform := combineTransformations(edge.Transformation, transform)
		sourceTableName := edge.Source.Table.Name
		if nestedRef, ok := sp.FindRelation(scope.RelationKeyOf(edge.Source.Table)); ok && (nestedRef.IsCTE || nestedRef.IsSubquery) {
			a.appendFlattenedLineage(lineage, sp, nestedRef, edge.Source.Name, targetTable, actualTarget, combinedTransform)
			continue
		}
		*lineage = append(*lineage, scope.NewLineageEdge(
			edge.Source.Table.Database, sourceTableName, edge.Source.Name,
			"", targetTable, actualTarget,
			combinedTransform,
			true,
		))
	}
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
	default:
		// INSERT ... VALUES and INSERT ... SET carry literal rows only: the
		// column list of the target still describes the write, but there is no
		// source to resolve.
	}

	a.generateEdgesForDataModification(targetSchema, targetTable, targetColumns)

	if !stmt.IsReplace && len(stmt.OnDuplicateKey) > 0 {
		a.processInsertUpdateList(stmt.OnDuplicateKey, targetSchema, targetTable, targetColumns)
	}
}

// processInsertUpdateList processes the ON DUPLICATE KEY UPDATE clause.
func (a *Analyzer) processInsertUpdateList(assignments []*nodes.Assignment, targetSchema, targetTable string, targetColumns []string) {
	sp := a.currentScope()
	insertSources := insertSourceMap(sp, targetColumns)
	for _, elem := range assignments {
		if elem == nil || elem.Column == nil || elem.Value == nil {
			continue
		}
		sourceColumns := collectUpsertSources(elem.Value, insertSources)
		if len(sourceColumns) == 0 {
			continue
		}
		transform := a.analyzeExpressionOperator(elem.Value)
		for _, sourceCol := range sourceColumns {
			resolutions, err := sp.ResolveColumnRefs(sourceCol)
			if err != nil {
				resolutions = []scope.ResolvedColumn{{Ref: sourceCol}}
			}
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
			for _, res := range resolutions {
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

// insertSourceMap maps each written target column to the sources the insert
// writes into it, so VALUES(col) in an upsert can be traced to the inserted
// value instead of being resolved as a column of the source tables.
func insertSourceMap(sp *scope.Scope, targetColumns []string) map[string][]scope.ColumnRef {
	out := make(map[string][]scope.ColumnRef)
	for i, outputCol := range sp.GetOutputColumns() {
		if len(targetColumns) > 0 && i >= len(targetColumns) {
			break
		}
		name := outputCol.Alias
		if i < len(targetColumns) {
			name = targetColumns[i]
		}
		out[name] = append(out[name], outputCol.SourceColumns...)
	}
	return out
}

// collectUpsertSources collects the sources of an upsert assignment value,
// replacing VALUES(col) with the sources the insert writes to col.
func collectUpsertSources(expr nodes.ExprNode, insertSources map[string][]scope.ColumnRef) []scope.ColumnRef {
	out := make([]scope.ColumnRef, 0)
	if expr == nil {
		return out
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		switch x := n.(type) {
		case *nodes.FuncCallExpr:
			if strings.EqualFold(x.Name, "VALUES") {
				for _, arg := range x.Args {
					if cr, ok := arg.(*nodes.ColumnRef); ok && cr.Column != "" {
						out = append(out, insertSources[cr.Column]...)
					}
				}
				return false
			}
		case *nodes.SubqueryExpr:
			return false
		case *nodes.ColumnRef:
			if x.Column != "" {
				out = append(out, scope.ColumnRef{Schema: x.Schema, Table: x.Table, Column: x.Column})
			}
		default:
			// Other nodes are traversed for the column references they contain.
		}
		return true
	})
	return out
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
		a.emitSources(sp, outputCol.SourceColumns, targetSchema, targetTable, outputCol.Alias, outputCol.Transform)
	}
	a.emitPredicateInfluences(targetSchema, targetTable)
}

// processCreateView processes CREATE VIEW.
func (a *Analyzer) processCreateView(stmt *nodes.CreateViewStmt) {
	if stmt == nil || stmt.Name == nil || stmt.Select == nil {
		return
	}
	targetView := stmt.Name.Name
	targetSchema := stmt.Name.Schema
	explicitColumnNames := stmt.Columns

	previous := a.realTarget
	a.realTarget = true
	a.processSelectStatement(stmt.Select)
	a.realTarget = previous

	sp := a.currentScope()
	for i, outputCol := range sp.GetOutputColumns() {
		targetColName := outputCol.Alias
		if i < len(explicitColumnNames) {
			targetColName = explicitColumnNames[i]
		}
		a.emitSources(sp, outputCol.SourceColumns, targetSchema, targetView, targetColName, outputCol.Transform)
	}
	a.emitPredicateInfluences(targetSchema, targetView)
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
		resolved, err := sp.ResolveColumn(targetCol)
		if err != nil {
			continue
		}

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
				resolutions = []scope.ResolvedColumn{{Ref: sourceCol}}
			}
			isTemp := resolved.Table == resultTableName || a.isTableTempInCurrentScope(resolved.Schema, resolved.Table)
			for _, res := range resolutions {
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
			resolutions, err := sp.ResolveColumnRefs(condCol)
			if err != nil {
				resolutions = []scope.ResolvedColumn{{Ref: condCol}}
			}
			transform := []model.Transformation{
				model.NewDeleteTransformation(whereText),
			}
			isTemp := actualTargetTable.Table == resultTableName || a.isTableTempInCurrentScope(actualTargetTable.Schema, actualTargetTable.Table)
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

	isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
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
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
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
	columns := make([]scope.ColumnRef, 0)
	if expr == nil {
		return columns
	}
	var subqueries []*nodes.SelectStmt
	nodes.Inspect(expr, func(n nodes.Node) bool {
		switch x := n.(type) {
		case *nodes.SubqueryExpr:
			if x.Select != nil {
				subqueries = append(subqueries, x.Select)
			}
			return false // handled as a unit below
		case *nodes.InExpr:
			// The value list and the left operand are ordinary columns; only the
			// subquery operand is a scope of its own.
			columns = append(columns, a.collectExprColumns(x.Expr, sp)...)
			for _, item := range x.List {
				columns = append(columns, a.collectExprColumns(item, sp)...)
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
	var out []scope.ColumnRef
	for _, col := range resolveOutputColumns(subScope, subScope.GetOutputColumns()) {
		out = append(out, col.SourceColumns...)
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

// extractWindowClauses extracts PARTITION BY and ORDER BY from a window function.
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

// combineTransformations merges two transformation chains.
func combineTransformations(base, additional []model.Transformation) []model.Transformation {
	if len(base) == 0 {
		return additional
	}
	if len(additional) == 0 {
		return base
	}
	combined := make([]model.Transformation, len(base)+len(additional))
	copy(combined, base)
	copy(combined[len(base):], additional)
	return combined
}

// ---------------------------------------------------------------------------
// Identifier helpers
// ---------------------------------------------------------------------------

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
		return normalizeIdentifier(parts[len(parts)-1])
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

// addRelation adds a column relation, avoiding duplicates.
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	if a.isTempRelation(relation.Source.Table) {
		return
	}
	if a.isTempRelation(relation.Target.Table) && relation.Target.Table.Name != resultTableName {
		return
	}
	// The qualifier of a MySQL-family name is its database, which NewLineageEdge
	// stores in Database; Schema is never populated. Reading Schema here left
	// every key's database empty, so two edges that differed only by database
	// collided and the second was dropped.
	key := columnEdgeKey{
		sourceDatabase: relation.Source.Table.Database,
		sourceTable:    relation.Source.Table.Name,
		sourceColumn:   relation.Source.Name,
		targetDatabase: relation.Target.Table.Database,
		targetTable:    relation.Target.Table.Name,
		targetColumn:   relation.Target.Name,
	}
	if _, exists := a.edgeSet[key]; exists {
		return
	}
	a.edgeSet[key] = struct{}{}
	a.edges = append(a.edges, relation)
}

// attachTempColumnLookup lets the scope resolver treat a CTE or derived table as
// a relation whose columns are known, so an unqualified name it owns resolves
// through its own lineage instead of being guessed from name order. declared is
// the CTE's column list when the query wrote one, or the names the query's output
// exposes; a nil list falls back to the targets the lineage carries.
func attachTempColumnLookup(tableRef *scope.TableRef, declared []string) {
	if names := tempColumnNames(declared, tableRef.Lineage); names != nil {
		tableRef.SetColumnLookup(func() []string { return names })
	}
}

// tempColumnNames reports the columns a temporary relation exposes, or nil when
// one of them cannot be named. A wildcard the catalog did not expand, and an
// output without a name, both leave the list incomplete; an incomplete list is
// reported as unknown so the resolver keeps its fallback for the relation.
func tempColumnNames(declared []string, lineage []model.ColumnRelation) []string {
	names := declared
	if len(names) == 0 {
		seen := make(map[string]struct{}, len(lineage))
		for _, edge := range lineage {
			if _, ok := seen[edge.Target.Name]; ok {
				continue
			}
			seen[edge.Target.Name] = struct{}{}
			names = append(names, edge.Target.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	for _, name := range names {
		if name == "" || name == wildcardColumn {
			return nil
		}
	}
	return names
}

// exposedColumnNames lists the names a temporary relation exposes, in the order
// the relation offers them. A declared list — a CTE's `WITH c (a, b)` or a
// derived table's `AS d (a, b)` — renames the body's output positionally, so both
// the relation's own lineage and the columns the scope resolves against have to
// use the renamed names. The list is honoured only when its arity matches the
// body, because a mismatch is a statement MySQL rejects.
func exposedColumnNames(declared []string, cols []scope.OutputColumn) []string {
	if len(declared) > 0 && len(declared) == len(cols) {
		return declared
	}
	names := make([]string, 0, len(cols))
	for _, col := range cols {
		names = append(names, col.Alias)
	}
	return names
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

// catalogTable looks up a base table's metadata once per analysis.
func (a *Analyzer) catalogTable(tableRef *scope.TableRef) *catalog.TableMeta {
	if a.catalog == nil {
		return nil
	}
	id := model.ObjectIdentifier{Database: tableRef.Schema, Name: tableRef.Table}
	if meta, ok := a.tableCache[id]; ok {
		return meta
	}
	meta, err := a.catalog.GetTable(a.ctx, id)
	if err != nil {
		meta = nil
	}
	a.tableCache[id] = meta
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
			SourceColumns: []scope.ColumnRef{{
				Schema:   tableRef.Schema,
				Table:    tableRef.Table,
				Column:   colMeta.Name,
				Resolved: true,
			}},
		})
	}
	return true
}
