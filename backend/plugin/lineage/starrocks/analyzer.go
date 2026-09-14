// Package starrocks provides direct lineage analysis for StarRocks queries.
//
// This implementation is built on github.com/bytebase/omni's StarRocks parser
// and typed AST, mirroring backend/plugin/lineage/mysql. It is a port rather
// than a dialect copy: omni's StarRocks AST differs structurally from its MySQL
// AST (set operations are a separate node, the select list is []*SelectItem,
// and derived tables, CTAS and expression subqueries keep their query as raw
// text).
//
// Engine registration lands with the DDL/DML phases (see
// plan/starrocks_lineage_plan.md); until then the package is exercised by its
// own hermetic tests only, and statement kinds that are not implemented yet
// return an explicit error instead of a partial result.
package starrocks

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/starrocks/ast"
	starrocksparser "github.com/bytebase/omni/starrocks/parser"
)

// Special table/column markers shared with the other analyzers.
const (
	resultTableName = "__result__"
	wildcardColumn  = "*"
)

// Analyzer performs direct lineage analysis on one StarRocks statement.
type Analyzer struct {
	ctx context.Context
	sql string
	// sources is the text/token stack for the statement being analyzed; see
	// rawsql.go.
	sources []source
	// scopeStack tracks lexical scopes, innermost last.
	scopeStack []*scope.Scope
	// edges is the accumulated, deduplicated lineage.
	edges []model.ColumnRelation
	// edgeSet deduplicates edges by their source->target signature.
	edgeSet map[string]struct{}
	// errors collects analysis failures. A non-empty list fails the whole
	// analysis: a partial result is never returned.
	errors []string
	// catalog optionally expands wildcards and resolves metadata.
	catalog catalog.Provide
	// tempTables tracks CTE and subquery names so intermediate edges can be
	// filtered out.
	tempTables map[string]struct{}
	// inDDLTarget is set while the query body of a CREATE VIEW / MATERIALIZED
	// VIEW / TABLE ... AS statement is analyzed. Those output columns are
	// mapped onto the new object, so they must not also be emitted against
	// __result__.
	inDDLTarget bool
}

// Analyze parses a single StarRocks statement and returns its column relations.
func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	return NewAnalyzer(ctx, sql, lineage.CatelogProvide).AnalyzeRelations()
}

// NewAnalyzer creates a StarRocks lineage analyzer for a single statement.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	return &Analyzer{
		ctx:        ctx,
		sql:        sql,
		sources:    []source{newSource(sql)},
		scopeStack: []*scope.Scope{scope.NewScope(nil)}, // root scope
		edges:      make([]model.ColumnRelation, 0),
		edgeSet:    make(map[string]struct{}),
		errors:     make([]string, 0),
		catalog:    catalogProvide,
		tempTables: make(map[string]struct{}),
	}
}

// AnalyzeRelations parses the SQL and returns its column relations.
//
// Parsing is strict: a statement omni cannot parse is an error, never a partial
// result. Multi-statement input is rejected because the analyzer is defined for
// exactly one statement.
func (a *Analyzer) AnalyzeRelations() ([]model.ColumnRelation, error) {
	if segments := starrocksparser.Split(a.sql); len(segments) != 1 {
		return nil, errors.Errorf("expected exactly 1 statement, got %d", len(segments))
	}

	file, errs := starrocksparser.Parse(a.sql)
	if len(errs) > 0 {
		// omni's CREATE VIEW grammar accepts only a plain SELECT body (finding
		// F2). A WITH / set-operation / parenthesized body is analyzed from the
		// extracted query instead; anything else is still a hard failure.
		if ddl, ok := extractViewDDL(a.sql); ok {
			a.processViewDDL(ddl)
			if len(a.errors) > 0 {
				return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
			}
			return a.edges, nil
		}
		return nil, errors.Wrap(&errs[0], "failed to parse StarRocks SQL")
	}
	if file == nil || len(file.Stmts) != 1 {
		return nil, errors.New("expected exactly 1 statement")
	}

	a.dispatch(file.Stmts[0])

	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
	}
	return a.edges, nil
}

// dispatch routes a parsed statement to its analyzer.
func (a *Analyzer) dispatch(stmt nodes.Node) {
	switch s := stmt.(type) {
	case *nodes.SelectStmt:
		a.processSelectStatement(s)
	case *nodes.SetOpStmt:
		a.processSetOperation(s)
	case *nodes.ParenSelect:
		a.processQueryNode(s)
	case *nodes.CreateViewStmt:
		a.processDDLTarget(s.Name, viewColumnNames(s.Columns), s.Query)
	case *nodes.AlterViewStmt:
		a.processDDLTarget(s.Name, viewColumnNames(s.Columns), s.Query)
	case *nodes.CreateMTMVStmt:
		a.processDDLTarget(s.Name, viewColumnNames(s.Columns), s.Query)
	case *nodes.CreateTableStmt:
		a.processCreateTable(s)
	case *nodes.InsertStmt:
		a.unsupported("INSERT")
	case *nodes.UpdateStmt:
		a.unsupported("UPDATE")
	case *nodes.DeleteStmt:
		a.unsupported("DELETE")
	default:
		// Statement kinds that carry no lineage are ignored, as in the MySQL
		// analyzer.
	}
}

// unsupported records a statement kind this phase cannot analyze yet. It is an
// error rather than a silent empty result, so an unimplemented shape is never
// mistaken for "this statement has no lineage".
func (a *Analyzer) unsupported(what string) {
	a.errors = append(a.errors, fmt.Sprintf("%s analysis is not implemented yet", what))
}

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// processSelectStatement processes a SELECT statement.
func (a *Analyzer) processSelectStatement(stmt *nodes.SelectStmt) {
	if stmt == nil {
		return
	}
	if stmt.With != nil {
		a.processCTEs(stmt.With.CTEs)
	}
	a.processQuerySpecification(stmt)
}

// processQueryNode dispatches a query expression: a SELECT, a set operation or
// a parenthesized query. Unlike the MySQL analyzer, omni models set operations
// and parenthesized queries as their own nodes rather than as fields of
// SelectStmt.
func (a *Analyzer) processQueryNode(node nodes.Node) {
	switch n := node.(type) {
	case *nodes.SelectStmt:
		a.processSelectStatement(n)
	case *nodes.SetOpStmt:
		a.processSetOperation(n)
	case *nodes.ParenSelect:
		a.processQueryNode(n.Sel)
	default:
		a.unsupported("query expression")
	}
}

// processQuerySpecification processes FROM, then the SELECT list, then emits
// edges.
func (a *Analyzer) processQuerySpecification(stmt *nodes.SelectStmt) {
	sp := a.currentScope()
	a.processFromClause(stmt.From)
	a.processSelectItemList(stmt.Items, sp)
	a.generateEdges(sp)
}

// ---------------------------------------------------------------------------
// FROM clause
// ---------------------------------------------------------------------------

// processFromClause processes the FROM clause.
func (a *Analyzer) processFromClause(from []nodes.Node) {
	for _, te := range from {
		a.processTableExpr(te)
	}
}

// processTableExpr processes a table reference or join.
func (a *Analyzer) processTableExpr(te nodes.Node) {
	switch t := te.(type) {
	case *nodes.TableRef:
		a.processSingleTableRef(t)
	case *nodes.JoinClause:
		a.processTableExpr(t.Left)
		a.processTableExpr(t.Right)
	default:
		// Inline tables and table functions reference no physical table.
	}
}

// processSingleTableRef adds a base table reference to the current scope.
func (a *Analyzer) processSingleTableRef(ref *nodes.TableRef) {
	if ref == nil {
		return
	}
	if ref.Subquery != nil {
		a.processDerivedTable(ref)
		return
	}
	schema, table, ok := tableRefFromObjectName(ref.Name)
	if !ok {
		return
	}
	alias := ref.Alias
	if alias == "" {
		alias = table
	}
	if schema == "" {
		if cte, ok := a.currentScope().FindCTE(table); ok {
			a.currentScope().AddTable(&scope.TableRef{
				Table:   table,
				Alias:   alias,
				IsCTE:   true,
				Columns: cte.Columns,
				Lineage: cte.Lineage,
			})
			return
		}
	}
	a.currentScope().AddTable(&scope.TableRef{
		Schema:  schema,
		Table:   table,
		Alias:   alias,
		Columns: []string{},
	})
}

// processDerivedTable processes a derived table (a subquery in FROM). omni
// keeps the subquery body as raw text, so it is re-parsed with a matching
// source pushed; the alias is registered as a temporary table carrying the
// subquery's lineage.
func (a *Analyzer) processDerivedTable(ref *nodes.TableRef) {
	sub := ref.Subquery
	if sub == nil || strings.TrimSpace(sub.RawText) == "" {
		return
	}
	alias := ref.Alias
	a.markTempTable(alias)

	subqueryScope := a.analyzeRawQueryScope(sub.RawText, fmt.Sprintf("derived table %q", alias))
	if subqueryScope == nil {
		return
	}

	columns, lineage := a.tempTableShape(subqueryScope, alias)
	a.currentScope().AddTable(&scope.TableRef{
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		Columns:    columns,
		Lineage:    lineage,
	})
}

// analyzeRawQueryScope parses and analyzes a query body that omni exposes only
// as text (a derived table, a CTAS body or an expression subquery) in a fresh
// scope and source. It returns nil, after recording the failure, when the body
// does not parse.
func (a *Analyzer) analyzeRawQueryScope(raw string, what string) *scope.Scope {
	a.pushSource(newSource(raw))
	query, err := parseRawQuery(raw)
	if err != nil {
		a.popSource()
		a.errors = append(a.errors, fmt.Sprintf("%s: %v", what, err))
		return nil
	}
	a.pushScope()
	a.processQueryNode(query)
	subScope := a.popScope()
	a.popSource()
	return subScope
}

// tempTableShape builds the column list and base-table lineage a temporary
// table (a CTE or derived table) exposes under targetName.
func (a *Analyzer) tempTableShape(sp *scope.Scope, targetName string) ([]string, []model.ColumnRelation) {
	columns := make([]string, 0)
	lineage := make([]model.ColumnRelation, 0)
	for _, col := range sp.GetOutputColumns() {
		colName := col.Alias
		if colName == "" {
			colName = "column"
		}
		columns = append(columns, colName)
		for _, sourceCol := range col.SourceColumns {
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}
			if a.flattenTempSourceLineage(sp, resolved, targetName, colName, col.Transform, &lineage) {
				continue
			}
			lineage = append(lineage, scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				"", targetName, colName,
				col.Transform,
				true, // the temporary table is not a real object
			))
		}
	}
	return columns, lineage
}

// ---------------------------------------------------------------------------
// DDL targets
// ---------------------------------------------------------------------------

// processDDLTarget analyzes the query body of a CREATE/ALTER VIEW or CREATE
// MATERIALIZED VIEW and maps its output columns onto the created object.
func (a *Analyzer) processDDLTarget(name *nodes.ObjectName, columns []string, query nodes.Node) {
	if name == nil || query == nil {
		return
	}
	schema, table, ok := tableRefFromObjectName(name)
	if !ok {
		return
	}
	a.inDDLTarget = true
	a.processQueryNode(query)
	a.inDDLTarget = false

	a.generateEdgesForTarget(a.currentScope(), schema, table, columns)
}

// processViewDDL is processDDLTarget for a view statement whose body omni
// could not parse in place; the body was extracted by extractViewDDL and is
// analyzed as a top-level query in its own scope.
func (a *Analyzer) processViewDDL(ddl *viewDDL) {
	sp := a.analyzeRawQueryScope(ddl.body, "view query")
	if sp == nil {
		return
	}
	a.generateEdgesForTarget(sp, ddl.schema, ddl.name, ddl.columns)
}

// processCreateTable processes CREATE TABLE ... AS SELECT. omni keeps the query
// as raw text, so it is re-parsed like a derived table's body.
func (a *Analyzer) processCreateTable(stmt *nodes.CreateTableStmt) {
	if stmt == nil || stmt.AsSelect == nil {
		return
	}
	schema, table, ok := tableRefFromObjectName(stmt.Name)
	if !ok {
		return
	}
	raw := strings.TrimSpace(stmt.AsSelect.RawText)
	if raw == "" {
		return
	}
	sp := a.analyzeRawQueryScope(raw, "CREATE TABLE AS SELECT")
	if sp == nil {
		return
	}
	a.generateEdgesForTarget(sp, schema, table, stmt.CTASColumns)
}

// generateEdgesForTarget maps a scope's output columns onto the columns of the
// object being created. A column list declared on the statement wins over the
// query's own output alias.
func (a *Analyzer) generateEdgesForTarget(sp *scope.Scope, targetSchema, targetTable string, targetColumns []string) {
	if sp == nil {
		return
	}
	for i, outputCol := range sp.GetOutputColumns() {
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			targetColName = targetColumns[i]
		}
		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}
			if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
				a.traceThroughTableLineageToTarget(tableRef, resolved.Column, targetSchema, targetTable, targetColName, outputCol.Transform)
				continue
			}
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
			a.addRelation(scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				targetSchema, targetTable, targetColName,
				outputCol.Transform,
				isTemp,
			))
		}
	}
}

// viewColumnNames extracts the declared column names of a view or materialized
// view statement.
func viewColumnNames(cols []*nodes.ViewColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c != nil && c.Name != "" {
			out = append(out, c.Name)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// SELECT list
// ---------------------------------------------------------------------------

// processSelectItemList processes the SELECT item list.
func (a *Analyzer) processSelectItemList(items []*nodes.SelectItem, sp *scope.Scope) {
	for _, item := range items {
		if item == nil {
			continue
		}
		switch {
		case item.Star && item.TableName == nil:
			a.processStar(sp, item.ExceptColumns)
		case item.Star:
			a.processTableWildcard(item, sp)
		default:
			a.processSelectExpr(item.Expr, item.Alias, item.Aliased, sp)
		}
	}
}

// processStar expands SELECT * against every table in scope.
func (a *Analyzer) processStar(sp *scope.Scope, except []string) {
	for _, tableRef := range sp.GetTables() {
		if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
			if a.expandWildcardWithCatalog(tableRef, sp, except) {
				continue
			}
		}
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:         wildcardColumn,
			Expression:    wildcardColumn,
			SourceColumns: []scope.ColumnRef{wildcardSourceRef(tableRef)},
		})
	}
}

// processTableWildcard expands table.* against the named table.
func (a *Analyzer) processTableWildcard(item *nodes.SelectItem, sp *scope.Scope) {
	if item.TableName == nil || len(item.TableName.Parts) == 0 {
		return
	}
	tableName := item.TableName.Parts[len(item.TableName.Parts)-1]
	tableRef, ok := sp.FindTable(tableName)
	if !ok {
		return
	}
	if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
		if a.expandWildcardWithCatalog(tableRef, sp, item.ExceptColumns) {
			return
		}
	}
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:         wildcardColumn,
		Expression:    tableName + "." + wildcardColumn,
		SourceColumns: []scope.ColumnRef{wildcardSourceRef(tableRef)},
	})
}

// wildcardSourceRef builds the source reference for a `*` expansion of one FROM
// relation.
//
// A base table is already fully identified here, so the reference is marked
// resolved: looking it up again by name would fail whenever the relation has an
// alias, which silently produced no lineage at all for `SELECT * FROM t x`.
// (The MySQL analyzer still has that gap.) A CTE or derived table is instead
// looked up by its scope key so the temp-table trace can find it.
func wildcardSourceRef(tableRef *scope.TableRef) scope.ColumnRef {
	if tableRef.IsSubquery || tableRef.IsCTE {
		key := tableRef.Alias
		if key == "" {
			key = tableRef.Table
		}
		return scope.ColumnRef{Table: key, Column: wildcardColumn}
	}
	return scope.ColumnRef{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn, Resolved: true}
}

// processSelectExpr turns one select expression into an output column. aliased
// records whether the SQL carried an explicit alias, so an explicit empty alias
// is not replaced by an inferred one.
func (a *Analyzer) processSelectExpr(expr nodes.Node, alias string, aliased bool, sp *scope.Scope) {
	if expr == nil {
		return
	}
	exprText := a.exprTextOf(expr)
	if !aliased && alias == "" {
		alias = inferColumnAlias(exprText)
	}
	sourceColumns := collectColumns(expr)
	sourceColumns = append(sourceColumns, a.expressionSubquerySources(expr)...)
	isDerived := isExpressionDerivedText(exprText)

	// A derived expression with no column reference (for example COUNT(*))
	// contributes a synthetic reference to every table in scope.
	if isDerived && len(sourceColumns) == 0 {
		for _, tableRef := range sp.GetTables() {
			sourceColumns = append(sourceColumns, wildcardSourceRef(tableRef))
		}
	}

	outputCol := scope.OutputColumn{
		Alias:         alias,
		Expression:    exprText,
		SourceColumns: sourceColumns,
		IsDerived:     isDerived,
	}
	if isDerived {
		outputCol.Transform = a.analyzeExpressionOperator(expr)
	}
	sp.AddOutputColumn(outputCol)
}

// expressionSubquerySources analyzes the subqueries embedded in a select
// expression. omni models a scalar / IN / EXISTS subquery as a leaf carrying
// only raw text, so each is re-parsed in its own scope and flattened to
// base-table references. Those references are marked resolved, because the
// table they resolved to lives in the subquery's scope and the enclosing query
// resolves its output columns in a different one.
func (a *Analyzer) expressionSubquerySources(expr nodes.Node) []scope.ColumnRef {
	var subqueries []*nodes.SubqueryExpr
	nodes.Inspect(expr, func(n nodes.Node) bool {
		sub, ok := n.(*nodes.SubqueryExpr)
		if !ok {
			return true
		}
		subqueries = append(subqueries, sub)
		return false // a SubqueryExpr is a leaf; its body is raw text
	})
	if len(subqueries) == 0 {
		return nil
	}

	refs := make([]scope.ColumnRef, 0)
	for i, sub := range subqueries {
		if strings.TrimSpace(sub.RawText) == "" {
			continue
		}
		subScope := a.analyzeRawQueryScope(sub.RawText, "expression subquery")
		if subScope == nil {
			continue
		}
		synthetic := fmt.Sprintf("__subquery_%d__", i)
		_, lineage := a.tempTableShape(subScope, synthetic)
		for _, edge := range lineage {
			refs = append(refs, scope.ColumnRef{
				Schema:   edge.Source.Table.Database,
				Table:    edge.Source.Table.Name,
				Column:   edge.Source.Name,
				Resolved: true,
			})
		}
	}
	return refs
}

// ---------------------------------------------------------------------------
// Edge generation
// ---------------------------------------------------------------------------

// generateEdges creates ColumnRelation objects from the scope's output columns.
// Only the root query emits edges to the final result, and a DDL body does not
// (its columns are mapped onto the created object instead).
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	if a.inDDLTarget || sp == nil || sp.Parent() != nil {
		return
	}
	for _, outputCol := range sp.GetOutputColumns() {
		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}
			if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
				a.traceThroughTableLineage(tableRef, resolved.Column, outputCol.Alias, outputCol.Transform)
				continue
			}
			a.addRelation(scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				"", resultTableName, outputCol.Alias,
				outputCol.Transform,
				true, // __result__ is always temporary
			))
		}
	}
}

// addRelation adds a column relation, skipping temp-table endpoints and
// duplicate signatures.
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	if a.isTempTable(relation.Source.Table.Name) {
		return
	}
	if a.isTempTable(relation.Target.Table.Name) && relation.Target.Table.Name != resultTableName {
		return
	}
	signature := fmt.Sprintf("%s.%s.%s->%s.%s.%s",
		relation.Source.Table.Schema, relation.Source.Table.Name, relation.Source.Name,
		relation.Target.Table.Schema, relation.Target.Table.Name, relation.Target.Name)
	if _, exists := a.edgeSet[signature]; exists {
		return
	}
	a.edgeSet[signature] = struct{}{}
	a.edges = append(a.edges, relation)
}

// expandWildcardWithCatalog expands a wildcard using catalog metadata, omitting
// any column named in except. It reports false when no metadata is available so
// the caller falls back to a bulk wildcard edge.
func (a *Analyzer) expandWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope, except []string) bool {
	tableID := model.ObjectIdentifier{
		Database: tableRef.Schema,
		Name:     tableRef.Table,
	}
	tableMeta, err := a.catalog.GetTable(a.ctx, tableID)
	if err != nil || tableMeta == nil {
		return false
	}
	excluded := make(map[string]struct{}, len(except))
	for _, name := range except {
		excluded[strings.ToLower(name)] = struct{}{}
	}
	for _, colMeta := range tableMeta.Columns {
		if _, ok := excluded[strings.ToLower(colMeta.Name)]; ok {
			continue
		}
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:      colMeta.Name,
			Expression: tableRef.Table + "." + colMeta.Name,
			SourceColumns: []scope.ColumnRef{{
				Schema: tableRef.Schema,
				Table:  tableRef.Table,
				Column: colMeta.Name,
				// The catalog identified the real column, so the reference must
				// not be rebound by name (which fails for an aliased relation).
				Resolved: true,
			}},
		})
	}
	return true
}

// ---------------------------------------------------------------------------
// Scope stack and temp-table tracking
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

// currentScope returns the current (innermost) scope.
func (a *Analyzer) currentScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	return a.scopeStack[len(a.scopeStack)-1]
}

// markTempTable records a temporary table name (CTE or derived table) so
// intermediate edges can be filtered out.
func (a *Analyzer) markTempTable(name string) {
	if name == "" {
		return
	}
	a.tempTables[name] = struct{}{}
}

// isTempTable checks whether a table name was marked as temporary.
func (a *Analyzer) isTempTable(name string) bool {
	_, ok := a.tempTables[name]
	return ok
}

// isTableTempInCurrentScope reports whether a table name is a CTE or derived
// table visible from any scope on the stack.
func (a *Analyzer) isTableTempInCurrentScope(tableName string) bool {
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

// ---------------------------------------------------------------------------
// CTE
// ---------------------------------------------------------------------------

// processCTEs processes a WITH clause.
func (a *Analyzer) processCTEs(ctes []*nodes.CTE) {
	for _, cte := range ctes {
		a.processCTE(cte)
	}
}

// processCTE processes a single CTE: its body is analyzed in a nested scope,
// and its output columns are flattened into a lineage the outer query can
// trace through.
func (a *Analyzer) processCTE(cte *nodes.CTE) {
	if cte == nil {
		return
	}
	cteName := cte.Name
	a.markTempTable(cteName)

	lineage := make([]model.ColumnRelation, 0)
	if cte.Query != nil {
		a.pushScope()
		a.processQueryNode(cte.Query)
		cteScope := a.popScope()

		for i, outputCol := range cteScope.GetOutputColumns() {
			// A CTE may rename its output columns: WITH c (a, b) AS (SELECT id,
			// name ...). The MySQL analyzer ignores that list and emits no
			// lineage at all for this shape; mapping it is strictly more
			// correct.
			targetName := outputCol.Alias
			if i < len(cte.Columns) {
				targetName = cte.Columns[i]
			}
			for _, sourceCol := range outputCol.SourceColumns {
				resolved, err := cteScope.ResolveColumn(sourceCol)
				if err != nil {
					continue
				}
				if a.flattenTempSourceLineage(cteScope, resolved, cteName, targetName, outputCol.Transform, &lineage) {
					continue
				}
				lineage = append(lineage, scope.NewLineageEdge(
					resolved.Schema, resolved.Table, resolved.Column,
					"", cteName, targetName,
					outputCol.Transform,
					true, // a CTE is temporary
				))
			}
		}
	}

	a.currentScope().AddCTE(&scope.CTEDefinition{
		Name:          cteName,
		Columns:       cte.Columns,
		DefiningScope: a.currentScope(),
		Lineage:       lineage,
	})
}

// ---------------------------------------------------------------------------
// Set operations
// ---------------------------------------------------------------------------

// flattenSetOpArms flattens a set-operation tree into its leaf SELECTs in
// order.
func flattenSetOpArms(node nodes.Node) []*nodes.SelectStmt {
	switch n := node.(type) {
	case *nodes.SelectStmt:
		return []*nodes.SelectStmt{n}
	case *nodes.ParenSelect:
		return flattenSetOpArms(n.Sel)
	case *nodes.SetOpStmt:
		var out []*nodes.SelectStmt
		out = append(out, flattenSetOpArms(n.Left)...)
		out = append(out, flattenSetOpArms(n.Right)...)
		return out
	default:
		return nil
	}
}

// processSetOperation handles UNION/INTERSECT/EXCEPT by processing every arm
// and merging their output columns positionally. Each arm is analyzed in its
// own scope so one arm's FROM relations cannot leak into the next; arms at the
// root emit their own edges, and a set operation nested in a CTE or derived
// table contributes the merged columns to its parent instead.
func (a *Analyzer) processSetOperation(stmt *nodes.SetOpStmt) {
	arms := flattenSetOpArms(stmt)
	if len(arms) == 0 {
		return
	}

	baseScope := a.currentScope()
	var allOutputColumns [][]scope.OutputColumn

	for i, arm := range arms {
		if i == 0 {
			a.processSelectStatement(arm)
			allOutputColumns = append(allOutputColumns, resolveOutputColumns(baseScope, baseScope.GetOutputColumns()))
			continue
		}
		tempScope := scope.NewScope(baseScope.Parent())
		for _, cte := range baseScope.GetCTEs() {
			tempScope.AddCTE(cte)
		}
		originalScope := a.currentScope()
		a.scopeStack[len(a.scopeStack)-1] = tempScope
		a.processSelectStatement(arm)
		a.scopeStack[len(a.scopeStack)-1] = originalScope
		allOutputColumns = append(allOutputColumns, resolveOutputColumns(tempScope, tempScope.GetOutputColumns()))
	}

	mergeUnionOutputColumns(baseScope, allOutputColumns)
}

// resolveOutputColumns resolves each output column's source references against
// the scope the arm was analyzed in, marking them resolved.
//
// The merge below runs after every arm's own scope is gone, and the merged
// references are later resolved again in the enclosing scope. Without this
// step an unqualified reference from a non-first arm would bind to the first
// arm's table, silently dropping the later arms' lineage.
func resolveOutputColumns(sp *scope.Scope, cols []scope.OutputColumn) []scope.OutputColumn {
	out := make([]scope.OutputColumn, len(cols))
	copy(out, cols)
	for i := range out {
		resolved := make([]scope.ColumnRef, 0, len(out[i].SourceColumns))
		for _, ref := range out[i].SourceColumns {
			if r, err := sp.ResolveColumn(ref); err == nil {
				r.Resolved = true
				resolved = append(resolved, *r)
			} else {
				resolved = append(resolved, ref)
			}
		}
		out[i].SourceColumns = resolved
	}
	return out
}

// mergeUnionOutputColumns merges output columns from multiple set-operation
// arms positionally.
func mergeUnionOutputColumns(baseScope *scope.Scope, allOutputColumns [][]scope.OutputColumn) {
	if len(allOutputColumns) == 0 || len(allOutputColumns[0]) == 0 {
		return
	}
	firstQueryOutputs := allOutputColumns[0]
	for colIdx := 0; colIdx < len(firstQueryOutputs); colIdx++ {
		firstCol := firstQueryOutputs[colIdx]
		var mergedSources []scope.ColumnRef
		var hasDerivedTransform bool
		for queryIdx := 0; queryIdx < len(allOutputColumns); queryIdx++ {
			if colIdx < len(allOutputColumns[queryIdx]) {
				queryCol := allOutputColumns[queryIdx][colIdx]
				mergedSources = append(mergedSources, queryCol.SourceColumns...)
				if queryCol.IsDerived {
					hasDerivedTransform = true
				}
			}
		}
		firstCol.SourceColumns = mergedSources
		if hasDerivedTransform && firstCol.Transform == nil {
			firstCol.Transform = []model.Transformation{model.NewUnionTransformation()}
		}
		baseScope.SetOutputColumn(colIdx, firstCol)
	}
}

// ---------------------------------------------------------------------------
// Temporary-table lineage flattening
// ---------------------------------------------------------------------------

// traceThroughTableLineage traces lineage through a CTE or derived table to the
// final result.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName string, outputAlias string, transform []model.Transformation) {
	for _, edge := range tableRef.Lineage {
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}
		actualOutput := outputAlias
		if columnName == wildcardColumn && outputAlias == wildcardColumn {
			actualOutput = edge.Target.Name
		}
		a.addRelation(scope.NewLineageEdge(
			edge.Source.Table.Database, edge.Source.Table.Name, edge.Source.Name,
			"", resultTableName, actualOutput,
			combineTransformations(edge.Transformation, transform),
			true,
		))
	}
}

// traceThroughTableLineageToTarget traces lineage through a CTE or derived
// table to a specific target column on a real object.
func (a *Analyzer) traceThroughTableLineageToTarget(tableRef *scope.TableRef, columnName string, targetSchema string, targetTable string, targetColumn string, transform []model.Transformation) {
	for _, edge := range tableRef.Lineage {
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}
		actualTargetColumn := targetColumn
		if columnName == wildcardColumn && targetColumn == wildcardColumn {
			actualTargetColumn = edge.Target.Name
		}
		isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
		a.addRelation(scope.NewLineageEdge(
			edge.Source.Table.Database, edge.Source.Table.Name, edge.Source.Name,
			targetSchema, targetTable, actualTargetColumn,
			combineTransformations(edge.Transformation, transform),
			isTemp,
		))
	}
}

// appendFlattenedLineage traces through nested temporary tables to real tables.
func (a *Analyzer) appendFlattenedLineage(lineage *[]model.ColumnRelation, sp *scope.Scope, tableRef *scope.TableRef, columnName string, targetTable string, targetColumn string, transform []model.Transformation) {
	for _, edge := range tableRef.Lineage {
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}
		actualTarget := targetColumn
		if columnName == wildcardColumn && targetColumn == wildcardColumn {
			actualTarget = edge.Target.Name
		}
		combinedTransform := combineTransformations(edge.Transformation, transform)
		sourceTableName := edge.Source.Table.Name
		if nestedRef, ok := sp.FindTable(sourceTableName); ok && (nestedRef.IsCTE || nestedRef.IsSubquery) {
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

// flattenTempSourceLineage resolves a column from a temporary table into base
// table lineage. It reports whether the source was handled.
func (a *Analyzer) flattenTempSourceLineage(sp *scope.Scope, resolved *scope.ColumnRef, targetTable string, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	if resolved == nil {
		return false
	}
	if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsSubquery || tableRef.IsCTE) {
		a.appendFlattenedLineage(lineage, sp, tableRef, resolved.Column, targetTable, targetColumn, transform)
		return true
	}
	if cte, ok := sp.FindCTE(resolved.Table); ok {
		tempRef := &scope.TableRef{
			Table:   cte.Name,
			Alias:   cte.Name,
			IsCTE:   true,
			Columns: cte.Columns,
			Lineage: cte.Lineage,
		}
		a.appendFlattenedLineage(lineage, sp, tempRef, resolved.Column, targetTable, targetColumn, transform)
		return true
	}
	return false
}
